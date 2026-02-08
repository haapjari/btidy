package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"btidy/pkg/unzipper"
	"btidy/pkg/usecase"
)

func buildUnzipCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unzip [path]",
		Short: "Extract zip archives recursively and remove extracted archives",
		Long: `Extracts .zip archives recursively:
  - Finds all .zip files in the target directory tree
  - Extracts archive contents in place (next to each archive)
  - Recursively extracts nested zip archives
  - Removes each archive only after successful extraction

Safety:
  - Rejects archive entries that escape the target directory
  - Rejects symlink archive entries
  - Keeps source archive if extraction fails

Examples:
  btidy unzip --dry-run ./backup     # Preview extraction plan
  btidy unzip ./backup               # Extract archives and remove them
  btidy unzip -v ./backup            # Verbose operation output`,
		Args: cobra.ExactArgs(1),
		RunE: runUnzip,
	}
}

func runUnzip(_ *cobra.Command, args []string) error {
	execution, empty, err := runFileCommand(
		"UNZIP",
		true,
		func(progress *progressReporter) (usecase.UnzipExecution, error) {
			return newUseCaseService().RunUnzip(usecase.UnzipRequest{
				TargetDir: args[0],
				DryRun:    dryRun,
				OnProgress: func(stage string, processed, total int) {
					progress.Report(stage, processed, total)
				},
			})
		},
		func(execution usecase.UnzipExecution) fileCommandExecutionInfo {
			return infoFromMeta(execution.Meta())
		},
		nil,
	)
	if err != nil {
		return err
	}
	if empty {
		return nil
	}

	result := execution.Result
	if result.ArchivesFound == 0 {
		fmt.Println("No zip archives to process.")
		fmt.Println()
	}

	printDetailedOperations(result.Operations, printUnzipOperation)

	printSummary(
		fmt.Sprintf("Total files:        %d", result.TotalFiles),
		fmt.Sprintf("Archives found:     %d", result.ArchivesFound),
		fmt.Sprintf("Archives processed: %d", result.ArchivesProcessed),
		fmt.Sprintf("Archives extracted: %d", result.ExtractedArchives),
		fmt.Sprintf("Archives deleted:   %d", result.DeletedArchives),
		fmt.Sprintf("Files extracted:    %d", result.ExtractedFiles),
		fmt.Sprintf("Dir entries:        %d", result.ExtractedDirs),
		fmt.Sprintf("Errors:             %d", result.ErrorCount),
	)
	printDryRunHint()

	return nil
}

func printUnzipOperation(op unzipper.ExtractOperation) {
	switch {
	case op.Error != nil:
		fmt.Printf("ERROR: %s: %v\n", op.ArchivePath, op.Error)
	case op.Skipped:
		fmt.Printf("SKIP: %s (%s)\n", op.ArchivePath, op.SkipReason)
	default:
		fmt.Printf("UNZIP: %s\n", op.ArchivePath)
		fmt.Printf(" FILES: %d\n", op.ExtractedFiles)
		fmt.Printf("  DIRS: %d\n", op.ExtractedDirs)
		if op.NestedArchives > 0 {
			fmt.Printf("NESTED: %d\n", op.NestedArchives)
		}
		if op.DeletedArchive {
			if dryRun {
				fmt.Println("DELETE: source archive (dry-run)")
			} else {
				fmt.Println("DELETE: source archive")
			}
		} else {
			fmt.Println("DELETE: source archive not removed")
		}
	}
}
