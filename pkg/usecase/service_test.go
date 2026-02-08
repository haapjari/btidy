package usecase

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
	"btidy/pkg/filelock"
	"btidy/pkg/journal"
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

func TestService_RunDuplicate_WritesJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.JournalPath, "journal path should be set")
	_, err = os.Stat(execution.JournalPath)
	require.NoError(t, err, "journal file should exist")

	reader := journal.NewReader(execution.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)

	require.Len(t, entries, 1, "should have one trash entry for the duplicate")
	assert.Equal(t, "trash", entries[0].Type)
	assert.True(t, entries[0].Success)
	assert.NotEmpty(t, entries[0].Hash, "trash entry should include content hash")
	assert.NotEmpty(t, entries[0].Source, "source path should not be empty")
	assert.NotEmpty(t, entries[0].Dest, "dest (trash path) should not be empty")

	// Verify paths are relative (not absolute).
	assert.False(t, filepath.IsAbs(entries[0].Source), "source should be a relative path")
	assert.False(t, filepath.IsAbs(entries[0].Dest), "dest should be a relative path")
}

func TestService_RunFlatten_WritesJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "sub", "file.txt"), "content", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.JournalPath, "journal path should be set")

	reader := journal.NewReader(execution.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)

	require.Len(t, entries, 1, "should have one rename entry for the moved file")
	assert.Equal(t, "rename", entries[0].Type)
	assert.True(t, entries[0].Success)
	assert.Equal(t, filepath.Join("sub", "file.txt"), entries[0].Source)
	assert.Equal(t, "file.txt", entries[0].Dest)
}

func TestService_RunRename_DryRunSkipsJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    true,
	})
	require.NoError(t, err)

	assert.Empty(t, execution.JournalPath, "journal path should be empty for dry-run")

	// Verify no journal directory was created.
	_, err = os.Stat(filepath.Join(tmpDir, ".btidy", "journal"))
	assert.True(t, os.IsNotExist(err), "no journal directory should be created in dry-run")
}

func TestService_RunRename_WritesJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.JournalPath, "journal path should be set")

	reader := journal.NewReader(execution.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)

	require.Len(t, entries, 1, "should have one rename entry")
	assert.Equal(t, "rename", entries[0].Type)
	assert.True(t, entries[0].Success)
	assert.Equal(t, "My Document.pdf", entries[0].Source)
	assert.Equal(t, "2018-06-15_my_document.pdf", entries[0].Dest)
}

func TestService_RunOrganize_WritesJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "photo.jpg"), "image-data", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "notes.txt"), "text-data", modTime)

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunOrganize(OrganizeRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.JournalPath, "journal path should be set")

	reader := journal.NewReader(execution.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)

	require.Len(t, entries, 2, "should have two rename entries")

	// Collect entries by source for stable assertions.
	entryBySource := make(map[string]journal.Entry, len(entries))
	for _, e := range entries {
		entryBySource[e.Source] = e
	}

	jpgEntry, ok := entryBySource["photo.jpg"]
	require.True(t, ok, "should have entry for photo.jpg")
	assert.Equal(t, "rename", jpgEntry.Type)
	assert.True(t, jpgEntry.Success)
	assert.Equal(t, filepath.Join("jpg", "photo.jpg"), jpgEntry.Dest)

	txtEntry, ok := entryBySource["notes.txt"]
	require.True(t, ok, "should have entry for notes.txt")
	assert.Equal(t, "rename", txtEntry.Type)
	assert.True(t, txtEntry.Success)
	assert.Equal(t, filepath.Join("txt", "notes.txt"), txtEntry.Dest)
}

func TestService_RunUnzip_WritesJournal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "docs.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "readme.txt", content: []byte("hello")},
	})

	s := New(Options{NoSnapshot: true})
	execution, err := s.RunUnzip(UnzipRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, execution.JournalPath, "journal path should be set")

	reader := journal.NewReader(execution.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)

	// Should have an extract entry and a trash entry (deleted archive).
	require.Len(t, entries, 2, "should have extract + trash entries")

	// Collect entries by type for stable assertions.
	entryByType := make(map[string]journal.Entry, len(entries))
	for _, e := range entries {
		entryByType[e.Type] = e
	}

	extractEntry, ok := entryByType["extract"]
	require.True(t, ok, "should have extract entry")
	assert.True(t, extractEntry.Success)
	assert.Equal(t, "docs.zip", extractEntry.Source)

	trashEntry, ok := entryByType["trash"]
	require.True(t, ok, "should have trash entry for deleted archive")
	assert.True(t, trashEntry.Success)
	assert.Equal(t, "docs.zip", trashEntry.Source)
	assert.NotEmpty(t, trashEntry.Dest)
}

