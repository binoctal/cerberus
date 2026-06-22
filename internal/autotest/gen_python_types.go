package autotest

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

// PythonTestGenerator generates pytest tests for Python projects
type PythonTestGenerator struct {
	driver    *ai.Driver
	logger    *zap.Logger
	pythonCmd string
}

// PythonAstInfo represents extracted function/class info from Python AST
type PythonAstInfo struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"` // "function", "class", "method"
	LineNo    int      `json:"lineno"`
	ClassName string   `json:"class,omitempty"`
	Docstring string   `json:"docstring,omitempty"`
	Args      []string `json:"args,omitempty"`
}

// NewPythonTestGenerator creates a new Python test generator. A wrong type or
// nil driver is a programming error and fails fast (previously it silently left
// driver=nil and panicked later inside Generate).
func NewPythonTestGenerator(driver interface{}) *PythonTestGenerator {
	d, ok := driver.(*ai.Driver)
	if !ok || d == nil {
		panic(fmt.Sprintf("NewPythonTestGenerator: requires non-nil *ai.Driver, got %T", driver))
	}
	return &PythonTestGenerator{
		driver:    d,
		logger:    zap.NewNop(),
		pythonCmd: "python3",
	}
}

// SetLogger sets the logger for the generator
func (g *PythonTestGenerator) SetLogger(logger *zap.Logger) {
	g.logger = logger
}

// SetPythonCmd sets the Python interpreter command
func (g *PythonTestGenerator) SetPythonCmd(cmd string) {
	g.pythonCmd = cmd
}

// abs returns the absolute value of x
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
