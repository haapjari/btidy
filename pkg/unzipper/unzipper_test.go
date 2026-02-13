package unzipper

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"btidy/pkg/collector"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnzip(t *testing.T) {
	t.Run("extracts files from valid zip archive", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "data.bin"), []byte("binary data"), 0644))

		archivePath := filepath.Join(root, "test.zip")
		createZipArchive(t, srcDir, archivePath)

		require.NoError(t, os.RemoveAll(srcDir))

		file := collector.FileInfo{
			Dir:  root,
			Name: "test.zip",
			Path: archivePath,
		}

		_, err := unzip(file)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(root, "hello.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(content))

		content, err = os.ReadFile(filepath.Join(root, "data.bin"))
		require.NoError(t, err)
		assert.Equal(t, "binary data", string(content))
	})

	t.Run("extracts nested directory structure", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "src")
		nestedDir := filepath.Join(srcDir, "a", "b", "c")
		require.NoError(t, os.MkdirAll(nestedDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "deep.txt"), []byte("deep content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a", "shallow.txt"), []byte("shallow content"), 0644))

		archivePath := filepath.Join(root, "nested.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		file := collector.FileInfo{
			Dir:  root,
			Name: "nested.zip",
			Path: archivePath,
		}

		_, err := unzip(file)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(root, "a", "b", "c", "deep.txt"))
		require.NoError(t, err)
		assert.Equal(t, "deep content", string(content))

		content, err = os.ReadFile(filepath.Join(root, "a", "shallow.txt"))
		require.NoError(t, err)
		assert.Equal(t, "shallow content", string(content))
	})

	t.Run("rejects path traversal entries", func(t *testing.T) {
		root := t.TempDir()

		archivePath := filepath.Join(root, "evil.zip")
		f, err := os.Create(archivePath)
		require.NoError(t, err)
		zw := zip.NewWriter(f)
		w, err := zw.Create("../escape.txt")
		require.NoError(t, err)
		_, err = w.Write([]byte("escaped"))
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())

		file := collector.FileInfo{
			Dir:  root,
			Name: "evil.zip",
			Path: archivePath,
		}

		_, err = unzip(file)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "illegal entry path")
		assert.Contains(t, err.Error(), "contains path traversal")
	})

	t.Run("allows filename containing double dots", func(t *testing.T) {
		root := t.TempDir()

		archivePath := filepath.Join(root, "double_dots.zip")
		f, err := os.Create(archivePath)
		require.NoError(t, err)

		zw := zip.NewWriter(f)
		w, err := zw.Create("Tiedostot/Suunnitelma/Isompi kuin -ohjelma..txt")
		require.NoError(t, err)
		_, err = w.Write([]byte("ok"))
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())

		file := collector.FileInfo{
			Dir:  root,
			Name: "double_dots.zip",
			Path: archivePath,
		}

		_, err = unzip(file)
		require.NoError(t, err)

		extractedPath := filepath.Join(root, "Tiedostot", "Suunnitelma", "Isompi kuin -ohjelma..txt")
		content, err := os.ReadFile(extractedPath)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(content))
	})

	t.Run("allows non utf8 filename bytes", func(t *testing.T) {
		root := t.TempDir()
		nonUTF8Name := "Ensimm" + string([]byte{0x84}) + "inen kirjoitus.docx"

		archivePath := filepath.Join(root, "non_utf8.zip")
		f, err := os.Create(archivePath)
		require.NoError(t, err)

		entryName := "Tiedostot/Blog/" + nonUTF8Name
		zw := zip.NewWriter(f)
		w, err := zw.Create(entryName)
		require.NoError(t, err)
		_, err = w.Write([]byte("doc content"))
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())

		file := collector.FileInfo{
			Dir:  root,
			Name: "non_utf8.zip",
			Path: archivePath,
		}

		_, err = unzip(file)
		require.NoError(t, err)

		extractedPath := filepath.Join(root, "Tiedostot", "Blog", nonUTF8Name)
		content, err := os.ReadFile(extractedPath)
		require.NoError(t, err)
		assert.Equal(t, "doc content", string(content))
	})

	t.Run("returns error for non-existent archive", func(t *testing.T) {
		root := t.TempDir()

		file := collector.FileInfo{
			Dir:  root,
			Name: "missing.zip",
			Path: filepath.Join(root, "missing.zip"),
		}

		_, err := unzip(file)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open archive")
	})

	t.Run("returns error for corrupt archive", func(t *testing.T) {
		root := t.TempDir()

		corruptPath := filepath.Join(root, "corrupt.zip")
		require.NoError(t, os.WriteFile(corruptPath, []byte("this is not a zip file"), 0644))

		file := collector.FileInfo{
			Dir:  root,
			Name: "corrupt.zip",
			Path: corruptPath,
		}

		_, err := unzip(file)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open archive")
	})

	t.Run("empty zip archive extracts successfully", func(t *testing.T) {
		root := t.TempDir()

		archivePath := filepath.Join(root, "empty.zip")
		f, err := os.Create(archivePath)
		require.NoError(t, err)
		zw := zip.NewWriter(f)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())

		file := collector.FileInfo{
			Dir:  root,
			Name: "empty.zip",
			Path: archivePath,
		}

		_, err = unzip(file)
		require.NoError(t, err)
	})

	t.Run("overwrites existing files", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("new content"), 0644))

		archivePath := filepath.Join(root, "overwrite.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), []byte("old content"), 0644))

		file := collector.FileInfo{
			Dir:  root,
			Name: "overwrite.zip",
			Path: archivePath,
		}

		_, err := unzip(file)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(root, "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, "new content", string(content))
	})

	t.Run("does not delete archive after extraction", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "keep.txt"), []byte("keep"), 0644))

		archivePath := filepath.Join(root, "archive.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		file := collector.FileInfo{
			Dir:  root,
			Name: "archive.zip",
			Path: archivePath,
		}

		_, err := unzip(file)
		require.NoError(t, err)

		_, err = os.Stat(archivePath)
		assert.NoError(t, err, "archive should still exist after extraction")
	})
}

