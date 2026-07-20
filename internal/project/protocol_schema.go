package project

// Protocol declares the stable, knowable-ahead-of-time WebSocket protocol
// facts for a service. A nil Protocol on a Service means "fall back to M0
// behavior" (top-level type matching, json framing, no auto auth).
type Protocol struct {
	// Framing is the wire framing. M1 supports "json" only (the default when
	// empty); "text"/"binary" are reserved for M2 and rejected by validation.
	Framing string `yaml:"framing,omitempty"`
	// TypePath is the dotted path to the message-routing key; default "type".
	TypePath string `yaml:"type_path,omitempty"`
	// Auth declares how credentials are attached and which actor supplies them.
	Auth *ProtocolAuth `yaml:"auth,omitempty"`
	// Roles maps named connection types (e.g. "web", "bridge") to their
	// per-role declaration. Empty means no roles (M1 behavior).
	Roles map[string]*ProtocolRole `yaml:"roles,omitempty"`
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
// query params, and optional mandatory handshake. The executor expands a
// ws_connect that names this role.
type ProtocolRole struct {
	// CredentialRef names the actor whose resolved raw token is injected for
	// this role (overrides protocol.auth.credential_ref).
	CredentialRef string `yaml:"credential_ref"`
	// Params are discriminator query params strip-then-injected onto the dial
	// url. Must not include protocol.auth.param (the token slot).
	Params map[string]string `yaml:"params,omitempty"`
	// Handshake is the optional mandatory post-connect exchange.
	Handshake *RoleHandshake `yaml:"handshake,omitempty"`
}

// RoleHandshake declares the message the executor auto-awaits after connect.
type RoleHandshake struct {
	// AwaitType is the routing-key value (at protocol.type_path) to wait for.
	AwaitType string `yaml:"await_type"`
	// Timeout is seconds to wait; must be > 0 (validation) so a mandatory
	// handshake cannot hang a case indefinitely.
	Timeout int `yaml:"timeout,omitempty"`
}
