// Package renamer provides file renaming utilities.
// Phase 1: Renames files in place with timestamp prefix and sanitized names.
package renamer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/safepath"
	"file-organizer/pkg/sanitizer"
)

// RenameOperation represents a single rename operation.
type RenameOperation struct {
	OriginalPath string
	NewPath      string
	OriginalName string
	NewName      string
	Skipped      bool
	SkipReason   string
	Deleted      bool
	Error        error
}

// Result contains the results of a rename operation.
type Result struct {
	Operations   []RenameOperation
	TotalFiles   int
	RenamedCount int
	SkippedCount int
	DeletedCount int
	ErrorCount   int
}

// Renamer handles file renaming operations.
type Renamer struct {
	dryRun    bool
	validator *safepath.Validator
}

var tbdPrefixPattern = regexp.MustCompile(`^\d{4}-TBD-TBD_`)

type nameUsage struct {
	count int
	file  collector.FileInfo
}

// New creates a new Renamer with path containment validation.
func New(rootDir string, dryRun bool) (*Renamer, error) {
	v, err := safepath.New(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create path validator: %w", err)
	}

	return &Renamer{
		dryRun:    dryRun,
		validator: v,
	}, nil
}

// RenameFiles renames all files in the given list according to the naming conventions.
// Files are renamed in place (same directory).
func (r *Renamer) RenameFiles(files []collector.FileInfo) Result {
	result := Result{
		TotalFiles: len(files),
		Operations: make([]RenameOperation, 0, len(files)),
	}

	// Track new names within each directory to handle conflicts and duplicates
	dirNames := make(map[string]map[string]nameUsage) // dir -> name -> usage

	for _, f := range files {
		op := r.processFile(f, dirNames)
		result.Operations = append(result.Operations, op)

		if op.Error != nil {
			result.ErrorCount++
		} else if op.Deleted {
			result.DeletedCount++
		} else if op.Skipped {
			result.SkippedCount++
		} else {
			result.RenamedCount++
		}
	}

	return result
}

// processFile handles renaming a single file.
func (r *Renamer) processFile(f collector.FileInfo, dirNames map[string]map[string]nameUsage) RenameOperation {
	op := RenameOperation{
		OriginalPath: f.Path,
		OriginalName: f.Name,
	}

	if tbdPrefixPattern.MatchString(f.Name) {
		op.Skipped = true
		op.SkipReason = "already has TBD prefix"
		return op
	}

	// Validate source path is within root.
	if err := r.validator.ValidatePath(f.Path); err != nil {
		op.Error = fmt.Errorf("source path escapes root: %w", err)
		return op
	}

	// Generate new name
	newName := sanitizer.GenerateTimestampedName(f.Name, f.ModTime)

	// Initialize directory tracking if needed
	if dirNames[f.Dir] == nil {
		dirNames[f.Dir] = make(map[string]nameUsage)
	}

	// Handle naming conflicts within the same directory
	baseName := newName
	ext := filepath.Ext(newName)
	nameWithoutExt := newName[:len(newName)-len(ext)]

	newName, handled := r.resolveNameConflict(&op, f, baseName, nameWithoutExt, ext, dirNames[f.Dir])
	if handled {
		return op
	}

	op.NewName = newName
	op.NewPath = filepath.Join(f.Dir, newName)

	// Validate destination path is within root.
	if err := r.validator.ValidatePath(op.NewPath); err != nil {
		op.Error = fmt.Errorf("destination path escapes root: %w", err)
		return op
	}

	// Skip if name hasn't changed
	if f.Name == newName {
		op.Skipped = true
		op.SkipReason = "name unchanged"
		return op
	}

	// Skip if target already exists (safety check)
	if info, err := os.Stat(op.NewPath); err == nil {
		r.handleExistingTarget(&op, f, info)
		return op
	}

	// Perform rename if not dry run, using safe rename.
	if !r.dryRun {
		if err := r.validator.SafeRename(f.Path, op.NewPath); err != nil {
			op.Error = err
			return op
		}
	}

	return op
}

// DryRun returns whether the renamer is in dry-run mode.
func (r *Renamer) DryRun() bool {
	return r.dryRun
}

// Root returns the root directory being validated against.
func (r *Renamer) Root() string {
	return r.validator.Root()
}

func sameFileInfo(a, b collector.FileInfo) bool {
	return a.Size == b.Size && a.ModTime.Equal(b.ModTime)
}

func (r *Renamer) resolveNameConflict(op *RenameOperation, f collector.FileInfo, baseName, nameWithoutExt, ext string, usageMap map[string]nameUsage) (string, bool) {
	usage, ok := usageMap[baseName]
	if !ok {
		usageMap[baseName] = nameUsage{
			count: 1,
			file:  f,
		}
		return baseName, false
	}

	if sameFileInfo(usage.file, f) {
		op.Skipped = true
		op.SkipReason = "duplicate file already exists"
		op.Deleted = true
		if !r.dryRun {
			if err := os.Remove(f.Path); err != nil {
				op.Error = err
			}
		} else {
			op.Deleted = false
		}
		return "", true
	}

	newName := fmt.Sprintf("%s_%d%s", nameWithoutExt, usage.count, ext)
	usage.count++
	usageMap[baseName] = usage
	return newName, false
}

func (r *Renamer) handleExistingTarget(op *RenameOperation, f collector.FileInfo, info os.FileInfo) {
	if info.Size() == f.Size && info.ModTime().Equal(f.ModTime) {
		op.Skipped = true
		op.SkipReason = "duplicate file already exists"
		op.Deleted = true
		if r.dryRun {
			op.Deleted = false
			return
		}
		if err := os.Remove(f.Path); err != nil {
			op.Error = err
		}
		return
	}

	op.Skipped = true
	op.SkipReason = "target file already exists"
}
