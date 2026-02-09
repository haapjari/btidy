package unzipper

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
	assert "github.com/stretchr/testify/require"
)

func TestGetAllFilesRecursively(t *testing.T) {
	t.Run("traverse 1 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 1)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.Greater(t, len(files), 0, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Greater(t, f.Size, int64(0), "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 31, "expected at least 30 test files + 1 archive")
		assert.LessOrEqual(t, len(files), 51, "expected at most 50 test files + 1 archive")
	})
	
	t.Run("traverse 5 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 5)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.Greater(t, len(files), 0, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Greater(t, f.Size, int64(0), "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 5*31, "expected at least 5*(30 files + 1 archive)")
		assert.LessOrEqual(t, len(files), 5*51, "expected at most 5*(50 files + 1 archive)")
	})

t.Run("traverse 10 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 10)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.Greater(t, len(files), 0, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Greater(t, f.Size, int64(0), "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 10*30+10, "expected at least 10*30 test files + 10 archives")
		assert.LessOrEqual(t, len(files), 10*50+10, "expected at most 10*50 test files + 10 archives")
	})
}

// createTestFiles generates a specified number of test files (30-50) in the given directory
func createTestFiles(t *testing.T, rootPath string, level int) {
	t.Helper()

	numFiles := 30 + rand.Intn(21)
	for i := range numFiles {
		fileName := fmt.Sprintf("file_%d.txt", i)
		filePath := filepath.Join(rootPath, fileName)
		content := fmt.Sprintf("content of file %d at level %d", i, level)
		require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))
	}
}

// createTestFileAndFolderStructure builds a nested directory tree of the given depth
// inside a temporary directory. Each level contains 30-50 random test files and a
// subdirectory for the next level (subdir_0/subdir_1/…). After all directories and
// files are created, zip archives are generated bottom-up so that each archive at
// level i includes the contents of its subdirectory (and thus the child archive at
// level i+1). Returns the root temp directory path, or "" if level < 0.
func createTestFileAndFolderStructure(t *testing.T, level int) string {
	t.Helper()

	if level < 0 {
		return ""
	}

	path := t.TempDir()

	currentDir := path
	dirs := make([]string, level)

	// create all the directories and files
	for i := range level {
		subDir := filepath.Join(currentDir, fmt.Sprintf("subdir_%d", i))
		require.NoError(t, os.MkdirAll(subDir, 0755))
		createTestFiles(t, subDir, i+1)
		dirs[i] = subDir
		currentDir = subDir
	}

	// create archive bottom-up so each zip includes a child zip
	for i := level - 1; i >= 0; i-- {
		parent := path
		if i > 0 {
			parent = dirs[i-1]
		}
		zipPath := filepath.Join(parent, fmt.Sprintf("archive_level_%d.zip", i))
		createZipArchive(t, dirs[i], zipPath)
	}

	return path
}

// createZipArchive creates a ZIP archive at zipPath containing all files found
// recursively under sourceDir. File paths inside the archive are stored as
// slash-separated paths relative to sourceDir. Directories themselves are not
// stored as explicit entries. The test is failed immediately if the zip file
// cannot be created, and an assertion error is reported if the directory walk
// encounters any issue.
func createZipArchive(t *testing.T, sourceDir, zipPath string) {
	t.Helper()

	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip file: %v", err)
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	err = filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		w, err := zw.Create(filepath.ToSlash(path[len(sourceDir)+1:]))
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
	})

	assert.NoError(t, err, "unable to create zip archive")
}
