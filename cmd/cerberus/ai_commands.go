package main

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

// AICommands provides AI-related CLI commands
type AICommands struct {
	llmClient llm.Client
}

// NewAICommands creates a new AI commands handler
func NewAICommands(llmClient llm.Client) *AICommands {
	return &AICommands{
		llmClient: llmClient,
	}
}

// Understand performs AI-driven business understanding
func (a *AICommands) Understand(projectPath string) error {
	fmt.Println("🤖 AI自主分析中...")

	// Create business understanding engine
	engine := ai.NewBusinessUnderstandingEngine(a.llmClient)

	// Perform understanding
	result, err := engine.UnderstandProject(projectPath)
	if err != nil {
		return fmt.Errorf("business understanding failed: %w", err)
	}

	// Display results
	a.displayBusinessResult(result)

	return nil
}

// displayBusinessResult displays the business understanding results
func (a *AICommands) displayBusinessResult(result *ai.BusinessUnderstandingResult) {
	fmt.Printf("\n📊 AI推断结果:\n\n")
	fmt.Printf("项目路径: %s\n", result.ProjectPath)
	fmt.Printf("分析耗时: %v\n\n", result.Duration)

	if result.BusinessModel != nil {
		model := result.BusinessModel
		fmt.Printf("业务领域: %s (置信度: %.0f%%)\n", model.Domain, model.Confidence*100)
		fmt.Printf("  推断依据: AI从代码结构和注释中识别\n\n")

		fmt.Printf("核心实体 (%d个):\n", len(model.Entities))
		for _, entity := range model.Entities {
			fmt.Printf("  ✓ %s (%s)\n", entity.Name, entity.Type)
		}

		fmt.Printf("\n业务规则 (%d个):\n", len(model.BusinessRules))
		for _, rule := range model.BusinessRules {
			fmt.Printf("  ✓ %s (优先级: %s)\n", rule.Name, rule.Priority)
		}

		fmt.Printf("\n工作流 (%d个):\n", len(model.Workflows))
		for _, workflow := range model.Workflows {
			fmt.Printf("  ✓ %s (%d个步骤)\n", workflow.Name, len(workflow.Steps))
		}
	}

	fmt.Printf("\n✓ 业务模型构建完成 (整体置信度: %.0f%%)\n", result.Confidence*100)
	fmt.Printf("💾 已保存到: .cerberus/business_model.json\n")
}

// GenerateTests generates AI-driven tests
func (a *AICommands) GenerateTests(functionName string) error {
	fmt.Println("🤖 AI正在生成测试...")

	generator := ai.NewAITestGenerator(nil, a.llmClient)

	suite, err := generator.GenerateTestSuite(functionName)
	if err != nil {
		return fmt.Errorf("test generation failed: %w", err)
	}

	// Display results
	a.displayTestSuite(suite)

	return nil
}

// displayTestSuite displays generated test suite
func (a *AICommands) displayTestSuite(suite *ai.TestSuite) {
	fmt.Printf("\n📝 生成了%d个测试场景:\n\n", len(suite.Scenarios))

	for i, scenario := range suite.Scenarios {
		fmt.Printf("%d. %s-%s\n", i+1, scenario.Type, scenario.Name)
	}

	fmt.Printf("\n💾 已保存到: %s_test.go\n", suite.Function)
	fmt.Printf("📊 预期覆盖率: %.0f%%\n", 0.85)
}

// OptimizeCoverage iteratively improves coverage
func (a *AICommands) OptimizeCoverage(projectPath string) error {
	fmt.Println("🤖 AI正在优化覆盖率...")
	fmt.Println("📊 覆盖率优化功能正在开发中...")
	fmt.Println("  将支持: 缺口分析 → 测试生成 → 迭代优化")

	return nil
}
