package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"btidy/pkg/renamer"
	"btidy/pkg/usecase"
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
  btidy rename --dry-run ./backup    # Preview changes
  btidy rename ./backup              # Apply changes
  btidy rename -v ./backup           # Verbose output

Before: "My Document.pdf" (modified 2018-06-15)
After:  "2018-06-15_my_document.pdf"`,
		Args: cobra.ExactArgs(1),
		RunE: runRename,
	}
}

func runRename(_ *cobra.Command, args []string) error {
	execution, empty, err := runFileCommand(
		"RENAME",
		true,
		func(progress *progressReporter) (usecase.RenameExecution, error) {
			return newUseCaseService().RunRename(usecase.RenameRequest{
				TargetDir: args[0],
				DryRun:    dryRun,
				OnProgress: func(stage string, processed, total int) {
					progress.Report(stage, processed, total)
				},
			})
		},
		func(execution usecase.RenameExecution) fileCommandExecutionInfo {
			return fileCommandExecutionInfo{
				rootDir:         execution.RootDir,
				fileCount:       execution.FileCount,
				collectDuration: execution.CollectDuration,
				snapshotPath:    execution.SnapshotPath,
			}
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

	printDetailedOperations(result.Operations, func(op renamer.RenameOperation) {
		if op.Skipped {
			fmt.Printf("SKIP: %s (%s)\n", op.OriginalName, op.SkipReason)
		} else if op.Error != nil {
			fmt.Printf("ERROR: %s -> %s: %v\n", op.OriginalName, op.NewName, op.Error)
		} else {
			fmt.Printf("RENAME: %s\n", op.OriginalPath)
			fmt.Printf("    TO: %s\n", op.NewPath)
		}
	})

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
