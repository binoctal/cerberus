package architecture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// analyzeScenarios analyzes scenario documentation coverage
func (a *Analyzer) analyzeScenarios(report *ArchitectureReport) error {
	// Check for documentation directory
	docDirs := []string{
		"cerberus-docs",
		"docs",
		"design",
		"documentation",
		".cerberus",
	}

	foundDocs := false
	for _, dir := range docDirs {
		if _, err := os.Stat(filepath.Join(a.projectPath, dir)); err == nil {
			foundDocs = true
			break
		}
	}

	// Check: No documentation directory found
	if !foundDocs {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          "missing-docs-dir",
			Type:        MissingScenario,
			Severity:    SeverityWarning,
			File:        "",
			Line:        0,
			Description: "项目缺少文档目录 (cerberus-docs/, docs/, design/)",
			Rationale:   "缺少文档使架构决策不透明，难以理解设计理由",
			Suggestion:  "创建 cerberus-docs/ 目录记录架构决策和设计文档",
			Confidence:  0.8,
			Evidence:    []string{"检查的目录: cerberus-docs, docs, design, documentation, .cerberus"},
		})
	}

	// Check for ADR (Architecture Decision Records)
	adrFiles := 0
	designDocs := 0
	planDocs := 0

	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip vendor and hidden dirs
			if strings.Contains(path, "vendor") || strings.Contains(path, ".git") {
				return filepath.SkipDir
			}
			return nil
		}

		// Check for ADR files (common naming patterns)
		base := filepath.Base(path)
		lowerBase := strings.ToLower(base)
		
		if strings.HasSuffix(lowerBase, "adr.md") ||
		   strings.HasPrefix(lowerBase, "adr-") ||
		   strings.Contains(lowerBase, "decision") {
			adrFiles++
		}

		// Check for design documents
		if strings.Contains(lowerBase, "design") ||
		   strings.Contains(lowerBase, "spec") ||
		   strings.Contains(lowerBase, "architecture") {
			designDocs++
		}

		// Check for implementation plans
		if strings.Contains(lowerBase, "plan") ||
		   strings.Contains(lowerBase, "implementation") {
			planDocs++
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Report findings
	if adrFiles == 0 && foundDocs {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          "no-adr-found",
			Type:        MissingScenario,
			Severity:    SeverityInfo,
			File:        "",
			Line:        0,
			Description: "未找到架构决策记录 (ADR)",
			Rationale:   "ADR 记录重要架构决策的理由和权衡",
			Suggestion:  "在 cerberus-docs/ 中创建 ADR 记录关键决策",
			Confidence:  0.7,
			Evidence:    []string{fmt.Sprintf("扫描文件数: >100")},
		})
	}

	if designDocs == 0 && foundDocs {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          "no-design-docs",
			Type:        MissingScenario,
			Severity:    SeverityInfo,
			File:        "",
			Line:        0,
			Description: "未找到设计文档",
			Rationale:   "设计文档记录系统架构和组件设计",
			Suggestion:  "创建设计文档说明系统架构、组件交互、数据流",
			Confidence:  0.6,
			Evidence:    []string{},
		})
	}

	// Update metrics
	report.Metrics.ADRFiles = adrFiles
	report.Metrics.DesignDocs = designDocs
	report.Metrics.PlanDocs = planDocs

	return nil
}
