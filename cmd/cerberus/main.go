package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "Cerberus — Universal AI Testing Framework",
	}

	rootCmd.AddCommand(
		initCmd(),
		runCmd(),
		verifyCmd(),
		serveCmd(),
		mcpCmd(),
		reportCmd(),
		dashboardCmd(),
		architectureCmd(),
		regressionCmd(),
		accuracyCmd(),
		knownIssueCmd(),
		memoryCmd(),
		versionCmd(),
		selftestCmd(),
		discoverCmd(),
		authCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
