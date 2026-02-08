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
	assert.Equal(t, 1, result.ExtractedArchives, "archive completes with skipped entries")
	assert.Equal(t, 1, result.DeletedArchives, "archive deleted after skipping bad entries")
	assert.Equal(t, 0, result.ErrorCount, "no archive-level error")
	require.Len(t, result.Operations, 1)
	require.NoError(t, result.Operations[0].Error, "per-entry failures are not archive-level errors")
	assert.Equal(t, 1, result.Operations[0].SkippedEntries, "zip-slip entry should be skipped")
	assert.Contains(t, result.Operations[0].EntryErrors[0], "escape")

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
	assert.Equal(t, 1, result.ExtractedArchives, "archive completes with skipped entries")
	assert.Equal(t, 1, result.DeletedArchives, "archive deleted after skipping bad entries")
	assert.Equal(t, 0, result.ErrorCount, "no archive-level error")
	require.Len(t, result.Operations, 1)
	require.NoError(t, result.Operations[0].Error, "per-entry failures are not archive-level errors")
	assert.Equal(t, 1, result.Operations[0].SkippedEntries, "symlink entry should be skipped")
	assert.Contains(t, result.Operations[0].EntryErrors[0], "symlink")

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

	assert.Equal(t, 0, result.ErrorCount, "no archive-level error")
	assert.Equal(t, 1, result.ExtractedArchives, "archive completes with skipped entry")

	require.Len(t, result.Operations, 1)
	require.NoError(t, result.Operations[0].Error, "per-entry failures are not archive-level errors")
	assert.Equal(t, 1, result.Operations[0].SkippedEntries, "overwrite entry should be skipped")
	require.Len(t, result.Operations[0].EntryErrors, 1)
	assert.Contains(t, result.Operations[0].EntryErrors[0], "refusing to overwrite existing file")

	// The original file must be preserved with its original content.
	got, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, got, "original file must not be modified")
}

// TestUnzipper_ExtractArchives_SkipsBadEntriesContinuesGood verifies that a
// single bad entry (e.g. symlink) does not abort the entire archive.  Good
// entries are extracted, the bad entry is skipped, and the archive is still
// deleted after extraction.
func TestUnzipper_ExtractArchives_SkipsBadEntriesContinuesGood(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "mixed.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "good.txt", content: []byte("hello")},
		{name: "bad-symlink", content: []byte("/etc/passwd"), mode: os.ModeSymlink | 0o777},
		{name: "also-good.txt", content: []byte("world")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.ArchivesFound)
	assert.Equal(t, 1, result.ArchivesProcessed)
	assert.Equal(t, 1, result.ExtractedArchives, "archive should still be marked extracted")
	assert.Equal(t, 1, result.DeletedArchives, "archive should be deleted despite skipped entry")
	assert.Equal(t, 2, result.ExtractedFiles, "both good files should be extracted")
	assert.Equal(t, 0, result.ErrorCount, "archive-level error count should be zero")

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.True(t, op.ExtractionComplete, "extraction should complete")
	assert.True(t, op.DeletedArchive, "archive should be deleted")
	require.NoError(t, op.Error, "no archive-level error")
	assert.Equal(t, 1, op.SkippedEntries, "one entry should be skipped")
	require.Len(t, op.EntryErrors, 1)
	assert.Contains(t, op.EntryErrors[0], "symlink")

	// Good files extracted.
	assert.FileExists(t, filepath.Join(tmpDir, "good.txt"))
	assert.FileExists(t, filepath.Join(tmpDir, "also-good.txt"))

	// Archive removed.
	assert.NoFileExists(t, archivePath)

	// Bad entry not written.
	assert.NoFileExists(t, filepath.Join(tmpDir, "bad-symlink"))
}

// TestUnzipper_ExtractArchives_SkipsBadEntriesZipSlip verifies that a zip-slip
// entry is skipped while other entries in the same archive are extracted.
func TestUnzipper_ExtractArchives_SkipsBadEntriesZipSlip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "zipslip.zip")
	writeZipArchive(t, archivePath, []zipFixtureEntry{
		{name: "safe.txt", content: []byte("safe content")},
		{name: "../escape.txt", content: []byte("attack")},
	})

	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 1, result.ExtractedArchives, "archive should be extracted")
	assert.Equal(t, 1, result.DeletedArchives, "archive should be deleted")
	assert.Equal(t, 1, result.ExtractedFiles, "safe file should be extracted")
	assert.Equal(t, 0, result.ErrorCount)

	require.Len(t, result.Operations, 1)
	op := result.Operations[0]
	assert.Equal(t, 1, op.SkippedEntries)
	assert.Contains(t, op.EntryErrors[0], "escape")

	assert.FileExists(t, filepath.Join(tmpDir, "safe.txt"))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(tmpDir), "escape.txt"))
}

