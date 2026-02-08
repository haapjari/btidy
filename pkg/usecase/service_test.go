package usecase

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
	"btidy/pkg/filelock"
	"btidy/pkg/manifest"
)

type zipFixtureEntry struct {
	name    string
	content []byte
}

func writeZipArchive(t *testing.T, archivePath string, entries []zipFixtureEntry) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(archivePath), 0o755))

	file, err := os.Create(archivePath)
	require.NoError(t, err)

	writer := zip.NewWriter(file)
	for _, entry := range entries {
		entryWriter, err := writer.Create(entry.name)
		require.NoError(t, err)

		_, err = entryWriter.Write(entry.content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
}

func zipBytes(t *testing.T, entries []zipFixtureEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		entryWriter, err := writer.Create(entry.name)
		require.NoError(t, err)

		_, err = entryWriter.Write(entry.content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	return buffer.Bytes()
}

func TestService_RunRename_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	progressCalls := 0
	lastStage := ""
	s := New(Options{})
	execution, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		OnProgress: func(stage string, _, _ int) {
			progressCalls++
			lastStage = stage
		},
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 1, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.RenamedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)
	assert.GreaterOrEqual(t, progressCalls, 1)
	assert.NotEmpty(t, lastStage)

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

	progressCalls := 0
	lastStage := ""
	s := New(Options{})
	execution, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   3,
		OnProgress: func(stage string, _, _ int) {
			progressCalls++
			lastStage = stage
		},
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 1, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.MovedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)
	assert.GreaterOrEqual(t, progressCalls, 1)
	assert.NotEmpty(t, lastStage)

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

	progressCalls := 0
	lastStage := ""
	s := New(Options{})
	execution, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   3,
		OnProgress: func(stage string, _, _ int) {
			progressCalls++
			lastStage = stage
		},
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 2, execution.FileCount)
	assert.Equal(t, 2, execution.Result.TotalFiles)
	assert.Equal(t, 1, execution.Result.DuplicatesFound)
	assert.Equal(t, 1, execution.Result.DeletedCount)
	assert.Equal(t, 0, execution.Result.ErrorCount)
	assert.GreaterOrEqual(t, progressCalls, 1)
	assert.NotEmpty(t, lastStage)

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
		OnProgress: func(_ string, _, _ int) {
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

func TestService_RunManifest_RelativeOutputResolvesInsideTarget(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testutil.CreateFileWithModTime(
		t,
		filepath.Join(tmpDir, "keep.txt"),
		"keep",
		time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC),
	)

	s := New(Options{})
	execution, err := s.RunManifest(ManifestRequest{
		TargetDir:  tmpDir,
		OutputPath: "manifest.json",
		Workers:    1,
	})
	require.NoError(t, err)

	expectedOutputPath := filepath.Join(tmpDir, "manifest.json")
	assert.Equal(t, expectedOutputPath, execution.OutputPath)
	_, err = os.Stat(expectedOutputPath)
	require.NoError(t, err)
}

func TestService_RunManifest_OutputOutsideTargetRejected(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testutil.CreateFileWithModTime(
		t,
		filepath.Join(tmpDir, "keep.txt"),
		"keep",
		time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC),
	)

	outsideDir := t.TempDir()
	outsideOutputPath := filepath.Join(outsideDir, "manifest.json")

	s := New(Options{})
	_, err := s.RunManifest(ManifestRequest{
		TargetDir:  tmpDir,
		OutputPath: outsideOutputPath,
		Workers:    1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest output path must stay within target directory")

	_, statErr := os.Stat(outsideOutputPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestService_RunUnzip_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "photos.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "nested/photo.jpg", content: []byte("photo-bytes")},
	})

	progressCalls := 0
	lastStage := ""
	s := New(Options{})
	execution, err := s.RunUnzip(UnzipRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		OnProgress: func(stage string, _, _ int) {
			progressCalls++
			lastStage = stage
		},
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 1, execution.Result.ArchivesFound)
	assert.Equal(t, 1, execution.Result.ArchivesProcessed)
	assert.Equal(t, 1, execution.Result.ExtractedArchives)
	assert.Equal(t, 1, execution.Result.DeletedArchives)
	assert.Equal(t, 1, execution.Result.ExtractedFiles)
	assert.Equal(t, 0, execution.Result.ExtractedDirs)
	assert.Equal(t, 0, execution.Result.ErrorCount)
	assert.GreaterOrEqual(t, progressCalls, 1)
	assert.NotEmpty(t, lastStage)

	_, err = os.Stat(archivePath)
	require.NoError(t, err, "dry-run must not remove archives")

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "photo.jpg"))
	assert.True(t, os.IsNotExist(err), "dry-run must not extract files")
}

