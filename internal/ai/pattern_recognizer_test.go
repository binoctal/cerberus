package ai

import (
	"testing"
)

func TestNewPatternRecognizer(t *testing.T) {
	recognizer := NewPatternRecognizer()
	if recognizer == nil {
		t.Fatal("Expected recognizer to be created, got nil")
	}

	if recognizer.database == nil {
		t.Error("Expected database to be initialized, got nil")
	}

	// Check that all pattern type collections exist
	if recognizer.database.BusinessPatterns == nil {
		t.Error("Expected BusinessPatterns to be initialized")
	}
	if recognizer.database.DomainPatterns == nil {
		t.Error("Expected DomainPatterns to be initialized")
	}
	if recognizer.database.WorkflowPatterns == nil {
		t.Error("Expected WorkflowPatterns to be initialized")
	}
	if recognizer.database.StateMachinePatterns == nil {
		t.Error("Expected StateMachinePatterns to be initialized")
	}
	if recognizer.database.RulePatterns == nil {
		t.Error("Expected RulePatterns to be initialized")
	}
	if recognizer.database.EdgeCasePatterns == nil {
		t.Error("Expected EdgeCasePatterns to be initialized")
	}
	if recognizer.database.ErrorHandlingPatterns == nil {
		t.Error("Expected ErrorHandlingPatterns to be initialized")
	}
}

func TestPatternRecognizer_RecognizeBusinessPatterns(t *testing.T) {
	recognizer := NewPatternRecognizer()
	code := "test code"
	comments := []*Comment{}

	patterns, err := recognizer.RecognizeBusinessPatterns(code, comments)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if patterns == nil {
		t.Error("Expected patterns slice, got nil")
	}
}

func TestPatternRecognizer_CalculatePatternConfidence(t *testing.T) {
	recognizer := NewPatternRecognizer()

	// Test pattern with minimal information
	minimalPattern := &Pattern{
		ID:     "test-1",
		Name:   "Minimal Pattern",
		Type:   BusinessPatterns,
		Locations: []PatternLocation{},
	}

	confidence := recognizer.calculatePatternConfidence(minimalPattern)
	if confidence < 0.0 || confidence > 1.0 {
		t.Errorf("Expected confidence between 0.0 and 1.0, got %f", confidence)
	}

	// Test pattern with full information
	fullPattern := &Pattern{
		ID:          "test-2",
		Name:        "Full Pattern",
		Type:        BusinessPatterns,
		Description: "This is a comprehensive pattern",
		Locations: []PatternLocation{
			{FilePath: "file1.go", LineNumber: 10},
			{FilePath: "file2.go", LineNumber: 20},
			{FilePath: "file3.go", LineNumber: 30},
		},
		Metadata: map[string]interface{}{
			"version": "1.0",
			"author":  "test",
		},
	}

	confidence = recognizer.calculatePatternConfidence(fullPattern)
	if confidence < 0.0 || confidence > 1.0 {
		t.Errorf("Expected confidence between 0.0 and 1.0, got %f", confidence)
	}

	// Full pattern should have higher confidence
	if recognizer.calculatePatternConfidence(fullPattern) <= recognizer.calculatePatternConfidence(minimalPattern) {
		t.Error("Expected full pattern to have higher confidence than minimal pattern")
	}
}

func TestPatternRecognizer_AddPattern(t *testing.T) {
	recognizer := NewPatternRecognizer()

	pattern := &Pattern{
		ID:     "test-1",
		Name:   "Test Pattern",
		Type:   BusinessPatterns,
		Locations: []PatternLocation{
			{FilePath: "test.go", LineNumber: 10},
		},
	}

	recognizer.addPattern(pattern)

	retrieved := recognizer.getPatternsByType(BusinessPatterns)
	if len(retrieved) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(retrieved))
	}

	if retrieved[0].ID != pattern.ID {
		t.Errorf("Expected pattern ID %s, got %s", pattern.ID, retrieved[0].ID)
	}
}

func TestPatternRecognizer_GetPatternsByType(t *testing.T) {
	recognizer := NewPatternRecognizer()

	// Add patterns of different types
	recognizer.addPattern(&Pattern{ID: "business-1", Name: "Business", Type: BusinessPatterns})
	recognizer.addPattern(&Pattern{ID: "domain-1", Name: "Domain", Type: DomainPattern})
	recognizer.addPattern(&Pattern{ID: "workflow-1", Name: "Workflow", Type: WorkflowPattern})

	businessPatterns := recognizer.getPatternsByType(BusinessPatterns)
	if len(businessPatterns) != 1 {
		t.Errorf("Expected 1 business pattern, got %d", len(businessPatterns))
	}

	domainPatterns := recognizer.getPatternsByType(DomainPattern)
	if len(domainPatterns) != 1 {
		t.Errorf("Expected 1 domain pattern, got %d", len(domainPatterns))
	}

	workflowPatterns := recognizer.getPatternsByType(WorkflowPattern)
	if len(workflowPatterns) != 1 {
		t.Errorf("Expected 1 workflow pattern, got %d", len(workflowPatterns))
	}
}

