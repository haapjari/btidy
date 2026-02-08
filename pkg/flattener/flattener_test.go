package flattener

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
	"btidy/pkg/collector"
	"btidy/pkg/safepath"
)

func createTestFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	testutil.CreateFileWithModTime(t, path, content, modTime)
}

func collectFiles(t *testing.T, root string) []collector.FileInfo {
	t.Helper()

	c := collector.New(collector.Options{})
	files, err := c.Collect(root)
	require.NoError(t, err)

	return files
}

func TestFlattener_FlattenFiles_Basic(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "subdir", "file.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 1, result.MovedCount)
	assert.Equal(t, 0, result.DuplicatesCount)
	assert.Equal(t, 0, result.ErrorCount)

	// Verify file is now in root.
	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	require.NoError(t, err)

	// Verify original location is empty.
	_, err = os.Stat(filepath.Join(tmpDir, "subdir", "file.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestFlattener_FlattenFiles_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "subdir", "file.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)

	f, err := New(tmpDir, true) // dry run
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 1, result.MovedCount)

	// File should NOT have moved (dry run).
	_, err = os.Stat(filepath.Join(tmpDir, "subdir", "file.txt"))
	require.NoError(t, err, "file should still be in original location")

	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	assert.True(t, os.IsNotExist(err), "file should not be in root")
}

func TestFlattener_FlattenFiles_DryRun_NoFilesystemMutations(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "content", modTime)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	f, err := New(tmpDir, true)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 1, result.MovedCount)
	assert.Equal(t, 1, result.DuplicatesCount)
	assert.Equal(t, 0, result.DeletedDirsCount)
	assert.Equal(t, 0, result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "dir1", "file.txt"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "dir2", "file.txt"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	assert.True(t, os.IsNotExist(err), "dry-run must not move files to root")

	_, err = os.Stat(filepath.Join(tmpDir, "dir1"))
	require.NoError(t, err, "dry-run must not remove directories")
	_, err = os.Stat(filepath.Join(tmpDir, "dir2"))
	require.NoError(t, err, "dry-run must not remove directories")
}

func TestFlattener_FlattenFiles_Duplicates(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Same name, same size, same mtime = duplicate.
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "content", modTime)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 1, result.MovedCount)
	assert.Equal(t, 1, result.DuplicatesCount)

	// Only one file should exist in root.
	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	require.NoError(t, err)
}

func TestFlattener_FlattenFiles_SameMetadataDifferentContent_NotDuplicate(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "alpha-1", modTime)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "omega-2", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 2, result.MovedCount)
	assert.Equal(t, 0, result.DuplicatesCount)
	assert.Equal(t, 0, result.ErrorCount)

	basePath := filepath.Join(tmpDir, "file.txt")
	suffixPath := filepath.Join(tmpDir, "file_1.txt")

	_, err = os.Stat(basePath)
	require.NoError(t, err)
	_, err = os.Stat(suffixPath)
	require.NoError(t, err)

	baseContent, err := os.ReadFile(basePath)
	require.NoError(t, err)
	suffixContent, err := os.ReadFile(suffixPath)
	require.NoError(t, err)

	assert.NotEqual(t, string(baseContent), string(suffixContent))
}

func TestFlattener_FlattenFiles_NameConflict(t *testing.T) {
	tmpDir := t.TempDir()

	modTime1 := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	modTime2 := time.Date(2018, 7, 20, 12, 0, 0, 0, time.UTC)
	// Same name, different mtime = NOT duplicate, needs suffix.
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "content1", modTime1)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "content2", modTime2)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 2, result.MovedCount)
	assert.Equal(t, 0, result.DuplicatesCount)

	// Both files should exist with different names.
	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "file_1.txt"))
	require.NoError(t, err)
}

func TestFlattener_FlattenFiles_AlreadyInRoot(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "rootfile.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.MovedCount)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, "already in root", result.Operations[0].SkipReason)
}

func TestFlattener_FlattenFiles_RemovesEmptyDirs(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "a", "b", "c", "file.txt"), "content", modTime)

	files := collectFiles(t, tmpDir)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 1, result.MovedCount)
	assert.Equal(t, 3, result.DeletedDirsCount) // a, b, c

	// Verify directories are gone.
	_, err = os.Stat(filepath.Join(tmpDir, "a"))
	assert.True(t, os.IsNotExist(err))
}

