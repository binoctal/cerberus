package ai

import (
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/llm"
)

func TestNewBusinessUnderstandingEngine(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	if engine == nil {
		t.Fatal("Expected engine to be created, got nil")
	}

	if engine.codeAnalyzer == nil {
		t.Error("Expected code analyzer to be initialized")
	}

	if engine.commentMiner == nil {
		t.Error("Expected comment miner to be initialized")
	}

	if engine.patternRecognizer == nil {
		t.Error("Expected pattern recognizer to be initialized")
	}

	if engine.interactionController == nil {
		t.Error("Expected interaction controller to be initialized")
	}
}

func TestBusinessUnderstandingEngine_UnderstandProject(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	result, err := engine.UnderstandProject("/tmp/test-project")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify result structure
	if result.ProjectPath != "/tmp/test-project" {
		t.Errorf("Expected project path /tmp/test-project, got %s", result.ProjectPath)
	}

	if result.CodeInsights == nil {
		t.Error("Expected code insights, got nil")
	}

	if result.BusinessModel == nil {
		t.Error("Expected business model, got nil")
	}

	if result.StartTime.IsZero() {
		t.Error("Expected start time to be set")
	}

	if result.EndTime.IsZero() {
		t.Error("Expected end time to be set")
	}

	if result.Duration <= 0 {
		t.Error("Expected positive duration")
	}
}

func TestBusinessUnderstandingEngine_InferBusinessModel(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	insights := &CodeInsights{
		APIContracts: []APIContract{
			{Method: "GET", Endpoint: "/api/orders"},
		},
		Layers: []Layer{
			{Name: "API", Level: 1},
			{Name: "Service", Level: 2},
		},
		StateMachines: []StateMachine{
			{Name: "OrderState", States: []string{"pending", "processing", "complete"}},
		},
		Coupling: &CouplingAnalysis{AverageCoupling: 0.3},
	}

	comments := []*Comment{
		{
			Text:       "Business rule: orders must be positive",
			FilePath:   "order.go",
			LineNumber: 10,
			Semantics: &CommentSemantics{
				Purpose:      "business_rule",
				BusinessTerm: "order",
				Confidence:   0.8,
			},
		},
	}

	patterns := []*Pattern{
		{
			ID:     "pattern-1",
			Name:   "Order Workflow",
			Type:   WorkflowPattern,
			Confidence: 0.75,
		},
	}

	model, err := engine.inferBusinessModel(insights, comments, patterns)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if model == nil {
		t.Fatal("Expected model, got nil")
	}

	// Verify model structure
	if model.Domain == "" {
		t.Error("Expected domain to be inferred")
	}

	// Verify metrics
	if model.APICount != 1 {
		t.Errorf("Expected 1 API, got %d", model.APICount)
	}

	if model.LayerCount != 2 {
		t.Errorf("Expected 2 layers, got %d", model.LayerCount)
	}

	if model.CouplingScore != 0.3 {
		t.Errorf("Expected coupling score 0.3, got %f", model.CouplingScore)
	}

	// Verify business rules were extracted
	if len(model.BusinessRules) == 0 {
		t.Error("Expected business rules to be extracted")
	}

	// Verify confidence is calculated
	if model.Confidence < 0.0 || model.Confidence > 1.0 {
		t.Errorf("Expected confidence between 0.0 and 1.0, got %f", model.Confidence)
	}
}

