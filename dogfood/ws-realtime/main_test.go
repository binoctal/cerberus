package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/binoctal/cerberus/internal/project"
)

func TestServer_Login(t *testing.T) {
	srv := httptest.NewServer(newServer().routes())
	defer srv.Close()

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantToken  bool
	}{
		{name: "valid creds", body: `{"email":"web@dogfood.local","password":"dogfood-web"}`, wantStatus: http.StatusOK, wantToken: true},
		{name: "missing creds", body: `{"email":"","password":""}`, wantStatus: http.StatusUnauthorized, wantToken: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/login", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tc.wantStatus)
			}
			var got struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&got)
			if tc.wantToken && got.Token == "" {
				t.Fatal("expected non-empty token")
			}
		})
	}
}

func TestServer_Realtime(t *testing.T) {
	srv := httptest.NewServer(newServer().routes())
	defer srv.Close()

	tok := login(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Invalid token: Accept always completes the WS handshake (101); the
	// handler closes post-upgrade, so Dial succeeds and the first Read fails.
	bad, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/realtime", "bogus", "web"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, _, err := bad.Read(ctx); err == nil {
		t.Fatal("expected read error after invalid-token close")
	}
	_ = bad.CloseNow()

	// Valid token: devices:sync on connect, then device:command -> device:ack.
	c, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/realtime", tok, "web"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()

	if _, data, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	} else if msgType(data) != "devices:sync" {
		t.Fatalf("first msg=%s want devices:sync", data)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"device:command"}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ack ackShape
	if err := json.Unmarshal(data, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Type != "device:ack" || !ack.Payload.Approved || ack.Payload.Role != "web" {
		t.Fatalf("ack=%s", data)
	}
}

// login posts valid credentials and returns the issued token.
func login(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Post(base+"/login", "application/json", strings.NewReader(`{"email":"web@dogfood.local","password":"dogfood-web"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var got struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	return got.Token
}

// wsURL builds a ws:// URL with token and type query params.
func wsURL(base, path, token, role string) string {
	u := strings.Replace(base, "http://", "ws://", 1) + path
	return fmt.Sprintf("%s?token=%s&type=%s", u, token, role)
}

func msgType(data []byte) string {
	var m struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &m)
	return m.Type
}

type ackShape struct {
	Type    string `json:"type"`
	Payload struct {
		Approved bool   `json:"approved"`
		Role     string `json:"role"`
	} `json:"payload"`
}

func TestProjectConfig_Loads(t *testing.T) {
	cfg, err := project.LoadFromFile(".cerberus/project.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(cfg.Services); got != 1 || cfg.Services[0].Name != "realtime" {
		t.Fatalf("services=%+v", cfg.Services)
	}
	svc := cfg.Services[0]
	if svc.Protocol == nil {
		t.Fatal("protocol_ref not resolved into svc.Protocol")
	}
	if svc.Protocol.Framing != "json" {
		t.Fatalf("framing=%q want json", svc.Protocol.Framing)
	}
	if svc.Protocol.Auth == nil || svc.Protocol.Auth.Param != "token" || svc.Protocol.Auth.CredentialRef != "web-actor" {
		t.Fatalf("auth=%+v", svc.Protocol.Auth)
	}
	web := svc.Protocol.Roles["web"]
	if web == nil || web.Handshake == nil || web.Handshake.AwaitType != "device:online" {
		t.Fatalf("web role/handshake=%+v", web)
	}
	if len(cfg.Actors) != 1 || cfg.Actors[0].Name != "web-actor" {
		t.Fatalf("actors=%+v", cfg.Actors)
	}
}
