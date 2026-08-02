//go:build integration

package agent

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCaptureServerRoundTrip validates the capture server with no external
// dependency: start it, POST to it, awaitPOST must observe the capture.
func TestCaptureServerRoundTrip(t *testing.T) {
	c := newCaptureServer(t, 9099)
	t.Cleanup(c.stop)

	go func() {
		resp, err := http.Post(c.base()+"/api/multiagent/internal/orchestrator/event",
			"application/json", strings.NewReader(`{"type":"multiagent:task_progress"}`))
		if err != nil {
			t.Errorf("post: %v", err)
			return
		}
		_ = resp.Body.Close()
	}()

	got, ok := c.awaitPOST("/api/multiagent/internal/orchestrator/event", "task_progress", 2*time.Second)
	if !ok {
		t.Fatal("awaitPOST: no capture within timeout")
	}
	if !strings.Contains(got.Body, "task_progress") {
		t.Fatalf("captured body = %q, want task_progress substring", got.Body)
	}

	// reset clears recorded POSTs.
	c.reset()
	if _, ok := c.awaitPOST("/api/multiagent/internal/orchestrator/event", "", 100*time.Millisecond); ok {
		t.Fatal("reset did not clear captured POSTs")
	}
}

type capturedPOST struct {
	Path string
	Body string
	At   time.Time
}

type captureServer struct {
	baseURL string
	mu      sync.Mutex
	posts   []capturedPOST
	srv     *http.Server
}

// newCaptureServer binds a fixed port and serves until stop. If the port is
// unavailable, t.Skipf with a clear prerequisite message (matches the existing
// reachable()-skip idiom) rather than failing the suite.
func newCaptureServer(t *testing.T, port int) *captureServer {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	c := &captureServer{baseURL: "http://" + addr}
	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handle)
	c.srv = &http.Server{Addr: addr, Handler: mux}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("capture server: cannot bind %s (%v); is another instance running?", addr, err)
	}
	go func() { _ = c.srv.Serve(ln) }()
	return c
}

func (c *captureServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	c.mu.Lock()
	c.posts = append(c.posts, capturedPOST{Path: r.URL.Path, Body: string(body), At: time.Now()})
	c.mu.Unlock()
	_, _ = w.Write([]byte("ok"))
}

// awaitPOST polls recorded POSTs until one matches path (and bodySubstring when
// non-empty) or timeout elapses. Returns the capture and true on match.
func (c *captureServer) awaitPOST(path, bodySubstring string, timeout time.Duration) (capturedPOST, bool) {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		for _, p := range c.posts {
			if p.Path == path && (bodySubstring == "" || strings.Contains(p.Body, bodySubstring)) {
				c.mu.Unlock()
				return p, true
			}
		}
		c.mu.Unlock()
		if time.Now().After(deadline) {
			return capturedPOST{}, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *captureServer) reset() {
	c.mu.Lock()
	c.posts = nil
	c.mu.Unlock()
}

func (c *captureServer) base() string { return c.baseURL }

func (c *captureServer) stop() { _ = c.srv.Close() }
