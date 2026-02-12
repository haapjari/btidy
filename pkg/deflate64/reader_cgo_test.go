//go:build cgo

package deflate64

import (
	"bytes"
	"compress/flate"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReader_TruncatedInputReturnsUnexpectedEOF(t *testing.T) {
	payload := []byte("hello")
	stream := deflateStream(t, payload)
	require.Greater(t, len(stream), 1)
	truncated := stream[:len(stream)-1]

	r := newReader(bytes.NewReader(truncated))
	t.Cleanup(func() {
		require.NoError(t, r.Close())
	})

	decoded, err := io.ReadAll(r)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode")
	assert.Less(t, len(decoded), len(payload))
}

func TestReader_SourceNoProgressReturnsErrNoProgress(t *testing.T) {
	r := newReader(noProgressReader{})
	t.Cleanup(func() {
		require.NoError(t, r.Close())
	})

	n, err := r.Read(make([]byte, 8))
	assert.Zero(t, n)
	require.Error(t, err)
	assert.ErrorIs(t, err, io.ErrNoProgress)
}

func TestReader_ReadAfterCloseReturnsError(t *testing.T) {
	r := newReader(bytes.NewReader(deflateStoredBlock(t, []byte("ok"))))
	require.NoError(t, r.Close())

	n, err := r.Read(make([]byte, 1))
	assert.Zero(t, n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read after close")
}

type noProgressReader struct{}

func (noProgressReader) Read([]byte) (int, error) {
	return 0, nil
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

func deflateStream(t *testing.T, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)

	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return buf.Bytes()
}
