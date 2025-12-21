// Package duplicates provides duplicate file detection utilities.
// Files are considered duplicates if they have the same size and modification time.
package duplicates

import (
	"file-organizer/pkg/collector"
)

// FileGroup represents a group of files that would have the same target name.
type FileGroup struct {
	TargetName string
	Files      []collector.FileInfo
}

// DuplicateSet represents files that are true duplicates (same size + mtime).
type DuplicateSet struct {
	Keep    collector.FileInfo   // The file to keep
	Delete  []collector.FileInfo // Files to delete
	Size    int64
	ModTime int64 // Unix timestamp
}

// Result contains the analysis results.
type Result struct {
	UniqueFiles    []collector.FileInfo // Files with no duplicates
	DuplicateSets  []DuplicateSet       // Groups of duplicate files
	TotalFiles     int
	UniqueCount    int
	DuplicateCount int // Total number of duplicate files to delete
}

// Detector finds duplicate files.
type Detector struct{}

// New creates a new Detector.
func New() *Detector {
	return &Detector{}
}

// fileKey creates a unique key for identifying duplicates based on size and mtime.
type fileKey struct {
	size  int64
	mtime int64
}

// FindDuplicates analyzes files and identifies duplicates.
// Files with the same size and modification time are considered duplicates.
func (d *Detector) FindDuplicates(files []collector.FileInfo) Result {
	result := Result{
		TotalFiles: len(files),
	}

	if len(files) == 0 {
		return result
	}

	// Group files by (size, mtime)
	groups := make(map[fileKey][]collector.FileInfo)

	for _, f := range files {
		key := fileKey{
			size:  f.Size,
			mtime: f.ModTime.Unix(),
		}
		groups[key] = append(groups[key], f)
	}

	// Analyze groups
	for key, group := range groups {
		if len(group) == 1 {
			// Unique file
			result.UniqueFiles = append(result.UniqueFiles, group[0])
			result.UniqueCount++
		} else {
			// Duplicates found - keep first, mark rest for deletion
			set := DuplicateSet{
				Keep:    group[0],
				Delete:  group[1:],
				Size:    key.size,
				ModTime: key.mtime,
			}
			result.DuplicateSets = append(result.DuplicateSets, set)
			result.UniqueCount++
			result.DuplicateCount += len(group) - 1
		}
	}

	return result
}

// FindDuplicatesByName finds duplicates among files that would have the same target name.
// This is used after renaming, when multiple files might end up with the same name.
// Files with same target name + same size + same mtime are true duplicates.
func (d *Detector) FindDuplicatesByName(files []collector.FileInfo, nameFunc func(collector.FileInfo) string) Result {
	result := Result{
		TotalFiles: len(files),
	}

	if len(files) == 0 {
		return result
	}

	// First group by target name
	nameGroups := make(map[string][]collector.FileInfo)
	for _, f := range files {
		name := nameFunc(f)
		nameGroups[name] = append(nameGroups[name], f)
	}

	// For each name group, find duplicates by size+mtime
	for _, group := range nameGroups {
		if len(group) == 1 {
			result.UniqueFiles = append(result.UniqueFiles, group[0])
			result.UniqueCount++
			continue
		}

		// Within this name group, find true duplicates (same size + mtime)
		subGroups := make(map[fileKey][]collector.FileInfo)
		for _, f := range group {
			key := fileKey{
				size:  f.Size,
				mtime: f.ModTime.Unix(),
			}
			subGroups[key] = append(subGroups[key], f)
		}

		for key, subGroup := range subGroups {
			if len(subGroup) == 1 {
				result.UniqueFiles = append(result.UniqueFiles, subGroup[0])
				result.UniqueCount++
			} else {
				set := DuplicateSet{
					Keep:    subGroup[0],
					Delete:  subGroup[1:],
					Size:    key.size,
					ModTime: key.mtime,
				}
				result.DuplicateSets = append(result.DuplicateSets, set)
				result.UniqueCount++
				result.DuplicateCount += len(subGroup) - 1
			}
		}
	}

	return result
}
