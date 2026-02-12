package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const upstreamTag = "v1.3.1"

var zlibFiles = []string{
	"LICENSE",
	"zlib.h",
	"zconf.h",
	"zutil.h",
	"contrib/infback9/infback9.c",
	"contrib/infback9/infback9.h",
	"contrib/infback9/inflate9.h",
	"contrib/infback9/inftree9.c",
	"contrib/infback9/inftree9.h",
	"contrib/infback9/inffix9.h",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	vendorRoot := filepath.Join(wd, "third_party", "zlib")
	client := &http.Client{Timeout: 30 * time.Second}

	allIdentical := true
	for _, rel := range zlibFiles {
		if err := verifyFile(client, vendorRoot, rel); err != nil {
			allIdentical = false
			fmt.Println(err.Error())
		}
	}

	if !allIdentical {
		return errors.New("one or more vendored zlib files differ from upstream")
	}

	fmt.Printf(
		"verified %d zlib files: all are byte-identical to madler/zlib %s\n",
		len(zlibFiles),
		upstreamTag,
	)

	return nil
}

func verifyFile(client *http.Client, vendorRoot, rel string) error {
	localPath := filepath.Join(vendorRoot, filepath.FromSlash(rel))
	localBytes, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("MISSING %s (%w)", rel, err)
	}

	url := fmt.Sprintf(
		"https://raw.githubusercontent.com/madler/zlib/%s/%s",
		upstreamTag,
		rel,
	)

	remoteBytes, err := fetchURL(client, url)
	if err != nil {
		return fmt.Errorf("FETCH-ERROR %s (%w)", rel, err)
	}

	if bytes.Equal(localBytes, remoteBytes) {
		fmt.Printf("IDENTICAL %s\n", rel)
		return nil
	}

	return fmt.Errorf(
		"DIFFERS %s local=%s upstream=%s",
		rel,
		sha256Hex(localBytes),
		sha256Hex(remoteBytes),
	)
}

func fetchURL(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return body, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
