package runtime

import "testing"

// contains must match a substring anywhere, not just as a prefix — a go.mod
// with a leading comment block must still be recognized as the cerberus module.
func TestContainsMatchesAnywhereNotJustPrefix(t *testing.T) {
	if !contains("// header comment\nmodule github.com/binoctal/cerberus\n\ngo 1.25",
		"module github.com/binoctal/cerberus") {
		t.Fatal("contains must match a substring anywhere, not just at the start")
	}
}
