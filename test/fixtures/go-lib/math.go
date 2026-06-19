// Package math provides simple math functions for fixture testing.
package math

// Add returns the sum of two integers. Intentionally has no test file
// (no math_test.go) so AutoTest detects it as an uncovered gap.
func Add(a, b int) int {
	return a + b
}

// Sub returns the difference. Also uncovered.
func Sub(a, b int) int {
	return a - b
}
