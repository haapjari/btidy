// Package deduplicator identifies and removes duplicate files using content hashing.
// It uses a hybrid approach for performance:
// 1. Group files by size (instant filter - different sizes can't be duplicates)
// 2. For same-size files, compute partial hash (first + last 4KB) for quick comparison
// 3. For files with matching partial hash, compute full SHA256 to confirm
// This approach is both fast and reliable - no false positives possible.
package deduplicator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/safepath"
)

const (
	// partialHashSize is the number of bytes to read from start and end for partial hash.
	partialHashSize = 4096
	// smallFileThreshold - files smaller than this skip partial hash and go straight to full hash.
	smallFileThreshold = partialHashSize * 2
)

// DeleteOperation represents a single delete operation.
type DeleteOperation struct {
	Path       string // Path of file to delete
	OriginalOf string // Path of the original file this is a duplicate of
	Size       int64
	Hash       string // SHA256 hash of the file
	Skipped    bool
	SkipReason string
	Error      error
}

// Result contains the results of a deduplication operation.
type Result struct {
	Operations      []DeleteOperation
	TotalFiles      int
	DuplicatesFound int
	DeletedCount    int
	SkippedCount    int
	ErrorCount      int
	BytesRecovered  int64
}

// Deduplicator identifies and removes duplicate files using content hashing.
type Deduplicator struct {
	dryRun    bool
	rootDir   string
	validator *safepath.Validator
}

// New creates a new Deduplicator with path containment validation.
func New(rootDir string, dryRun bool) (*Deduplicator, error) {
	v, err := safepath.New(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create path validator: %w", err)
	}

	return &Deduplicator{
		rootDir:   rootDir,
		dryRun:    dryRun,
		validator: v,
	}, nil
}

// DuplicateGroup represents a group of files that are duplicates of each other.
type DuplicateGroup struct {
	Hash  string             // SHA256 hash shared by all files
	Size  int64              // Size shared by all files
	Keep  collector.FileInfo // File to keep (first found)
	Dupes []collector.FileInfo
}

// FindDuplicates analyzes files and identifies duplicates using content hashing.
// Returns a Result containing all duplicate files that should be deleted.
func (d *Deduplicator) FindDuplicates(files []collector.FileInfo) Result {
	result := Result{
		TotalFiles: len(files),
		Operations: make([]DeleteOperation, 0),
	}

	if len(files) == 0 {
		return result
	}

	// Step 1: Group by size (files with unique sizes cannot be duplicates).
	sizeGroups := groupBySize(files)

	// Step 2: For each size group with multiple files, find duplicates by hash.
	for _, group := range sizeGroups {
		if len(group) < 2 {
			continue
		}

		duplicateGroups := d.findDuplicatesInSizeGroup(group)
		for i := range duplicateGroups {
			for j := range duplicateGroups[i].Dupes {
				op := d.deleteFile(duplicateGroups[i].Dupes[j], duplicateGroups[i].Keep.Path, duplicateGroups[i].Hash)
				result.Operations = append(result.Operations, op)
			}
		}
	}

	// Sort operations by path for deterministic output.
	sort.Slice(result.Operations, func(i, j int) bool {
		return result.Operations[i].Path < result.Operations[j].Path
	})

	// Calculate counts.
	for _, op := range result.Operations {
		result.DuplicatesFound++
		switch {
		case op.Error != nil:
			result.ErrorCount++
		case op.Skipped:
			result.SkippedCount++
		default:
			result.DeletedCount++
			result.BytesRecovered += op.Size
		}
	}

	return result
}

// groupBySize groups files by their size.
func groupBySize(files []collector.FileInfo) map[int64][]collector.FileInfo {
	groups := make(map[int64][]collector.FileInfo)
	for _, f := range files {
		groups[f.Size] = append(groups[f.Size], f)
	}
	return groups
}

// findDuplicatesInSizeGroup finds duplicates among files of the same size.
func (d *Deduplicator) findDuplicatesInSizeGroup(files []collector.FileInfo) []DuplicateGroup {
	if len(files) < 2 {
		return nil
	}

	size := files[0].Size

	// For small files, go straight to full hash.
	if size <= smallFileThreshold {
		return d.findDuplicatesByFullHash(files)
	}

	// For larger files, use partial hash first.
	return d.findDuplicatesByPartialThenFullHash(files)
}

