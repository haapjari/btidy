package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"file-organizer/pkg/usecase"
)

func buildManifestCommand() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "manifest [path]",
		Short: "Create a cryptographic inventory of all files",
		Long: `Creates a manifest (JSON file) containing SHA256 hashes of all files.

The manifest can be used to:
  - Verify no data was lost after operations (with verify command)
  - Track file inventory over time
  - Detect changes or corruption

Examples:
  file-organizer manifest ./backup -o inventory.json
  file-organizer manifest /path/to/photos -o before.json
  file-organizer manifest --workers 8 ./backup -o manifest.json

Typical safe workflow:
  1. file-organizer manifest /backup -o before.json
  2. file-organizer flatten /backup
  3. file-organizer verify --manifest before.json /backup`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runManifest(args, outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "manifest.json", "Output path for manifest file")

	return cmd
}

func runManifest(args []string, outputPath string) error {
	progress := startProgress("Hashing")
	fmt.Println("Collecting files and computing hashes...")

	var lastProgress int
	execution, err := newUseCaseService().RunManifest(usecase.ManifestRequest{
		TargetDir:  args[0],
		OutputPath: outputPath,
		Workers:    workers,
		OnProgress: func(processed, total int, _ string) {
			if verbose && processed%100 == 0 && processed != lastProgress {
				lastProgress = processed
				fmt.Printf("Progress: %d / %d files\n", processed, total)
			}
		},
	})
	progress.Stop()
	if err != nil {
		return err
	}

	printCommandHeader("MANIFEST", execution.RootDir)
	fmt.Printf("Output file: %s\n", outputPath)
	fmt.Printf("Workers: %d\n", workers)

	fmt.Printf("\nCompleted in %v\n", execution.Duration.Round(time.Millisecond))
	fmt.Println()
	printSummary(
		fmt.Sprintf("Total files:    %d", execution.Manifest.FileCount()),
		fmt.Sprintf("Unique files:   %d", execution.Manifest.UniqueFileCount()),
		"Total size:     "+formatBytes(execution.Manifest.TotalSize()),
		"Manifest saved: "+outputPath,
	)

	return nil
}