func TestGetRootDirectory(t *testing.T) {
	t.Run("empty slice returns empty string", func(t *testing.T) {
		result := getRootDirectory([]collector.FileInfo{})
		assert.Empty(t, result)
	})

	t.Run("nil slice returns empty string", func(t *testing.T) {
		result := getRootDirectory(nil)
		assert.Empty(t, result)
	})

	t.Run("single file returns its directory", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/home/user/documents"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/home/user/documents", result)
	})

	t.Run("files in same directory", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/home/user/documents"},
			{Dir: "/home/user/documents"},
			{Dir: "/home/user/documents"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/home/user/documents", result)
	})

	t.Run("files in nested subdirectories", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/home/user/documents/a"},
			{Dir: "/home/user/documents/b"},
			{Dir: "/home/user/documents/a/deep"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/home/user/documents", result)
	})

	t.Run("files with deeply nested common ancestor", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/a/b/c/d/e"},
			{Dir: "/a/b/c/x/y"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/a/b/c", result)
	})

	t.Run("files sharing only root as common ancestor", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/foo/bar"},
			{Dir: "/baz/qux"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/", result)
	})

	t.Run("parent and child directory", func(t *testing.T) {
		files := []collector.FileInfo{
			{Dir: "/home/user"},
			{Dir: "/home/user/sub/deep"},
		}
		result := getRootDirectory(files)
		assert.Equal(t, "/home/user", result)
	})
}

