//go:build !cgo

package deflate64

import (
	"errors"
	"io"
)

func newReader(_ io.Reader) io.ReadCloser {
	return &errorReadCloser{err: errors.New("deflate64 requires cgo support")}
}

type errorReadCloser struct {
	err error
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	if r.err == nil {
		return 0, io.EOF
	}

	err := r.err
	r.err = nil
	return 0, err
}

func (r *errorReadCloser) Close() error {
	return nil
}
