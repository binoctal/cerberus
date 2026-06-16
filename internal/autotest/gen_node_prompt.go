package autotest

import "fmt"

func (g *NodeTestGenerator) buildPrompt(pkg, file, snippet string) string {
	return fmt.Sprintf(`You are a Jest test author. Emit a single complete .test.js file using describe/it blocks.
Use modern JavaScript (ES6+) and Jest best practices.
Output ONLY the JavaScript source, no markdown fences.

Write a Jest test file for this code.

File: %s
Package: %s

Source code:
%s

Return a complete .test.js file with proper imports, describe blocks, and test cases.
Include edge cases and error handling where appropriate.`, file, pkg, snippet)
}
