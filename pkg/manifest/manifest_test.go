package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"btidy/internal/testutil"
)

func expectedHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func TestNewGenerator(t *testing.T) {
	t.Parallel()

	t.Run("valid directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		g, err := NewGenerator(tmpDir, 0)
		require.NoError(t, err)
		assert.NotNil(t, g)
	})

	t.Run("non-existent directory", func(t *testing.T) {
		t.Parallel()
		_, err := NewGenerator("/nonexistent/path", 0)
		require.Error(t, err)
	})

	t.Run("file instead of directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "file.txt")
		testutil.CreateFile(t, filePath, "content")

		_, err := NewGenerator(filePath, 0)
		require.Error(t, err)
	})

	t.Run("custom workers", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		g, err := NewGenerator(tmpDir, 4)
		require.NoError(t, err)
		assert.NotNil(t, g)
	})
}

func TestGenerator_Generate_EmptyDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, m.Version)
	assert.Empty(t, m.Entries)
	assert.Equal(t, tmpDir, m.RootPath)
	assert.False(t, m.CreatedAt.IsZero())
}

func TestGenerator_Generate_SingleFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	content := "hello world"
	testutil.CreateFile(t, filepath.Join(tmpDir, "test.txt"), content)

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{})
	require.NoError(t, err)

	require.Len(t, m.Entries, 1)
	assert.Equal(t, "test.txt", m.Entries[0].Path)
	assert.Equal(t, expectedHash(content), m.Entries[0].Hash)
	assert.Equal(t, int64(len(content)), m.Entries[0].Size)
}

func TestGenerator_Generate_MultipleFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	files := map[string]string{
		"a.txt":          "content a",
		"b.txt":          "content b",
		"subdir/c.txt":   "content c",
		"subdir/d.txt":   "content d",
		"deep/dir/e.txt": "content e",
	}

	for name, content := range files {
		testutil.CreateFile(t, filepath.Join(tmpDir, name), content)
	}

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{})
	require.NoError(t, err)

	assert.Len(t, m.Entries, len(files))

	// Check that entries are sorted by path
	for i := 1; i < len(m.Entries); i++ {
		assert.Less(t, m.Entries[i-1].Path, m.Entries[i].Path)
	}

	// Verify hashes
	for _, entry := range m.Entries {
		fullPath := filepath.Join(tmpDir, entry.Path)
		relPath, _ := filepath.Rel(tmpDir, fullPath)
		content := files[relPath]
		assert.Equal(t, expectedHash(content), entry.Hash)
	}
}

func TestGenerator_Generate_WithSkipFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	testutil.CreateFile(t, filepath.Join(tmpDir, "keep.txt"), "keep")
	testutil.CreateFile(t, filepath.Join(tmpDir, ".DS_Store"), "skip")
	testutil.CreateFile(t, filepath.Join(tmpDir, "Thumbs.db"), "skip")

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{
		SkipFiles: []string{".DS_Store", "Thumbs.db"},
	})
	require.NoError(t, err)

	require.Len(t, m.Entries, 1)
	assert.Equal(t, "keep.txt", m.Entries[0].Path)
}

func TestGenerator_Generate_WithProgress(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	numFiles := 5
	for i := range numFiles {
		name := string(rune('a'+i)) + ".txt"
		testutil.CreateFile(t, filepath.Join(tmpDir, name), "content")
	}

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	var progressCalls []int
	m, err := g.Generate(GenerateOptions{
		OnProgress: func(processed, _ int, _ string) {
			progressCalls = append(progressCalls, processed)
		},
	})
	require.NoError(t, err)

	assert.Len(t, m.Entries, numFiles)
	assert.Len(t, progressCalls, numFiles)

	// Progress should go from 1 to numFiles
	for i, p := range progressCalls {
		assert.LessOrEqual(t, p, numFiles)
		assert.GreaterOrEqual(t, p, i+1)
	}
}

