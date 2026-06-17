package main

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/store"
)

func runRegressionTests(ctx context.Context, s *store.Store, logger *zap.Logger, category string, verbose bool) error {
	regStore := store.NewRegressionStore(s)

	tests, err := regStore.ListRegressionTests(ctx, category, "")
	if err != nil {
		return fmt.Errorf("list regression tests: %w", err)
	}

	if len(tests) == 0 {
		fmt.Println("没有回归测试")
		fmt.Println("使用 'cerberus known-issue add' 添加已知问题后自动创建测试")
		return nil
	}

	fmt.Printf("运行 %d 个回归测试...\n\n", len(tests))

	passCount := 0
	failCount := 0
	skipCount := 0

	for _, test := range tests {
		fmt.Printf("[%s] %s\n", test.Category, test.Name)
		if test.Description.Valid && test.Description.String != "" {
			fmt.Printf("  描述: %s\n", test.Description.String)
		}

		var result string
		var status string
		var errorMsg string

		switch test.Category {
		case "complexity":
			result, status, errorMsg = runComplexityTest(ctx, test, verbose)
		case "abstraction":
			result, status, errorMsg = runAbstractionTest(ctx, test, verbose)
		case "solid":
			result, status, errorMsg = runSOLIDTest(ctx, test, verbose)
		default:
			status = "skip"
			skipCount++
			fmt.Printf("  ⊘ 跳过 (未知类别)\n\n")
			continue
		}

		if err := regStore.UpdateRegressionTestResult(ctx, test.ID, result, status, errorMsg); err != nil {
			logger.Warn("更新测试结果失败", zap.Error(err))
		}

		switch status {
		case "pass":
			passCount++
			fmt.Printf("  ✓ 通过\n\n")
		case "fail":
			failCount++
			fmt.Printf("  ✗ 失败: %s\n\n", errorMsg)
		case "skip":
			skipCount++
			fmt.Printf("  ⊘ 跳过\n\n")
		}
	}

	fmt.Println("=====================================")
	fmt.Printf("总计: %d 通过, %d 失败, %d 跳过\n", passCount, failCount, skipCount)

	if failCount > 0 {
		return fmt.Errorf("%d 个回归测试失败", failCount)
	}

	return nil
}
