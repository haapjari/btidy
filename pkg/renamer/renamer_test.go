package renamer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
	"btidy/pkg/collector"
)

// createTestFile creates a file with specific modification time.
func createTestFile(t *testing.T, dir, name string, modTime time.Time) {
	t.Helper()

	createTestFileWithContent(t, dir, name, "test content", modTime)
}

func createTestFileWithContent(t *testing.T, dir, name, content string, modTime time.Time) {
	t.Helper()
	testutil.CreateFileWithModTime(t, filepath.Join(dir, name), content, modTime)
}

func collectFiles(t *testing.T, root string) []collector.FileInfo {
	t.Helper()

	c := collector.New(collector.Options{})
	files, err := c.Collect(root)
	require.NoError(t, err)

	return files
}

func TestRenamer_RenameFiles_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, tmpDir, "My Document.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, true) // dry run
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 1, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)
	assert.Equal(t, 0, result.ErrorCount)

	// Verify file was NOT actually renamed (dry run)
	_, err = os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	require.NoError(t, err, "original file should still exist in dry run")

	// Verify operation details
	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.Equal(t, "My Document.pdf", op.OriginalName)
	assert.Equal(t, "2018-06-15_my_document.pdf", op.NewName)
	assert.False(t, op.Skipped)
	assert.NoError(t, op.Error)
}

func TestRenamer_RenameFiles_ActualRename(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, tmpDir, "My Document.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, false) // actual rename
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 1, result.RenamedCount)
	assert.Equal(t, 0, result.ErrorCount)

	// Verify file WAS renamed
	_, err = os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	assert.True(t, os.IsNotExist(err), "original file should not exist after rename")

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_document.pdf"))
	assert.NoError(t, err, "renamed file should exist")
}

func TestRenamer_RenameFiles_AlreadyNamedWithDatePrefix(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	// Create a file that already has a date prefix - it will get another one
	// because GenerateTimestampedName always adds the date prefix
	createTestFile(t, tmpDir, "2018-06-15_document.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.RenamedCount)
	assert.Equal(t, 1, result.SkippedCount)

	require.Len(t, result.Operations, 1)
	assert.Equal(t, "2018-06-15_document.pdf", result.Operations[0].NewName)
	assert.True(t, result.Operations[0].Skipped)
	assert.Equal(t, "name unchanged", result.Operations[0].SkipReason)
}

func TestRenamer_RenameFiles_TBDPrefixSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2019, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, tmpDir, "2019-TBD-TBD_document.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.RenamedCount)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, 0, result.ErrorCount)

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.True(t, op.Skipped)
	assert.Equal(t, "already has TBD prefix", op.SkipReason)

	_, err = os.Stat(filepath.Join(tmpDir, "2019-TBD-TBD_document.pdf"))
	require.NoError(t, err, "file with TBD prefix should remain")
}

func TestRenamer_RenameFiles_DoubleDatePrefixCollapsed(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)
	createTestFile(t, tmpDir, "2025-01-01_2025-01-01_report.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, true)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 1, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.Equal(t, "2025-01-01_report.pdf", op.NewName)
	assert.False(t, op.Skipped)
	assert.NoError(t, op.Error)
}

func TestRenamer_RenameFiles_HandleConflicts(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two files with same name but different content
	// They will have the same sanitized name
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFileWithContent(t, tmpDir, "Document.pdf", "content-a", modTime)
	createTestFileWithContent(t, tmpDir, "document.pdf", "content-bb", modTime) // different case, size

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	r, err := New(tmpDir, true) // dry run first
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 2, result.TotalFiles)

	// One should be normal, one should have suffix
	names := make(map[string]bool)
	for _, op := range result.Operations {
		names[op.NewName] = true
	}

	assert.True(t, names["2018-06-15_document.pdf"], "should have base name")
	assert.True(t, names["2018-06-15_document_1.pdf"], "should have suffixed name")
}

func TestRenamer_RenameFiles_SameSizeDifferentContent_BatchKeepsBoth(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFileWithContent(t, tmpDir, "Photo.jpg", "alpha-123", modTime)
	createTestFileWithContent(t, tmpDir, "photo.jpg", "omega-12", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 2, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)
	assert.Equal(t, 0, result.DeletedCount)
	assert.Equal(t, 0, result.ErrorCount)

	basePath := filepath.Join(tmpDir, "2018-06-15_photo.jpg")
	suffixPath := filepath.Join(tmpDir, "2018-06-15_photo_1.jpg")

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

func TestRenamer_RenameFiles_RemovesDuplicateInBatch(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFileWithContent(t, tmpDir, "Report.pdf", "same-content", modTime)
	createTestFileWithContent(t, tmpDir, "report.pdf", "same-content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 1, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)
	assert.Equal(t, 1, result.DeletedCount)
	assert.Equal(t, 0, result.ErrorCount)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "2018-06-15_report.pdf", entries[0].Name())
}

func TestRenamer_RenameFiles_RemovesDuplicateTarget(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFile(t, tmpDir, "My Doc.pdf", modTime)
	createTestFile(t, tmpDir, "2018-06-15_my_doc.pdf", modTime)

	originalPath := filepath.Join(tmpDir, "My Doc.pdf")
	info, err := os.Stat(originalPath)
	require.NoError(t, err)

	files := []collector.FileInfo{
		{
			Path:    originalPath,
			Dir:     tmpDir,
			Name:    "My Doc.pdf",
			Size:    info.Size(),
			ModTime: info.ModTime(),
		},
	}

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)
	assert.Equal(t, 1, result.DeletedCount)
	assert.Equal(t, 0, result.ErrorCount)

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.True(t, op.Skipped)
	assert.Equal(t, "duplicate file already exists", op.SkipReason)
	assert.True(t, op.Deleted)

	_, err = os.Stat(originalPath)
	assert.True(t, os.IsNotExist(err), "duplicate source should be removed")

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_doc.pdf"))
	require.NoError(t, err, "existing target should remain")
}

