package protocoldiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrNoProtocol signals the model found no WS protocol in the inputs. It is
// distinct from a hard error: the command reports it and exits cleanly rather
// than treating an unauthenticated channel as a failure.
var ErrNoProtocol = errors.New("no websocket protocol found")

// SourceFile is a doc/example file fed to the model. It mirrors
// authdiscover.SourceFile: {Path, Content}. Content never holds credential
// values — only public docs or message examples.
type SourceFile struct {
	Path    string
	Content string
}

// inferOutput is the JSON shape the LLM must return (mirrors authdiscover's
// discoverOutput). The Driver deserializes the response into this struct.
type inferOutput struct {
	Found    bool                   `json:"found"`
	Framing  string                 `json:"framing"`
	TypePath string                 `json:"type_path"`
	Auth     *inferAuth             `json:"auth,omitempty"`
	Roles    map[string]*inferRole  `json:"roles,omitempty"`
	Batches  map[string]*inferBatch `json:"batches,omitempty"`
	Notes    string                 `json:"notes"`
}

type inferBatch struct {
	ItemType  string `json:"item_type"`
	ItemsPath string `json:"items_path"`
}

type inferAuth struct {
	Strategy      string `json:"strategy"`
	Param         string `json:"param"`
	CredentialRef string `json:"credential_ref"`
}

type inferRole struct {
	CredentialRef string            `json:"credential_ref,omitempty"`
	Params        map[string]string `json:"params,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Subprotocols  []string          `json:"subprotocols,omitempty"`
	Handshake     *inferHandshake   `json:"handshake,omitempty"`
}

type inferHandshake struct {
	AwaitType string `json:"await_type"`
	Timeout   int    `json:"timeout"`
	Optional  bool   `json:"optional"`
}

// Infer asks the LLM to draft a protocol description from the given inputs,
// validates it, and returns it (not written to disk). The driver is passed in
// so tests inject a mock and Infer never builds LLM clients. serviceName names
// the service the draft targets; it is used only to label the prompt (the actor
// list for credential_ref validation comes from cfg).
//
// The model is required to call the protocol_draft tool. Three terminal states:
//   - found=false (or omitted): the model explicitly signalled "no WS protocol
//     here" -> ErrNoProtocol. Distinct from drift so the command can exit 0.
//   - drift (zero tool calls): the model produced no tool call at all -> a
//     hard error. Not reported as ErrNoProtocol.
//   - a populated tool call: parsed by argsToProtocol then validated.
//
// On a parse failure the returned error is a static message that does NOT
// include the raw tool args. On a validation failure the cause is wrapped.
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}

	prompt := buildInferPrompt(serviceName, inputs)
	res, err := driver.DecideWithTools(ctx, prompt, []llm.Tool{protocolDraftTool()})
	if err != nil {
		return nil, errors.New("could not run protocol inference")
	}
	if len(res.ToolCalls) == 0 {
		// Drift: the model produced no tool call. This is NOT "no protocol
		// found" — that is signalled explicitly by found=false below. Drift
		// is a hard error, aligned with the S2 ADR's drift policy.
		return nil, errors.New("model produced no protocol tool call")
	}

	input := res.ToolCalls[0].Input
	// Safe read: the model may omit `found`; a missing flag is treated as
	// "not found" rather than panicking on a failed type assertion.
	if found, _ := input["found"].(bool); !found {
		return nil, ErrNoProtocol
	}

	p, err := argsToProtocol(input)
	if err != nil {
		// Malformed args; do not propagate the raw map in the error.
		return nil, errors.New("could not parse model output")
	}

	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		return nil, fmt.Errorf("model produced an invalid protocol: %w", err)
	}
	return p, nil
}

// actorsOf returns the config's actor list, used to confirm credential_ref
// names a real actor. Nil cfg yields nil (validation then rejects any ref).
func actorsOf(cfg *project.Config) []project.Actor {
	if cfg == nil {
		return nil
	}
	return cfg.Actors
}

// buildInferPrompt assembles the prompt. It describes WHAT to recognize and
// leaves the JSON shape to the protocol_draft tool schema (the tool definition
// carries it; the prompt no longer hand-writes a shape block). It never
// includes credential values — credential_ref names an actor, it is not a token.
func buildInferPrompt(serviceName string, inputs []SourceFile) string {
	var b strings.Builder
	b.WriteString("You are drafting a WebSocket protocol description for a cerberus test config.\n")
	b.WriteString("Read the docs/source below and call the protocol_draft tool once with what you infer.\n")
	fmt.Fprintf(&b, "The target service is %q.\n\n", serviceName)
	b.WriteString("Recognize and fill these structures where present:\n")
	b.WriteString("- Wire framing (json/text/binary) and the envelope: the dotted path to the message routing key (e.g. a {type,payload,...} envelope -> type_path \"type\").\n")
	b.WriteString("- How auth is attached (query param / header / subprotocol) and which actor supplies it.\n")
	b.WriteString("- Connection roles: distinct connection types (e.g. web, bridge) and their discriminator params/headers/subprotocols.\n")
	b.WriteString("- Post-connect handshake: a message the server sends (or you must send) right after connect. If it is peer-gated (only sent when a peer socket is online), set handshake.optional=true so a timeout does not fail the connect.\n")
	b.WriteString("- Message batching: if the server coalesces frames (e.g. emits a batch type carrying an array), record the batch routing key with item_type (per-item routing key) and items_path (dotted path to the array).\n\n")
	b.WriteString("If no WebSocket protocol is described, call protocol_draft with found=false.\n")
	b.WriteString("credential_ref MUST name an actor that exists in project.yaml — NEVER invent one, and NEVER include real credential values or tokens.\n\n")
	for _, f := range inputs {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(truncateContent(f.Content, 8000))
		b.WriteString("\n\n")
	}
	return b.String()
}

// truncateContent caps an input file's contribution to the prompt so a single
// huge doc cannot blow the context budget. n is a rune count (not bytes) so the
// cut never splits a multi-byte UTF-8 sequence, which would yield malformed
// output at the truncation point.
func truncateContent(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…[truncated]"
}