func TestExtractArchivesWithProgressRecursively(t *testing.T) {
	newProgressTracker := func() (func(string, int, int), *[]struct {
		stage     string
		processed int
		total     int
	}) {
		var calls []struct {
			stage     string
			processed int
			total     int
		}
		fn := func(stage string, processed, total int) {
			calls = append(calls, struct {
				stage     string
				processed int
				total     int
			}{stage, processed, total})
		}
		return fn, &calls
	}

	setup := func(t *testing.T, root string, dryRun bool) (*Unzipper, []collector.FileInfo) {
		t.Helper()
		uz, err := New(root, dryRun)
		require.NoError(t, err)
		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		return uz, files
	}

	t.Run("empty file list returns zero result", func(t *testing.T) {
		uz, err := New(t.TempDir(), false)
		require.NoError(t, err)

		result, err := uz.ExtractArchivesWithProgressRecursively([]collector.FileInfo{}, nil)
		require.NoError(t, err)

		assert.Equal(t, 0, result.TotalFiles)
		assert.Equal(t, 0, result.ArchivesFound)
		assert.Equal(t, 0, result.ArchivesProcessed)
		assert.Equal(t, 0, result.ExtractedArchives)
		assert.Equal(t, 0, result.ExtractedFiles)
		assert.Equal(t, 0, result.ExtractedDirs)
		assert.Equal(t, 0, result.ErrorCount)
		assert.Empty(t, result.Operations)
	})

	t.Run("no archives among plain files", func(t *testing.T) {
		root := t.TempDir()
		createTestFiles(t, root, 0)
		subDir := filepath.Join(root, "subdir_0")
		require.NoError(t, os.MkdirAll(subDir, 0755))
		createTestFiles(t, subDir, 1)

		uz, files := setup(t, root, false)
		progress, calls := newProgressTracker()

		result, err := uz.ExtractArchivesWithProgressRecursively(files, progress)
		require.NoError(t, err)

		assert.Equal(t, 0, result.ArchivesFound)
		assert.Equal(t, 0, result.ArchivesProcessed)
		assert.Equal(t, 0, result.ErrorCount)
		assert.Equal(t, 0, result.ExtractedFiles)
		_ = calls
	})

	t.Run("extracts single archive", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "archived_content")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("bbb"), 0644))

		archivePath := filepath.Join(root, "test.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		uz, files := setup(t, root, false)
		progress, _ := newProgressTracker()

		result, err := uz.ExtractArchivesWithProgressRecursively(files, progress)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, result.ArchivesFound, 1)
		assert.Equal(t, 0, result.ErrorCount)
		assert.GreaterOrEqual(t, result.ExtractedFiles, 2, "expected at least the 2 files from the archive")

		// verify extracted content exists on disk
		content, err := os.ReadFile(filepath.Join(root, "a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "aaa", string(content))

		content, err = os.ReadFile(filepath.Join(root, "b.txt"))
		require.NoError(t, err)
		assert.Equal(t, "bbb", string(content))
	})

	t.Run("extracts nested archives recursively", func(t *testing.T) {
		root := t.TempDir()

		// create inner archive content
		innerDir := filepath.Join(root, "inner_src")
		require.NoError(t, os.MkdirAll(innerDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(innerDir, "deep.txt"), []byte("deep content"), 0644))

		innerZipPath := filepath.Join(root, "inner.zip")
		createZipArchive(t, innerDir, innerZipPath)
		require.NoError(t, os.RemoveAll(innerDir))

		// create outer archive containing the inner zip
		outerDir := filepath.Join(root, "outer_src")
		require.NoError(t, os.MkdirAll(outerDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(outerDir, "outer.txt"), []byte("outer content"), 0644))

		// copy inner zip into outer source dir
		innerData, err := os.ReadFile(innerZipPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(outerDir, "inner.zip"), innerData, 0644))

		outerZipPath := filepath.Join(root, "outer.zip")
		createZipArchive(t, outerDir, outerZipPath)
		require.NoError(t, os.RemoveAll(outerDir))
		require.NoError(t, os.Remove(innerZipPath))

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, result.ArchivesFound, 1, "expected at least the outer archive")
		assert.Equal(t, 0, result.ErrorCount)
		// the nested archive should have been discovered and extracted too
		assert.GreaterOrEqual(t, result.ExtractedArchives, 1)
	})

	t.Run("progress callback receives calls", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "content")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), 0644))

		archivePath := filepath.Join(root, "progress_test.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		uz, files := setup(t, root, false)
		progress, calls := newProgressTracker()

		_, err := uz.ExtractArchivesWithProgressRecursively(files, progress)
		require.NoError(t, err)

		assert.NotEmpty(t, *calls, "expected progress callback to be invoked at least once")
	})

	t.Run("nil progress callback does not panic", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "x.txt"), []byte("x"), 0644))

		archivePath := filepath.Join(root, "nil_progress.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))

		uz, files := setup(t, root, false)

		assert.NotPanics(t, func() {
			_, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
			require.NoError(t, err)
		})
	})

	t.Run("corrupt archive is filtered out by isArchive", func(t *testing.T) {
		root := t.TempDir()

		corruptPath := filepath.Join(root, "corrupt.zip")
		require.NoError(t, os.WriteFile(corruptPath, []byte("this is not a zip file at all"), 0644))

		// also add a valid non-archive file
		require.NoError(t, os.WriteFile(filepath.Join(root, "normal.txt"), []byte("hello"), 0644))

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, result.ArchivesFound, "corrupt zip should not pass isArchive filter")
	})

	t.Run("archive with deflate64 compression method is skipped", func(t *testing.T) {
		root := t.TempDir()
		archivePath := filepath.Join(root, "deflate64.zip")
		createDeflate64Archive(t, archivePath, "method9.txt", []byte("payload"))

		uz, files := setup(t, root, false)
		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		require.Len(t, result.Operations, 1)
		op := result.Operations[0]
		assert.True(t, op.Skipped)
		assert.Contains(t, op.SkipReason, "unsupported compression method")
		assert.Contains(t, op.SkipReason, "deflate64")
		assert.Equal(t, 1, result.SkippedCount)
		assert.Equal(t, 0, result.ExtractedArchives)
		assert.Equal(t, 0, result.DeletedArchives)

		_, readErr := os.Stat(filepath.Join(root, "method9.txt"))
		require.Error(t, readErr, "method 9 entry should not be extracted")

		_, statErr := os.Stat(archivePath)
		require.NoError(t, statErr, "deflate64 archive must remain on disk when skipped")
	})

	t.Run("archive with unsupported compression method is skipped", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "unsupported_method_src")
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "unsupported.txt"), []byte("payload"), 0o644))

		archivePath := filepath.Join(root, "unsupported_method.zip")
		createZipArchive(t, srcDir, archivePath)
		require.NoError(t, os.RemoveAll(srcDir))
		setAllZipEntryMethods(t, archivePath, 99)

		uz, files := setup(t, root, false)
		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		require.Len(t, result.Operations, 1)
		op := result.Operations[0]

		assert.True(t, op.Skipped)
		assert.Contains(t, op.SkipReason, "unsupported compression method")
		assert.Contains(t, op.SkipReason, "unknown")
		assert.Equal(t, 1, result.SkippedCount)
		assert.Equal(t, 0, result.ExtractedArchives)
		assert.Equal(t, 0, result.DeletedArchives)

		_, statErr := os.Stat(archivePath)
		require.NoError(t, statErr, "unsupported archive must remain on disk")
		_, readErr := os.Stat(filepath.Join(root, "unsupported.txt"))
		require.Error(t, readErr, "entry should not be extracted when archive is skipped")
	})

	t.Run("multiple archives at same level", func(t *testing.T) {
		root := t.TempDir()

		for i := range 3 {
			srcDir := filepath.Join(root, fmt.Sprintf("src_%d", i))
			require.NoError(t, os.MkdirAll(srcDir, 0755))
			require.NoError(t, os.WriteFile(
				filepath.Join(srcDir, fmt.Sprintf("file_%d.txt", i)),
				[]byte(fmt.Sprintf("content_%d", i)),
				0644,
			))
			createZipArchive(t, srcDir, filepath.Join(root, fmt.Sprintf("archive_%d.zip", i)))
			require.NoError(t, os.RemoveAll(srcDir))
		}

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, result.ArchivesFound, 3, "expected 3 archives")
		assert.Equal(t, 0, result.ErrorCount)

		// verify all extracted files exist
		for i := range 3 {
			content, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("file_%d.txt", i)))
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("content_%d", i), string(content))
		}
	})

	t.Run("archive in subdirectory", func(t *testing.T) {
		root := t.TempDir()
		subDir := filepath.Join(root, "nested", "dir")
		require.NoError(t, os.MkdirAll(subDir, 0755))

		srcDir := filepath.Join(subDir, "src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "nested_file.txt"), []byte("nested"), 0644))

		createZipArchive(t, srcDir, filepath.Join(subDir, "sub_archive.zip"))
		require.NoError(t, os.RemoveAll(srcDir))

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, result.ArchivesFound, 1)
		assert.Equal(t, 0, result.ErrorCount)

		content, err := os.ReadFile(filepath.Join(subDir, "nested_file.txt"))
		require.NoError(t, err)
		assert.Equal(t, "nested", string(content))
	})

	t.Run("uses full test structure with multiple levels", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 3)

		uz, files := setup(t, root, false)
		progress, calls := newProgressTracker()

		result, err := uz.ExtractArchivesWithProgressRecursively(files, progress)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, result.ArchivesFound, 1, "expected at least 1 archive from test structure")
		assert.Equal(t, 0, result.ErrorCount, "expected no errors")
		assert.NotEmpty(t, *calls, "expected progress to be called")
	})

	t.Run("result operations list matches archives processed", func(t *testing.T) {
		root := t.TempDir()

		srcDir := filepath.Join(root, "ops_src")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "op.txt"), []byte("op"), 0644))

		createZipArchive(t, srcDir, filepath.Join(root, "ops.zip"))
		require.NoError(t, os.RemoveAll(srcDir))

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		assert.Equal(t, len(result.Operations), result.ArchivesProcessed,
			"operations count should match archives processed")
	})

	t.Run("empty archive extracts without error", func(t *testing.T) {
		root := t.TempDir()

		archivePath := filepath.Join(root, "empty.zip")
		f, err := os.Create(archivePath)
		require.NoError(t, err)
		zw := zip.NewWriter(f)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())

		uz, files := setup(t, root, false)

		result, err := uz.ExtractArchivesWithProgressRecursively(files, nil)
		require.NoError(t, err)

		assert.Equal(t, 0, result.ErrorCount)
	})
}

