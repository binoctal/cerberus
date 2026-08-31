//go:build integration

// Live gate for the v2 authed param-chain tier (sampled): -authed cases with
// a capture + target step pair generated from the dogfood open-agents vocab
// must actually resolve their list-route capture and return 2xx (or the
// route's declared 2xx_4xx class) against the real wrangler dev server. This
// is the gate that would have caught the v2 final-review C1 break — picks
// that do not match real response shapes fail here, not silently mid-run.
package scout

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// authedSampleJWTs returns the web JWT (dev login) and the admin JWT.
// Admin provisioning mirrors the dogfood admin-actor: /api/dev/setup seeds
// role:'admin' (needs open-agents >= 5e9c3b1; the old /api/auth/dev/setup
// superadmin endpoint was removed in d6e5390 and returned the JWT directly),
// then /api/dev/login issues the token. Skips when the server rejects
// either login.
func authedSampleJWTs(t *testing.T, base string) (web, admin string) {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	post := func(path string, body string, wantToken bool) string {
		req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build %s: %v", path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", base)
		resp, err := client.Do(req)
		if err != nil {
			t.Skipf("%s: %v (server went away)", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode != http.StatusOK {
			t.Skipf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
		}
		var out struct {
			Success bool   `json:"success"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			t.Skipf("%s: bad json in response", path)
		}
		if wantToken && out.Token == "" {
			t.Skipf("%s: no token in response", path)
		}
		return out.Token
	}
	// Provisioning returns a success/user/device envelope with no JWT; only
	// the logins after it yield tokens.
	post("/api/dev/setup", `{"email":"admin@openagents.local","password":"admin123456","plan":"pro","role":"admin"}`, false)
	return post("/api/dev/login", `{}`, true),
		post("/api/dev/login", `{"email":"admin@openagents.local","password":"admin123456"}`, true)
}

// walkPick mirrors the executor's captureFromHTTPBody dot-path walk (map key
// or non-negative array index per segment). found=false with emptyArray=true
// means the chain is live-correct but the list is currently empty (no record
// to chain from) — a skip, not a failure.
func walkPick(body string, path string) (value string, found, emptyArray bool) {
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", false, false
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		if m, isMap := cur.(map[string]any); isMap {
			v, ok := m[seg]
			if !ok {
				return "", false, false
			}
			cur = v
			continue
		}
		arr, isArr := cur.([]any)
		if !isArr {
			return "", false, false
		}
		idx, err := strconv.Atoi(seg)
		if err != nil || idx < 0 {
			return "", false, false
		}
		if idx >= len(arr) {
			return "", false, len(arr) == 0
		}
		cur = arr[idx]
	}
	switch cur.(type) {
	case map[string]any, []any:
		return "", false, false
	}
	return fmt.Sprint(cur), true, false
}

// authedSampleDo performs one Bearer-authenticated request and returns the
// body and status.
func authedSampleDo(t *testing.T, client *http.Client, method, url, jwt, body string) (string, int) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:8989")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), resp.StatusCode
}

// TestHTTPRouteAuthedParamChainSampleLive samples the authed param-chain
// tier against the live server: the first 5 admin-carried and first 5
// web-carried capture chains in generation order. A chain executes both
// steps: capture (list route, list route's role) must 2xx and resolve its
// pick; the target must satisfy its declared status class. Chains whose list
// is currently empty are skipped (no record to chain from); the gate fails
// when fewer than 3 chains execute, either carrier is unexercised, or any
// executed chain fails.
func TestHTTPRouteAuthedParamChainSampleLive(t *testing.T) {
	vocabPath := filepath.Join("..", "..", "..", "dogfood", "realtime-e2e", ".cerberus", "vocab", "open-agents.vocab.yaml")
	if _, err := os.Stat(vocabPath); err != nil {
		t.Skipf("dogfood vocab not available: %v", err)
	}
	vocab, err := project.LoadVocabulary(vocabPath)
	if err != nil {
		t.Fatalf("load dogfood vocab: %v", err)
	}
	const base = "http://localhost:8989"
	client := &http.Client{Timeout: 15 * time.Second}
	if resp, err := client.Get(base + "/health"); err != nil {
		t.Skipf("open-agents dev server not reachable on %s: %v", base, err)
	} else {
		resp.Body.Close()
	}
	webJWT, adminJWT := authedSampleJWTs(t, base)

	// Real protocol role shapes (dogfood protocols/open-agents.yaml): the
	// web role carries the dev-login credential, admin the superadmin one.
	// CredentialRef presence is all roleForRoute needs.
	svc := project.Service{
		Name: "open-agents",
		URL:  base,
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web":   {CredentialRef: "web-actor"},
			"admin": {CredentialRef: "admin-actor"},
		}},
		Vocabulary: vocab,
	}
	jwtFor := map[string]string{"web": webJWT, "admin": adminJWT}

	var chains []agent.TestCase
	for _, c := range httpRouteCases(svc) {
		if strings.HasSuffix(c.ID, "-authed") && len(c.Steps) == 2 {
			chains = append(chains, c)
		}
	}
	if len(chains) == 0 {
		t.Fatal("zero 2-step -authed cases generated — the param-chain tier went vacuous")
	}
	var sample []agent.TestCase
	adminSeen, webSeen := 0, 0
	for _, c := range chains {
		admin := c.Steps[0].AuthRole == "admin"
		if (admin && adminSeen < 5) || (!admin && webSeen < 5) {
			sample = append(sample, c)
			if admin {
				adminSeen++
			} else {
				webSeen++
			}
		}
	}

	executed, passed, skippedEmpty := 0, 0, 0
	adminExec, webExec := false, false
	for _, c := range sample {
		capStep, tgtStep := c.Steps[0], c.Steps[1]
		capJWT := jwtFor[capStep.AuthRole]
		if capJWT == "" {
			t.Fatalf("%s: capture step role %q has no JWT wired", c.ID, capStep.AuthRole)
		}
		body, code := authedSampleDo(t, client, capStep.Method, capStep.URL, capJWT, "")
		if code < 200 || code > 299 {
			t.Errorf("%s: capture step %s %s status %d, want 2xx", c.ID, capStep.Method, capStep.URL, code)
			continue
		}
		params := map[string]string{}
		for pick, name := range capStep.Capture {
			v, found, empty := walkPick(body, pick)
			if !found {
				if empty {
					skippedEmpty++
					t.Logf("%s: capture pick %q — list empty, no record to chain (skip)", c.ID, pick)
				} else {
					t.Errorf("%s: capture pick %q not found in %s %s body", c.ID, pick, capStep.Method, capStep.URL)
				}
				continue
			}
			params[name] = v
		}
		if len(params) < len(capStep.Capture) {
			continue // capture unresolved — already reported or skipped
		}
		tgtURL := tgtStep.URL
		for name, v := range params {
			tgtURL = strings.ReplaceAll(tgtURL, "{{case."+name+"}}", v)
		}
		tgtJWT := jwtFor[tgtStep.AuthRole]
		if tgtJWT == "" {
			t.Fatalf("%s: target step role %q has no JWT wired", c.ID, tgtStep.AuthRole)
		}
		_, code = authedSampleDo(t, client, tgtStep.Method, tgtURL, tgtJWT, tgtStep.Body)
		executed++
		if capStep.AuthRole == "admin" {
			adminExec = true
		} else {
			webExec = true
		}
		ok := code >= 200 && code <= 299
		if tgtStep.ExpectStatusClass == "2xx_4xx" {
			ok = (code >= 200 && code <= 299) || (code >= 400 && code <= 499)
		}
		if ok {
			passed++
			continue
		}
		// The open-agents body-less-PUT 500 family is fixed (d729629: empty
		// bodies now 400) — a 5xx on the target step is a real failure.
		// Diagnostics only; no exemption.
		t.Logf("%s: target %s %s (body %q) status %d, want %s", c.ID, tgtStep.Method, tgtURL, tgtStep.Body, code, tgtStep.ExpectStatusClass)
		t.Errorf("%s: target %s %s status %d, want %s", c.ID, tgtStep.Method, tgtURL, code, tgtStep.ExpectStatusClass)
	}
	fmt.Printf("authed param-chain gate: sampled %d chains, %d executed, %d passed, %d skipped (empty list)\n",
		len(sample), executed, passed, skippedEmpty)
	if executed < 3 {
		t.Fatalf("only %d of %d sampled chains executed — the gate went vacuous", executed, len(sample))
	}
	if !adminExec || !webExec {
		t.Fatalf("gate must exercise both capture carriers: admin=%v web=%v", adminExec, webExec)
	}
	if passed != executed {
		t.Fatalf("%d of %d executed chains failed", executed-passed, executed)
	}
}
