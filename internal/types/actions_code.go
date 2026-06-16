package types

import "fmt"

// CodeAnalyzeAction represents analyzing code structure.
type CodeAnalyzeAction struct {
	// TargetPath is the directory or file to analyze.
	TargetPath string `json:"target_path"`
	// Language is the programming language.
	Language string `json:"language,omitempty"`
	// Checks are specific analysis checks to run.
	Checks []string `json:"checks,omitempty"`
	// Format controls the output format ("json", "text").
	Format string `json:"format,omitempty"`
}

func (a CodeAnalyzeAction) GetActionType() ActionType { return ActionCodeAnalyze }
func (a CodeAnalyzeAction) Target() string            { return a.TargetPath }
func (a CodeAnalyzeAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

// CodeLintAction represents running a linter on code.
type CodeLintAction struct {
	// TargetPath is the directory or file to lint.
	TargetPath string `json:"target_path"`
	// Language is the programming language.
	Language string `json:"language,omitempty"`
	// Tool is the linter tool to use (e.g., "eslint", "ruff", "golangci-lint").
	Tool string `json:"tool"`
	// Rules are specific lint rules or configurations.
	Rules []string `json:"rules,omitempty"`
	// Args are additional arguments for the linter.
	Args []string `json:"args,omitempty"`
}

func (a CodeLintAction) GetActionType() ActionType { return ActionCodeLint }
func (a CodeLintAction) Target() string            { return a.TargetPath }
func (a CodeLintAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

// CodeSymbolsAction represents extracting symbols from code.
type CodeSymbolsAction struct {
	// TargetPath is the file to analyze.
	TargetPath string `json:"target_path"`
	// Language is the programming language.
	Language string `json:"language,omitempty"`
}

func (a CodeSymbolsAction) GetActionType() ActionType { return ActionCodeSymbols }
func (a CodeSymbolsAction) Target() string            { return a.TargetPath }
func (a CodeSymbolsAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}