func TestRenamer_RenameFiles_SameSizeDifferentContent_TargetCollisionKeepsSource(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFileWithContent(t, tmpDir, "My Doc.pdf", "ABCD", modTime)
	createTestFileWithContent(t, tmpDir, "2018-06-15_my_doc.pdf", "WXYZ", modTime)

	originalPath := filepath.Join(tmpDir, "My Doc.pdf")
	info, err := os.Stat(originalPath)
	require.NoError(t, err)

	files := []collector.FileInfo{
		{
			Path:    originalPath,
			Dir:     tmpDir,
			Name:    "My Doc.pdf",
			Size:    info.Size(),
			ModTime: info.ModTime(),
		},
	}

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.RenamedCount)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, 0, result.DeletedCount)
	assert.Equal(t, 0, result.ErrorCount)

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.True(t, op.Skipped)
	assert.Equal(t, "target file already exists", op.SkipReason)
	assert.False(t, op.Deleted)

	_, err = os.Stat(originalPath)
	require.NoError(t, err, "source should remain when target has different bytes")

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_doc.pdf"))
	require.NoError(t, err, "existing target should remain")
}

func TestRenamer_RenameFiles_DryRun_NoFilesystemMutations(t *testing.T) {
	tmpDir := t.TempDir()

	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	createTestFileWithContent(t, tmpDir, "Report.pdf", "same-content", modTime)
	createTestFileWithContent(t, tmpDir, "report.pdf", "same-content", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 2)

	r, err := New(tmpDir, true)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 2, result.TotalFiles)
	assert.Equal(t, 1, result.RenamedCount)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, 0, result.DeletedCount)
	assert.Equal(t, 0, result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "Report.pdf"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "report.pdf"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_report.pdf"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create renamed files")
}

func TestRenamer_RenameFiles_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	modTime1 := time.Date(2018, 1, 15, 12, 0, 0, 0, time.UTC)
	modTime2 := time.Date(2018, 6, 20, 12, 0, 0, 0, time.UTC)
	modTime3 := time.Date(2018, 12, 25, 12, 0, 0, 0, time.UTC)

	createTestFile(t, tmpDir, "Report (Final).docx", modTime1)
	createTestFile(t, tmpDir, "Työpöytä.txt", modTime2)
	createTestFile(t, tmpDir, "KeePass.kdbx", modTime3)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 3)

	r, err := New(tmpDir, false) // actual rename
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 3, result.TotalFiles)
	assert.Equal(t, 3, result.RenamedCount)
	assert.Equal(t, 0, result.ErrorCount)

	// Verify all files exist with new names
	expectedFiles := []string{
		"2018-01-15_report_final.docx",
		"2018-06-20_tyopoyta.txt",
		"2018-12-25_keepass.kdbx",
	}

	for _, name := range expectedFiles {
		_, err := os.Stat(filepath.Join(tmpDir, name))
		assert.NoError(t, err, "expected file %s to exist", name)
	}
}

func TestRenamer_RenameFiles_SubdirectoriesInPlace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0o755)
	require.NoError(t, err)

	modTime := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	createTestFile(t, subDir, "Nested File.pdf", modTime)

	files := collectFiles(t, tmpDir)
	require.Len(t, files, 1)

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles(files)

	assert.Equal(t, 1, result.RenamedCount)

	// Verify file was renamed IN PLACE (still in subdir)
	_, err = os.Stat(filepath.Join(subDir, "2018-03-10_nested_file.pdf"))
	require.NoError(t, err, "file should be renamed in place within subdir")

	// Verify it's not in root
	_, err = os.Stat(filepath.Join(tmpDir, "2018-03-10_nested_file.pdf"))
	assert.True(t, os.IsNotExist(err), "file should not be in root dir")
}

func TestRenamer_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := New(tmpDir, true)
	require.NoError(t, err)
	assert.True(t, r.DryRun())

	r, err = New(tmpDir, false)
	require.NoError(t, err)
	assert.False(t, r.DryRun())
}

func TestRenamer_RenameFiles_EmptyList(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	result := r.RenameFiles([]collector.FileInfo{})

	assert.Equal(t, 0, result.TotalFiles)
	assert.Equal(t, 0, result.RenamedCount)
	assert.Equal(t, 0, result.SkippedCount)
	assert.Equal(t, 0, result.ErrorCount)
	assert.Empty(t, result.Operations)
}

func TestRenamer_Root(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := New(tmpDir, false)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, r.Root())
}

func TestNew_InvalidRoot(t *testing.T) {
	_, err := New("/nonexistent/path/12345", false)
	assert.Error(t, err)
}
