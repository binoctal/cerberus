package ai

import (
	"fmt"
	"testing"
)

func TestNewMinimalInteraction(t *testing.T) {
	config := InteractionConfig{
		ConfidenceThreshold:  0.8,
		MaxQuestions:         3,
		BusinessCriticalOnly: true,
	}

	mi := NewMinimalInteraction(config)
	if mi == nil {
		t.Fatal("Expected minimal interaction controller to be created, got nil")
	}

	if mi.config.ConfidenceThreshold != 0.8 {
		t.Errorf("Expected confidence threshold 0.8, got %f", mi.config.ConfidenceThreshold)
	}

	if mi.config.MaxQuestions != 3 {
		t.Errorf("Expected max questions 3, got %d", mi.config.MaxQuestions)
	}

	if mi.config.BusinessCriticalOnly != true {
		t.Error("Expected business critical only to be true")
	}
}

func TestNewMinimalInteraction_Defaults(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{})

	if mi.config.ConfidenceThreshold != 0.7 {
		t.Errorf("Expected default confidence threshold 0.7, got %f", mi.config.ConfidenceThreshold)
	}

	if mi.config.MaxQuestions != 5 {
		t.Errorf("Expected default max questions 5, got %d", mi.config.MaxQuestions)
	}
}

func TestMinimalInteraction_IsConfidenceLow(t *testing.T) {
	tests := []struct {
		name               string
		config             InteractionConfig
		confidence         float64
		isBusinessCritical bool
		expected           bool
	}{
		{
			name:               "Business critical with low confidence",
			config:             InteractionConfig{ConfidenceThreshold: 0.7, BusinessCriticalOnly: true},
			confidence:         0.5,
			isBusinessCritical: true,
			expected:           true,
		},
		{
			name:               "Business critical with high confidence",
			config:             InteractionConfig{ConfidenceThreshold: 0.7, BusinessCriticalOnly: true},
			confidence:         0.9,
			isBusinessCritical: true,
			expected:           false,
		},
		{
			name:               "Non-business critical when business only",
			config:             InteractionConfig{ConfidenceThreshold: 0.7, BusinessCriticalOnly: true},
			confidence:         0.5,
			isBusinessCritical: false,
			expected:           false,
		},
		{
			name:               "Non-business critical when not business only",
			config:             InteractionConfig{ConfidenceThreshold: 0.7, BusinessCriticalOnly: false},
			confidence:         0.5,
			isBusinessCritical: false,
			expected:           true,
		},
		{
			name:               "High confidence non-business critical",
			config:             InteractionConfig{ConfidenceThreshold: 0.7, BusinessCriticalOnly: false},
			confidence:         0.9,
			isBusinessCritical: false,
			expected:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mi := NewMinimalInteraction(tt.config)
			result := mi.IsConfidenceLow(tt.confidence, tt.isBusinessCritical)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMinimalInteraction_GenerateCriticalQuestionsOnly(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{
		ConfidenceThreshold:  0.7,
		MaxQuestions:         5,
		BusinessCriticalOnly: true,
	})

	// Create test comments
	comments := []*Comment{
		{
			Text:       "Business rule: orders must be > 0",
			FilePath:   "order.go",
			LineNumber: 10,
			Source:     SingleLineComments,
			Semantics: &CommentSemantics{
				Purpose:    "business_rule",
				Confidence: 0.5, // Low confidence
			},
		},
		{
			Text:       "Just a helper function",
			FilePath:   "helper.go",
			LineNumber: 20,
			Source:     SingleLineComments,
			Semantics: &CommentSemantics{
				Purpose:    "general",
				Confidence: 0.5, // Low confidence but not business critical
			},
		},
	}

	// Create test patterns
	patterns := []*Pattern{
		{
			ID:         "pattern-1",
			Name:       "Order Validation",
			Type:       BusinessPatterns,
			Confidence: 0.6, // Low confidence
		},
	}

	insights := &CodeInsights{}

	questions := mi.GenerateCriticalQuestionsOnly(insights, comments, patterns)

	// Should generate questions for business-critical items only
	if len(questions) < 1 {
		t.Error("Expected at least 1 question for business-critical items")
	}

	// Verify questions are sorted by priority
	for i := 0; i < len(questions)-1; i++ {
		if questions[i].Priority < questions[i+1].Priority {
			t.Error("Expected questions to be sorted by priority (highest first)")
		}
	}
}

func TestMinimalInteraction_GenerateCriticalQuestionsOnly_Limit(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{
		ConfidenceThreshold:  0.7,
		MaxQuestions:         2, // Limit to 2
		BusinessCriticalOnly: true,
	})

	// Create many low-confidence business comments
	var comments []*Comment
	for i := 0; i < 10; i++ {
		comments = append(comments, &Comment{
			Text:       fmt.Sprintf("Business rule %d", i),
			FilePath:   "file.go",
			LineNumber: i * 10,
			Source:     SingleLineComments,
			Semantics: &CommentSemantics{
				Purpose:    "business_rule",
				Confidence: 0.5,
			},
		})
	}

	insights := &CodeInsights{}
	patterns := []*Pattern{}

	questions := mi.GenerateCriticalQuestionsOnly(insights, comments, patterns)

	// Should be limited to MaxQuestions
	if len(questions) > 2 {
		t.Errorf("Expected max 2 questions, got %d", len(questions))
	}
}

func TestMinimalInteraction_IsBusinessCritical(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{})

	tests := []struct {
		name     string
		purpose  string
		expected bool
	}{
		{"Business rule", "business_rule", true},
		{"Validation", "validation", true},
		{"Constraint", "constraint", true},
		{"Security", "security", true},
		{"Compliance", "compliance", true},
		{"General", "general", false},
		{"Helper", "helper", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := &Comment{
				Semantics: &CommentSemantics{
					Purpose: tt.purpose,
				},
			}
			result := mi.isBusinessCritical(comment)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMinimalInteraction_IsPatternBusinessCritical(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{})

	tests := []struct {
		name        string
		patternType PatternType
		expected    bool
	}{
		{"Business pattern", BusinessPatterns, true},
		{"Rule pattern", RulePattern, true},
		{"Workflow pattern", WorkflowPattern, true},
		{"Domain pattern", DomainPattern, false},
		{"Edge case pattern", EdgeCasePattern, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := &Pattern{Type: tt.patternType}
			result := mi.isPatternBusinessCritical(pattern)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMinimalInteraction_CalculateQuestionPriority(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{})

	// Business-critical comment should have higher priority
	businessComment := &Comment{
		Text:   "Business rule: validate order",
		Source: FIXMEComments,
		Semantics: &CommentSemantics{
			Purpose:    "business_rule",
			Confidence: 0.3, // Very low confidence
		},
	}

	generalComment := &Comment{
		Text:   "Helper function",
		Source: SingleLineComments,
		Semantics: &CommentSemantics{
			Purpose:    "general",
			Confidence: 0.6,
		},
	}

	businessPriority := mi.calculateQuestionPriority(businessComment)
	generalPriority := mi.calculateQuestionPriority(generalComment)

	if businessPriority <= generalPriority {
		t.Error("Expected business-critical comment to have higher priority")
	}
}

func TestQuestion_Validate(t *testing.T) {
	// Valid question
	validQuestion := &Question{
		ID:          "q1",
		Text:        "What is the business rule?",
		Context:     "File: order.go, Line: 10",
		Criticality: "high",
		Category:    "business_rule",
		Priority:    100,
	}

	if err := validQuestion.Validate(); err != nil {
		t.Errorf("Expected valid question, got error: %v", err)
	}

	// Nil question
	var nilQuestion *Question
	if err := nilQuestion.Validate(); err == nil {
		t.Error("Expected error for nil question, got nil")
	}

	// Empty ID
	emptyIDQuestion := &Question{
		ID:          "",
		Text:        "Question text",
		Criticality: "high",
	}

	if err := emptyIDQuestion.Validate(); err == nil {
		t.Error("Expected error for empty ID, got nil")
	}

	// Empty text
	emptyTextQuestion := &Question{
		ID:          "q1",
		Text:        "",
		Criticality: "high",
	}

	if err := emptyTextQuestion.Validate(); err == nil {
		t.Error("Expected error for empty text, got nil")
	}

	// Invalid criticality
	invalidCriticalityQuestion := &Question{
		ID:          "q1",
		Text:        "Question text",
		Criticality: "urgent", // Not a valid criticality
	}

	if err := invalidCriticalityQuestion.Validate(); err == nil {
		t.Error("Expected error for invalid criticality, got nil")
	}
}

func TestMinimalInteraction_GetConfig(t *testing.T) {
	config := InteractionConfig{
		ConfidenceThreshold: 0.8,
		MaxQuestions:        3,
	}
	mi := NewMinimalInteraction(config)

	retrievedConfig := mi.GetConfig()
	if retrievedConfig.ConfidenceThreshold != 0.8 {
		t.Errorf("Expected confidence threshold 0.8, got %f", retrievedConfig.ConfidenceThreshold)
	}
}

func TestMinimalInteraction_SetConfig(t *testing.T) {
	mi := NewMinimalInteraction(InteractionConfig{})

	newConfig := InteractionConfig{
		ConfidenceThreshold:  0.9,
		MaxQuestions:         10,
		BusinessCriticalOnly: true,
	}

	mi.SetConfig(newConfig)

	if mi.config.ConfidenceThreshold != 0.9 {
		t.Errorf("Expected confidence threshold 0.9, got %f", mi.config.ConfidenceThreshold)
	}

	if mi.config.MaxQuestions != 10 {
		t.Errorf("Expected max questions 10, got %d", mi.config.MaxQuestions)
	}
}