// findDuplicatesByFullHash groups files by their full SHA256 hash.
func (d *Deduplicator) findDuplicatesByFullHash(files []collector.FileInfo) []DuplicateGroup {
	hashGroups := make(map[string][]collector.FileInfo)

	for _, f := range files {
		hash, err := computeFullHash(f.Path)
		if err != nil {
			continue // Skip files we can't read.
		}
		hashGroups[hash] = append(hashGroups[hash], f)
	}

	return buildDuplicateGroups(hashGroups)
}

// findDuplicatesByPartialThenFullHash uses partial hash for initial grouping,
// then confirms with full hash.
func (d *Deduplicator) findDuplicatesByPartialThenFullHash(files []collector.FileInfo) []DuplicateGroup {
	// First pass: group by partial hash.
	partialGroups := make(map[string][]collector.FileInfo)

	for _, f := range files {
		hash, err := computePartialHash(f.Path, f.Size)
		if err != nil {
			continue
		}
		partialGroups[hash] = append(partialGroups[hash], f)
	}

	// Second pass: for groups with multiple files, confirm with full hash.
	var result []DuplicateGroup
	for _, group := range partialGroups {
		if len(group) < 2 {
			continue
		}
		// These files have matching partial hash - compute full hash to confirm.
		confirmed := d.findDuplicatesByFullHash(group)
		result = append(result, confirmed...)
	}

	return result
}

// buildDuplicateGroups converts hash groups to DuplicateGroup slice.
func buildDuplicateGroups(hashGroups map[string][]collector.FileInfo) []DuplicateGroup {
	groups := make([]DuplicateGroup, 0, len(hashGroups))

	for hash, files := range hashGroups {
		if len(files) < 2 {
			continue
		}

		// Sort files by path for deterministic "keep" selection.
		sort.Slice(files, func(i, j int) bool {
			return files[i].Path < files[j].Path
		})

		groups = append(groups, DuplicateGroup{
			Hash:  hash,
			Size:  files[0].Size,
			Keep:  files[0],
			Dupes: files[1:],
		})
	}

	return groups
}

// computeFullHash computes SHA256 hash of entire file.
func computeFullHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// computePartialHash computes hash of first and last partialHashSize bytes.
// This is much faster than full hash for large files and catches most differences.
func computePartialHash(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()

	// Read first chunk.
	buf := make([]byte, partialHashSize)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	h.Write(buf[:n])

	// Read last chunk (if file is large enough to have distinct last chunk).
	if size > partialHashSize {
		_, err = f.Seek(-partialHashSize, io.SeekEnd)
		if err != nil {
			return "", err
		}
		n, err = f.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		h.Write(buf[:n])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// deleteFile creates a delete operation and optionally performs the deletion.
func (d *Deduplicator) deleteFile(file collector.FileInfo, originalPath, hash string) DeleteOperation {
	op := DeleteOperation{
		Path:       file.Path,
		OriginalOf: originalPath,
		Size:       file.Size,
		Hash:       hash,
	}

	// Validate path is within root.
	if err := d.validator.ValidatePath(file.Path); err != nil {
		op.Error = fmt.Errorf("path escapes root: %w", err)
		return op
	}

	// Perform deletion if not dry run.
	if !d.dryRun {
		if err := d.validator.SafeRemove(file.Path); err != nil {
			op.Error = fmt.Errorf("failed to delete: %w", err)
		}
	}

	return op
}

// DryRun returns whether the deduplicator is in dry-run mode.
func (d *Deduplicator) DryRun() bool {
	return d.dryRun
}

// Root returns the root directory being validated against.
func (d *Deduplicator) Root() string {
	return d.validator.Root()
}

// ComputeFileHash computes and returns the SHA256 hash of a file.
// Exported for use by callers who need to verify file hashes.
func ComputeFileHash(path string) (string, error) {
	return computeFullHash(path)
}
