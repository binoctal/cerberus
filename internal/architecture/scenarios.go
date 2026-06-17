package architecture

// analyzeScenarios analyzes scenario documentation coverage
func (a *Analyzer) analyzeScenarios(report *ArchitectureReport) error {
	// Phase 1: Check for documentation directory
	foundDocs := a.checkDocumentationDirectories()

	if !foundDocs {
		a.reportMissingDocsDirectory(report)
	}

	// Phase 2: Count documentation files
	counts, err := a.countDocumentationFiles()
	if err != nil {
		return err
	}

	// Phase 3: Report findings
	if counts.adrFiles == 0 && foundDocs {
		a.reportMissingADR(report)
	}

	if counts.designDocs == 0 && foundDocs {
		a.reportMissingDesignDocs(report)
	}

	// Update metrics
	report.Metrics.ADRFiles = counts.adrFiles
	report.Metrics.DesignDocs = counts.designDocs
	report.Metrics.PlanDocs = counts.planDocs

	return nil
}