func TestService_RunUndo_ReversesDuplicate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one of the files.
	dupExec, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, dupExec.Result.DeletedCount)

	// Verify one file was trashed (only one should remain).
	remaining := 0
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, statErr := os.Stat(filepath.Join(tmpDir, name)); statErr == nil {
			remaining++
		}
	}
	assert.Equal(t, 1, remaining, "only one file should remain after dedup")

	// Run undo.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, undoExec.RestoredCount, "should restore one trashed file")
	assert.Equal(t, 0, undoExec.ErrorCount)
	assert.Equal(t, 0, undoExec.SkippedCount)

	// Both files should exist again.
	_, err = os.Stat(filepath.Join(tmpDir, "a.txt"))
	require.NoError(t, err, "a.txt should be restored")
	_, err = os.Stat(filepath.Join(tmpDir, "b.txt"))
	require.NoError(t, err, "b.txt should be restored")
}

func TestService_RunUndo_ReversesFlatten(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "sub", "deep", "file.txt"), "content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run flatten to move file to root.
	flatExec, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, flatExec.Result.MovedCount)

	// Verify file is at root.
	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	require.NoError(t, err, "file should be at root after flatten")

	// Run undo.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, undoExec.ReversedCount, "should reverse one rename")
	assert.Equal(t, 0, undoExec.ErrorCount)

	// File should be back in original location.
	_, err = os.Stat(filepath.Join(tmpDir, "sub", "deep", "file.txt"))
	require.NoError(t, err, "file should be restored to original path")

	// File should not be at root.
	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	assert.True(t, os.IsNotExist(err), "file should not remain at root")
}

func TestService_RunUndo_ReversesRename(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run rename.
	renameExec, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, renameExec.Result.RenamedCount)

	// Verify renamed file exists.
	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_document.pdf"))
	require.NoError(t, err)

	// Run undo.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, undoExec.ReversedCount, "should reverse one rename")
	assert.Equal(t, 0, undoExec.ErrorCount)

	// Original name should be restored.
	_, err = os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	require.NoError(t, err, "original filename should be restored")

	// Renamed file should not exist.
	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_document.pdf"))
	assert.True(t, os.IsNotExist(err), "renamed file should not exist after undo")
}

func TestService_RunUndo_DryRunNoChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	// Count files before undo dry-run.
	filesBefore, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)

	// Run undo in dry-run mode.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    true,
	})
	require.NoError(t, err)

	assert.True(t, undoExec.DryRun)
	assert.Equal(t, 1, undoExec.RestoredCount, "dry-run should report what would be restored")
	assert.Equal(t, 0, undoExec.ErrorCount)

	// Verify no actual changes were made.
	filesAfter, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Len(t, filesAfter, len(filesBefore), "dry-run should not change file count")

	// Verify journal was NOT renamed (still active).
	journalDir := filepath.Join(tmpDir, ".btidy", "journal")
	journalEntries, readErr := os.ReadDir(journalDir)
	require.NoError(t, readErr)

	activeCount := 0
	for _, e := range journalEntries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			activeCount++
		}
	}
	assert.Equal(t, 1, activeCount, "journal should still be active after dry-run")
}

func TestService_RunUndo_SpecificRunID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run rename.
	renameExec, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, renameExec.JournalPath)

	// Extract run ID from journal path.
	runID := extractRunID(renameExec.JournalPath)

	// Run undo with specific run ID.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		RunID:     runID,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, runID, undoExec.RunID)
	assert.Equal(t, 1, undoExec.ReversedCount)
	assert.Equal(t, 0, undoExec.ErrorCount)

	// Original name should be restored.
	_, err = os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	require.NoError(t, err)
}

func TestService_RunUndo_NoJournalError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "file.txt"), "content",
		time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC))

	s := New(Options{NoSnapshot: true})

	_, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no journals found")
}

func TestService_RunPurge_PurgesSpecificRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	dupExec, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, dupExec.Result.DeletedCount)

	// Find the trash run ID from the journal.
	reader := journal.NewReader(dupExec.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// The trash dest is like ".btidy/trash/<run-id>/b.txt" — extract run ID.
	trashDest := entries[0].Dest
	trashParts := strings.SplitN(trashDest, string(filepath.Separator), 4)
	require.GreaterOrEqual(t, len(trashParts), 3, "expected .btidy/trash/<run-id>/...")
	trashRunID := trashParts[2]

	// Verify trash directory exists.
	trashDir := filepath.Join(tmpDir, ".btidy", "trash", trashRunID)
	_, err = os.Stat(trashDir)
	require.NoError(t, err, "trash directory should exist before purge")

	// Purge the specific run.
	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		RunID:     trashRunID,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, purgeExec.PurgedCount)
	assert.Equal(t, 0, purgeExec.ErrorCount)
	assert.Positive(t, purgeExec.PurgedSize)

	// Verify trash directory is gone.
	_, err = os.Stat(trashDir)
	assert.True(t, os.IsNotExist(err), "trash directory should be removed after purge")
}

func TestService_RunPurge_DryRunDoesNotDelete(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	// Purge all in dry-run mode.
	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		All:       true,
		DryRun:    true,
	})
	require.NoError(t, err)

	assert.True(t, purgeExec.DryRun)
	assert.Equal(t, 1, purgeExec.PurgedCount, "dry-run should report what would be purged")
	assert.Positive(t, purgeExec.PurgedSize)

	// Verify trash directory still exists.
	trashRoot := filepath.Join(tmpDir, ".btidy", "trash")
	dirEntries, readErr := os.ReadDir(trashRoot)
	require.NoError(t, readErr)
	assert.Len(t, dirEntries, 1, "trash directory should still exist after dry-run")
}