func TestIsArchive(t *testing.T) {
	root := t.TempDir()

	zipPath := filepath.Join(root, "valid.zip")
	createZipFile(t, zipPath)

	txtPath := filepath.Join(root, "plain.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte("not a zip"), 0644))

	fakeZipPath := filepath.Join(root, "fake.zip")
	require.NoError(t, os.WriteFile(fakeZipPath, []byte("this is not a zip"), 0644))

	emptyPath := filepath.Join(root, "empty.bin")
	require.NoError(t, os.WriteFile(emptyPath, nil, 0644))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "valid zip archive", path: zipPath, want: true},
		{name: "plain text file", path: txtPath, want: false},
		{name: "fake zip extension", path: fakeZipPath, want: false},
		{name: "empty file", path: emptyPath, want: false},
		{name: "non-existent file", path: filepath.Join(root, "missing.zip"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isArchive(tc.path)
			assert.Equal(t, tc.want, got, "isArchive(%s) returned unexpected result", tc.path)
		})
	}
}

func TestValidateArchiveEntryPath(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		wantErr bool
	}{
		{
			name:    "valid name with double dots",
			entry:   "Tiedostot/Suunnitelma/Isompi kuin -ohjelma..txt",
			wantErr: false,
		},
		{
			name:    "valid non utf8 bytes",
			entry:   "Tiedostot/Blog/Ensimm" + string([]byte{0x84}) + "inen kirjoitus.docx",
			wantErr: false,
		},
		{
			name:    "unix path traversal",
			entry:   "../escape.txt",
			wantErr: true,
		},
		{
			name:    "normalized parent traversal",
			entry:   "a/../b.txt",
			wantErr: true,
		},
		{
			name:    "windows path traversal",
			entry:   `..\\escape.txt`,
			wantErr: true,
		},
		{
			name:    "absolute unix path",
			entry:   "/escape.txt",
			wantErr: true,
		},
		{
			name:    "windows drive absolute path",
			entry:   "C:/escape.txt",
			wantErr: true,
		},
		{
			name:    "windows separators without traversal",
			entry:   `Tiedostot\\Suunnitelma\\safe.txt`,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArchiveEntryPath(tc.entry)
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, "contains path traversal")
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestFilterOnlyArchives(t *testing.T) {
	root := t.TempDir()

	zipPath := filepath.Join(root, "archive.zip")
	createZipFile(t, zipPath)

	zipPath2 := filepath.Join(root, "another.zip")
	createZipFile(t, zipPath2)

	txtPath := filepath.Join(root, "readme.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte("hello"), 0644))

	imgPath := filepath.Join(root, "photo.png")
	require.NoError(t, os.WriteFile(imgPath, []byte("not really a png"), 0644))

	fakeZipPath := filepath.Join(root, "fake.zip")
	require.NoError(t, os.WriteFile(fakeZipPath, []byte("not a zip"), 0644))

	mkInfo := func(path string) collector.FileInfo {
		info, err := os.Stat(path)
		require.NoError(t, err)
		return collector.FileInfo{
			Path:    path,
			Dir:     filepath.Dir(path),
			Name:    filepath.Base(path),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
	}

	t.Run("mixed archives and non-archives", func(t *testing.T) {
		input := []collector.FileInfo{
			mkInfo(zipPath),
			mkInfo(txtPath),
			mkInfo(zipPath2),
			mkInfo(imgPath),
		}

		filtered := filterOnlyArchives(input)
		assert.Len(t, filtered, 2, "expected exactly 2 archives")
		assert.Equal(t, "archive.zip", filtered[0].Name)
		assert.Equal(t, "another.zip", filtered[1].Name)
	})

	t.Run("no archives in input", func(t *testing.T) {
		input := []collector.FileInfo{
			mkInfo(txtPath),
			mkInfo(imgPath),
			mkInfo(fakeZipPath),
		}

		filtered := filterOnlyArchives(input)
		assert.Empty(t, filtered, "expected empty filtered slice")
	})

	t.Run("all archives", func(t *testing.T) {
		input := []collector.FileInfo{
			mkInfo(zipPath),
			mkInfo(zipPath2),
		}

		filtered := filterOnlyArchives(input)
		assert.Len(t, filtered, 2, "expected all entries to be archives")
	})

	t.Run("empty input", func(t *testing.T) {
		filtered := filterOnlyArchives([]collector.FileInfo{})
		assert.Empty(t, filtered, "expected empty filtered slice")
	})

	t.Run("nil input", func(t *testing.T) {
		filtered := filterOnlyArchives(nil)
		assert.Empty(t, filtered, "expected empty filtered slice")
	})

	t.Run("non-existent file in input", func(t *testing.T) {
		input := []collector.FileInfo{
			{
				Path: filepath.Join(root, "missing.zip"),
				Dir:  root,
				Name: "missing.zip",
				Size: 0,
			},
			mkInfo(zipPath),
		}

		filtered := filterOnlyArchives(input)
		assert.Len(t, filtered, 1, "expected only the valid archive")
		assert.Equal(t, "archive.zip", filtered[0].Name)
	})
}

func TestGetAllFilesRecursively(t *testing.T) {
	t.Run("traverse 1 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 1)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.NotEmpty(t, files, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Positive(t, f.Size, "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 31, "expected at least 30 test files + 1 archive")
		assert.LessOrEqual(t, len(files), 51, "expected at most 50 test files + 1 archive")
	})

	t.Run("traverse 5 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 5)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.NotEmpty(t, files, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Positive(t, f.Size, "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 5*31, "expected at least 5*(30 files + 1 archive)")
		assert.LessOrEqual(t, len(files), 5*51, "expected at most 5*(50 files + 1 archive)")
	})

	t.Run("traverse 10 level deep", func(t *testing.T) {
		root := createTestFileAndFolderStructure(t, 10)

		files, err := getAllFilesRecursively(root)
		require.NoError(t, err)
		assert.NotEmpty(t, files, "expected files to be returned")

		for _, f := range files {
			assert.True(t, filepath.IsAbs(f.Path), "expected absolute path, got %s", f.Path)
			rel, err := filepath.Rel(root, f.Path)
			require.NoError(t, err)
			assert.False(t, filepath.IsAbs(rel), "file %s escapes root", f.Path)
			assert.NotEmpty(t, f.Name, "expected non-empty filename")
			assert.Positive(t, f.Size, "expected positive file size for %s", f.Path)
		}

		assert.GreaterOrEqual(t, len(files), 10*30+10, "expected at least 10*30 test files + 10 archives")
		assert.LessOrEqual(t, len(files), 10*50+10, "expected at most 10*50 test files + 10 archives")
	})
}

