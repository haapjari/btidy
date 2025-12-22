// Package hasher provides SHA256 file hashing utilities with parallel processing support.
package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
)

const (
	// PartialHashSize is the number of bytes to read from start and end for partial hash.
	PartialHashSize = 4096 // TODO: Why 4096 (?)
	// SmallFileThreshold - files smaller than this skip partial hash and go straight to full hash.
	SmallFileThreshold = PartialHashSize * 2
)

// HashResult contains the result of hashing a single file.
type HashResult struct {
	Path  string
	Hash  string
	Size  int64
	Error error
}

// Hasher computes SHA256 hashes of files with optional parallel processing.
type Hasher struct {
	workers int
}

// TODO: I don't understand this path. We have a type that returns a function. 
// we pass slice of types to a constructor. Explain this constructor process to me.

// Option configures a Hasher.
type Option func(*Hasher)

// WithWorkers sets the number of worker goroutines for parallel hashing.
// Default is runtime.NumCPU().
func WithWorkers(n int) Option {
	return func(h *Hasher) {
		if n > 0 {
			h.workers = n
		}
	}
}

// New creates a new Hasher with the given options.
func New(opts ...Option) *Hasher {
	h := &Hasher{
		workers: runtime.NumCPU(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ComputeHash computes the full SHA256 hash of a file.
func (h *Hasher) ComputeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ComputePartialHash computes hash of first and last PartialHashSize bytes.
// This is much faster than full hash for large files and catches most differences.
// For files smaller than SmallFileThreshold, it hashes the entire file.
func (h *Hasher) ComputePartialHash(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()

	// Read first chunk.
	buf := make([]byte, PartialHashSize)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	hash.Write(buf[:n])

	// Read last chunk (if file is large enough to have distinct last chunk).
	if size > PartialHashSize {
		_, err = f.Seek(-PartialHashSize, io.SeekEnd)
		if err != nil {
			return "", err
		}
		n, err = f.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		hash.Write(buf[:n])
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// HashFiles computes hashes for multiple files concurrently.
// Returns a channel that will receive HashResult for each file.
// The channel is closed when all files have been processed.
func (h *Hasher) HashFiles(paths []string) <-chan HashResult {
	results := make(chan HashResult, h.workers)

	go func() {
		defer close(results)

		// Create work channel
		work := make(chan string, h.workers)

		// Start workers
		var wg sync.WaitGroup
		for range h.workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for path := range work {
					hash, err := h.ComputeHash(path)
					var size int64
					if err == nil {
						if info, statErr := os.Stat(path); statErr == nil {
							size = info.Size()
						}
					}
					results <- HashResult{
						Path:  path,
						Hash:  hash,
						Size:  size,
						Error: err,
					}
				}
			}()
		}

		// Send work
		for _, path := range paths {
			work <- path
		}
		close(work)

		// Wait for all workers to finish
		wg.Wait()
	}()

	return results
}

// HashFilesWithInfo computes hashes for files with known sizes concurrently.
// Uses partial hashing for pre-filtering when files have the same size.
type FileToHash struct {
	Path string
	Size int64
}

// HashFilesWithSizes computes hashes for files with known sizes.
// This allows the hasher to use size information for optimizations.
func (h *Hasher) HashFilesWithSizes(files []FileToHash) <-chan HashResult {
	results := make(chan HashResult, h.workers)

	go func() {
		defer close(results)

		work := make(chan FileToHash, h.workers)

		var wg sync.WaitGroup
		for range h.workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for file := range work {
					hash, err := h.ComputeHash(file.Path)
					results <- HashResult{
						Path:  file.Path,
						Hash:  hash,
						Size:  file.Size,
						Error: err,
					}
				}
			}()
		}

		for _, file := range files {
			work <- file
		}
		close(work)

		wg.Wait()
	}()

	return results
}

// Workers returns the number of worker goroutines configured.
func (h *Hasher) Workers() int {
	return h.workers
}
