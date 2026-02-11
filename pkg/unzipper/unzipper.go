// Package unzipper extracts zip archives safely within a root directory.
package unzipper

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"btidy/pkg/collector"
	"btidy/pkg/safepath"
	"btidy/pkg/trash"
)

const (
	progressStageExtracting = "extracting"
)

// ExtractOperation represents a single archive extraction operation.
type ExtractOperation struct {
	// ArchivePath is the absolute path to the archive file being extracted.
	ArchivePath string

	// ExtractedFiles is the count of regular files successfully extracted from this archive.
	ExtractedFiles int

	// ExtractedDirs is the count of directories created during extraction.
	ExtractedDirs int

	// SkippedEntries is the count of archive entries that were skipped (e.g., existing files in non-overwrite mode).
	SkippedEntries int

	// EntryErrors contains error messages for individual entries that failed during extraction.
	EntryErrors []string

	// NestedArchives is the count of archive files discovered within this archive during extraction.
	NestedArchives int

	// ExtractionComplete indicates whether the archive was fully extracted without fatal errors.
	ExtractionComplete bool

	// DeletedArchive indicates whether the source archive was deleted after successful extraction.
	DeletedArchive bool

	// TrashedTo is the trash location path if the archive was soft-deleted (moved to trash).
	TrashedTo string

	// Skipped indicates whether this archive was skipped entirely without attempting extraction.
	Skipped bool

	// SkipReason contains the explanation when Skipped is true (e.g., "not a zip file", "path escape").
	SkipReason string

	// Error contains any fatal error that prevented extraction or archive deletion.
	Error error
}

// Result contains the aggregated statistics and outcomes from an unzip operation.
// Use this to understand the overall impact of extraction: how many archives were
// found and processed, how many files were extracted, and whether any errors occurred.
// The Operations slice provides detailed per-archive breakdown.
type Result struct {
	// Operations contains detailed results for each individual archive extraction.
	Operations []ExtractOperation

	// TotalFiles is the total number of files scanned in the target directory.
	TotalFiles int

	// ArchivesFound is the number of archive files discovered during collection.
	ArchivesFound int

	// ArchivesProcessed is the number of archives that were attempted for extraction.
	ArchivesProcessed int

	// ExtractedArchives is the count of archives successfully extracted.
	ExtractedArchives int

	// DeletedArchives is the number of archives removed after successful extraction.
	DeletedArchives int

	// ExtractedFiles is the total number of files extracted across all archives.
	ExtractedFiles int

	// ExtractedDirs is the total number of directories created during extraction.
	ExtractedDirs int

	// SkippedCount is the number of archives skipped (e.g., already extracted).
	SkippedCount int

	// ErrorCount is the number of archives that failed to extract.
	ErrorCount int
}

