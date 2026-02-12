//go:build cgo

package deflate64

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/zlib -I${SRCDIR}/../../third_party/zlib/contrib/infback9
#include "deflate64_bridge.h"
#include "zlib.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

// deflate64InputChunkSize is the size of the input buffer used when reading
// compressed data from the source, set to 32 KiB.
const deflate64InputChunkSize = 32 << 10

// decodeProgress reports the outcome of a single Inflate call, indicating how
// many bytes of input and output were consumed/produced and whether more input,
// more output space, or neither is needed to continue.
type decodeProgress struct {
	inputUsed   int
	outputUsed  int
	needsInput  bool
	needsOutput bool
	finished    bool
}

// decoder wraps the C deflate64 decompression state.
type decoder struct {
	state *C.btidy_deflate64_state
}

// newDecoder allocates and initializes a new deflate64 decoder. The caller must
// call Close when the decoder is no longer needed to free the underlying C
// resources.
func newDecoder() (*decoder, error) {
	state := C.btidy_deflate64_new()
	if state == nil {
		return nil, errors.New("create deflate64 decoder: insufficient memory")
	}

	if code := C.btidy_deflate64_init(state); code != C.Z_OK {
		defer C.btidy_deflate64_free(state)
		return nil, fmt.Errorf("initialize deflate64 decoder: %s", decodeErrorMessage(state, code))
	}

	return &decoder{state: state}, nil
}

// Close releases the underlying C deflate64 state. It is safe to call Close on
// a nil decoder or one that has already been closed.
func (d *decoder) Close() {
	if d == nil || d.state == nil {
		return
	}

	C.btidy_deflate64_free(d.state)
	d.state = nil
}

// Inflate decompresses data from input into output. inputEOF should be true
// when input contains the final bytes from the compressed stream. It returns a
// decodeProgress describing how many bytes were consumed and produced, along
// with any decompression error.
func (d *decoder) Inflate(input, output []byte, inputEOF bool) (decodeProgress, error) {
	var inPtr *C.uint8_t
	if len(input) > 0 {
		inPtr = (*C.uint8_t)(unsafe.Pointer(&input[0]))
	}

	var outPtr *C.uint8_t
	if len(output) > 0 {
		outPtr = (*C.uint8_t)(unsafe.Pointer(&output[0]))
	}

	var (
		inputUsed   C.size_t
		outputUsed  C.size_t
		needsInput  C.int
		needsOutput C.int
		finished    C.int
		inputEOFInt C.int
	)

	if inputEOF {
		inputEOFInt = 1
	}

	code := C.btidy_deflate64_inflate(
		d.state,
		inPtr,
		C.size_t(len(input)),
		inputEOFInt,
		outPtr,
		C.size_t(len(output)),
		&inputUsed,
		&outputUsed,
		&needsInput,
		&needsOutput,
		&finished,
	)

	progress := decodeProgress{
		inputUsed:   int(inputUsed),
		outputUsed:  int(outputUsed),
		needsInput:  needsInput != 0,
		needsOutput: needsOutput != 0,
		finished:    finished != 0,
	}

	if code != C.Z_OK {
		return progress, fmt.Errorf("deflate64 decode failed: %s", decodeErrorMessage(d.state, code))
	}

	return progress, nil
}

// decodeErrorMessage extracts a human-readable error message from the C
// deflate64 state for the given error code. It returns "unknown error" if the
// state provides no message.
func decodeErrorMessage(state *C.btidy_deflate64_state, code C.int) string {
	msg := C.btidy_deflate64_error_message(state, code)
	if msg == nil {
		return "unknown error"
	}

	return C.GoString(msg)
}

// reader implements io.ReadCloser for deflate64-compressed data. It reads
// compressed bytes from src, decompresses them through a decoder, and returns
// plaintext bytes to the caller.
type reader struct {
	src      io.Reader
	decoder  *decoder
	inBuf    []byte
	inStart  int
	inEnd    int
	srcEOF   bool
	finished bool
	closed   bool
	err      error
}

// shortCircuitResult is used internally by reader.Read to handle early-return
// conditions such as reads after close, zero-length reads, deferred errors, and
// stream completion.
type shortCircuitResult struct {
	n    int
	done bool
	err  error
}

// newReader returns an io.ReadCloser that decompresses deflate64-compressed
// data read from src. If the decoder cannot be created, the returned
// ReadCloser will report the error on the first Read call.
func newReader(src io.Reader) io.ReadCloser {
	d, err := newDecoder()
	if err != nil {
		return &errorReadCloser{err: err}
	}

	return &reader{
		src:     src,
		decoder: d,
		inBuf:   make([]byte, deflate64InputChunkSize),
	}
}

