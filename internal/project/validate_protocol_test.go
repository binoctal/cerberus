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
		{name: "text framing ok", p: &Protocol{Framing: "text"}, actors: nil, wantErr: ""},
		{name: "binary framing ok", p: &Protocol{Framing: "binary"}, actors: nil, wantErr: ""},
		{name: "invalid framing rejected", p: &Protocol{Framing: "raw"}, actors: nil, wantErr: "framing"},
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

func TestValidateProtocol_Batches(t *testing.T) {
	cases := []struct {
		name    string
		p       *Protocol
		wantErr string
	}{
		{
			name:    "valid json batch",
			p:       &Protocol{Batches: map[string]*ProtocolBatch{"session:output-batch": {ItemType: "session:output", ItemsPath: "payload.lines"}}},
			wantErr: "",
		},
		{
			name:    "missing item_type",
			p:       &Protocol{Batches: map[string]*ProtocolBatch{"b": {ItemsPath: "payload.x"}}},
			wantErr: "item_type is required",
		},
		{
			name:    "missing items_path",
			p:       &Protocol{Batches: map[string]*ProtocolBatch{"b": {ItemType: "item"}}},
			wantErr: "items_path is required",
		},
		{
			name:    "recursive item_type rejected",
			p:       &Protocol{Batches: map[string]*ProtocolBatch{"b": {ItemType: "c", ItemsPath: "p"}, "c": {ItemType: "item", ItemsPath: "p"}}},
			wantErr: "itself a batch",
		},
		{
			name:    "non-json framing rejected",
			p:       &Protocol{Framing: "text", Batches: map[string]*ProtocolBatch{"b": {ItemType: "item", ItemsPath: "p"}}},
			wantErr: "batches require json framing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocol(tc.p, nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateIntegrationRejectsBadProtocol(t *testing.T) {
	cfg := &Config{
		Services: []Service{{Name: "rt", URL: "http://x", Protocol: &Protocol{Framing: "raw"}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want validation error for invalid framing")
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
		{name: "optional handshake missing await_type", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{Timeout: 5, Optional: true}}}},
			wantErr: "await_type"},
		{name: "optional handshake timeout zero", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{AwaitType: "x", Timeout: 0, Optional: true}}}},
			wantErr: "timeout"},
		{name: "optional handshake ok", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{AwaitType: "x", Timeout: 1, Optional: true}}}},
			wantErr: ""},
		{name: "handshake timeout zero", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{AwaitType: "x", Timeout: 0}}}},
			wantErr: "timeout"},
		{name: "role headers ok (no auth)", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"X-Role": "web"}}}},
			wantErr: ""},
		{name: "role subprotocols ok (no auth)", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Subprotocols: []string{"web.v1"}}}},
			wantErr: ""},
		{name: "role header collides with auth.param (header strategy)", p: &Protocol{Auth: &ProtocolAuth{Strategy: "header", Param: "X-Role"}, Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"X-Role": "web"}}}},
			wantErr: "auth.param"},
		{name: "role subprotocol collides with auth.param (subprotocol strategy)", p: &Protocol{Auth: &ProtocolAuth{Strategy: "subprotocol", Param: "token"}, Roles: map[string]*ProtocolRole{"web": {Subprotocols: []string{"token"}}}},
			wantErr: "auth.param"},
		{name: "role header ok when auth strategy differs", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"}, Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"token": "x"}}}},
			wantErr: ""},
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

func TestValidateProtocolRole_Responses(t *testing.T) {
	mk := func(responses map[string]string) *Protocol {
		return &Protocol{
			TypePath: "type",
			Auth:     &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}, Responses: responses},
			},
		}
	}

	if err := ValidateProtocol(mk(map[string]string{"session:start": "session:created"}), nil); err != nil {
		t.Fatalf("valid responses must pass: %v", err)
	}
	if err := ValidateProtocol(mk(map[string]string{"": "session:created"}), nil); err == nil {
		t.Fatalf("empty received_type key must fail")
	}
	if err := ValidateProtocol(mk(map[string]string{"session:start": ""}), nil); err == nil {
		t.Fatalf("empty reply_type value must fail")
	}
}

func TestValidateProtocolRole_RequestPayload(t *testing.T) {
	mk := func(rp map[string]map[string]string) *Protocol {
		return &Protocol{
			TypePath: "type",
			Auth:     &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}, RequestPayload: rp},
			},
		}
	}

	if err := ValidateProtocol(mk(map[string]map[string]string{
		"session:start": {"deviceId": "{{bridge.deviceId}}"},
	}), nil); err != nil {
		t.Fatalf("valid request_payload must pass: %v", err)
	}
	if err := ValidateProtocol(mk(map[string]map[string]string{
		"": {"deviceId": "x"},
	}), nil); err == nil {
		t.Fatalf("empty received_type key must fail")
	}
	if err := ValidateProtocol(mk(map[string]map[string]string{
		"session:start": {"": "x"},
	}), nil); err == nil {
		t.Fatalf("empty field name must fail")
	}
}
