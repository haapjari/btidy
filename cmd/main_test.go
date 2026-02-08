package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
)

func setCommandGlobals(t *testing.T, dryRunValue, verboseValue bool, workersValue int) {
	t.Helper()

	prevDryRun := dryRun
	prevVerbose := verbose
	prevWorkers := workers

	dryRun = dryRunValue
	verbose = verboseValue
	workers = workersValue

	t.Cleanup(func() {
		dryRun = prevDryRun
		verbose = prevVerbose
		workers = prevWorkers
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	require.NoError(t, writer.Close())
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	return string(out)
}

func writeZipArchive(t *testing.T, archivePath string, entries map[string]string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(archivePath), 0o755))

	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)

	writer := zip.NewWriter(archiveFile)
	for name, content := range entries {
		entryWriter, err := writer.Create(name)
		require.NoError(t, err)

		_, err = entryWriter.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, archiveFile.Close())
}

func TestRunRename_DryRun_OutputSummary(t *testing.T) {
	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "My Document.pdf"), "content", modTime)

	setCommandGlobals(t, true, false, 1)

	output := captureStdout(t, func() {
		err := runRename(nil, []string{tmpDir})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "=== DRY RUN - no changes will be made ===")
	assert.Contains(t, output, "Command: RENAME")
	assert.Contains(t, output, "=== Summary ===")
	assert.Contains(t, output, "Total files:  1")
	assert.Contains(t, output, "Renamed:      1")
	assert.Contains(t, output, "Skipped:      0")
	assert.Contains(t, output, "Deleted:      0")
	assert.Contains(t, output, "Errors:       0")
	assert.Contains(t, output, "Run without --dry-run to apply changes.")

	_, err := os.Stat(filepath.Join(tmpDir, "My Document.pdf"))
	require.NoError(t, err, "dry-run must not rename files")

	_, err = os.Stat(filepath.Join(tmpDir, "2018-06-15_my_document.pdf"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create renamed files")
}

func TestRunFlatten_DryRun_OutputSummary(t *testing.T) {
	tmpDir := t.TempDir()
	modTime := time.Date(2018, 6, 15, 12, 0, 0, 0, time.UTC)
	testutil.CreateFileWithModTime(t, filepath.Join(tmpDir, "subdir", "file.txt"), "content", modTime)

	setCommandGlobals(t, true, false, 1)

	output := captureStdout(t, func() {
		err := runFlatten(nil, []string{tmpDir})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "=== DRY RUN - no changes will be made ===")
	assert.Contains(t, output, "Command: FLATTEN")
	assert.Contains(t, output, "=== Summary ===")
	assert.Contains(t, output, "Total files:     1")
	assert.Contains(t, output, "Moved:           1")
	assert.Contains(t, output, "Duplicates:      0")
	assert.Contains(t, output, "Skipped:         0")
	assert.Contains(t, output, "Errors:          0")
	assert.NotContains(t, output, "Dirs removed:")
	assert.Contains(t, output, "Run without --dry-run to apply changes.")

	_, err := os.Stat(filepath.Join(tmpDir, "subdir", "file.txt"))
	require.NoError(t, err, "dry-run must not move files")

	_, err = os.Stat(filepath.Join(tmpDir, "file.txt"))
	assert.True(t, os.IsNotExist(err), "dry-run must not place file in root")
}

func TestRunUnzip_DryRun_OutputSummary(t *testing.T) {
	tmpDir := t.TempDir()
	writeZipArchive(t, filepath.Join(tmpDir, "photos.zip"), map[string]string{
		"nested/photo.jpg": "photo",
	})

	setCommandGlobals(t, true, false, 1)

	output := captureStdout(t, func() {
		err := runUnzip(nil, []string{tmpDir})
		require.NoError(t, err)
	})

	assert.Contains(t, output, "=== DRY RUN - no changes will be made ===")
	assert.Contains(t, output, "Command: UNZIP")
	assert.Contains(t, output, "=== Summary ===")
	assert.Contains(t, output, "Total files:        1")
	assert.Contains(t, output, "Archives found:     1")
	assert.Contains(t, output, "Archives processed: 1")
	assert.Contains(t, output, "Archives extracted: 1")
	assert.Contains(t, output, "Archives deleted:   1")
	assert.Contains(t, output, "Files extracted:    1")
	assert.Contains(t, output, "Dir entries:        0")
	assert.Contains(t, output, "Errors:             0")
	assert.Contains(t, output, "Run without --dry-run to apply changes.")

	_, err := os.Stat(filepath.Join(tmpDir, "photos.zip"))
	require.NoError(t, err, "dry-run must not remove archives")

	_, err = os.Stat(filepath.Join(tmpDir, "nested", "photo.jpg"))
	assert.True(t, os.IsNotExist(err), "dry-run must not extract files")
}
