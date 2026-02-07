package usecase

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
)

func TestService_RunRename_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{})
	execution, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    true,
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 1, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.RenamedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	require.NoError(t, err, "dry-run must not rename files")

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_document.pdf"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create renamed files")
}

func TestService_RunFlatten_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "nested", "file.txt"), "content", modTime)

	s := New(Options{})
	execution, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   3,
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 1, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.MovedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "file.txt"))
	require.NoError(t, err, "dry-run must not move files")

	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	assert.True(t, os.IsNotExist(err), "dry-run must not place files in root")
}

func TestService_RunDuplicate_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{})
	execution, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   3,
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 2, execution.FileCount)
	assert.Equal(t, 2, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.DuplicatesFound)
	assert.Equal(t, 1, execution.Result.DeletedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "a.txt"))
	require.NoError(t, err, "dry-run must not delete files")
	_, err = os.Stat(filepath.Join(tmpDir, "b.txt"))
	require.NoError(t, err, "dry-run must not delete files")
}

func TestService_RunManifest_WithSkipFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "keep.txt"), "keep", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, ".DS_Store"), "skip", modTime)

	outputPath := filepath.Join(tmpDir, "manifest.json")

	progressCalls := 0
	s := New(Options{SkipFiles: []string{".DS_Store"}})
	execution, err := s.RunManifest(ManifestRequest{
		TargetDir:  tmpDir,
		OutputPath: outputPath,
		Workers:    2,
		OnProgress: func(_, _ int, _ string) {
			progressCalls++
		},
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, outputPath, execution.OutputPath)
	require.NotNil(t, execution.Manifest)
	assert.Equal(t, 1, execution.Manifest.FileCount())
	assert.Equal(t, 1, execution.Manifest.UniqueFileCount())
	assert.GreaterOrEqual(t, progressCalls, 1)

	_, err = os.Stat(outputPath)
	require.NoError(t, err, "manifest file must be written")
}