func TestFlattener_FlattenFiles_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles([]collector.FileInfo{})

	assert.Equal(t, 0, result.TotalFiles)
	assert.Equal(t, 0, result.MovedCount)
	assert.Empty(t, result.Operations)
}

func TestFlattener_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	f, err := New(tmpDir, true)
	require.NoError(t, err)
	assert.True(t, f.DryRun())

	f, err = NewWithWorkers(tmpDir, false, 4)
	require.NoError(t, err)
	assert.False(t, f.DryRun())
}

func TestFlattener_FlattenFiles_DeepNesting(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Create files at various depths.
	createTestFile(t, filepath.Join(tmpDir, "l1", "file1.txt"), "1", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "file2.txt"), "2", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "file3.txt"), "3", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "l4", "file4.txt"), "4", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "l4", "l5", "file5.txt"), "5", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 5)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 5, result.TotalFiles)
	assert.Equal(t, 5, result.MovedCount)
	assert.Equal(t, 0, result.DuplicatesCount)

	// All files should be in root.
	for i := 1; i <= 5; i++ {
		name := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		_, statErr := os.Stat(name)
		require.NoError(t, statErr, "file%d.txt should exist in root", i)
	}

	// All subdirs should be removed.
	_, err = os.Stat(filepath.Join(tmpDir, "l1"))
	assert.True(t, os.IsNotExist(err))
}

func TestFlattener_Root(t *testing.T) {
	tmpDir := t.TempDir()

	f, err := NewWithWorkers(tmpDir, false, 2)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, f.Root())
}

func TestNew_InvalidRoot(t *testing.T) {
	_, err := NewWithWorkers("/nonexistent/path/12345", false, 2)
	assert.Error(t, err)
}

func TestFlattener_FlattenFiles_DuplicatePreservedWhenKeptFileDisappears(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Create two identical files in subdirectories.
	keptFile := filepath.Join(tmpDir, "dir1", "file.txt")
	dupeFile := filepath.Join(tmpDir, "dir2", "file.txt")
	createTestFile(t, keptFile, "content", modTime)
	createTestFile(t, dupeFile, "content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	f, err := New(tmpDir, false)
	require.NoError(t, err)

	// Pre-compute hashes (same as FlattenFiles does internally).
	fileHashes, _ := f.computeHashes(files, nil)
	seenHash := make(map[string]string)
	nameCount := make(map[string]int)

	// Process the first file (will be moved to root).
	op1 := f.processFile(&files[0], fileHashes[files[0].Path], seenHash, nameCount)
	require.NoError(t, op1.Error)
	require.False(t, op1.Duplicate)

	// Now delete the kept file to simulate it disappearing.
	require.NoError(t, os.Remove(op1.NewPath))

	// Process the second file — it has the same hash so it's a duplicate.
	// The kept file no longer exists, so deletion should be refused.
	op2 := f.processFile(&files[1], fileHashes[files[1].Path], seenHash, nameCount)
	require.Error(t, op2.Error, "should refuse to delete duplicate when kept file is missing")
	assert.True(t, op2.Duplicate)
	assert.Contains(t, op2.Error.Error(), "kept file missing")

	// The duplicate should still exist on disk.
	assert.FileExists(t, dupeFile)
}

func TestFlattener_FlattenFiles_UnsafeSymlinkFailsBeforeMutations(t *testing.T) {
	tmpDir := t.TempDir()

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0o600))

	safeFile := filepath.Join(tmpDir, "nested", "safe.txt")
	createTestFile(t, safeFile, "safe-content", time.Now().UTC())

	linkPath := filepath.Join(tmpDir, "nested", "escape_link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	files := collectFiles(t, tmpDir)

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	result := f.FlattenFiles(files)

	assert.Equal(t, 0, result.MovedCount)
	assert.Equal(t, 1, result.ErrorCount)
	require.Len(t, result.Operations, 1)
	require.ErrorIs(t, result.Operations[0].Error, safepath.ErrSymlinkEscape)

	assert.FileExists(t, safeFile)
	assert.NoFileExists(t, filepath.Join(tmpDir, "safe.txt"))
}
