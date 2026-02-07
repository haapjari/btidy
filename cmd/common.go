package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"btidy/pkg/usecase"
)

var defaultSkipFiles = []string{".DS_Store", "Thumbs.db", "organizer.log"}

func skipFiles() []string {
	return append([]string(nil), defaultSkipFiles...)
}

func newUseCaseService() *usecase.Service {
	return usecase.New(usecase.Options{
		SkipFiles: skipFiles(),
	})
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

func printFoundFiles(fileCount int, elapsed time.Duration, trailingBlankLine bool) {
	fmt.Printf("Found %d files in %v\n", fileCount, elapsed.Round(time.Millisecond))
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
