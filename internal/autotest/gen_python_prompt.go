package autotest

import "fmt"

// buildPrompt creates the prompt for test generation
func (g *PythonTestGenerator) buildPrompt(pkg, file, snippet string) string {
	return fmt.Sprintf(`You are a pytest test author. Emit a single complete test_*.py file using pytest fixtures and parametrize.
Use modern Python (3.7+) and pytest best practices.
Include proper imports, fixtures, and parameterized test cases using @pytest.mark.parametrize.
Output ONLY the Python source, no markdown fences.

Write a pytest test file for this code.

File: %s
Module: %s

Source code:
%s

Return a complete test_*.py file with:
- Proper imports (including pytest, unittest.mock if needed)
- Fixture functions using @pytest.fixture
- Parameterized test cases using @pytest.mark.parametrize
- Test class structure if testing a class
- Edge cases and error handling
- Clear test names that describe what is being tested`, file, pkg, snippet)
}
