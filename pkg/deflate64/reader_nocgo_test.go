//go:build !cgo

package deflate64

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReader_NoCGOReturnsHelpfulError(t *testing.T) {
	r := newReader(bytes.NewReader([]byte("anything")))
	t.Cleanup(func() {
		require.NoError(t, r.Close())
	})

	n, err := r.Read(make([]byte, 1))
	assert.Zero(t, n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires cgo")

	n, err = r.Read(make([]byte, 1))
	assert.Zero(t, n)
	assert.ErrorIs(t, err, io.EOF)
}
