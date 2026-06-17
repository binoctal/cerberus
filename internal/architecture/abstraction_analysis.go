package architecture

// analyzeAbstractions analyzes abstraction usage
func (a *Analyzer) analyzeAbstractions(report *ArchitectureReport) error {
	// Phase 1: Collect all interfaces
	interfaces, err := a.collectInterfaces()
	if err != nil {
		return err
	}

	// Phase 2: Analyze each interface for issues
	for _, iface := range interfaces {
		a.analyzeInterface(iface, report)
	}

	return nil
}
