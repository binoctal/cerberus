package agent

import "testing"

func TestSteerPromptDocumentsWSPrimitives(t *testing.T) {
	for _, want := range []string{
		"ws_connect", "ws_send", "ws_receive", "ws_disconnect",
		"connection_id", "decisive",
	} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
	if !contains(promptSteerSystem, "at most one decisive") {
		t.Fatal("steer prompt must state at-most-one-decisive")
	}
}

func TestSteerPromptMentionsProtocolDeclaration(t *testing.T) {
	for _, want := range []string{
		"declares a protocol",
		"omit credentials",
		"type_path",
	} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}

func TestSteerPromptMentionsRoles(t *testing.T) {
	for _, want := range []string{"role", "handshake"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}

func TestSteerPromptMentionsAssert(t *testing.T) {
	for _, want := range []string{"assert", "deterministic"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}

func TestSteerPromptMentionsMatchAll(t *testing.T) {
	// match_all is the only way to assert "every item satisfies P" when the
	// item count is unknown at authoring time. If the Steer prompt does not
	// surface it, the LLM never authors it and the feature is unreachable.
	for _, want := range []string{"match_all", "every item"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}

func TestSteerPromptMentionsBatches(t *testing.T) {
	// The pump decomposes a declared batch into per-item frames; the LLM must
	// know to await the item type (not the batch type) when a batch is declared.
	for _, want := range []string{"batches", "item type"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}
