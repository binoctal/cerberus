package architecture

// reportMissingDocsDirectory reports when no documentation directory exists
func (a *Analyzer) reportMissingDocsDirectory(report *ArchitectureReport) {
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

// reportMissingADR reports when ADR files are not found
func (a *Analyzer) reportMissingADR(report *ArchitectureReport) {
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
		Evidence:    []string{"扫描文件数: >100"},
	})
}

// reportMissingDesignDocs reports when design documents are not found
func (a *Analyzer) reportMissingDesignDocs(report *ArchitectureReport) {
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
