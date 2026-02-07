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
		Use:   "btidy",
		Short: "Organize backup files by renaming and flattening directory structures",
		Long: `btidy helps clean up backup directories.

Commands:
  rename     Renames files in place with consistent naming
  flatten    Moves all files to root directory, removes duplicates by content hash
  duplicate  Finds and removes duplicate files by content hash
  manifest   Creates a cryptographic inventory of all files

Examples:
  # Preview what rename would do (recommended first step)
	  btidy rename --dry-run /path/to/backup/2018

  # Actually rename files
	  btidy rename /path/to/backup/2018

  # Preview flatten operation
	  btidy flatten --dry-run /path/to/backup/2018

  # Flatten directory structure (move all to root, remove duplicates by content hash)
	  btidy flatten /path/to/backup/2018

  # Typical workflow: first rename, then flatten, then deduplicate
	  btidy rename /path/to/backup/2018
	  btidy flatten /path/to/backup/2018
	  btidy duplicate /path/to/backup/2018

  # Safe workflow with verification
	  btidy manifest /backup -o before.json
	  btidy flatten /backup
	  btidy manifest /backup -o after.json
  # compare hashes between manifests

Safety:
  The tool will NEVER modify files outside the specified directory.
  The manifest output file must also resolve inside the target directory.
  All operations are contained within the target path.`,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().IntVar(&workers, "workers", runtime.NumCPU(), "Number of parallel workers for hashing")

	return cmd
}
