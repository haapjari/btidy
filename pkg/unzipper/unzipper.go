// Package unzipper extracts zip archives safely within a root directory.
package unzipper

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"btidy/pkg/collector"
	"btidy/pkg/progress"
	"btidy/pkg/safepath"
	"btidy/pkg/trash"
)

const (
	progressStageExtracting = "extracting"
	maxExtractedFileSize    = int64(8 * 1024 * 1024 * 1024) // 8 GiB per file
)

// ExtractOperation represents a single archive extraction operation.
type ExtractOperation struct {
	ArchivePath        string
	ExtractedFiles     int
	ExtractedDirs      int
	NestedArchives     int
	ExtractionComplete bool
	DeletedArchive     bool
	TrashedTo          string // Trash destination for deleted archive (empty when trasher is nil)
	Skipped            bool
	SkipReason         string
	Error              error
}

// Result contains results for an unzip run.
type Result struct {
	Operations        []ExtractOperation
	TotalFiles        int
	ArchivesFound     int
	ArchivesProcessed int
	ExtractedArchives int
	DeletedArchives   int
	ExtractedFiles    int
	ExtractedDirs     int
	SkippedCount      int
	ErrorCount        int
}

// Unzipper extracts archives recursively while enforcing path containment.
type Unzipper struct {
	dryRun    bool
	validator *safepath.Validator
	trasher   *trash.Trasher
}

type entryResult struct {
	extractedFiles int
	extractedDirs  int
	nestedArchive  string
}

// New creates an Unzipper rooted at rootDir.
func New(rootDir string, dryRun bool) (*Unzipper, error) {
	validator, err := safepath.New(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create path validator: %w", err)
	}

	return NewWithValidator(validator, dryRun, nil)
}

// NewWithValidator creates an Unzipper with an existing validator.
// An optional trasher enables soft-delete (move to trash) instead of permanent removal.
func NewWithValidator(validator *safepath.Validator, dryRun bool, trasher *trash.Trasher) (*Unzipper, error) {
	if validator == nil {
		return nil, errors.New("validator is required")
	}

	return &Unzipper{
		dryRun:    dryRun,
		validator: validator,
		trasher:   trasher,
	}, nil
}

// ExtractArchives extracts all .zip files from the provided file list.
func (u *Unzipper) ExtractArchives(files []collector.FileInfo) Result {
	return u.ExtractArchivesWithProgress(files, nil)
}

// ExtractArchivesWithProgress extracts archives recursively and emits progress.
func (u *Unzipper) ExtractArchivesWithProgress(files []collector.FileInfo, onProgress func(stage string, processed, total int)) Result {
	result := Result{
		TotalFiles: len(files),
		Operations: make([]ExtractOperation, 0),
	}

	queue := collectInitialArchives(files)
	if len(queue) == 0 {
		return result
	}

	result.ArchivesFound = len(queue)
	processed := make(map[string]bool, len(queue))
	queued := make(map[string]bool, len(queue))
	for _, archivePath := range queue {
		queued[archivePath] = true
	}

	for len(queue) > 0 {
		archivePath := queue[0]
		queue = queue[1:]
		delete(queued, archivePath)

		if processed[archivePath] {
			continue
		}
		processed[archivePath] = true

		op, discoveredArchives := u.processArchive(archivePath)
		result.Operations = append(result.Operations, op)
		result.accumulateOperation(op)

		if op.ExtractionComplete {
			for _, discoveredPath := range discoveredArchives {
				if processed[discoveredPath] || queued[discoveredPath] {
					continue
				}

				queue = append(queue, discoveredPath)
				queued[discoveredPath] = true
				result.ArchivesFound++
			}
			sort.Strings(queue)
		}

		progress.EmitStage(onProgress, progressStageExtracting, len(result.Operations), len(result.Operations)+len(queue))
	}

	return result
}