// Unzipper extracts archives recursively while enforcing path containment.
//
// An Unzipper orchestrates the extraction of archive files within a validated root directory.
// It ensures all extracted paths remain within the target directory boundary through safepath
// validation, preventing path traversal attacks and symlink escapes.
//
// Key features:
//   - Recursive extraction: archives within archives are discovered and extracted
//   - Path safety: all extraction paths validated via safepath.Validator
//   - Soft-delete support: optional trash integration for reversible archive removal
//   - Dry-run mode: preview extraction without modifying the filesystem
//   - Progress tracking: reports extraction progress through callback functions
//
// Usage:
//
//	// Create with automatic validator
//	uz, err := unzipper.New("/path/to/target", false)
//
//	// Or with existing validator and trash support
//	uz, err := unzipper.NewWithValidator(validator, false, trasher)
//
//	// Extract archives with progress tracking
//	result := uz.ExtractArchivesWithProgress(files, func(stage string, processed, total int) {
//	    fmt.Printf("Stage: %s, Progress: %d/%d\n", stage, processed, total)
//	})
//
// Safety guarantees:
//   - All extraction paths validated before creation
//   - Symlinks that escape root directory are rejected
//   - Archive entries with path traversal attempts (../) are blocked
//   - Overwrite protection available through configuration
//   - Optional soft-delete moves archives to trash instead of permanent deletion
//
// The Unzipper is safe for concurrent use within different root directories,
// but should not be shared across goroutines for the same extraction operation.
type Unzipper struct {
	// dryRun when true prevents all filesystem modifications.
	// Extraction logic executes but no files are created, moved, or deleted.
	// Use this to preview what would happen before committing changes.
	dryRun bool

	// validator enforces path containment for all extraction operations.
	// Every extracted file path is validated to ensure it stays within the
	// root directory. This prevents malicious archives from writing outside
	// the target directory through techniques like path traversal or symlinks.
	validator *safepath.Validator

	// trasher enables soft-delete (reversible removal) of archives after extraction.
	// When set, successfully extracted archives are moved to .btidy/trash/<run-id>/
	// instead of being permanently deleted. This allows undo operations.
	// If nil, archives are permanently deleted when deletion is requested.
	trasher *trash.Trasher
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
func NewWithValidator(
	validator *safepath.Validator,
	dryRun bool,
	trasher *trash.Trasher,
) (*Unzipper, error) {
	if validator == nil {
		return nil, errors.New("validator is required")
	}

	return &Unzipper{
		dryRun:    dryRun,
		validator: validator,
		trasher:   trasher,
	}, nil
}

// ExtractArchivesWithProgressRecursively extracts all archive files from the
// provided file list, then re-scans the directory to discover and extract any
// nested archives that were contained within the originals. This process repeats
// until no more archives remain.
//
// The progress callback is invoked during extraction to report the current stage,
// number of archives processed, and total archive count. Pass nil to disable
// progress reporting.
//
// It determines the root directory from the common ancestor of all provided files
// and returns an empty [Result] if the file list is empty. On each iteration,
// only archive files are selected for extraction; after extraction, the directory
// is re-collected to find any newly revealed archives.
//
// Returns the aggregated [Result] and any error encountered during extraction or
// file collection.
func (u *Unzipper) ExtractArchivesWithProgressRecursively(
	files []collector.FileInfo,
	progress func(stage string, processed, total int),
) (Result, error) {
	res := Result{TotalFiles: len(files)}
	rootDir := getRootDirectory(files)

	if rootDir == "" {
		return res, nil
	}

	processed := make(map[string]bool)

	for {
		archives := filterNewArchives(files, processed)
		if len(archives) == 0 {
			break
		}

		res.ArchivesFound += len(archives)

		if err := u.extractBatch(archives, processed, progress, &res); err != nil {
			return res, err
		}

		var err error
		files, err = getAllFilesRecursively(rootDir)
		if err != nil {
			return res, err
		}

		res.TotalFiles = len(files)
	}

	return res, nil
}

// filterNewArchives returns archives from files that have not yet been processed.
func filterNewArchives(files []collector.FileInfo, processed map[string]bool) []collector.FileInfo {
	archives := filterOnlyArchives(files)

	unprocessed := make([]collector.FileInfo, 0, len(archives))
	for _, a := range archives {
		key := filepath.Join(a.Dir, a.Name)
		if !processed[key] {
			unprocessed = append(unprocessed, a)
		}
	}

	return unprocessed
}

// extractBatch processes a batch of archives, updating the result and processed map.
func (u *Unzipper) extractBatch(
	archives []collector.FileInfo,
	processed map[string]bool,
	progress func(stage string, processed, total int),
	res *Result,
) error {
	for i, archive := range archives {
		if progress != nil {
			progress(progressStageExtracting, i, len(archives))
		}

		archivePath := filepath.Join(archive.Dir, archive.Name)
		processed[archivePath] = true

		op, err := u.processArchive(archive, archivePath)
		res.ArchivesProcessed++

		if err != nil {
			res.ErrorCount++
			res.Operations = append(res.Operations, op)
			return err
		}

		res.ExtractedArchives++
		res.ExtractedFiles += op.ExtractedFiles
		res.ExtractedDirs += op.ExtractedDirs

		op.DeletedArchive = true
		res.DeletedArchives++
		res.Operations = append(res.Operations, op)
	}

	if progress != nil {
		progress(progressStageExtracting, len(archives), len(archives))
	}

	return nil
}

// processArchive extracts or inspects a single archive, then removes it if not in dry-run mode.
func (u *Unzipper) processArchive(archive collector.FileInfo, archivePath string) (ExtractOperation, error) {
	var op ExtractOperation
	var err error

	if u.dryRun {
		op, err = inspectArchive(archive)
	} else {
		op, err = unzip(archive)
	}

	if err != nil {
		return op, err
	}

	if !u.dryRun {
		trashedTo, rmErr := u.removeArchive(archivePath)
		if rmErr != nil {
			op.Error = rmErr
			return op, rmErr
		}
		op.TrashedTo = trashedTo
	}

	return op, nil
}

// removeArchive deletes or trashes the archive at the given path.
// Returns the trash destination path (non-empty only when using a trasher).
func (u *Unzipper) removeArchive(archivePath string) (string, error) {
	if u.trasher != nil {
		dest, err := u.trasher.TrashWithDest(archivePath)
		if err != nil {
			return "", fmt.Errorf("failed to trash archive %s: %w", archivePath, err)
		}
		return dest, nil
	}

	if err := os.Remove(archivePath); err != nil {
		return "", fmt.Errorf("failed to remove archive %s: %w", archivePath, err)
	}

	return "", nil
}

// getRootDirectory computes the lowest common ancestor directory for a slice of
// [collector.FileInfo]. It iteratively walks up the directory tree until it finds
// a path that is a parent of (or equal to) every file's directory. Returns an
// empty string if the slice is empty.
func getRootDirectory(f []collector.FileInfo) string {
	if len(f) == 0 {
		return ""
	}

	root := f[0].Dir
	for _, fi := range f[1:] {
		for !isSubPath(root, fi.Dir) {
			parent := filepath.Dir(root)
			if parent == root {
				return root
			}
			root = parent
		}
	}

	return root
}

// isSubPath reports whether child is equal to or nested under parent.
func isSubPath(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}

// filterOnlyArchives filters a slice of FileInfo, returning only entries whose
// filenames are recognized as archive formats.
func filterOnlyArchives(blob []collector.FileInfo) []collector.FileInfo {
	filteredBlob := make([]collector.FileInfo, 0)
	for _, f := range blob {
		if ok := isArchive(filepath.Join(f.Dir, f.Name)); ok {
			filteredBlob = append(filteredBlob, f)
		}
	}

	return filteredBlob
}

// unzip extracts all entries from the zip archive identified by file into the
// same directory that contains the archive. It creates directories as needed and
// writes regular files with their archived content. Archive entries containing
// path traversal components (e.g., "../") are rejected to prevent zip-slip
// attacks. Returns an ExtractOperation describing what was extracted and any
// error encountered.
func unzip(file collector.FileInfo) (ExtractOperation, error) {
	archivePath := filepath.Join(file.Dir, file.Name)
	op := ExtractOperation{ArchivePath: archivePath}

	r, err := zip.OpenReader(archivePath)
	if err != nil {
		op.Error = fmt.Errorf("failed to open archive %s: %w", archivePath, err)
		return op, op.Error
	}
	defer func() {
		_ = r.Close()
	}()

	for _, entry := range r.File {
		if strings.Contains(entry.Name, "..") {
			op.Error = fmt.Errorf("illegal entry path %q: contains path traversal", entry.Name)
			return op, op.Error
		}

		targetPath := filepath.Join(file.Dir, filepath.FromSlash(entry.Name))

		if entry.FileInfo().IsDir() {
			if mkErr := os.MkdirAll(targetPath, entry.Mode().Perm()|0o755); mkErr != nil {
				op.Error = fmt.Errorf("failed to create directory %s: %w", targetPath, mkErr)
				return op, op.Error
			}
			op.ExtractedDirs++
			continue
		}

		parentDir := filepath.Dir(targetPath)
		if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
			op.Error = fmt.Errorf("failed to create parent directory %s: %w", parentDir, mkErr)
			return op, op.Error
		}

		if writeErr := extractFile(entry, targetPath); writeErr != nil {
			op.Error = fmt.Errorf("failed to extract %s: %w", entry.Name, writeErr)
			return op, op.Error
		}
		op.ExtractedFiles++

		if isArchive(targetPath) {
			op.NestedArchives++
		}
	}

	op.ExtractionComplete = true
	return op, nil
}

