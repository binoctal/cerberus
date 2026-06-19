package main

import (
	"github.com/spf13/cobra"
)

// architectureCmd runs architecture quality check
func architectureCmd() *cobra.Command {
	var (
		outputFormat string
		verbose      bool
	)

	cmd := &cobra.Command{
		Use:   "architecture",
		Short: "Check architecture quality",
		Long:  "Run architecture quality analysis to detect over-engineering, circular dependencies, SOLID violations, and other architectural issues.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."

			// Run architecture check
			if err := runArchitectureCheck(projectPath); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (text, json)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	return cmd
}
