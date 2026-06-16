package ai

import (
	"testing"
)

func TestNewCommentMiner(t *testing.T) {
	miner := NewCommentMiner()
	if miner == nil {
		t.Fatal("Expected miner to be created, got nil")
	}

	if miner.patterns == nil {
		t.Error("Expected patterns to be initialized, got nil")
	}

	// Check that all comment source types have patterns
	expectedSources := []CommentSource{
		SingleLineComments,
		MultiLineComments,
		DocComments,
		PackageComments,
		TODOComments,
		FIXMEComments,
		NOTEComments,
		HACKComments,
		WARNINGComments,
	}

	for _, source := range expectedSources {
		if _, exists := miner.patterns[source]; !exists {
			t.Errorf("Expected patterns for source %v", source)
		}
	}
}

func TestCommentMiner_MineAggressively(t *testing.T) {
	miner := NewCommentMiner()
	comments, err := miner.MineAggressively("/tmp/project", []string{"file.go"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if comments == nil {
		t.Error("Expected comments slice, got nil")
	}
}

func TestCommentMiner_IsBusinessComment(t *testing.T) {
	miner := NewCommentMiner()

	// Test business comments
	businessComments := []string{
		"This implements the business rule for pricing",
		"Validates the constraint that orders must be > 0",
		"Domain logic: users can only have one active session",
		"Workflow: submit → review → approve → deploy",
		"State transition: pending → processing → complete",
	}

	for _, comment := range businessComments {
		if !miner.isBusinessComment(comment) {
			t.Errorf("Expected comment to be recognized as business-relevant: %s", comment)
		}
	}

	// Test non-business comments
	nonBusinessComments := []string{
		"Initialize the variable",
		"Loop through the array",
		"Return the result",
		"This is a helper function",
	}

	for _, comment := range nonBusinessComments {
		if miner.isBusinessComment(comment) {
			t.Errorf("Expected comment NOT to be business-relevant: %s", comment)
		}
	}
}

func TestCommentMiner_InferPurpose(t *testing.T) {
	miner := NewCommentMiner()

	testCases := []struct {
		comment    string
		expected   string
	}{
		{"User must have valid email", "validation"},
		{"When order is created, then send notification", "workflow"},
		{"State changes from pending to active", "state_transition"},
		{"Business rule: maximum 3 items per order", "business_rule"},
		{"Just a regular helper function", "general"},
	}

	for _, tc := range testCases {
		result := miner.inferPurpose(tc.comment)
		if result != tc.expected {
			t.Errorf("Expected purpose %q for comment %q, got %q", tc.expected, tc.comment, result)
		}
	}
}

func TestCommentMiner_CalculateConfidence(t *testing.T) {
	miner := NewCommentMiner()

	comment := &Comment{
		Text:       "Test comment",
		FilePath:   "test.go",
		LineNumber: 10,
		Source:     NOTEComments,
		Semantics: &CommentSemantics{
			Purpose:      "validation",
			BusinessTerm: "order_validation",
		},
	}

	confidence := miner.calculateConfidence(comment)
	if confidence < 0.5 || confidence > 1.0 {
		t.Errorf("Expected confidence between 0.5 and 1.0, got %f", confidence)
	}
}

func TestCommentSource_String(t *testing.T) {
	testCases := []struct {
		source    CommentSource
		expected  string
	}{
		{SingleLineComments, "SingleLine"},
		{MultiLineComments, "MultiLine"},
		{DocComments, "Doc"},
		{TODOComments, "TODO"},
		{FIXMEComments, "FIXME"},
		{NOTEComments, "NOTE"},
		{HACKComments, "HACK"},
		{WARNINGComments, "WARNING"},
	}

	for _, tc := range testCases {
		result := tc.source.String()
		if result != tc.expected {
			t.Errorf("Expected %q, got %q", tc.expected, result)
		}
	}
}

func TestComment_Validate(t *testing.T) {
	// Valid comment
	validComment := &Comment{
		Text:       "This is a test comment",
		FilePath:   "test.go",
		LineNumber: 10,
		Source:     SingleLineComments,
	}

	if err := validComment.Validate(); err != nil {
		t.Errorf("Expected valid comment, got error: %v", err)
	}

	// Nil comment
	var nilComment *Comment
	if err := nilComment.Validate(); err == nil {
		t.Error("Expected error for nil comment, got nil")
	}

	// Empty text
	emptyTextComment := &Comment{
		Text:       "",
		FilePath:   "test.go",
		LineNumber: 10,
	}

	if err := emptyTextComment.Validate(); err == nil {
		t.Error("Expected error for empty text, got nil")
	}

	// Empty file path
	emptyPathComment := &Comment{
		Text:       "Test comment",
		FilePath:   "",
		LineNumber: 10,
	}

	if err := emptyPathComment.Validate(); err == nil {
		t.Error("Expected error for empty file path, got nil")
	}

	// Invalid line number
	invalidLineComment := &Comment{
		Text:       "Test comment",
		FilePath:   "test.go",
		LineNumber: 0,
	}

	if err := invalidLineComment.Validate(); err == nil {
		t.Error("Expected error for invalid line number, got nil")
	}
}
