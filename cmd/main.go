package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"file-organizer/pkg/collector"
	"file-organizer/pkg/flattener"
	"file-organizer/pkg/renamer"
)

var (
	dryRun  bool
	verbose bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "file-organizer",
		Short: "Organize backup files by renaming and flattening directory structures",
		Long: `file-organizer helps clean up backup directories.

Commands:
  rename   Renames files in place with consistent naming
  flatten  Moves all files to root directory, removes duplicates

Examples:
  # Preview what rename would do (recommended first step)
  file-organizer rename --dry-run /path/to/backup/2018

  # Actually rename files
  file-organizer rename /path/to/backup/2018

  # Preview flatten operation
  file-organizer flatten --dry-run /path/to/backup/2018

  # Flatten directory structure (move all to root, remove duplicates)
  file-organizer flatten /path/to/backup/2018

  # Typical workflow: first rename, then flatten
  file-organizer rename /path/to/backup/2018
  file-organizer flatten /path/to/backup/2018

Safety:
  The tool will NEVER modify files outside the specified directory.
  All operations are contained within the target path.`,
	}

	// Global flags available to all subcommands.
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	// Rename command.
	renameCmd := &cobra.Command{
		Use:   "rename [path]",
		Short: "Rename files with date prefix and sanitized names",
		Long: `Renames files in place with consistent naming:
  - Adds modification date prefix (YYYY-MM-DD_)
  - Converts to lowercase
  - Replaces spaces with underscores
  - Converts Finnish characters (ä→a, ö→o, å→a)

Examples:
  file-organizer rename --dry-run ./backup    # Preview changes
  file-organizer rename ./backup              # Apply changes
  file-organizer rename -v ./backup           # Verbose output

Before: "My Document.pdf" (modified 2018-06-15)
After:  "2018-06-15_my_document.pdf"`,
		Args: cobra.ExactArgs(1),
		RunE: runRename,
	}

	// Flatten command.
	flattenCmd := &cobra.Command{
		Use:   "flatten [path]",
		Short: "Move all files to root directory, remove duplicates",
		Long: `Moves all files to root directory:
  - Removes true duplicates (same name + size + mtime)
  - Adds suffix for name conflicts
  - Deletes empty directories

Examples:
  file-organizer flatten --dry-run ./backup   # Preview changes
  file-organizer flatten ./backup             # Apply changes
  file-organizer flatten -v ./backup          # Verbose output

Before:
  backup/
    Documents/Work/report.pdf
    Photos/Vacation/photo.jpg
    Music/song.mp3

After:
  backup/
    report.pdf
    photo.jpg
    song.mp3`,
		Args: cobra.ExactArgs(1),
		RunE: runFlatten,
	}

	rootCmd.AddCommand(renameCmd)
	rootCmd.AddCommand(flattenCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

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

func runRename(_ *cobra.Command, args []string) error {
	absPath, err := validateAndResolvePath(args[0])
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Println("=== DRY RUN - no changes will be made ===")
		fmt.Println()
	}

	fmt.Printf("Command: RENAME\n")
	fmt.Printf("Root directory: %s\n", absPath)
	fmt.Println("Collecting files...")
	progress := startProgress("Working", 5*time.Second)
	startTime := time.Now()

	c := collector.New(collector.Options{
		SkipFiles: []string{".DS_Store", "Thumbs.db", "organizer.log"},
	})

	files, err := c.Collect(absPath)
	if err != nil {
		progress.Stop()
		return fmt.Errorf("failed to collect files: %w", err)
	}

	fmt.Printf("Found %d files in %v\n\n", len(files), time.Since(startTime).Round(time.Millisecond))

	if len(files) == 0 {
		progress.Stop()
		fmt.Println("No files to process.")
		return nil
	}

	r, err := renamer.New(absPath, dryRun)
	if err != nil {
		progress.Stop()
		return fmt.Errorf("failed to create renamer: %w", err)
	}

	result := r.RenameFiles(files)
	progress.Stop()

	if verbose || dryRun {
		for _, op := range result.Operations {
			if op.Skipped {
				fmt.Printf("SKIP: %s (%s)\n", op.OriginalName, op.SkipReason)
			} else if op.Error != nil {
				fmt.Printf("ERROR: %s -> %s: %v\n", op.OriginalName, op.NewName, op.Error)
			} else {
				fmt.Printf("RENAME: %s\n", op.OriginalPath)
				fmt.Printf("    TO: %s\n", op.NewPath)
			}
		}
		fmt.Println()
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Total files:  %d\n", result.TotalFiles)
	fmt.Printf("Renamed:      %d\n", result.RenamedCount)
	fmt.Printf("Skipped:      %d\n", result.SkippedCount)
	fmt.Printf("Deleted:      %d\n", result.DeletedCount)
	fmt.Printf("Errors:       %d\n", result.ErrorCount)

	if dryRun {
		fmt.Println()
		fmt.Println("Run without --dry-run to apply changes.")
	}

	return nil
}

func runFlatten(_ *cobra.Command, args []string) error {
	absPath, err := validateAndResolvePath(args[0])
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Println("=== DRY RUN - no changes will be made ===")
		fmt.Println()
	}

	fmt.Printf("Command: FLATTEN\n")
	fmt.Printf("Root directory: %s\n", absPath)
	fmt.Println("Collecting files...")
	progress := startProgress("Working", 5*time.Second)
	startTime := time.Now()

	c := collector.New(collector.Options{
		SkipFiles: []string{".DS_Store", "Thumbs.db", "organizer.log"},
	})

	files, err := c.Collect(absPath)
	if err != nil {
		progress.Stop()
		return fmt.Errorf("failed to collect files: %w", err)
	}

	fmt.Printf("Found %d files in %v\n\n", len(files), time.Since(startTime).Round(time.Millisecond))

	if len(files) == 0 {
		progress.Stop()
		fmt.Println("No files to process.")
		return nil
	}

	f, err := flattener.New(absPath, dryRun)
	if err != nil {
		progress.Stop()
		return fmt.Errorf("failed to create flattener: %w", err)
	}

	result := f.FlattenFiles(files)
	progress.Stop()

	if verbose || dryRun {
		for _, op := range result.Operations {
			printFlattenOperation(op)
		}
		fmt.Println()
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Total files:     %d\n", result.TotalFiles)
	fmt.Printf("Moved:           %d\n", result.MovedCount)
	fmt.Printf("Duplicates:      %d\n", result.DuplicatesCount)
	fmt.Printf("Skipped:         %d\n", result.SkippedCount)
	fmt.Printf("Errors:          %d\n", result.ErrorCount)
	if !dryRun {
		fmt.Printf("Dirs removed:    %d\n", result.DeletedDirsCount)
	}

	if dryRun {
		fmt.Println()
		fmt.Println("Run without --dry-run to apply changes.")
	}

	return nil
}

func printFlattenOperation(op flattener.MoveOperation) {
	switch {
	case op.Error != nil:
		fmt.Printf("ERROR: %s: %v\n", op.OriginalPath, op.Error)
	case op.Duplicate:
		fmt.Printf("DUPLICATE: %s\n", op.OriginalPath)
		fmt.Printf("   KEPT: %s\n", op.NewPath)
	case op.Skipped:
		fmt.Printf("SKIP: %s (%s)\n", op.OriginalPath, op.SkipReason)
	default:
		fmt.Printf("MOVE: %s\n", op.OriginalPath)
		fmt.Printf("  TO: %s\n", op.NewPath)
	}
}

type progressReporter struct {
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func startProgress(label string, interval time.Duration) *progressReporter {
	p := &progressReporter{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	startTime := time.Now()
	ticker := time.NewTicker(interval)

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
