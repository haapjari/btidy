// Package flattener moves all files to root directory and removes duplicates.
package flattener

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/hasher"
	"file-organizer/pkg/safepath"
)

// MoveOperation represents a single move operation.
type MoveOperation struct {
	OriginalPath string
	NewPath      string
	Hash         string // SHA256 content hash
	Duplicate    bool   // true if this file was deleted as duplicate
	Skipped      bool   // true if skipped (e.g., already in root)
	SkipReason   string
	Error        error
}

// Result contains the results of a flatten operation.
type Result struct {
	Operations       []MoveOperation
	TotalFiles       int
	MovedCount       int
	DuplicatesCount  int
	SkippedCount     int
	ErrorCount       int
	DeletedDirsCount int
}

// Flattener moves files to root and removes duplicates.
type Flattener struct {
	dryRun    bool
	rootDir   string
	validator *safepath.Validator
	hasher    *hasher.Hasher
}

// New creates a new Flattener with path containment validation.
func New(rootDir string, dryRun bool) (*Flattener, error) {
	v, err := safepath.New(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create path validator: %w", err)
	}

	return &Flattener{
		rootDir:   rootDir,
		dryRun:    dryRun,
		validator: v,
		hasher:    hasher.New(),
	}, nil
}

// FlattenFiles moves all files to root directory, removing duplicates.
// Duplicates are identified by SHA256 content hash - this is safe and reliable.
func (f *Flattener) FlattenFiles(files []collector.FileInfo) Result {
	result := Result{
		TotalFiles: len(files),
		Operations: make([]MoveOperation, 0, len(files)),
	}

	if len(files) == 0 {
		return result
	}

	// Step 1: Pre-compute hashes for all files using parallel hashing.
	fileHashes := f.computeHashes(files)

	// Step 2: Track seen content hashes to detect duplicates.
	seenHash := make(map[string]string) // hash -> path of first occurrence

	// Track name conflicts (same name but different content).
	nameCount := make(map[string]int)

	for i := range files {
		hash := fileHashes[files[i].Path]
		op := f.processFile(&files[i], hash, seenHash, nameCount)
		result.Operations = append(result.Operations, op)

		if op.Error != nil {
			result.ErrorCount++
		} else if op.Duplicate {
			result.DuplicatesCount++
		} else if op.Skipped {
			result.SkippedCount++
		} else {
			result.MovedCount++
		}
	}

	// Remove empty directories if not dry run.
	if !f.dryRun {
		result.DeletedDirsCount = f.removeEmptyDirs()
	}

	return result
}

// computeHashes pre-computes SHA256 hashes for all files using parallel hashing.
func (f *Flattener) computeHashes(files []collector.FileInfo) map[string]string {
	hashes := make(map[string]string, len(files))

	// Prepare files for parallel hashing.
	toHash := make([]hasher.FileToHash, len(files))
	for i, file := range files {
		toHash[i] = hasher.FileToHash{
			Path: file.Path,
			Size: file.Size,
		}
	}

	// Hash files in parallel.
	for result := range f.hasher.HashFilesWithSizes(toHash) {
		if result.Error == nil {
			hashes[result.Path] = result.Hash
		}
	}

	return hashes
}

func (f *Flattener) processFile(file *collector.FileInfo, hash string, seenHash map[string]string, nameCount map[string]int) MoveOperation {
	op := MoveOperation{
		OriginalPath: file.Path,
		Hash:         hash,
	}

	// Validate source path is within root.
	if err := f.validator.ValidatePath(file.Path); err != nil {
		op.Error = fmt.Errorf("source path escapes root: %w", err)
		return op
	}
	if err := f.validator.ValidateSymlink(file.Path); err != nil {
		op.Error = fmt.Errorf("source path escapes root: %w", err)
		return op
	}

	// If we couldn't compute hash, skip this file with error.
	if hash == "" {
		op.Error = errors.New("could not compute hash for file")
		return op
	}

	// Check if already in root directory.
	if file.Dir == f.rootDir {
		op.Skipped = true
		op.SkipReason = "already in root"
		op.NewPath = file.Path
		// Still track the hash so duplicates in subdirs are detected.
		if _, exists := seenHash[hash]; !exists {
			seenHash[hash] = file.Path
		}
		return op
	}

	// Check if this is a duplicate by content hash.
	if existingPath, exists := seenHash[hash]; exists {
		op.Duplicate = true
		op.NewPath = existingPath // reference to the kept file

		if !f.dryRun {
			if err := f.validator.SafeRemove(file.Path); err != nil {
				op.Error = fmt.Errorf("failed to delete duplicate: %w", err)
			}
		}
		return op
	}

	// Determine target path, handling name conflicts.
	targetName := file.Name
	count := nameCount[file.Name]
	if count > 0 {
		ext := filepath.Ext(file.Name)
		base := file.Name[:len(file.Name)-len(ext)]
		targetName = fmt.Sprintf("%s_%d%s", base, count, ext)
	}
	nameCount[file.Name] = count + 1

	op.NewPath = filepath.Join(f.rootDir, targetName)

	// Validate destination path is within root.
	if err := f.validator.ValidatePath(op.NewPath); err != nil {
		op.Error = fmt.Errorf("destination path escapes root: %w", err)
		return op
	}

	// Mark this content hash as seen.
	seenHash[hash] = op.NewPath

	// Move the file using safe rename.
	if !f.dryRun {
		if err := f.validator.SafeRename(file.Path, op.NewPath); err != nil {
			op.Error = fmt.Errorf("failed to move: %w", err)
		}
	}

	return op
}

// removeEmptyDirs removes all empty directories under rootDir.
func (f *Flattener) removeEmptyDirs() int {
	count := 0

	// Walk bottom-up by collecting all dirs first.
	var dirs []string
	err := filepath.Walk(f.rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // Continue walking despite errors.
		}
		if info.IsDir() && path != f.rootDir {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return 0
	}

	// Remove in reverse order (deepest first).
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			if err := f.validator.SafeRemoveDir(dir); err == nil {
				count++
			}
		}
	}

	return count
}

// DryRun returns whether the flattener is in dry-run mode.
func (f *Flattener) DryRun() bool {
	return f.dryRun
}

// Root returns the root directory being validated against.
func (f *Flattener) Root() string {
	return f.validator.Root()
}
