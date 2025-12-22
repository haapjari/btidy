// Package manifest provides file inventory generation and verification.
// A manifest is a cryptographic inventory of all files in a directory,
// which can be used to verify that no data was lost after operations.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/hasher"
)

// ManifestEntry represents a single file in the manifest.
type ManifestEntry struct {
	Path    string    `json:"path"`
	Hash    string    `json:"hash"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

// Manifest represents a complete file inventory.
type Manifest struct {
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	RootPath  string          `json:"root_path"`
	Entries   []ManifestEntry `json:"entries"`
}

// ProgressCallback is called during manifest generation to report progress.
type ProgressCallback func(processed, total int, currentFile string)

// GenerateOptions configures manifest generation.
type GenerateOptions struct {
	SkipFiles  []string
	Workers    int
	OnProgress ProgressCallback
}

// Generator creates manifests from directories.
type Generator struct {
	rootDir string
	hasher  *hasher.Hasher
}

// NewGenerator creates a new manifest generator for the given directory.
func NewGenerator(rootDir string, workers int) (*Generator, error) {
	absPath, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", absPath)
	}

	opts := []hasher.Option{}
	if workers > 0 {
		opts = append(opts, hasher.WithWorkers(workers))
	}

	return &Generator{
		rootDir: absPath,
		hasher:  hasher.New(opts...),
	}, nil
}

// Generate creates a manifest of all files in the directory.
func (g *Generator) Generate(opts GenerateOptions) (*Manifest, error) {
	// Collect all files
	c := collector.New(collector.Options{
		SkipFiles: opts.SkipFiles,
	})

	files, err := c.Collect(g.rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect files: %w", err)
	}

	manifest := &Manifest{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		RootPath:  g.rootDir,
		Entries:   make([]ManifestEntry, 0, len(files)),
	}

	if len(files) == 0 {
		return manifest, nil
	}

	// Prepare files for parallel hashing
	toHash := make([]hasher.FileToHash, len(files))
	for i, f := range files {
		toHash[i] = hasher.FileToHash{
			Path: f.Path,
			Size: f.Size,
		}
	}

	// Create a map for quick lookup of file info by path
	fileInfoByPath := make(map[string]collector.FileInfo, len(files))
	for _, f := range files {
		fileInfoByPath[f.Path] = f
	}

	// Hash files in parallel
	results := g.hasher.HashFilesWithSizes(toHash)

	processed := 0
	total := len(files)

	for result := range results {
		processed++

		if result.Error != nil {
			// Skip files we can't read, but continue
			continue
		}

		fileInfo := fileInfoByPath[result.Path]

		// Make path relative to root for portability
		relPath, err := filepath.Rel(g.rootDir, result.Path)
		if err != nil {
			relPath = result.Path
		}

		manifest.Entries = append(manifest.Entries, ManifestEntry{
			Path:    relPath,
			Hash:    result.Hash,
			Size:    fileInfo.Size,
			ModTime: fileInfo.ModTime,
		})

		if opts.OnProgress != nil {
			opts.OnProgress(processed, total, relPath)
		}
	}

	// Sort entries by path for deterministic output
	sort.Slice(manifest.Entries, func(i, j int) bool {
		return manifest.Entries[i].Path < manifest.Entries[j].Path
	})

	return manifest, nil
}

// Save writes the manifest to a file as pretty-printed JSON.
func (m *Manifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

// Load reads a manifest from a JSON file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// UniqueHashes returns the set of unique content hashes in the manifest.
func (m *Manifest) UniqueHashes() map[string]struct{} {
	hashes := make(map[string]struct{}, len(m.Entries))
	for _, entry := range m.Entries {
		hashes[entry.Hash] = struct{}{}
	}
	return hashes
}

// HashIndex returns a map of hash -> []paths for quick lookup.
// This is useful for finding all files with the same content.
func (m *Manifest) HashIndex() map[string][]string {
	index := make(map[string][]string)
	for _, entry := range m.Entries {
		index[entry.Hash] = append(index[entry.Hash], entry.Path)
	}
	return index
}

// TotalSize returns the total size of all files in the manifest.
func (m *Manifest) TotalSize() int64 {
	var total int64
	for _, entry := range m.Entries {
		total += entry.Size
	}
	return total
}

// FileCount returns the number of files in the manifest.
func (m *Manifest) FileCount() int {
	return len(m.Entries)
}

// UniqueFileCount returns the number of unique files (by content hash).
func (m *Manifest) UniqueFileCount() int {
	return len(m.UniqueHashes())
}
