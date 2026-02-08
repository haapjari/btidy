package unzipper

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/pkg/collector"
	"btidy/pkg/metadata"
	"btidy/pkg/safepath"
	"btidy/pkg/trash"
)

type zipFixtureEntry struct {
	name    string
	content []byte
	mode    os.FileMode
}

func collectFiles(t *testing.T, rootDir string) []collector.FileInfo {
	t.Helper()

	c := collector.New(collector.Options{})
	files, err := c.Collect(rootDir)
	require.NoError(t, err)

	return files
}

func writeZipArchive(t *testing.T, archivePath string, entries []zipFixtureEntry) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(archivePath), 0o755))

	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)

	writer := zip.NewWriter(archiveFile)
	for _, entry := range entries {
		header := zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}

		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)

		entryWriter, err := writer.CreateHeader(&header)
		require.NoError(t, err)

		_, err = entryWriter.Write(entry.content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, archiveFile.Close())
}

func zipBytes(t *testing.T, entries []zipFixtureEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}

		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)

		entryWriter, err := writer.CreateHeader(&header)
		require.NoError(t, err)

		_, err = entryWriter.Write(entry.content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	return buffer.Bytes()
}

func TestUnzipper_ExtractArchives_NoArchives(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	regularFile := filepath.Join(tmpDir, "plain.txt")
	require.NoError(t, os.WriteFile(regularFile, []byte("content"), 0o600))

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 0, result.ArchivesFound)
	assert.Equal(t, 0, result.ArchivesProcessed)
	assert.Equal(t, 0, result.ExtractedArchives)
	assert.Equal(t, 0, result.DeletedArchives)
	assert.Equal(t, 0, result.ErrorCount)
	assert.Empty(t, result.Operations)
}

func TestUnzipper_ExtractArchives_DryRunNoMutations(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "photos.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "nested/photo.jpg", content: []byte("photo")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, true)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.Equal(t, 1, result.ExtractedArchives)
	assert.Equal(t, 1, result.DeletedArchives)
	assert.Equal(t, 1, result.ExtractedFiles)
	assert.Equal(t, 0, result.ExtractedDirs)
	assert.Equal(t, 0, result.ErrorCount)
	require.Len(t, result.Operations, 1)
	assert.True(t, result.Operations[0].ExtractionComplete)
	assert.True(t, result.Operations[0].DeletedArchive)

	_, err = os.Stat(archivePath)
	require.NoError(t, err, "dry-run must not remove source archive")

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "photo.jpg"))
	assert.True(t, os.IsNotExist(err), "dry-run must not extract files")
}

func TestUnzipper_ExtractArchives_RecursiveNestedArchives(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	innerArchive := zipBytes(t, []zipFixtureEntry{
		{name: "deep/final.txt", content: []byte("done")},
	})

	outerArchivePath := filepath.Join(tmpDir, "outer.zip")
	writeZipArchive(t, outerArchivePath, []zipFixtureEntry{
		{name: "nested/inner.zip", content: innerArchive},
		{name: "outer.txt", content: []byte("outer")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.TotalFiles)
	assert.Equal(t, 2, result.ArchivesFound)
	assert.Equal(t, 2, result.ArchivesProcessed)
	assert.Equal(t, 2, result.ExtractedArchives)
	assert.Equal(t, 2, result.DeletedArchives)
	assert.Equal(t, 3, result.ExtractedFiles)
	assert.Equal(t, 0, result.ExtractedDirs)
	assert.Equal(t, 0, result.ErrorCount)

	_, err = os.Stat(filepath.Join(tmpDir, "outer.zip"))
	assert.True(t, os.IsNotExist(err), "outer archive should be removed")

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "inner.zip"))
	assert.True(t, os.IsNotExist(err), "nested archive should be removed")

	_, err = os.Stat(filepath.Join(tmpDir, "outer.txt"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "deep", "final.txt"))
	require.NoError(t, err)
}