func TestService_RunUnzip_RecursiveNestedArchives(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	innerArchive := zipBytes(t, []zipFixtureEntry{
		{name: "inner/final.txt", content: []byte("payload")},
	})

	outerArchivePath := filepath.Join(tmpDir, "outer.zip")
	writeZipArchive(t, outerArchivePath, []zipFixtureEntry{
		{name: "nested/inner.zip", content: innerArchive},
		{name: "outer.txt", content: []byte("outer")},
	})

	s := New(Options{})
	execution, err := s.RunUnzip(UnzipRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, tmpDir, execution.RootDir)
	assert.Equal(t, 1, execution.FileCount)
	assert.Equal(t, 2, execution.Result.ArchivesFound)
	assert.Equal(t, 2, execution.Result.ArchivesProcessed)
	assert.Equal(t, 2, execution.Result.ExtractedArchives)
	assert.Equal(t, 2, execution.Result.DeletedArchives)
	assert.Equal(t, 3, execution.Result.ExtractedFiles)
	assert.Equal(t, 0, execution.Result.ExtractedDirs)
	assert.Equal(t, 0, execution.Result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "outer.zip"))
	assert.True(t, os.IsNotExist(err), "outer archive should be removed")

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "inner.zip"))
	assert.True(t, os.IsNotExist(err), "nested archive should be removed")

	_, err = os.Stat(filepath.Join(tmpDir, "outer.txt"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "inner", "final.txt"))
	require.NoError(t, err)
}

func TestService_RunDuplicate_GeneratesSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{})
	execution, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.SnapshotPath, "snapshot path should be set for non-dry-run")
	_, err = os.Stat(execution.SnapshotPath)
	require.NoError(t, err, "snapshot file should exist")

	// Verify the snapshot is a valid manifest with 2 entries (the original files).
	m, err := manifest.Load(execution.SnapshotPath)
	require.NoError(t, err)
	assert.Equal(t, 2, m.FileCount())
}

func TestService_RunDuplicate_DryRunSkipsSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{})
	execution, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   2,
	})
	require.NoError(t, err)

	assert.Empty(t, execution.SnapshotPath, "snapshot path should be empty for dry-run")

	// Verify .btidy/manifests/ does not exist.
	_, err = os.Stat(filepath.Join(tmpDir, ".btidy", "manifests"))
	assert.True(t, os.IsNotExist(err), "no manifests directory should be created in dry-run")
}

func TestService_RunFlatten_NoSnapshotOption(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "nested", "file.txt"), "content", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	assert.Empty(t, execution.SnapshotPath, "snapshot path should be empty when NoSnapshot is true")

	// Verify .btidy/manifests/ does not exist.
	_, err = os.Stat(filepath.Join(tmpDir, ".btidy", "manifests"))
	assert.True(t, os.IsNotExist(err), "no manifests directory should be created when NoSnapshot is true")
}

func TestService_RunRename_GeneratesSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{})
	execution, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.SnapshotPath, "snapshot path should be set")
	_, err = os.Stat(execution.SnapshotPath)
	require.NoError(t, err, "snapshot file should exist")

	m, err := manifest.Load(execution.SnapshotPath)
	require.NoError(t, err)
	assert.Equal(t, 1, m.FileCount())
}

func TestService_RunDuplicate_LockReleasedAfterWorkflow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// First run should succeed.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   2,
	})
	require.NoError(t, err)

	// Lock should be released — second run on same directory should succeed.
	_, err = s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    true,
		Workers:   2,
	})
	require.NoError(t, err, "second run should succeed after lock is released")

	// Lock file should be cleaned up.
	_, err = os.Stat(filepath.Join(tmpDir, ".btidy", "lock"))
	assert.True(t, os.IsNotExist(err), "lock file should be removed after workflow")
}

func TestService_RunRename_LockPreventsConflict(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "file.txt"), "content", modTime)

	// Manually acquire the lock to simulate a concurrent btidy process.
	metaDir := filepath.Join(tmpDir, ".btidy")
	require.NoError(t, os.MkdirAll(metaDir, 0o755))
	lockPath := filepath.Join(metaDir, "lock")

	lock, err := filelock.Acquire(lockPath)
	require.NoError(t, err, "manual lock acquisition should succeed")

	t.Cleanup(func() {
		_ = lock.Close()
	})

	s := New(Options{NoSnapshot: true})
	_, err = s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    true,
	})
	require.Error(t, err, "should fail when lock is held")
	assert.Contains(t, err.Error(), "another btidy process")
}
