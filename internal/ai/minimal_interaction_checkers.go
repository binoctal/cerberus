package ai

// isBusinessCritical determines if a comment is business-critical
func (mi *MinimalInteraction) isBusinessCritical(comment *Comment) bool {
	if comment.Semantics == nil {
		return false
	}

	criticalPurposes := []string{
		"business_rule",
		"validation",
		"constraint",
		"security",
		"compliance",
	}

	for _, purpose := range criticalPurposes {
		if comment.Semantics.Purpose == purpose {
			return true
		}
	}

	return false
}

// isPatternBusinessCritical determines if a pattern is business-critical
func (mi *MinimalInteraction) isPatternBusinessCritical(pattern *Pattern) bool {
	criticalTypes := []PatternType{
		BusinessPatterns,
		RulePattern,
		WorkflowPattern,
	}

	for _, ptype := range criticalTypes {
		if pattern.Type == ptype {
			return true
		}
	}

	return false
}
