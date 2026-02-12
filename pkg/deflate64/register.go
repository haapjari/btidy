package deflate64

import (
	"archive/zip"
	"fmt"
	"sync"
)

// Method is the ZIP compression method ID for Deflate64 (PKWare method 9).
const Method uint16 = 9

var (
	registerOnce sync.Once
	registerErr  error
)

// Register installs Deflate64 support into archive/zip.
func Register() error {
	registerOnce.Do(func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				registerErr = fmt.Errorf("register deflate64 decompressor: %v", recovered)
			}
		}()

		zip.RegisterDecompressor(Method, newReader)
	})

	return registerErr
}
