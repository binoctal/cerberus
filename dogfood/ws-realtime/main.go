// Package main is a minimal WebSocket target for the cerberus WS-realtime
// dogfood. It mirrors open-agents' shape: an HTTP login endpoint issues a
// token, and a WebSocket endpoint validates it. See
// cerberus-docs/superpowers/specs/2026-07-21-ws-realtime-dogfood-design.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// server is the in-memory dogfood target. Tokens issued by /login are held
// here and validated on WS connect. No persistence; loose validation only.
type server struct {
	mu     sync.Mutex
	tokens map[string]bool
	next   int
}

func newServer() *server {
	return &server{tokens: make(map[string]bool)}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin issues a token for any non-empty credentials. The dogfood does
// not authenticate the password; it only needs a round-trippable token for
// the executor's auth_flow -> rawToken -> WS query injection chain.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password required", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	s.next++
	tok := fmt.Sprintf("tok-%d", s.next)
	s.tokens[tok] = true
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
}

// handleWS accepts a WebSocket, validates the ?token= query against issued
// tokens, sends devices:sync unconditionally, then replies device:ack to any
// device:command. ?type= flavors the ack role (default "web").
func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()

	s.mu.Lock()
	ok := s.tokens[r.URL.Query().Get("token")]
	s.mu.Unlock()
	if !ok {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid token")
		return
	}
	role := r.URL.Query().Get("type")
	if role == "" {
		role = "web"
	}

	ctx := r.Context()
	syncMsg, _ := json.Marshal(map[string]any{"type": "devices:sync", "devices": []any{}})
	if err := c.Write(ctx, websocket.MessageText, syncMsg); err != nil {
		return
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg["type"] == "device:command" {
			ack, _ := json.Marshal(map[string]any{
				"type":    "device:ack",
				"payload": map[string]any{"approved": true, "role": role},
			})
			if err := c.Write(ctx, websocket.MessageText, ack); err != nil {
				return
			}
		}
	}
}

// routes wires the HTTP endpoints: POST /login issues tokens; /realtime and
// the lenient / both upgrade to the WebSocket handler.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("/realtime", s.handleWS)
	mux.HandleFunc("/", s.handleWS) // lenient: accept WS upgrade at root too
	return mux
}

func main() {
	addr := flag.String("addr", ":8787", "listen address")
	flag.Parse()
	log.Printf("ws-realtime dogfood target listening on %s (POST /login, WS /realtime)", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer().routes()))
}
