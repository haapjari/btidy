package duplicates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"file-organizer/pkg/collector"
)

func makeFile(path, name string, size int64, modTime time.Time) collector.FileInfo {
	return collector.FileInfo{
		Path:    path,
		Dir:     "/test",
		Name:    name,
		Size:    size,
		ModTime: modTime,
	}
}

func TestDetector_FindDuplicates_NoDuplicates(t *testing.T) {
	files := []collector.FileInfo{
		makeFile("/test/file1.txt", "file1.txt", 100, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)),
		makeFile("/test/file2.txt", "file2.txt", 200, time.Date(2018, 1, 2, 0, 0, 0, 0, time.UTC)),
		makeFile("/test/file3.txt", "file3.txt", 300, time.Date(2018, 1, 3, 0, 0, 0, 0, time.UTC)),
	}

	d := New()
	result := d.FindDuplicates(files)

	assert.Equal(t, 3, result.TotalFiles)
	assert.Equal(t, 3, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
	assert.Len(t, result.UniqueFiles, 3)
	assert.Empty(t, result.DuplicateSets)
}

func TestDetector_FindDuplicates_WithDuplicates(t *testing.T) {
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)

	files := []collector.FileInfo{
		makeFile("/test/file1.txt", "file1.txt", 100, modTime),
		makeFile("/backup/file1.txt", "file1.txt", 100, modTime), // duplicate
		makeFile("/test/file2.txt", "file2.txt", 200, time.Date(2018, 1, 2, 0, 0, 0, 0, time.UTC)),
	}

	d := New()
	result := d.FindDuplicates(files)

	assert.Equal(t, 3, result.TotalFiles)
	assert.Equal(t, 2, result.UniqueCount) // file1 + file2
	assert.Equal(t, 1, result.DuplicateCount)
	assert.Len(t, result.DuplicateSets, 1)

	// Check the duplicate set
	set := result.DuplicateSets[0]
	assert.Equal(t, int64(100), set.Size)
	assert.Len(t, set.Delete, 1)
}

func TestDetector_FindDuplicates_MultipleDuplicates(t *testing.T) {
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)

	files := []collector.FileInfo{
		makeFile("/a/file.txt", "file.txt", 100, modTime),
		makeFile("/b/file.txt", "file.txt", 100, modTime), // duplicate
		makeFile("/c/file.txt", "file.txt", 100, modTime), // duplicate
		makeFile("/d/file.txt", "file.txt", 100, modTime), // duplicate
	}

	d := New()
	result := d.FindDuplicates(files)

	assert.Equal(t, 4, result.TotalFiles)
	assert.Equal(t, 1, result.UniqueCount)
	assert.Equal(t, 3, result.DuplicateCount)
	require.Len(t, result.DuplicateSets, 1)
	assert.Len(t, result.DuplicateSets[0].Delete, 3)
}

func TestDetector_FindDuplicates_SameSizeDifferentTime(t *testing.T) {
	// Same size but different modification time = NOT duplicates
	files := []collector.FileInfo{
		makeFile("/test/file1.txt", "file1.txt", 100, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)),
		makeFile("/test/file2.txt", "file2.txt", 100, time.Date(2018, 1, 2, 0, 0, 0, 0, time.UTC)),
	}

	d := New()
	result := d.FindDuplicates(files)

	assert.Equal(t, 2, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
	assert.Empty(t, result.DuplicateSets)
}

func TestDetector_FindDuplicates_SameTimeDifferentSize(t *testing.T) {
	// Same time but different size = NOT duplicates
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)

	files := []collector.FileInfo{
		makeFile("/test/file1.txt", "file1.txt", 100, modTime),
		makeFile("/test/file2.txt", "file2.txt", 200, modTime),
	}

	d := New()
	result := d.FindDuplicates(files)

	assert.Equal(t, 2, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
	assert.Empty(t, result.DuplicateSets)
}

func TestDetector_FindDuplicates_Empty(t *testing.T) {
	d := New()
	result := d.FindDuplicates([]collector.FileInfo{})

	assert.Equal(t, 0, result.TotalFiles)
	assert.Equal(t, 0, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
	assert.Empty(t, result.UniqueFiles)
	assert.Empty(t, result.DuplicateSets)
}

func TestDetector_FindDuplicatesByName_NoDuplicates(t *testing.T) {
	files := []collector.FileInfo{
		makeFile("/a/doc.pdf", "doc.pdf", 100, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)),
		makeFile("/b/image.jpg", "image.jpg", 200, time.Date(2018, 1, 2, 0, 0, 0, 0, time.UTC)),
	}

	nameFunc := func(f collector.FileInfo) string {
		return f.Name
	}

	d := New()
	result := d.FindDuplicatesByName(files, nameFunc)

	assert.Equal(t, 2, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
}

func TestDetector_FindDuplicatesByName_SameNameDifferentContent(t *testing.T) {
	// Same target name but different size = NOT duplicates (conflict, not duplicate)
	files := []collector.FileInfo{
		makeFile("/a/doc.pdf", "doc.pdf", 100, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)),
		makeFile("/b/doc.pdf", "doc.pdf", 200, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)),
	}

	nameFunc := func(f collector.FileInfo) string {
		return f.Name
	}

	d := New()
	result := d.FindDuplicatesByName(files, nameFunc)

	assert.Equal(t, 2, result.UniqueCount)
	assert.Equal(t, 0, result.DuplicateCount)
}

func TestDetector_FindDuplicatesByName_TrueDuplicates(t *testing.T) {
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)

	// Same target name + same size + same mtime = TRUE duplicates
	files := []collector.FileInfo{
		makeFile("/a/doc.pdf", "doc.pdf", 100, modTime),
		makeFile("/b/doc.pdf", "doc.pdf", 100, modTime),
	}

	nameFunc := func(f collector.FileInfo) string {
		return f.Name
	}

	d := New()
	result := d.FindDuplicatesByName(files, nameFunc)

	assert.Equal(t, 1, result.UniqueCount)
	assert.Equal(t, 1, result.DuplicateCount)
	require.Len(t, result.DuplicateSets, 1)
}

func TestDetector_FindDuplicatesByName_MixedScenario(t *testing.T) {
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)

	files := []collector.FileInfo{
		// Group 1: same name "doc.pdf", true duplicates (same size+mtime)
		makeFile("/a/doc.pdf", "doc.pdf", 100, modTime),
		makeFile("/b/doc.pdf", "doc.pdf", 100, modTime),
		// Group 2: same name "doc.pdf", different size (not duplicate, conflict)
		makeFile("/c/doc.pdf", "doc.pdf", 200, modTime),
		// Group 3: unique file
		makeFile("/d/image.jpg", "image.jpg", 300, modTime),
	}

	nameFunc := func(f collector.FileInfo) string {
		return f.Name
	}

	d := New()
	result := d.FindDuplicatesByName(files, nameFunc)

	assert.Equal(t, 4, result.TotalFiles)
	assert.Equal(t, 3, result.UniqueCount)    // doc.pdf(100), doc.pdf(200), image.jpg
	assert.Equal(t, 1, result.DuplicateCount) // one duplicate of doc.pdf(100)
}

func TestDetector_FindDuplicatesByName_Empty(t *testing.T) {
	nameFunc := func(f collector.FileInfo) string {
		return f.Name
	}

	d := New()
	result := d.FindDuplicatesByName([]collector.FileInfo{}, nameFunc)

	assert.Equal(t, 0, result.TotalFiles)
	assert.Empty(t, result.UniqueFiles)
	assert.Empty(t, result.DuplicateSets)
}