// Read decompresses data into dst, returning the number of bytes written and
// any error. It implements the io.Reader interface. When the compressed stream
// is fully consumed, subsequent calls return 0, io.EOF. Errors are deferred
// when bytes have already been produced so the caller can process partial output
// before seeing the error on the next call.
func (r *reader) Read(dst []byte) (int, error) {
	if short := r.tryReadShortCircuit(dst); short.done {
		return short.n, short.err
	}

	produced := 0
	for {
		ensureErr := r.ensureInput()
		if ensureErr != nil {
			return r.returnWithDeferredError(produced, ensureErr)
		}

		input := r.inBuf[r.inStart:r.inEnd]
		progress, inflateErr := r.decoder.Inflate(input, dst[produced:], r.srcEOF)
		r.inStart += progress.inputUsed
		produced += progress.outputUsed

		if inflateErr != nil {
			return r.returnWithDeferredError(produced, inflateErr)
		}

		done, resultErr := r.finishDecodeStep(progress, len(input) == 0, produced)
		if done {
			if resultErr != nil {
				return r.returnWithDeferredError(produced, resultErr)
			}

			return produced, nil
		}
	}
}

// tryReadShortCircuit checks for conditions that allow Read to return
// immediately without performing any decompression: closed reader, zero-length
// destination, a deferred error from a previous call, or a finished stream.
func (r *reader) tryReadShortCircuit(dst []byte) shortCircuitResult {
	if r.closed {
		return shortCircuitResult{
			done: true,
			err:  errors.New("deflate64: read after close"),
		}
	}

	if len(dst) == 0 {
		return shortCircuitResult{done: true}
	}

	if r.err != nil {
		err := r.err
		r.err = nil
		return shortCircuitResult{
			done: true,
			err:  err,
		}
	}

	if r.finished {
		return shortCircuitResult{
			done: true,
			err:  io.EOF,
		}
	}

	return shortCircuitResult{}
}

// ensureInput guarantees that the internal input buffer contains unprocessed
// compressed data. If the buffer is empty it refills it from the source reader.
func (r *reader) ensureInput() error {
	if r.inStart != r.inEnd {
		return nil
	}

	return r.fillInput()
}

// returnWithDeferredError returns produced bytes without an error when bytes
// have already been written to the caller's buffer, saving the error for the
// next Read call. When no bytes were produced the error is returned immediately.
func (r *reader) returnWithDeferredError(produced int, err error) (int, error) {
	if produced > 0 {
		r.err = err
		return produced, nil
	}

	return 0, err
}

// finishDecodeStep evaluates the result of a single Inflate call and decides
// whether the Read loop should continue or return. It handles stream
// completion, output availability, input exhaustion, and stall detection.
func (r *reader) finishDecodeStep(
	progress decodeProgress,
	noInput bool,
	produced int,
) (done bool, resultErr error) {
	if progress.finished {
		r.finished = true
		if produced == 0 {
			return true, io.EOF
		}

		return true, nil
	}

	if produced > 0 {
		return true, nil
	}

	if progress.needsInput {
		if r.srcEOF {
			return true, io.ErrUnexpectedEOF
		}

		return false, nil
	}

	if progress.needsOutput {
		return true, errors.New("deflate64: output buffer too small for progress")
	}

	if r.srcEOF && noInput {
		return true, io.ErrUnexpectedEOF
	}

	return true, errors.New("deflate64: decoder made no progress")
}

// Close releases the underlying decoder resources. It is safe to call Close
// multiple times; subsequent calls are no-ops.
func (r *reader) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true
	r.decoder.Close()
	return nil
}

// fillInput reads a chunk of compressed data from the source into the internal
// buffer. It sets srcEOF when the source is exhausted and returns
// io.ErrNoProgress if the source returns zero bytes without an error.
func (r *reader) fillInput() error {
	if r.srcEOF {
		return nil
	}

	n, err := r.src.Read(r.inBuf)
	r.inStart = 0
	r.inEnd = n

	if err == io.EOF {
		r.srcEOF = true
		return nil
	}

	if err != nil {
		return err
	}

	if n == 0 {
		return io.ErrNoProgress
	}

	return nil
}

// errorReadCloser is an io.ReadCloser that returns a stored error on the first
// Read call and io.EOF on subsequent reads. It is used as a fallback when
// decoder creation fails in newReader.
type errorReadCloser struct {
	err error
}

// Read returns the stored error on the first call and io.EOF thereafter.
func (r *errorReadCloser) Read([]byte) (int, error) {
	if r.err == nil {
		return 0, io.EOF
	}

	err := r.err
	r.err = nil
	return 0, err
}

// Close is a no-op that satisfies the io.Closer interface.
func (r *errorReadCloser) Close() error {
	return nil
}
