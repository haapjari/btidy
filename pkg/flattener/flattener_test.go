package flattener

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"file-organizer/pkg/collector"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "flattener-test-*")
	require.NoError(t, err)
	return tmpDir
}

func createTestFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)
	err = os.Chtimes(path, modTime, modTime)
	require.NoError(t, err)
}

func TestFlattener_FlattenFiles_Basic(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "subdir", "file.txt"), "content", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)
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
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "subdir", "file.txt"), "content", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)

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

func TestFlattener_FlattenFiles_Duplicates(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Same name, same size, same mtime = duplicate.
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "content", modTime)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "content", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)
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

func TestFlattener_FlattenFiles_NameConflict(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime1 := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	modTime2 := time.Date(2018, 7, 20, 12, 0, 0, 0, time.UTC)
	// Same name, different mtime = NOT duplicate, needs suffix.
	createTestFile(t, filepath.Join(tmpDir, "dir1", "file.txt"), "content1", modTime1)
	createTestFile(t, filepath.Join(tmpDir, "dir2", "file.txt"), "content2", modTime2)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)
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
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "rootfile.txt"), "content", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)
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
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, filepath.Join(tmpDir, "a", "b", "c", "file.txt"), "content", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)

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
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

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

	f, err = New(tmpDir, false)
	require.NoError(t, err)
	assert.False(t, f.DryRun())
}

func TestFlattener_FlattenFiles_DeepNesting(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer os.RemoveAll(tmpDir)

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Create files at various depths.
	createTestFile(t, filepath.Join(tmpDir, "l1", "file1.txt"), "1", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "file2.txt"), "2", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "file3.txt"), "3", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "l4", "file4.txt"), "4", modTime)
	createTestFile(t, filepath.Join(tmpDir, "l1", "l2", "l3", "l4", "l5", "file5.txt"), "5", modTime)

	c := collector.New(collector.Options{})
	files, err := c.Collect(tmpDir)
	require.NoError(t, err)
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

	f, err := New(tmpDir, false)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, f.Root())
}

func TestNew_InvalidRoot(t *testing.T) {
	_, err := New("/nonexistent/path/12345", false)
	assert.Error(t, err)
}