func (u *Unzipper) processArchive(archivePath string) (operation ExtractOperation, discovered []string) {
	operation = ExtractOperation{ArchivePath: archivePath}

	if !isZipArchive(archivePath) {
		operation.Skipped = true
		operation.SkipReason = "not a zip archive"
		return operation, nil
	}

	if err := u.validator.ValidatePathForRead(archivePath); err != nil {
		operation.Error = fmt.Errorf("archive path escapes root: %w", err)
		return operation, nil
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		operation.Error = fmt.Errorf("failed to open archive: %w", err)
		return operation, nil
	}
	defer reader.Close()

	destinationDir := filepath.Dir(archivePath)
	discovered = make([]string, 0)

	for _, file := range reader.File {
		entryOutcome, entryErr := u.extractEntry(archivePath, destinationDir, file)
		operation.ExtractedFiles += entryOutcome.extractedFiles
		operation.ExtractedDirs += entryOutcome.extractedDirs
		if entryOutcome.nestedArchive != "" {
			discovered = append(discovered, entryOutcome.nestedArchive)
			operation.NestedArchives++
		}

		if entryErr != nil {
			operation.Error = entryErr
			return operation, nil
		}
	}

	// Verify all extracted regular files exist with expected sizes before
	// deleting the archive. This prevents data loss if extraction was partial.
	if !u.dryRun {
		if err := u.verifyExtractedFiles(reader.File, destinationDir); err != nil {
			operation.Error = fmt.Errorf("post-extraction verification failed: %w", err)
			return operation, nil
		}
	}

	operation.ExtractionComplete = true
	operation.DeletedArchive = true

	if u.dryRun {
		return operation, nil
	}

	trashedTo, trashErr := u.trashOrRemove(archivePath)
	if trashErr != nil {
		operation.DeletedArchive = false
		operation.Error = trashErr
	} else {
		operation.TrashedTo = trashedTo
	}

	return operation, discovered
}

// trashOrRemove soft-deletes a file when a trasher is configured, otherwise
// permanently removes it. Returns the trash destination (empty on hard delete).
func (u *Unzipper) trashOrRemove(path string) (string, error) {
	if u.trasher != nil {
		return u.trasher.TrashWithDest(path)
	}

	if err := u.validator.SafeRemove(path); err != nil {
		return "", fmt.Errorf("failed to delete archive: %w", err)
	}

	return "", nil
}

// backupExistingFile checks whether a file already exists at the given path and,
// if so, backs it up to trash before extraction overwrites it. Without a trasher
// the extraction is refused to prevent silent data loss.
func (u *Unzipper) backupExistingFile(entryPath string) error {
	if _, err := os.Lstat(entryPath); err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to protect
		}
		return fmt.Errorf("cannot inspect existing file %q: %w", entryPath, err)
	}

	if u.trasher != nil {
		if _, err := u.trasher.TrashWithDest(entryPath); err != nil {
			return fmt.Errorf("cannot back up existing file %q before overwrite: %w", entryPath, err)
		}
		return nil
	}

	return fmt.Errorf("refusing to overwrite existing file: %s", entryPath)
}

func (u *Unzipper) verifyExtractedFiles(entries []*zip.File, destinationDir string) error {
	for _, file := range entries {
		if isDirectoryEntry(file) || file.Mode()&os.ModeSymlink != 0 {
			continue
		}

		entryPath, err := u.resolveEntryPath(destinationDir, file.Name)
		if err != nil {
			return fmt.Errorf("cannot resolve %q: %w", file.Name, err)
		}

		info, err := os.Lstat(entryPath)
		if err != nil {
			return fmt.Errorf("extracted file missing %q: %w", entryPath, err)
		}

		actualSize := info.Size()
		expectedSize := file.UncompressedSize64
		if actualSize < 0 || expectedSize > uint64(math.MaxInt64) || actualSize != int64(expectedSize) {
			return fmt.Errorf("size mismatch for %q (expected %d, got %d)",
				entryPath, file.UncompressedSize64, info.Size())
		}
	}

	return nil
}

