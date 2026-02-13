package main

import (
	"runtime"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

var (
	dryRun     bool
	verbose    bool
	workers    int
	noSnapshot bool
)

func buildRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "btidy",
		Version: version,
		Short:   "Organize backup files by unzipping, renaming, and flattening directory structures",
		Long: `btidy helps clean up backup directories. Every destructive operation is
reversible through soft-delete, journaling, and undo.

Commands:
  unzip      Extracts zip archives recursively and removes extracted archives
  rename     Renames files in place with consistent naming
  flatten    Moves all files to root directory, removes duplicates by content hash
  organize   Groups files into subdirectories by file extension
  duplicate  Finds and removes duplicate files by content hash
  manifest   Creates a cryptographic inventory of all files
  undo       Reverses the most recent operation using its journal
  purge      Permanently deletes trashed files (only irrecoverable command)

Examples:
  # Typical workflow: unzip, rename, flatten, organize, deduplicate
  btidy unzip /path/to/backup/2018
  btidy rename /path/to/backup/2018
  btidy flatten /path/to/backup/2018
  btidy organize /path/to/backup/2018
  btidy duplicate /path/to/backup/2018

  # Preview any command with --dry-run
  btidy unzip --dry-run /path/to/backup/2018
  btidy rename --dry-run /path/to/backup/2018

  # Undo the last operation
  btidy undo /path/to/backup/2018
  btidy undo --run <run-id> /path/to/backup/2018

  # Purge old trash
  btidy purge --older-than 30d /path/to/backup
  btidy purge --all --force /path/to/backup

  # Skip pre-operation manifest snapshot
  btidy flatten --no-snapshot /path/to/backup

  # Manual manifest workflow
  btidy manifest /backup -o before.json
  btidy flatten /backup
  btidy manifest /backup -o after.json

Safety:
  Files are never permanently deleted; they are moved to .btidy/trash/.
  Every mutation is journaled to .btidy/journal/ for undo support.
  A manifest snapshot is saved to .btidy/manifests/ before each operation.
  Advisory file locking prevents concurrent btidy processes.

Compression:
  ZIP methods store (0) and deflate (8) are supported.
  Deflate64 (method 9) archives are currently skipped.

  The tool will NEVER modify files outside the specified directory.`,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().IntVar(&workers, "workers", runtime.NumCPU(), "Number of parallel workers for hashing")
	cmd.PersistentFlags().BoolVar(&noSnapshot, "no-snapshot", false, "Skip pre-operation manifest snapshot")

	return cmd
}
