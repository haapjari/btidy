package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"file-organizer/pkg/deduplicator"
)

func buildDuplicateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "duplicate [path]",
		Short: "Find and remove duplicate files by content hash",
		Long: `Finds and removes duplicate files using content hashing:
  - Groups files by size (fast pre-filter)
  - Computes SHA256 hash to identify true duplicates
  - Uses partial hashing for large files (performance optimization)
  - Keeps one copy, removes the rest

This is safe and reliable - files are only considered duplicates
if their content is byte-for-byte identical (verified by SHA256).

Examples:
  file-organizer duplicate --dry-run ./backup   # Preview (recommended!)
  file-organizer duplicate ./backup             # Apply changes
  file-organizer duplicate -v ./backup          # Verbose output

Use --dry-run first to review what would be deleted!`,
		Args: cobra.ExactArgs(1),
		RunE: runDuplicate,
	}
}

func runDuplicate(_ *cobra.Command, args []string) error {
	absPath, err := validateAndResolvePath(args[0])
	if err != nil {
		return err
	}

	printDryRunBanner()
	printCommandHeader("DUPLICATE", absPath)

	files, progress, err := collectFilesForCommand(absPath, false)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		progress.Stop()
		fmt.Println("No files to process.")
		return nil
	}

	fmt.Println("Computing hashes and finding duplicates...")

	d, err := deduplicator.New(absPath, dryRun)
	if err != nil {
		progress.Stop()
		return fmt.Errorf("failed to create deduplicator: %w", err)
	}

	result := d.FindDuplicates(files)
	progress.Stop()

	if verbose || dryRun {
		for _, op := range result.Operations {
			printDuplicateOperation(op)
		}
		fmt.Println()
	}

	printSummary(
		fmt.Sprintf("Total files:      %d", result.TotalFiles),
		fmt.Sprintf("Duplicates found: %d", result.DuplicatesFound),
		fmt.Sprintf("Deleted:          %d", result.DeletedCount),
		fmt.Sprintf("Skipped:          %d", result.SkippedCount),
		fmt.Sprintf("Errors:           %d", result.ErrorCount),
		"Space recovered:  "+formatBytes(result.BytesRecovered),
	)
	printDryRunHint()

	return nil
}

func printDuplicateOperation(op deduplicator.DeleteOperation) {
	switch {
	case op.Error != nil:
		fmt.Printf("ERROR: %s: %v\n", op.Path, op.Error)
	case op.Skipped:
		fmt.Printf("SKIP: %s (%s)\n", op.Path, op.SkipReason)
	default:
		fmt.Printf("DELETE: %s\n", op.Path)
		fmt.Printf("   KEPT: %s\n", op.OriginalOf)
		if verbose {
			fmt.Printf("   HASH: %s\n", op.Hash)
		}
	}
}