func TestManifest_SaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a manifest
	original := &Manifest{
		Version:   1,
		CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		RootPath:  "/path/to/backup",
		Entries: []ManifestEntry{
			{Path: "file1.txt", Hash: "abc123", Size: 100, ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Path: "file2.txt", Hash: "def456", Size: 200, ModTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		},
	}

	// Save it
	savePath := filepath.Join(tmpDir, "manifest.json")
	err := original.Save(savePath)
	require.NoError(t, err)

	// Load it back
	loaded, err := Load(savePath)
	require.NoError(t, err)

	// Compare
	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.CreatedAt, loaded.CreatedAt)
	assert.Equal(t, original.RootPath, loaded.RootPath)
	require.Len(t, loaded.Entries, len(original.Entries))

	for i := range original.Entries {
		assert.Equal(t, original.Entries[i].Path, loaded.Entries[i].Path)
		assert.Equal(t, original.Entries[i].Hash, loaded.Entries[i].Hash)
		assert.Equal(t, original.Entries[i].Size, loaded.Entries[i].Size)
		assert.True(t, original.Entries[i].ModTime.Equal(loaded.Entries[i].ModTime))
	}
}

func TestManifest_Load_NonExistent(t *testing.T) {
	t.Parallel()
	_, err := Load("/nonexistent/manifest.json")
	require.Error(t, err)
}

func TestManifest_Load_InvalidJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	invalidPath := filepath.Join(tmpDir, "invalid.json")
	err := os.WriteFile(invalidPath, []byte("not json"), 0644)
	require.NoError(t, err)

	_, err = Load(invalidPath)
	require.Error(t, err)
}

func TestManifest_UniqueHashes(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Entries: []ManifestEntry{
			{Hash: "hash1"},
			{Hash: "hash2"},
			{Hash: "hash1"}, // duplicate
			{Hash: "hash3"},
			{Hash: "hash2"}, // duplicate
		},
	}

	hashes := m.UniqueHashes()
	assert.Len(t, hashes, 3)
	assert.Contains(t, hashes, "hash1")
	assert.Contains(t, hashes, "hash2")
	assert.Contains(t, hashes, "hash3")
}

func TestManifest_HashIndex(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Entries: []ManifestEntry{
			{Path: "file1.txt", Hash: "hash1"},
			{Path: "file2.txt", Hash: "hash2"},
			{Path: "file3.txt", Hash: "hash1"}, // same hash as file1
		},
	}

	index := m.HashIndex()

	assert.Len(t, index, 2)
	assert.ElementsMatch(t, []string{"file1.txt", "file3.txt"}, index["hash1"])
	assert.ElementsMatch(t, []string{"file2.txt"}, index["hash2"])
}

func TestManifest_TotalSize(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Entries: []ManifestEntry{
			{Size: 100},
			{Size: 200},
			{Size: 300},
		},
	}

	assert.Equal(t, int64(600), m.TotalSize())
}

func TestManifest_FileCount(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Entries: []ManifestEntry{
			{Path: "file1.txt"},
			{Path: "file2.txt"},
			{Path: "file3.txt"},
		},
	}

	assert.Equal(t, 3, m.FileCount())
}

func TestManifest_UniqueFileCount(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Entries: []ManifestEntry{
			{Hash: "hash1"},
			{Hash: "hash2"},
			{Hash: "hash1"}, // duplicate content
		},
	}

	assert.Equal(t, 3, m.FileCount())
	assert.Equal(t, 2, m.UniqueFileCount())
}

func TestManifest_RelativePaths(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create nested structure
	testutil.CreateFile(t, filepath.Join(tmpDir, "root.txt"), "root")
	testutil.CreateFile(t, filepath.Join(tmpDir, "dir1", "file1.txt"), "file1")
	testutil.CreateFile(t, filepath.Join(tmpDir, "dir1", "dir2", "file2.txt"), "file2")

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{})
	require.NoError(t, err)

	// All paths should be relative
	for _, entry := range m.Entries {
		assert.False(t, filepath.IsAbs(entry.Path), "path should be relative: %s", entry.Path)
	}
}

func TestGenerator_Generate_DuplicateContent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create files with same content
	sameContent := "duplicate content"
	testutil.CreateFile(t, filepath.Join(tmpDir, "file1.txt"), sameContent)
	testutil.CreateFile(t, filepath.Join(tmpDir, "file2.txt"), sameContent)
	testutil.CreateFile(t, filepath.Join(tmpDir, "file3.txt"), "unique content")

	g, err := NewGenerator(tmpDir, 0)
	require.NoError(t, err)

	m, err := g.Generate(GenerateOptions{})
	require.NoError(t, err)

	assert.Equal(t, 3, m.FileCount())
	assert.Equal(t, 2, m.UniqueFileCount()) // Only 2 unique hashes
}
