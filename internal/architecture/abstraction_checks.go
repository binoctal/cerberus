package architecture

import (
	"fmt"
)

// checkSingleImplementation checks if an interface has only one implementation
func (a *Analyzer) checkSingleImplementation(iface *InterfaceInfo, report *ArchitectureReport) {
	if iface.Implementations == 1 {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          fmt.Sprintf("single-impl-%s", iface.Name),
			Type:        PrematureAbstraction,
			Severity:    SeverityInfo,
			File:        iface.FilePath,
			Line:        iface.LineNumber,
			Description: fmt.Sprintf("接口 %s 只有 1 个实现", iface.Name),
			Rationale:   "单一实现的接口可能是过早抽象，YAGNI原则",
			Suggestion:  "考虑在真正需要多实现时再抽象，或使用具体类型",
			Confidence:  0.7,
			Evidence:    []string{fmt.Sprintf("实现数: %d", iface.Implementations)},
		})
		report.Metrics.SingleImplInterfaces++
	}
}

// checkUnusedAbstraction checks if an interface has no implementations
func (a *Analyzer) checkUnusedAbstraction(iface *InterfaceInfo, report *ArchitectureReport) {
	if iface.Implementations == 0 {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          fmt.Sprintf("unused-interface-%s", iface.Name),
			Type:        PrematureAbstraction,
			Severity:    SeverityWarning,
			File:        iface.FilePath,
			Line:        iface.LineNumber,
			Description: fmt.Sprintf("接口 %s 未找到实现", iface.Name),
			Rationale:   "未使用的抽象增加复杂性",
			Suggestion:  "删除接口或实现它，如果是为了未来预留请记录理由",
			Confidence:  0.5,
			Evidence:    []string{"可能实现在分析范围外"},
		})
		report.Metrics.UnusedAbstractions++
	}
}

// analyzeInterface checks a single interface for issues
func (a *Analyzer) analyzeInterface(iface *InterfaceInfo, report *ArchitectureReport) {
	report.Metrics.TotalInterfaces++

	a.checkSingleImplementation(iface, report)
	a.checkUnusedAbstraction(iface, report)
}
