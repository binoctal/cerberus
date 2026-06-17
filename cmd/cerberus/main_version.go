package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd prints version information
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("cerberus %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}
