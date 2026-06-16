package architecture

// FunctionMetrics represents metrics for a single function
type FunctionMetrics struct {
	Name         string
	Parameters   int
	Cyclomatic   int
	NestingDepth int
	LineNumber   int
}

// complexityVisitor visits AST nodes to calculate complexity
type complexityVisitor struct {
	depth           int
	maxDepth        int
	complexity      int
	inControlFlow   bool // Track if we're inside a control flow structure
}

// controlFlowVisitor handles visiting children of control flow structures
type controlFlowVisitor struct {
	parent *complexityVisitor
}
