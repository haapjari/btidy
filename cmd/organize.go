package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"btidy/pkg/organizer"
	"btidy/pkg/usecase"
)

func buildOrganizeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "organize [path]",
		Short: "Group files into subdirectories by file extension",
		Long: `Groups files into subdirectories based on their file extension:
  - .pdf files go into pdf/
  - .jpg files go into jpg/
  - Files with no extension go into other/
  - Case insensitive (.JPG and .jpg both go to jpg/)

Examples:
  btidy organize --dry-run ./backup    # Preview changes
  btidy organize ./backup              # Apply changes
  btidy organize -v ./backup           # Verbose output

Before:
  backup/
    report.pdf
    photo.jpg
    notes.txt
    Makefile

After:
  backup/
    pdf/report.pdf
    jpg/photo.jpg
    txt/notes.txt
    other/Makefile`,
		Args: cobra.ExactArgs(1),
		RunE: runOrganize,
	}
}

func runOrganize(_ *cobra.Command, args []string) error {
	execution, empty, err := runFileCommand(
		"ORGANIZE",
		true,
		func(progress *progressReporter) (usecase.OrganizeExecution, error) {
			return newUseCaseService().RunOrganize(usecase.OrganizeRequest{
				TargetDir: args[0],
				DryRun:    dryRun,
				OnProgress: func(stage string, processed, total int) {
					progress.Report(stage, processed, total)
				},
			})
		},
		func(execution usecase.OrganizeExecution) fileCommandExecutionInfo {
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

	printDetailedOperations(result.Operations, printOrganizeOperation)

	printSummary(
		fmt.Sprintf("Total files:     %d", result.TotalFiles),
		fmt.Sprintf("Moved:           %d", result.MovedCount),
		fmt.Sprintf("Skipped:         %d", result.SkippedCount),
		fmt.Sprintf("Errors:          %d", result.ErrorCount),
		fmt.Sprintf("Dirs created:    %d", result.CreatedDirsCount),
	)
	printDryRunHint()

	return nil
}

func printOrganizeOperation(op organizer.MoveOperation) {
	switch {
	case op.Error != nil:
		fmt.Printf("ERROR: %s: %v\n", op.OriginalPath, op.Error)
	case op.Skipped:
		fmt.Printf("SKIP: %s (%s)\n", op.OriginalPath, op.SkipReason)
	default:
		fmt.Printf("MOVE: %s\n", op.OriginalPath)
		fmt.Printf("  TO: %s\n", op.NewPath)
	}
}
