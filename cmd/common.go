package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"file-organizer/pkg/collector"
)

var defaultSkipFiles = []string{".DS_Store", "Thumbs.db", "organizer.log"}

func validateAndResolvePath(targetDir string) (string, error) {
	// Validate directory exists.
	info, err := os.Stat(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", targetDir)
	}

	// Convert to absolute path.
	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %w", err)
	}

	return absPath, nil
}

func skipFiles() []string {
	return append([]string(nil), defaultSkipFiles...)
}

func newFileCollector() *collector.Collector {
	return collector.New(collector.Options{
		SkipFiles: skipFiles(),
	})
}

func collectFiles(rootDir string) ([]collector.FileInfo, error) {
	return newFileCollector().Collect(rootDir)
}

func collectFilesForCommand(rootDir string, trailingBlankLine bool) ([]collector.FileInfo, *progressReporter, error) {
	printCollectingFiles()

	progress := startProgress("Working")
	startTime := time.Now()

	files, err := collectFiles(rootDir)
	if err != nil {
		progress.Stop()
		return nil, nil, fmt.Errorf("failed to collect files: %w", err)
	}

	printFoundFiles(len(files), startTime, trailingBlankLine)

	return files, progress, nil
}

func printDryRunBanner() {
	if !dryRun {
		return
	}

	fmt.Println("=== DRY RUN - no changes will be made ===")
	fmt.Println()
}

func printCommandHeader(command, rootDir string) {
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("Root directory: %s\n", rootDir)
}

func printCollectingFiles() {
	fmt.Println("Collecting files...")
}

func printFoundFiles(fileCount int, startTime time.Time, trailingBlankLine bool) {
	fmt.Printf("Found %d files in %v\n", fileCount, time.Since(startTime).Round(time.Millisecond))
	if trailingBlankLine {
		fmt.Println()
	}
}

func printSummary(lines ...string) {
	fmt.Println("=== Summary ===")
	for _, line := range lines {
		fmt.Println(line)
	}
}

func printDryRunHint() {
	if !dryRun {
		return
	}

	fmt.Println()
	fmt.Println("Run without --dry-run to apply changes.")
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

type progressReporter struct {
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func startProgress(label string) *progressReporter {
	p := &progressReporter{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	startTime := time.Now()
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		defer close(p.doneCh)
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime).Round(time.Second)
				fmt.Fprintf(os.Stderr, "%s... %s elapsed\n", label, elapsed)
			case <-p.stopCh:
				ticker.Stop()
				return
			}
		}
	}()

	return p
}

func (p *progressReporter) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		<-p.doneCh
	})
}
