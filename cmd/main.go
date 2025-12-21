package main

import "os"

func main() {
	rootCmd := buildRootCommand()
	rootCmd.AddCommand(buildRenameCommand())
	rootCmd.AddCommand(buildFlattenCommand())
	rootCmd.AddCommand(buildDuplicateCommand())
	rootCmd.AddCommand(buildManifestCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