func (u *Unzipper) extractEntry(archivePath, destinationDir string, file *zip.File) (entryResult, error) {
	result := entryResult{}

	entryPath, err := u.resolveEntryPath(destinationDir, file.Name)
	if err != nil {
		return result, err
	}

	if entryPath == archivePath {
		return result, fmt.Errorf("archive entry would overwrite source archive: %s", file.Name)
	}

	if err := u.validator.ValidatePathForWrite(entryPath); err != nil {
		return result, fmt.Errorf("entry path escapes root: %w", err)
	}

	if isDirectoryEntry(file) {
		result.extractedDirs = 1
		if u.dryRun {
			return result, nil
		}

		if err := os.MkdirAll(entryPath, 0o755); err != nil {
			return result, fmt.Errorf("failed to create directory %q: %w", entryPath, err)
		}

		return result, nil
	}

	if file.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("symlink entries are not supported: %s", file.Name)
	}

	if err := u.ensureParentDirectory(entryPath); err != nil {
		return result, err
	}

	result.extractedFiles = 1
	if isZipArchive(entryPath) {
		result.nestedArchive = entryPath
	}

	if u.dryRun {
		return result, nil
	}

	// Protect existing files from being silently overwritten during extraction.
	if err := u.backupExistingFile(entryPath); err != nil {
		return result, err
	}

	if err := extractRegularFile(file, entryPath); err != nil {
		return result, err
	}

	return result, nil
}

func (u *Unzipper) resolveEntryPath(destinationDir, entryName string) (string, error) {
	trimmedName := strings.TrimSpace(entryName)
	if trimmedName == "" {
		return "", errors.New("archive entry has empty path")
	}

	relativePath := filepath.Clean(filepath.FromSlash(trimmedName))
	resolvedPath, err := u.validator.ResolveSafePath(destinationDir, relativePath)
	if err != nil {
		return "", fmt.Errorf("entry path escapes root: %w", err)
	}

	return resolvedPath, nil
}

func (u *Unzipper) ensureParentDirectory(path string) error {
	parentDir := filepath.Dir(path)
	if err := u.validator.ValidatePathForWrite(parentDir); err != nil {
		return fmt.Errorf("entry parent path escapes root: %w", err)
	}

	if u.dryRun {
		return nil
	}

	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory %q: %w", parentDir, err)
	}

	return nil
}

func extractRegularFile(file *zip.File, destinationPath string) error {
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open archive entry %q: %w", file.Name, err)
	}
	defer reader.Close()

	if file.UncompressedSize64 > uint64(maxExtractedFileSize) {
		return fmt.Errorf("archive entry %q exceeds size limit (%d bytes)", file.Name, maxExtractedFileSize)
	}

	mode := file.Mode().Perm()
	if mode == 0 {
		mode = 0o600
	}

	writer, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to create extracted file %q: %w", destinationPath, err)
	}

	if err := copyArchiveEntry(writer, reader, maxExtractedFileSize); err != nil {
		_ = writer.Close()
		_ = os.Remove(destinationPath) // Clean up partial file.
		return fmt.Errorf("failed to write extracted file %q: %w", destinationPath, err)
	}

	if err := writer.Close(); err != nil {
		_ = os.Remove(destinationPath) // Clean up potentially incomplete file.
		return fmt.Errorf("failed to close extracted file %q: %w", destinationPath, err)
	}

	return nil
}

func copyArchiveEntry(writer io.Writer, reader io.Reader, maxBytes int64) error {
	const copyBufferSize = 32 * 1024

	buffer := make([]byte, copyBufferSize)
	var written int64

	for {
		readCount, readErr := reader.Read(buffer)
		if readCount > 0 {
			written += int64(readCount)
			if written > maxBytes {
				return errors.New("entry exceeds configured size limit")
			}

			writeCount, writeErr := writer.Write(buffer[:readCount])
			if writeErr != nil {
				return writeErr
			}
			if writeCount != readCount {
				return io.ErrShortWrite
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func collectInitialArchives(files []collector.FileInfo) []string {
	archives := make([]string, 0)
	for _, file := range files {
		if !isZipArchive(file.Path) {
			continue
		}

		archives = append(archives, file.Path)
	}

	sort.Strings(archives)
	return archives
}

func isDirectoryEntry(file *zip.File) bool {
	return file.FileInfo().IsDir() || strings.HasSuffix(file.Name, "/")
}

func isZipArchive(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}

func (r *Result) accumulateOperation(op ExtractOperation) {
	r.ArchivesProcessed++

	if op.ExtractionComplete {
		r.ExtractedArchives++
		r.ExtractedFiles += op.ExtractedFiles
		r.ExtractedDirs += op.ExtractedDirs
		if op.DeletedArchive {
			r.DeletedArchives++
		}
	}

	switch {
	case op.Error != nil:
		r.ErrorCount++
	case op.Skipped:
		r.SkippedCount++
	}
}
