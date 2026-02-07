package main

import (
	"runtime"

	"github.com/spf13/cobra"
)

var (
	dryRun  bool
	verbose bool
	workers int
)

func buildRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file-organizer",
		Short: "Organize backup files by renaming and flattening directory structures",
		Long: `file-organizer helps clean up backup directories.

Commands:
  rename     Renames files in place with consistent naming
  flatten    Moves all files to root directory, removes duplicates
  duplicate  Finds and removes duplicate files by content hash
  manifest   Creates a cryptographic inventory of all files

Examples:
  # Preview what rename would do (recommended first step)
  file-organizer rename --dry-run /path/to/backup/2018

  # Actually rename files
  file-organizer rename /path/to/backup/2018

  # Preview flatten operation
  file-organizer flatten --dry-run /path/to/backup/2018

  # Flatten directory structure (move all to root, remove duplicates)
  file-organizer flatten /path/to/backup/2018

  # Typical workflow: first rename, then flatten, then deduplicate
  file-organizer rename /path/to/backup/2018
  file-organizer flatten /path/to/backup/2018
  file-organizer duplicate /path/to/backup/2018

  # Safe workflow with verification
  file-organizer manifest /backup -o before.json
  file-organizer flatten /backup
  file-organizer verify --manifest before.json /backup

Safety:
  The tool will NEVER modify files outside the specified directory.
  All operations are contained within the target path.`,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().IntVar(&workers, "workers", runtime.NumCPU(), "Number of parallel workers for hashing")

	return cmd
}
