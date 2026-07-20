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
