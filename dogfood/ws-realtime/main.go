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

// routes wires the HTTP endpoints. Task 1 registers only /login; Task 2 adds
// the WebSocket /realtime (and lenient /) routes.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.handleLogin)
	return mux
}

func main() {
	addr := flag.String("addr", ":8787", "listen address")
	flag.Parse()
	log.Printf("ws-realtime dogfood target listening on %s (POST /login, WS /realtime)", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer().routes()))
}
