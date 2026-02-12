package unzipper

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchedReaderAt(t *testing.T) {
	base := bytes.NewReader([]byte("abcdefghijkl"))
	r := &patchedReaderAt{
		base:        base,
		patchOffset: 3,
		patchBytes:  []byte("XYZ"),
	}

	buf := make([]byte, 12)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 12, n)
	assert.Equal(t, "abcXYZghijkl", string(buf))

	buf = make([]byte, 4)
	n, err = r.ReadAt(buf, 2)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	assert.Equal(t, "cXYZ", string(buf))
}

func TestDetectZip64LocatorTotalDisksZero(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "zip64.zip")
	createSparseZip64Archive(t, archivePath)

	offset, found, err := detectZip64LocatorTotalDisksZero(archivePath)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Zero(t, offset)

	locatorOffset := setZip64LocatorTotalDisks(t, archivePath, 0)

	offset, found, err = detectZip64LocatorTotalDisksZero(archivePath)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, locatorOffset, offset)
}

func TestOpenArchiveReaderWithZip64LocatorCompatibility(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "zip64_disks_zero.zip")
	createSparseZip64Archive(t, archivePath)
	setZip64LocatorTotalDisks(t, archivePath, 0)

	standardReader, standardErr := zip.OpenReader(archivePath)
	if standardReader != nil {
		_ = standardReader.Close()
	}
	require.Error(t, standardErr)
	require.ErrorIs(t, standardErr, zip.ErrFormat)

	reader, err := openArchiveReader(archivePath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, reader.Close())
	})

	if assert.Len(t, reader.files, 1) {
		assert.Equal(t, "hello.txt", reader.files[0].Name)
	}

	rc, err := reader.files[0].Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rc.Close())
	})

	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "hello zip64", string(content))

	assert.True(t, isArchive(archivePath))
}

func createSparseZip64Archive(t *testing.T, archivePath string) {
	t.Helper()

	const sparseOffset = int64(zip32Marker) + 128

	f, err := os.Create(archivePath)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()

	_, err = f.Seek(sparseOffset, io.SeekStart)
	require.NoError(t, err)

	zw := zip.NewWriter(f)
	zw.SetOffset(sparseOffset)

	w, err := zw.Create("hello.txt")
	require.NoError(t, err)

	_, err = w.Write([]byte("hello zip64"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
}

func setZip64LocatorTotalDisks(t *testing.T, archivePath string, totalDisks uint32) int64 {
	t.Helper()

	f, err := os.OpenFile(archivePath, os.O_RDWR, 0)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()

	info, err := f.Stat()
	require.NoError(t, err)

	locator, found, err := readZip64LocatorRecord(f, info.Size())
	require.NoError(t, err)
	require.True(t, found)

	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], totalDisks)

	_, err = f.WriteAt(raw[:], locator.offset+zip64LocatorTotalDisksFieldOffset)
	require.NoError(t, err)

	return locator.offset
}