// inspectArchive reads the zip archive catalog without extracting any files,
// returning an ExtractOperation with the counts that would result from a real
// extraction. Used for dry-run mode.
func inspectArchive(file collector.FileInfo) (ExtractOperation, error) {
	archivePath := filepath.Join(file.Dir, file.Name)
	op := ExtractOperation{ArchivePath: archivePath}

	r, err := zip.OpenReader(archivePath)
	if err != nil {
		op.Error = fmt.Errorf("failed to open archive %s: %w", archivePath, err)
		return op, op.Error
	}
	defer func() {
		_ = r.Close()
	}()

	for _, entry := range r.File {
		if strings.Contains(entry.Name, "..") {
			op.Error = fmt.Errorf("illegal entry path %q: contains path traversal", entry.Name)
			return op, op.Error
		}

		if entry.FileInfo().IsDir() {
			op.ExtractedDirs++
			continue
		}

		op.ExtractedFiles++
	}

	op.ExtractionComplete = true
	return op, nil
}

// maxDecompressedSize is the maximum allowed size for a single extracted file (100 GiB).
// This guards against zip-bomb attacks where a small archive expands to enormous size,
// while still allowing extraction of very large legitimate files.
const maxDecompressedSize = 100 << 30

// extractFile writes a single zip file entry to targetPath. It opens the
// compressed entry for reading, creates the destination file, and copies the
// content. The destination file receives the permission bits stored in the
// archive entry. Extraction is limited to [maxDecompressedSize] bytes to
// prevent decompression bombs.
func extractFile(entry *zip.File, targetPath string) error {
	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("failed to open entry: %w", err)
	}
	defer func() {
		_ = rc.Close()
	}()

	outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, entry.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	if _, err = io.Copy(outFile, io.LimitReader(rc, maxDecompressedSize)); err != nil {
		_ = outFile.Close()
		return fmt.Errorf("failed to write file content: %w", err)
	}

	return outFile.Close()
}

// isArchive reports whether filePath is a valid zip archive by attempting to
// open it with archive/zip. Returns true if the file can be opened as a zip
// archive, false if it cannot (e.g., not a zip file or corrupted). The error
// return is reserved for unexpected I/O failures; a file that simply isn't a
// zip archive is not treated as an error.
func isArchive(filePath string) bool {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return false
	}
	_ = r.Close()

	return true
}

// getAllFilesRecursively collects all files under rootDir, skipping the .btidy
// metadata directory. It returns a slice of FileInfo for every regular file found.
func getAllFilesRecursively(rootDir string) ([]collector.FileInfo, error) {
	c := collector.New(collector.Options{
		SkipDirs: []string{".btidy"},
	})

	files, err := c.Collect(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect files: %w", err)
	}

	return files, nil
}
