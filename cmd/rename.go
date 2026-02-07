package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"file-organizer/pkg/renamer"
)

func buildRenameCommand() *cobra.Command {
	return &cobra.Command{
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
}

func runRename(_ *cobra.Command, args []string) error {
	absPath, err := validateAndResolvePath(args[0])
	if err != nil {
		return err
	}

	printDryRunBanner()
	printCommandHeader("RENAME", absPath)

	files, progress, err := collectFilesForCommand(absPath, true)
	if err != nil {
		return err
	}

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

	printSummary(
		fmt.Sprintf("Total files:  %d", result.TotalFiles),
		fmt.Sprintf("Renamed:      %d", result.RenamedCount),
		fmt.Sprintf("Skipped:      %d", result.SkippedCount),
		fmt.Sprintf("Deleted:      %d", result.DeletedCount),
		fmt.Sprintf("Errors:       %d", result.ErrorCount),
	)
	printDryRunHint()

	return nil
}