// createTestFiles generates a specified number of test files (30-50) in the given directory.
func createTestFiles(t *testing.T, rootPath string, level int) {
	t.Helper()

	numFiles := 30 + rand.IntN(21) //nolint:gosec // test fixture; cryptographic randomness not needed
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

// createZipFile creates a minimal valid zip archive at the given path.
func createZipFile(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	zw := zip.NewWriter(f)
	w, err := zw.Create("hello.txt")
	require.NoError(t, err)

	_, err = w.Write([]byte("hello"))
	require.NoError(t, err)

	require.NoError(t, zw.Close())
}

func setAllZipEntryMethods(t *testing.T, archivePath string, method uint16) {
	t.Helper()

	data, err := os.ReadFile(archivePath)
	require.NoError(t, err)

	localSig := []byte("PK\x03\x04")
	centralSig := []byte("PK\x01\x02")

	for offset := 0; ; {
		idx := bytes.Index(data[offset:], localSig)
		if idx < 0 {
			break
		}
		abs := offset + idx
		require.GreaterOrEqual(t, len(data), abs+10)
		binary.LittleEndian.PutUint16(data[abs+8:abs+10], method)
		offset = abs + len(localSig)
	}

	for offset := 0; ; {
		idx := bytes.Index(data[offset:], centralSig)
		if idx < 0 {
			break
		}
		abs := offset + idx
		require.GreaterOrEqual(t, len(data), abs+12)
		binary.LittleEndian.PutUint16(data[abs+10:abs+12], method)
		offset = abs + len(centralSig)
	}

	require.NoError(t, os.WriteFile(archivePath, data, 0o644))
}

func createDeflate64Archive(t *testing.T, archivePath, entryName string, payload []byte) {
	t.Helper()

	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)
	defer archiveFile.Close()

	zw := zip.NewWriter(archiveFile)

	compressed := deflateStoredBlock(t, payload)
	fh := &zip.FileHeader{
		Name:               filepath.ToSlash(entryName),
		Method:             deflate64Method,
		CRC32:              crc32.ChecksumIEEE(payload),
		UncompressedSize64: uint64(len(payload)),
		CompressedSize64:   uint64(len(compressed)),
	}

	w, err := zw.CreateRaw(fh)
	require.NoError(t, err)

	_, err = w.Write(compressed)
	require.NoError(t, err)

	require.NoError(t, zw.Close())
}

func deflateStoredBlock(t *testing.T, payload []byte) []byte {
	t.Helper()
	require.LessOrEqual(t, len(payload), 0xffff)

	length := len(payload)
	block := make([]byte, 5+len(payload))
	block[0] = 0x01
	block[1] = byte(length & 0xff)
	block[2] = byte((length >> 8) & 0xff)
	nlen := ^length & 0xffff
	block[3] = byte(nlen & 0xff)
	block[4] = byte((nlen >> 8) & 0xff)
	copy(block[5:], payload)

	return block
}
