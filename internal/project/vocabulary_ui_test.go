package project

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const uiVocabSrc = `
source: {protocol_ref: ""}
edges: []
ui:
  base_url: http://localhost:5183
  locale: en
  auth_actor: web-actor
  assertions:
    - id: missions-conn-status
      route: /dashboard/missions
      target: "text=Connected"
      expectation: text_present
      timeout: 15
    - id: devices-counter
      route: /dashboard/devices
      target: "text=devices online"
      expectation: text_present
    - id: devices-table
      route: /dashboard/devices
      target: "css=table tbody tr"
      expectation: element_count>=1
    - id: exempt-page
      route: /dashboard/plan
      target: "css=.billing"
      expectation: element_visible
      unsupported: true
      reason: plan-gated render depends on the seeded plan row
`

func TestVocabularyUIDecodeAndValidate(t *testing.T) {
	var v Vocabulary
	if err := yaml.Unmarshal([]byte(uiVocabSrc), &v); err != nil {
		t.Fatal(err)
	}
	if v.UI == nil || len(v.UI.Assertions) != 4 {
		t.Fatalf("decode: %+v", v.UI)
	}
	if v.UI.BaseURL != "http://localhost:5183" || v.UI.Locale != "en" || v.UI.AuthActor != "web-actor" {
		t.Fatalf("ui fields: %+v", v.UI)
	}
	if err := ValidateVocabulary(&v); err != nil {
		t.Fatalf("valid ui vocab rejected: %v", err)
	}
}

func TestVocabularyUIValidateRejects(t *testing.T) {
	bad := []string{
		// missing base_url
		"ui:\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present}]",
		// missing locale
		"ui:\n  base_url: http://x\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present}]",
		// missing route
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, target: \"text=x\", expectation: text_present}]",
		// missing target
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, expectation: text_present}]",
		// missing expectation
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\"}]",
		// unknown comparator
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: wat}]",
		// duplicate id
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present},{id: a, route: /r2, target: \"text=y\", expectation: text_present}]",
		// unsupported without reason
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present, unsupported: true}]",
	}
	for i, src := range bad {
		var v Vocabulary
		if err := yaml.Unmarshal([]byte(src), &v); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		if err := ValidateVocabulary(&v); err == nil {
			t.Errorf("case %d: invalid ui vocab accepted", i)
		}
	}
}