// TestUnzipper_ExtractArchives_DryRunNestedArchiveDiscovery verifies that
// dry-run mode correctly discovers and counts nested archives (regression test
// for a bug where dry-run returned nil instead of the discovered slice).
func TestUnzipper_ExtractArchives_DryRunNestedArchiveDiscovery(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Build a 3-level nesting: outer.zip -> mid/inner.zip -> deep.txt
	innerZip := zipBytes(t, []zipFixtureEntry{
		{name: "deep.txt", content: []byte("deep")},
	})
	outerPath := filepath.Join(tmpDir, "outer.zip")
	writeZipArchive(t, outerPath, []zipFixtureEntry{
		{name: "mid/inner.zip", content: innerZip},
		{name: "top.txt", content: []byte("top")},
	})

	files := collectFiles(t, tmpDir)

	// Dry-run should discover the same archives as a real run.
	dryU, err := New(tmpDir, true)
	require.NoError(t, err)
	dryResult := dryU.ExtractArchives(files)

	assert.Equal(t, 2, dryResult.ArchivesFound, "dry-run should discover both archive levels")
	assert.Equal(t, 2, dryResult.ArchivesProcessed, "dry-run should process both archives")
	assert.Equal(t, 2, dryResult.ExtractedArchives, "dry-run should report both as extracted")
	assert.Equal(t, 0, dryResult.ErrorCount)
	require.Len(t, dryResult.Operations, 2)

	// No files should have been created or deleted.
	assert.FileExists(t, outerPath, "dry-run must not remove archive")
	assert.NoDirExists(t, filepath.Join(tmpDir, "mid"), "dry-run must not create directories")
}

// TestUnzipper_ExtractArchives_DeeplyNestedArchives verifies that zip-in-zip
// nesting four levels deep is fully extracted in a single run, and that a
// second run from the top finds nothing left to process (idempotent).
func TestUnzipper_ExtractArchives_DeeplyNestedArchives(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Build archives from the inside out (4 levels).
	//   level4.zip -> "final.txt"
	//   level3.zip -> "l3/level4.zip"
	//   level2.zip -> "l2/level3.zip"
	//   level1.zip -> "l1/level2.zip" + "top.txt"
	level4 := zipBytes(t, []zipFixtureEntry{
		{name: "final.txt", content: []byte("deeply nested content")},
	})
	level3 := zipBytes(t, []zipFixtureEntry{
		{name: "l3/level4.zip", content: level4},
	})
	level2 := zipBytes(t, []zipFixtureEntry{
		{name: "l2/level3.zip", content: level3},
	})

	level1Path := filepath.Join(tmpDir, "level1.zip")
	writeZipArchive(t, level1Path, []zipFixtureEntry{
		{name: "l1/level2.zip", content: level2},
		{name: "top.txt", content: []byte("top level")},
	})

	// --- Phase 1: extract everything recursively ---
	files := collectFiles(t, tmpDir)
	u, err := New(tmpDir, false)
	require.NoError(t, err)

	result := u.ExtractArchives(files)

	assert.Equal(t, 4, result.ArchivesFound, "should discover all 4 archive levels")
	assert.Equal(t, 4, result.ArchivesProcessed, "should process all 4 archives")
	assert.Equal(t, 4, result.ExtractedArchives, "should extract all 4 archives")
	assert.Equal(t, 4, result.DeletedArchives, "should delete all 4 archives")
	assert.Equal(t, 0, result.ErrorCount, "should complete without errors")
	require.Len(t, result.Operations, 4, "should have one operation per archive")

	// Every operation should be complete and error-free.
	for i, op := range result.Operations {
		assert.True(t, op.ExtractionComplete, "operation %d should be complete", i)
		assert.True(t, op.DeletedArchive, "operation %d archive should be deleted", i)
		require.NoError(t, op.Error, "operation %d should have no error", i)
	}

	// Final extracted files must exist with correct content.
	topContent, err := os.ReadFile(filepath.Join(tmpDir, "top.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("top level"), topContent)

	finalContent, err := os.ReadFile(filepath.Join(tmpDir, "l1", "l2", "l3", "final.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("deeply nested content"), finalContent)

	// No .zip files should remain anywhere in the tree.
	var remainingZips []string
	require.NoError(t, filepath.Walk(tmpDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && isZipArchive(path) {
			remainingZips = append(remainingZips, path)
		}
		return nil
	}))
	assert.Empty(t, remainingZips, "no zip archives should remain after extraction")

	// --- Phase 2: re-run from the top — nothing left to extract ---
	filesAfter := collectFiles(t, tmpDir)
	u2, err := New(tmpDir, false)
	require.NoError(t, err)

	resultAfter := u2.ExtractArchives(filesAfter)

	assert.Equal(t, 0, resultAfter.ArchivesFound, "second run should find zero archives")
	assert.Equal(t, 0, resultAfter.ArchivesProcessed, "second run should process nothing")
	assert.Empty(t, resultAfter.Operations, "second run should produce no operations")

	// Extracted files must still be intact after the second run.
	topAgain, err := os.ReadFile(filepath.Join(tmpDir, "top.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("top level"), topAgain, "top.txt must survive second run")

	finalAgain, err := os.ReadFile(filepath.Join(tmpDir, "l1", "l2", "l3", "final.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("deeply nested content"), finalAgain, "final.txt must survive second run")
}
