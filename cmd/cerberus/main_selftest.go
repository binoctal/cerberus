package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/smoke"
)

// selftestCmd runs a deterministic self-test (mock LLM, no network) as a
// binary health check suitable for CI.
func selftestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "selftest",
		Short: "Run a deterministic self-test (mock LLM, no network)",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := smoke.RunSelfTest(context.Background())
			for _, c := range res.Checks {
				fmt.Println("  ✓", c)
			}
			if err != nil {
				return fmt.Errorf("selftest: %w", err)
			}
			fmt.Println("selftest PASSED")
			return nil
		},
	}
}