func TestUnzipper_ExtractArchives_ZipSlipBlocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outsidePath := filepath.Join(filepath.Dir(tmpDir), "should-not-exist.txt")

	archivePath := filepath.Join(tmpDir, "bad.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "../should-not-exist.txt", content: []byte("attack")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.Equal(t, 0, result.ExtractedArchives)
	assert.Equal(t, 0, result.DeletedArchives)
	assert.Equal(t, 1, result.ErrorCount)
	require.Len(t, result.Operations, 1)
	require.ErrorIs(t, result.Operations[0].Error, safepath.ErrPathEscape)

	_, err = os.Stat(archivePath)
	require.NoError(t, err, "archive must remain when extraction fails")

	_, err = os.Stat(outsidePath)
	assert.True(t, os.IsNotExist(err), "zip-slip target must not be created")
}

func TestUnzipper_ExtractArchives_SymlinkEntryRejected(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "symlink.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{
			name:    "escape-link",
			content: []byte("/etc/passwd"),
			mode:    os.ModeSymlink | 0o777,
		},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.Equal(t, 0, result.ExtractedArchives)
	assert.Equal(t, 0, result.DeletedArchives)
	assert.Equal(t, 1, result.ErrorCount)
	require.Len(t, result.Operations, 1)
	assert.Contains(t, result.Operations[0].Error.Error(), "symlink entries are not supported")

	_, err = os.Stat(archivePath)
	require.NoError(t, err, "archive must remain when extraction fails")

	_, err = os.Lstat(filepath.Join(tmpDir, "escape-link"))
	assert.True(t, os.IsNotExist(err), "symlink entry must not be written")
}

func TestUnzipper_ExtractArchives_UnsafeSymlinkArchiveBlocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outsideDir := t.TempDir()

	outsideArchive := filepath.Join(outsideDir, "outside.zip")
	writeZipArchive(t, outsideArchive, []zipFixtureEntry{
		{name: "outside.txt", content: []byte("outside")},
	})

	symlinkArchive := filepath.Join(tmpDir, "escape.zip")
	if err := os.Symlink(outsideArchive, symlinkArchive); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.Equal(t, 0, result.ExtractedArchives)
	assert.Equal(t, 0, result.DeletedArchives)
	assert.Equal(t, 1, result.ErrorCount)
	require.Len(t, result.Operations, 1)
	require.ErrorIs(t, result.Operations[0].Error, safepath.ErrSymlinkEscape)

	_, err = os.Lstat(symlinkArchive)
	require.NoError(t, err, "symlink archive must remain untouched")

	_, err = os.Stat(filepath.Join(tmpDir, "outside.txt"))
	assert.True(t, os.IsNotExist(err), "outside archive must not be extracted")
}

// Test that archives are trashed (not permanently deleted) when a trasher is provided.
func TestUnzipper_ExtractArchives_TrashesArchiveWhenTrasherProvided(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "photos.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "photo.jpg", content: []byte("photo-data")},
	})

	v, err := safepath.New(tmpDir)
	require.NoError(t, err)

	metaDir, err := metadata.Init(tmpDir, v)
	require.NoError(t, err)

	runID := metaDir.RunID("unzip")
	trasher, err := trash.New(metaDir, runID, v)
	require.NoError(t, err)

	u, err := NewWithValidator(v, false, trasher)
	require.NoError(t, err)

	files := collectFiles(t, tmpDir)
	// Filter out .btidy directory files.
	var archiveFiles []collector.FileInfo
	for _, f := range files {
		if filepath.Ext(f.Name) == ".zip" {
			archiveFiles = append(archiveFiles, f)
		}
	}
	require.Len(t, archiveFiles, 1)

	result := u.ExtractArchives(archiveFiles)

	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ExtractedArchives)
	assert.Equal(t, 1, result.DeletedArchives)
	assert.Equal(t, 0, result.ErrorCount)

	// Archive should be gone from original location.
	assert.NoFileExists(t, archivePath)

	// Extracted file should exist.
	assert.FileExists(t, filepath.Join(tmpDir, "photo.jpg"))

	// The operation should have a populated TrashedTo field.
	require.Len(t, result.Operations, 1)
	assert.NotEmpty(t, result.Operations[0].TrashedTo, "TrashedTo should be populated")
	assert.Contains(t, result.Operations[0].TrashedTo, ".btidy/trash/")

	// The trashed archive should exist at the trash destination.
	assert.FileExists(t, result.Operations[0].TrashedTo)
}

