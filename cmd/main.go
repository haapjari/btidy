package main

import "os"

func main() {
	rootCmd := buildRootCommand()
	rootCmd.AddCommand(buildUnzipCommand())
	rootCmd.AddCommand(buildRenameCommand())
	rootCmd.AddCommand(buildFlattenCommand())
	rootCmd.AddCommand(buildDuplicateCommand())
	rootCmd.AddCommand(buildManifestCommand())
	rootCmd.AddCommand(buildOrganizeCommand())
	rootCmd.AddCommand(buildUndoCommand())
	rootCmd.AddCommand(buildPurgeCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
