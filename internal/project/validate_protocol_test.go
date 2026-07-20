package project

import (
	"strings"
	"testing"
)

func TestValidateProtocol(t *testing.T) {
	actor := Actor{Name: "web"}
	cases := []struct {
		name    string
		p       *Protocol
		actors  []Actor
		wantErr string // non-empty substring expected when invalid
	}{
		{name: "nil ok", p: nil, actors: nil, wantErr: ""},
		{name: "empty defaults ok", p: &Protocol{}, actors: nil, wantErr: ""},
		{name: "json framing ok", p: &Protocol{Framing: "json"}, actors: nil, wantErr: ""},
		{name: "text framing rejected", p: &Protocol{Framing: "text"}, actors: nil, wantErr: "framing"},
		{name: "binary framing rejected", p: &Protocol{Framing: "binary"}, actors: nil, wantErr: "framing"},
		{name: "bad strategy", p: &Protocol{Auth: &ProtocolAuth{Strategy: "cookie", Param: "t"}}, actors: nil, wantErr: "strategy"},
		{name: "strategy without param", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query"}}, actors: nil, wantErr: "param"},
		{name: "credential_ref missing actor", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "ghost"}}, actors: []Actor{actor}, wantErr: "credential_ref"},
		{name: "credential_ref ok", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}, actors: []Actor{actor}, wantErr: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocol(tc.p, tc.actors)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
		})
	}
}

func TestValidateIntegrationRejectsBadProtocol(t *testing.T) {
	cfg := &Config{
		Services: []Service{{Name: "rt", URL: "http://x", Protocol: &Protocol{Framing: "text"}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want validation error for text framing")
	}
}

func TestValidateProtocolRoles(t *testing.T) {
	actor := Actor{Name: "web"}
	cases := []struct {
		name    string
		p       *Protocol
		wantErr string
	}{
		{name: "role ok", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{"web": {CredentialRef: "web", Params: map[string]string{"type": "web"}}}},
			wantErr: ""},
		{name: "role credential_ref missing actor", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {CredentialRef: "ghost"}}},
			wantErr: "credential_ref"},
		{name: "role param collides with auth.param", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{"web": {Params: map[string]string{"token": "x"}}}},
			wantErr: "auth.param"},
		{name: "handshake missing await_type", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{Timeout: 5}}}},
			wantErr: "await_type"},
		{name: "handshake timeout zero", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{AwaitType: "x", Timeout: 0}}}},
			wantErr: "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocol(tc.p, []Actor{actor})
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