func TestService_RunPurge_PurgesAll(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	// Purge all.
	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		All:       true,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, purgeExec.PurgedCount)
	assert.Equal(t, 0, purgeExec.ErrorCount)

	// Verify trash directory is empty.
	trashRoot := filepath.Join(tmpDir, ".btidy", "trash")
	dirEntries, readErr := os.ReadDir(trashRoot)
	require.NoError(t, readErr)
	assert.Empty(t, dirEntries, "all trash runs should be removed")
}

func TestService_RunPurge_NoTrashReturnsEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "file.txt"), "content",
		time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC))

	s := New(Options{NoSnapshot: true})

	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		All:       true,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Empty(t, purgeExec.Runs)
	assert.Empty(t, purgeExec.Operations)
	assert.Equal(t, 0, purgeExec.PurgedCount)
}

func TestService_RunPurge_OlderThanFilter(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	// Purge with OlderThan = 1000 hours (the trash is seconds old, so it won't match).
	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		OlderThan: 1000 * time.Hour,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, purgeExec.PurgedCount, "nothing should match older-than filter")
	assert.Len(t, purgeExec.Runs, 1, "should still list existing runs")

	// Purge with OlderThan = 0 seconds (everything is older than 0s effectively, but
	// we need Age > OlderThan, and OlderThan = 1ns should match anything).
	purgeExec2, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		OlderThan: time.Nanosecond,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, purgeExec2.PurgedCount, "trash older than 1ns should be purged")
}

func TestService_RunPurge_NoFilterReturnsNothing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	_, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)

	// Purge with no filter — should match nothing.
	purgeExec, err := s.RunPurge(PurgeRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, purgeExec.PurgedCount)
	assert.Len(t, purgeExec.Runs, 1, "should still list existing runs")
	assert.Empty(t, purgeExec.Operations, "no operations with no filter")
}

func TestService_RunUndo_MarksJournalRolledBack(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run rename to create a journal.
	renameExec, err := s.RunRename(RenameRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, renameExec.JournalPath)

	// Verify journal exists before undo.
	_, err = os.Stat(renameExec.JournalPath)
	require.NoError(t, err)

	// Run undo.
	_, err = s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	// Original journal should no longer exist.
	_, err = os.Stat(renameExec.JournalPath)
	assert.True(t, os.IsNotExist(err), "original journal should be renamed")

	// Rolled-back journal should exist.
	rolledBackPath := renameExec.JournalPath[:len(renameExec.JournalPath)-len(".jsonl")] + ".rolled-back.jsonl"
	_, err = os.Stat(rolledBackPath)
	require.NoError(t, err, "rolled-back journal should exist")

	// Running undo again should fail (no active journals).
	_, err = s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active journals")
}

func TestService_RunUndo_SkipsWhenHashChanged(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "a.txt"), "same-content", modTime)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "b.txt"), "same-content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run duplicate to trash one file.
	dupExec, err := s.RunDuplicate(DuplicateRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, dupExec.Result.DeletedCount)

	// Find and modify the trashed file to simulate content change.
	reader := journal.NewReader(dupExec.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)
	require.Len(t, entries, 1)

	trashedAbs := filepath.Join(tmpDir, entries[0].Dest)
	require.NoError(t, os.WriteFile(trashedAbs, []byte("modified-content"), 0o644))

	// Run undo — should skip the entry due to hash mismatch.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, undoExec.RestoredCount, "should not restore when hash changed")
	assert.Equal(t, 1, undoExec.SkippedCount, "should skip due to hash mismatch")
	require.Len(t, undoExec.Operations, 1)
	assert.Equal(t, "skip", undoExec.Operations[0].Action)
	assert.Contains(t, undoExec.Operations[0].SkipReason, "hash mismatch")
}

func TestService_RunUndo_ProceedsWhenNoHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "sub", "file.txt"), "content", modTime)

	s := New(Options{NoSnapshot: true})

	// Run flatten — rename entries don't have hashes.
	flatExec, err := s.RunFlatten(FlattenRequest{
		TargetDir: tmpDir,
		DryRun:    false,
		Workers:   2,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, flatExec.JournalPath)

	// Verify journal entries have no hash.
	reader := journal.NewReader(flatExec.JournalPath)
	entries, err := reader.Entries()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	hasRenameWithoutHash := false
	for _, e := range entries {
		if e.Type == "rename" && e.Hash == "" {
			hasRenameWithoutHash = true
		}
	}
	assert.True(t, hasRenameWithoutHash, "flatten should produce rename entries without hashes")

	// Run undo — should proceed normally since no hash to verify.
	undoExec, err := s.RunUndo(UndoRequest{
		TargetDir: tmpDir,
		DryRun:    false,
	})
	require.NoError(t, err)

	assert.Positive(t, undoExec.ReversedCount, "should reverse-rename when no hash to verify")
	assert.Equal(t, 0, undoExec.SkippedCount)
}