func TestUnzipper_ExtractArchives_ReportsProgress(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "docs.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "docs/readme.txt", content: []byte("hello")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, true)
	require.NoError(t, err)

	progressCalls := 0
	lastStage := ""
	lastProcessed := 0
	lastTotal := 0

	result := u.ExtractArchivesWithProgress(files, func(stage string, processed, total int) {
		progressCalls++
		lastStage = stage
		lastProcessed = processed
		lastTotal = total
	})

	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.GreaterOrEqual(t, progressCalls, 1)
	assert.Equal(t, progressStageExtracting, lastStage)
	assert.Equal(t, lastProcessed, lastTotal)
	assert.Equal(t, 1, lastTotal)
}

// Test that extracting over an existing file backs it up to trash when a trasher is provided.
func TestUnzipper_ExtractArchives_BacksUpExistingFileToTrash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Pre-create a file that will be in the extraction path.
	existingPath := filepath.Join(tmpDir, "photo.jpg")
	originalContent := []byte("original precious data")
	require.NoError(t, os.WriteFile(existingPath, originalContent, 0o600))

	// Create an archive that extracts a file with the same name.
	archivePath := filepath.Join(tmpDir, "photos.zip")
	newContent := []byte("new photo data from archive")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "photo.jpg", content: newContent},
	})

	v, err := safepath.New(tmpDir)
	require.NoError(t, err)

	metaDir, err := metadata.Init(tmpDir, v)
	require.NoError(t, err)

	runID := metaDir.RunID("unzip")
	trasher, err := trash.New(metaDir, runID, v)
	require.NoError(t, err)

	u, err := NewWithValidator(v, false, trasher)
	require.NoError(t, err)

	archiveFiles := []collector.FileInfo{
		{Path: archivePath, Dir: tmpDir, Name: "photos.zip"},
	}
	result := u.ExtractArchives(archiveFiles)

	assert.Equal(t, 1, result.ExtractedArchives)
	assert.Equal(t, 0, result.ErrorCount)

	// The extracted file should contain the new content.
	got, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Equal(t, newContent, got, "file should contain new archive content")

	// The original file should be preserved in trash.
	trashDir := filepath.Join(tmpDir, ".btidy", "trash")
	entries, err := os.ReadDir(trashDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "trash directory should have a run subdirectory")

	// Find the backed-up file in the trash.
	backedUp := filepath.Join(trashDir, entries[0].Name(), "photo.jpg")
	assert.FileExists(t, backedUp, "original file should be in trash")

	backedUpContent, err := os.ReadFile(backedUp)
	require.NoError(t, err)
	assert.Equal(t, originalContent, backedUpContent, "trash should contain the original data")
}

// Test that extracting over an existing file is refused when no trasher is configured.
func TestUnzipper_ExtractArchives_RefusesOverwriteWithoutTrasher(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Pre-create a file that will be in the extraction path.
	existingPath := filepath.Join(tmpDir, "readme.txt")
	originalContent := []byte("important existing data")
	require.NoError(t, os.WriteFile(existingPath, originalContent, 0o600))

	// Create an archive that extracts a file with the same name.
	archivePath := filepath.Join(tmpDir, "docs.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "readme.txt", content: []byte("overwrite attempt")},
	})

	// No trasher — should refuse to overwrite.
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	files := collectFiles(t, tmpDir)
	// Filter to just the archive.
	var archiveFiles []collector.FileInfo
	for _, f := range files {
		if filepath.Ext(f.Name) == ".zip" {
			archiveFiles = append(archiveFiles, f)
		}
	}
	require.Len(t, archiveFiles, 1)

	result := u.ExtractArchives(archiveFiles)

	assert.Equal(t, 1, result.ErrorCount, "should have an error for refused overwrite")
	assert.Equal(t, 0, result.ExtractedArchives, "archive should not be marked as extracted")

	require.Len(t, result.Operations, 1)
	require.Error(t, result.Operations[0].Error)
	assert.Contains(t, result.Operations[0].Error.Error(), "refusing to overwrite existing file")

	// The original file must be preserved with its original content.
	got, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, got, "original file must not be modified")

	// The archive must still exist (extraction failed, so archive not deleted).
	assert.FileExists(t, archivePath, "archive must remain when extraction fails")
}
