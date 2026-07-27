package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/llm"
)

// TestPlanTools_WSReceiveAssertSchemaGuidesEmission locks the ws_receive
// `assert` property description that steers the planning LLM away from the
// malformed shapes observed in dogfood (2026-07-28): expression-style
// {field,op,value} and wrong-path-name {msgType:...}. Both false-failed
// correctly-matched device:online relays at execution (checkAsserts treats
// each key as a JSON path). The description must convey (a) path->value map
// shape, (b) omit for arrival-only, and (c) not an expression. Regression
// guard: if someone strips the description, the LLM reverts to improvising.
func TestPlanTools_WSReceiveAssertSchemaGuidesEmission(t *testing.T) {
	tools := planTools()
	var wsReceive *llm.Tool
	for i := range tools {
		if tools[i].Name == "ws_receive" {
			wsReceive = &tools[i]
			break
		}
	}
	if wsReceive == nil {
		t.Fatal("planTools: ws_receive tool not found")
	}
	props, _ := wsReceive.InputSchema["properties"].(map[string]any)
	assertProp, _ := props["assert"].(map[string]any)
	desc, _ := assertProp["description"].(string)
	if desc == "" {
		t.Fatalf("ws_receive.assert missing description (the lever that prevents malformed assert emission)")
	}
	for what, needle := range map[string]string{
		"path->value shape": "path",
		"omit-for-arrival":  "OMIT",
		"not-an-expression": "field/op/value",
	} {
		if !strings.Contains(desc, needle) {
			t.Errorf("ws_receive.assert description missing %s (want substring %q):\n%s", what, needle, desc)
		}
	}
}