func TestBusinessUnderstandingEngine_AskMinimalQuestions(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	result := &BusinessUnderstandingResult{
		Questions: []*Question{
			{ID: "q1", Text: "Question 1"},
			{ID: "q2", Text: "Question 2"},
		},
	}

	err := engine.askMinimalQuestions(result)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestBusinessUnderstandingEngine_RefineWithAnswers(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	result := &BusinessUnderstandingResult{
		BusinessModel: &BusinessModel{
			Domain:    "ecommerce",
			Confidence: 0.7,
		},
		Questions: []*Question{
			{ID: "q1", Text: "What is the max order quantity?"},
		},
	}

	answers := map[string]string{
		"q1": "Maximum 100 items per order",
	}

	refinedModel, err := engine.refineWithAnswers(result, answers)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if refinedModel == nil {
		t.Fatal("Expected refined model, got nil")
	}

	// Verify confidence increased
	if refinedModel.Confidence <= result.BusinessModel.Confidence {
		t.Error("Expected refined model confidence to be higher")
	}

	// Verify answers were stored
	if result.UserAnswers == nil {
		t.Error("Expected user answers to be stored")
	}

	if len(result.UserAnswers) != 1 {
		t.Errorf("Expected 1 user answer, got %d", len(result.UserAnswers))
	}
}

func TestBusinessUnderstandingEngine_SaveAndDisplay(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	result := &BusinessUnderstandingResult{
		ProjectPath: "/tmp/test",
		CodeInsights: &CodeInsights{},
		BusinessModel: &BusinessModel{},
	}

	err := engine.saveAndDisplay(result)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test with nil result
	err = engine.saveAndDisplay(nil)
	if err == nil {
		t.Error("Expected error for nil result, got nil")
	}
}

func TestBusinessUnderstandingEngine_ExtractBusinessRules(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	comments := []*Comment{
		{
			Text:       "Business rule: age must be >= 18",
			FilePath:   "validation.go",
			LineNumber: 15,
			Semantics: &CommentSemantics{
				Purpose: "business_rule",
			},
		},
		{
			Text:       "Just a helper function",
			FilePath:   "helper.go",
			LineNumber: 20,
			Semantics: &CommentSemantics{
				Purpose: "general",
			},
		},
	}

	rules := engine.extractBusinessRules(comments)

	if len(rules) != 1 {
		t.Errorf("Expected 1 business rule, got %d", len(rules))
	}

	if rules[0].Priority != "medium" {
		t.Errorf("Expected priority medium, got %s", rules[0].Priority)
	}
}

func TestBusinessUnderstandingEngine_CalculateModelConfidence(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	model := &BusinessModel{
		Entities:      []Entity{{Name: "Order"}},
		Workflows:     []Workflow{{Name: "OrderProcess"}},
		BusinessRules: []BusinessRule{{Name: "MaxQuantity"}},
		Constraints:   []Constraint{{Name: "PositiveQuantity"}},
	}

	comments := []*Comment{
		{Semantics: &CommentSemantics{Confidence: 0.8}},
		{Semantics: &CommentSemantics{Confidence: 0.9}},
		{Semantics: &CommentSemantics{Confidence: 0.7}},
	}

	confidence := engine.calculateModelConfidence(model, comments, []*Pattern{})

	if confidence < 0.0 || confidence > 1.0 {
		t.Errorf("Expected confidence between 0.0 and 1.0, got %f", confidence)
	}

	// Model with complete data should have higher confidence
	emptyModel := &BusinessModel{}
	emptyConfidence := engine.calculateModelConfidence(emptyModel, []*Comment{}, []*Pattern{})

	if confidence <= emptyConfidence {
		t.Error("Expected complete model to have higher confidence")
	}
}

func TestBusinessUnderstandingEngine_Getters(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
	})
	engine := NewBusinessUnderstandingEngine(mockClient)

	if engine.GetCodeAnalyzer() == nil {
		t.Error("Expected code analyzer")
	}

	if engine.GetCommentMiner() == nil {
		t.Error("Expected comment miner")
	}

	if engine.GetPatternRecognizer() == nil {
		t.Error("Expected pattern recognizer")
	}

	if engine.GetInteractionController() == nil {
		t.Error("Expected interaction controller")
	}
}

func TestBusinessModel_String(t *testing.T) {
	model := &BusinessModel{
		Domain:         "ecommerce",
		Entities:       []Entity{{Name: "Order"}},
		Workflows:      []Workflow{{Name: "Checkout"}},
		BusinessRules:  []BusinessRule{{Name: "MaxQuantity"}},
		Constraints:    []Constraint{{Name: "PositiveQty"}},
		StateMachines:  []StateMachine{{Name: "OrderState"}},
		EdgeCases:      []EdgeCase{{Name: "ZeroQuantity"}},
		Confidence:     0.85,
	}

	str := model.String()

	if str == "" {
		t.Error("Expected non-empty string representation")
	}

	// Check that key information is present
	keyInfo := []string{
		"Domain: ecommerce",
		"Entities: 1",
		"Workflows: 1",
		"Business Rules: 1",
		"Constraints: 1",
		"State Machines: 1",
		"Edge Cases: 1",
		"Confidence: 0.85",
	}

	for _, info := range keyInfo {
		if !contains(str, info) {
			t.Errorf("Expected string to contain %q", info)
		}
	}
}

func TestBusinessUnderstandingResult_Validate(t *testing.T) {
	// Valid result
	validResult := &BusinessUnderstandingResult{
		ProjectPath:    "/tmp/test",
		CodeInsights:   &CodeInsights{},
		BusinessModel:  &BusinessModel{},
		StartTime:      time.Now(),
		EndTime:        time.Now(),
	}

	if err := validResult.Validate(); err != nil {
		t.Errorf("Expected valid result, got error: %v", err)
	}

	// Nil result
	var nilResult *BusinessUnderstandingResult
	if err := nilResult.Validate(); err == nil {
		t.Error("Expected error for nil result, got nil")
	}

	// Empty project path
	emptyPathResult := &BusinessUnderstandingResult{
		ProjectPath:   "",
		CodeInsights:  &CodeInsights{},
		BusinessModel: &BusinessModel{},
	}

	if err := emptyPathResult.Validate(); err == nil {
		t.Error("Expected error for empty project path, got nil")
	}

	// Nil code insights
	nilInsightsResult := &BusinessUnderstandingResult{
		ProjectPath:   "/tmp/test",
		CodeInsights:  nil,
		BusinessModel: &BusinessModel{},
	}

	if err := nilInsightsResult.Validate(); err == nil {
		t.Error("Expected error for nil code insights, got nil")
	}

	// Nil business model
	nilModelResult := &BusinessUnderstandingResult{
		ProjectPath:   "/tmp/test",
		CodeInsights:  &CodeInsights{},
		BusinessModel: nil,
	}

	if err := nilModelResult.Validate(); err == nil {
		t.Error("Expected error for nil business model, got nil")
	}
}

func TestCodeInsights_GetFilePaths(t *testing.T) {
	insights := &CodeInsights{
		Modules: []Module{
			{
				Name: "Module1",
				FilePaths: []string{"file1.go", "file2.go"},
			},
			{
				Name: "Module2",
				FilePaths: []string{"file3.go"},
			},
		},
	}

	paths := insights.getFilePaths()

	if len(paths) != 3 {
		t.Errorf("Expected 3 file paths, got %d", len(paths))
	}
}
