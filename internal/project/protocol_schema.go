package project

// Protocol declares the stable, knowable-ahead-of-time WebSocket protocol
// facts for a service. A nil Protocol on a Service means "fall back to M0
// behavior" (top-level type matching, json framing, no auto auth).
type Protocol struct {
	// Framing is the wire framing: "json" (the default when empty), "text", or
	// "binary". json routes receive by type_path over text frames; text matches a
	// whole-frame string exactly; binary matches whole-frame bytes exactly with
	// the message/type carried as base64. See the WS framing design spec.
	Framing string `yaml:"framing,omitempty"`
	// TypePath is the dotted path to the message-routing key; default "type".
	TypePath string `yaml:"type_path,omitempty"`
	// Auth declares how credentials are attached and which actor supplies them.
	Auth *ProtocolAuth `yaml:"auth,omitempty"`
	// Roles maps named connection types (e.g. "web", "bridge") to their
	// per-role declaration. Empty means no roles (M1 behavior).
	Roles map[string]*ProtocolRole `yaml:"roles,omitempty"`
	// Batches maps a batch routing type to its decomposition: when a frame whose
	// routing key (at type_path) matches a key here, the read pump expands it into
	// N synthetic item frames (one per element of the array at items_path), each
	// retyped to item_type — so every consumer sees the items with no per-callsite
	// change. json framing only. Empty means no batch decomposition (backwards-
	// compat). See the WS batch decomposition design spec.
	Batches map[string]*ProtocolBatch `yaml:"batches,omitempty"`
	// Violations declares the protocol's negative behaviors: trigger a
	// violation from a role and expect the declared rejection (error frame,
	// close code, or HTTP status). Hand-authored SUT facts; see the negative
	// case family design spec.
	Violations []Violation `yaml:"violations,omitempty"`
	// HTTPTriggers declares HTTP routes that trigger a WS message push when hit
	// (a public HTTP route whose handler fans a message out to WS clients via the
	// DO /broadcast). Each trigger drives one deterministic Steps case
	// (connect the recipient → http_request → receive the pushed type). Empty ⇒
	// no trigger cases (backwards-compatible).
	HTTPTriggers []*HTTPTrigger `yaml:"http_triggers,omitempty"`
}

// HTTPTrigger declares one HTTP→WS push trigger for the deterministic generator.
type HTTPTrigger struct {
	ID      string             `yaml:"id"`
	Request HTTPTriggerRequest `yaml:"request"`
	Effect  HTTPTriggerEffect  `yaml:"effect"`
}

// HTTPTriggerRequest describes the HTTP request that triggers the push.
type HTTPTriggerRequest struct {
	Method       string `yaml:"method"`        // HTTP method; defaults to POST at generation
	Path         string `yaml:"path"`          // host-relative (e.g. /api/devices/{{bridge.deviceId}}/restart)
	AuthRole     string `yaml:"auth_role"`     // declared role whose actor's http_login token authorizes the request
	ExpectStatus int    `yaml:"expect_status"` // expected response status; 0 ⇒ no assertion
}

// HTTPTriggerEffect describes the WS message the push delivers.
type HTTPTriggerEffect struct {
	MessageType string `yaml:"message_type"` // routing type received on the WS connection
	ToRole      string `yaml:"to_role"`      // declared role whose connection receives it
}

// ProtocolBatch declares how a single batch frame decomposes into N item frames.
// Phase 1: each array element becomes the whole "payload" of a synthetic json
// frame {"type": item_type, "payload": <element>}.
type ProtocolBatch struct {
	// ItemType is the routing key applied to each expanded item frame.
	ItemType string `yaml:"item_type"`
	// ItemsPath is the dotted JSON path to the array within the batch frame
	// (e.g. "payload.lines").
	ItemsPath string `yaml:"items_path"`
}

// ProtocolAuth declares where a credential goes and which actor supplies it.
type ProtocolAuth struct {
	// Strategy is where the credential is placed: query | header | subprotocol.
	Strategy string `yaml:"strategy"`
	// Param is the query-param name, header name, or subprotocol name.
	Param string `yaml:"param"`
	// CredentialRef names an entry in actors[] whose resolved raw token is used.
	CredentialRef string `yaml:"credential_ref"`
}

// ProtocolRole declares a named connection type's credential, discriminator
// facts, and optional mandatory handshake. The executor expands a ws_connect
// that names this role.
type ProtocolRole struct {
	// CredentialRef names the actor whose resolved raw token is injected for
	// this role (overrides protocol.auth.credential_ref).
	CredentialRef string `yaml:"credential_ref"`
	// Params are discriminator query params strip-then-injected onto the dial
	// url. Must not include protocol.auth.param when auth.strategy is query
	// (the token slot).
	Params map[string]string `yaml:"params,omitempty"`
	// Headers are discriminator dial headers strip-then-injected (delete-then-
	// set). Must not include protocol.auth.param when auth.strategy is header.
	Headers map[string]string `yaml:"headers,omitempty"`
	// Subprotocols are discriminator subprotocol names offered (strip-then-
	// injected: remove-then-append). Must not include protocol.auth.param when
	// auth.strategy is subprotocol.
	Subprotocols []string `yaml:"subprotocols,omitempty"`
	// Handshake is the optional mandatory post-connect exchange.
	Handshake *RoleHandshake `yaml:"handshake,omitempty"`
	// HTTPOnly marks a role that never connects over WebSocket — it exists
	// solely so HTTP steps can AuthRole-inject its credential (e.g. an admin
	// JWT for the /api/admin route sweep). Client-role selection skips it.
	HTTPOnly bool `yaml:"http_only,omitempty"`
	// Responses maps a received message type to the reply type this role's test
	// driver sends in response (received_type → reply_type). Drives the
	// deterministic two-role request-response case generator. Empty ⇒ this role
	// is never driven as a responder (backward-compatible).
	Responses map[string]string `yaml:"responses,omitempty" json:"responses,omitempty"`
	// RequestPayload declares the payload fields a requester must include when
	// sending a given received_type to this role (received_type → {field → template}).
	// Templates carry {{param}}/{{role.param}} placeholders resolved at send time
	// from provisioned actor state. Drives the deterministic two-role
	// request-response generator. Empty ⇒ the requester sends a bare type envelope.
	RequestPayload map[string]map[string]string `yaml:"request_payload,omitempty" json:"request_payload,omitempty"`
}

// RoleHandshake declares the message the executor auto-awaits after connect.
type RoleHandshake struct {
	// AwaitType is the routing-key value (at protocol.type_path) to wait for.
	AwaitType string `yaml:"await_type"`
	// Timeout is seconds to wait; must be > 0 (validation) so a mandatory
	// handshake cannot hang a case indefinitely.
	Timeout int `yaml:"timeout,omitempty"`
	// Optional makes the handshake best-effort: when true, a timeout still
	// succeeds the connect (the connection stays usable via the read pump).
	// Default false = mandatory: a timeout fails the connect and tears down the
	// connection. An optional handshake still requires await_type and timeout>0.
	Optional bool `yaml:"optional,omitempty"`
}
