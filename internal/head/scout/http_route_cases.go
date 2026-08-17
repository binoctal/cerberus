package scout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// httpRouteCases emits one reachability smoke per mounted HTTP route in the
// service's vocabulary: a bare-client request whose pass condition is ANY
// response (expect_status_class "any") — transport errors fail, proving the
// route is reachable, nothing more. Expectation text carries the honesty
// tier: flat routes claim reachability; :param routes note placeholder
// values; /api/admin/ routes note the admin JWT is injected when a protocol
// role named "admin" exists (reachability + authenticated routing; handler
// semantics with placeholder params remain unverified).
//
// Side-effect note: public unauthenticated mutation routes (e.g. POST
// /api/errors) will really write junk rows into the dev database. Requests
// carry no body; the cost is accepted for dev dogfood.
//
// Not claim-bound: HTTP reachability is not a ledger promise; binding would
// be ignored by reconciliation anyway, but the semantics would be dishonest.
// May overlap httpSmokeCase lazy fallbacks on the same path — different
// source (vocab vs LLM-covered) and priority, harmless by design.
func httpRouteCases(svc project.Service) []agent.TestCase {
	if svc.Vocabulary == nil || len(svc.Vocabulary.HTTPRoutes) == 0 {
		return nil
	}
	// An "admin" protocol role (HTTP-only, never WS-connects) upgrades the
	// admin route block from bare 401-reachability to authenticated requests
	// via AuthRole token injection.
	adminRole := ""
	if svc.Protocol != nil {
		if r := svc.Protocol.Roles["admin"]; r != nil && r.CredentialRef != "" {
			adminRole = "admin"
		}
	}
	routes := make([]project.VocabHTTPRoute, 0, len(svc.Vocabulary.HTTPRoutes))
	for _, r := range svc.Vocabulary.HTTPRoutes {
		if r.Partial || r.Unsupported {
			continue // exempt routes are outside the coverage denominator
		}
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	host := serviceHost(svc.URL)
	var cases []agent.TestCase
	for _, r := range routes {
		method := r.Method
		if method == "ALL" {
			method = "GET" // app.all wildcard: probe with the base verb
		}
		path := fillRouteParams(r.Path)
		authRole := ""
		if adminRole != "" && isAdminPath(r.Path) {
			authRole = adminRole
		}
		cases = append(cases, agent.TestCase{
			ID:          fmt.Sprintf("http-route-%s-%s-%s", svc.Name, strings.ToLower(method), trimPathForID(path)),
			Name:        fmt.Sprintf("%s %s reachability", method, path),
			Service:     svc.Name,
			Target:      host,
			Action:      "ws_flow",
			Expectation: routeExpectation(r.Path, authRole != ""),
			Priority:    0.5,
			Steps: []agent.TestStep{{
				Action:            "http_request",
				URL:               host + path,
				Method:            method,
				AuthRole:          authRole,
				ExpectStatusClass: "any",
			}},
		})
	}
	return cases
}

// isAdminPath: the admin route block (prefix per the open-agents layout).
func isAdminPath(path string) bool {
	return strings.HasPrefix(path, "/api/admin") || strings.Contains(path, "/admin/")
}

// fillRouteParams replaces :param segments with a stable placeholder and a
// trailing * with a fixed tail so the request path matches the route pattern.
func fillRouteParams(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = "1"
		} else if s == "*" {
			segs[i] = "x"
		}
	}
	return strings.Join(segs, "/")
}

// routeExpectation renders the honesty tier for one route.
func routeExpectation(path string, authed bool) string {
	exp := "route reachable over HTTP (any status response; no transport error)"
	if strings.Contains(path, ":") {
		exp += "; placeholder param values — handler semantics unverified"
	}
	if isAdminPath(path) {
		if authed {
			exp += "; admin JWT injected — authenticated routing"
		} else {
			exp += "; auth not penetrated — reachability only"
		}
	}
	return exp
}