func TestPatternRecognizer_GetDatabase(t *testing.T) {
	recognizer := NewPatternRecognizer()
	database := recognizer.GetDatabase()

	if database == nil {
		t.Error("Expected database to be returned, got nil")
	}
}

func TestPatternRecognizer_GetPatternCount(t *testing.T) {
	recognizer := NewPatternRecognizer()

	initialCount := recognizer.GetPatternCount()
	if initialCount != 0 {
		t.Errorf("Expected initial count to be 0, got %d", initialCount)
	}

	// Add some patterns
	recognizer.addPattern(&Pattern{ID: "p1", Name: "P1", Type: BusinessPatterns})
	recognizer.addPattern(&Pattern{ID: "p2", Name: "P2", Type: DomainPattern})
	recognizer.addPattern(&Pattern{ID: "p3", Name: "P3", Type: WorkflowPattern})

	newCount := recognizer.GetPatternCount()
	if newCount != 3 {
		t.Errorf("Expected count to be 3, got %d", newCount)
	}
}

func TestPatternRecognizer_ClearPatterns(t *testing.T) {
	recognizer := NewPatternRecognizer()

	// Add some patterns
	recognizer.addPattern(&Pattern{ID: "p1", Name: "P1", Type: BusinessPatterns})
	recognizer.addPattern(&Pattern{ID: "p2", Name: "P2", Type: DomainPattern})

	if recognizer.GetPatternCount() != 2 {
		t.Errorf("Expected count to be 2 before clear, got %d", recognizer.GetPatternCount())
	}

	recognizer.ClearPatterns()

	if recognizer.GetPatternCount() != 0 {
		t.Errorf("Expected count to be 0 after clear, got %d", recognizer.GetPatternCount())
	}
}

func TestPatternType_String(t *testing.T) {
	testCases := []struct {
		patternType PatternType
		expected    string
	}{
		{BusinessPatterns, "Business"},
		{DomainPattern, "Domain"},
		{WorkflowPattern, "Workflow"},
		{StateMachinePattern, "StateMachine"},
		{RulePattern, "Rule"},
		{EdgeCasePattern, "EdgeCase"},
		{ErrorHandlingPattern, "ErrorHandling"},
	}

	for _, tc := range testCases {
		result := tc.patternType.String()
		if result != tc.expected {
			t.Errorf("Expected %q, got %q", tc.expected, result)
		}
	}
}

func TestPattern_Validate(t *testing.T) {
	// Valid pattern
	validPattern := &Pattern{
		ID:         "test-1",
		Name:       "Test Pattern",
		Type:       BusinessPatterns,
		Confidence: 0.8,
		Locations: []PatternLocation{
			{FilePath: "test.go", LineNumber: 10},
		},
	}

	if err := validPattern.Validate(); err != nil {
		t.Errorf("Expected valid pattern, got error: %v", err)
	}

	// Nil pattern
	var nilPattern *Pattern
	if err := nilPattern.Validate(); err == nil {
		t.Error("Expected error for nil pattern, got nil")
	}

	// Empty ID
	emptyIDPattern := &Pattern{
		ID:   "",
		Name: "Test",
		Type: BusinessPatterns,
	}

	if err := emptyIDPattern.Validate(); err == nil {
		t.Error("Expected error for empty ID, got nil")
	}

	// Empty name
	emptyNamePattern := &Pattern{
		ID:   "test-1",
		Name: "",
		Type: BusinessPatterns,
	}

	if err := emptyNamePattern.Validate(); err == nil {
		t.Error("Expected error for empty name, got nil")
	}

	// Invalid confidence
	invalidConfidencePattern := &Pattern{
		ID:         "test-1",
		Name:       "Test",
		Type:       BusinessPatterns,
		Confidence: 1.5,
	}

	if err := invalidConfidencePattern.Validate(); err == nil {
		t.Error("Expected error for invalid confidence, got nil")
	}
}

func TestPattern_ToJSON(t *testing.T) {
	pattern := &Pattern{
		ID:         "test-1",
		Name:       "Test Pattern",
		Type:       BusinessPatterns,
		Description: "Test description",
		Confidence: 0.8,
	}

	json, err := pattern.ToJSON()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if json == "" {
		t.Error("Expected JSON string, got empty string")
	}

	// Check that JSON contains expected fields
	if !contains(json, "test-1") {
		t.Error("Expected JSON to contain pattern ID")
	}
	if !contains(json, "Test Pattern") {
		t.Error("Expected JSON to contain pattern name")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
