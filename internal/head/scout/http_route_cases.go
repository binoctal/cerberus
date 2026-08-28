package scout

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// httpRouteCases emits tiered cases per mounted HTTP route in the service's
// vocabulary, driven by the vocab auth facts. Auth "required" routes emit:
// an unauth probe (bare client, 4xx expected — proves the auth middleware
// actually rejects), the v1 reachability smoke (any-response honesty
// fallback), and — when a credentialed protocol role exists and every :param
// has a param source — an authed case asserting 2xx with real param values
// chained from list routes and the minimal legal body when the vocab
// declares one. Auth none/unknown/unset (pre-v2 vocab) routes emit the v1
// reachability case only.
//
// Side-effect note: public unauthenticated mutation routes (e.g. POST
// /api/errors) will really write junk rows into the dev database, and the
// authed tier's legal-body mutations write real records. The cost is
// accepted for dev dogfood.
//
// Not claim-bound: HTTP reachability is not a ledger promise; binding would
// be ignored by reconciliation anyway, but the semantics would be dishonest.
// May overlap httpSmokeCase lazy fallbacks on the same path — different
// source (vocab vs LLM-covered) and priority, harmless by design.
func httpRouteCases(svc project.Service) []agent.TestCase {
	if svc.Vocabulary == nil || len(svc.Vocabulary.HTTPRoutes) == 0 {
		return nil
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
		if r.Auth == "required" {
			cases = append(cases,
				unauthRouteCase(svc, host, r, method),
				reachabilityRouteCase(svc, host, r, method))
			// The authed 2xx tier needs a credential to inject and real
			// :param values — a guessed id would 404 and lie red.
			if role := roleForRoute(svc, r.Path); role != "" && paramResolvable(r) {
				cases = append(cases, authedRouteCase(svc, host, r, method, role))
			}
			continue
		}
		// none | unknown | "" (pre-v2 vocab): reachability honesty fallback.
		cases = append(cases, reachabilityRouteCase(svc, host, r, method))
	}
	return cases
}

// routeBaseID is the v1 ID scheme shared by every tier; the suffix
// differentiates (-unauth | -authed | "" for reachability).
func routeBaseID(svc project.Service, method, path string) string {
	return fmt.Sprintf("http-route-%s-%s-%s", svc.Name, strings.ToLower(method), trimPathForID(fillRouteParams(path)))
}

// reachabilityRouteCase is the v1 shape verbatim: a bare-client request
// whose pass condition is ANY response (expect_status_class "any") —
// transport errors fail, proving the route is reachable, nothing more. The
// v1 admin-block JWT injection now flows through the vocab role map
// (roleForRoute) instead of an isAdminPath literal.
func reachabilityRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method string) agent.TestCase {
	path := fillRouteParams(r.Path)
	authRole := roleForRoute(svc, r.Path)
	return agent.TestCase{
		ID:          routeBaseID(svc, method, r.Path),
		Name:        fmt.Sprintf("%s %s reachability", method, path),
		Service:     svc.Name,
		Target:      host,
		Action:      "ws_flow",
		Expectation: routeExpectation(svc, r.Path, authRole),
		Priority:    0.5,
		Steps: []agent.TestStep{{
			Action:            "http_request",
			URL:               host + path,
			Method:            method,
			AuthRole:          authRole,
			ExpectStatusClass: "any",
		}},
	}
}

// unauthRouteCase proves the auth middleware rejects bare clients: no
// credential injected, any 4xx passes (401/403 both legitimate rejections).
func unauthRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method string) agent.TestCase {
	path := fillRouteParams(r.Path)
	return agent.TestCase{
		ID:          routeBaseID(svc, method, r.Path) + "-unauth",
		Name:        fmt.Sprintf("%s %s unauthenticated", method, path),
		Service:     svc.Name,
		Target:      host,
		Action:      "ws_flow",
		Expectation: "route rejects unauthenticated requests (4xx) — auth middleware present in vocab",
		Priority:    0.5,
		Steps: []agent.TestStep{{
			Action:            "http_request",
			URL:               host + path,
			Method:            method,
			ExpectStatusClass: "4xx",
		}},
	}
}

// authedRouteCase drives the route with a role JWT, real :param values
// (captured from each param's list route) and the minimal legal body when
// the vocab declares one. Body-less GET/DELETE claim 2xx; body-carrying
// mutations accept 2xx or 4xx (legitimate validation rejections) but never
// 5xx. Body-less mutations take 2xx_4xx too — with no extracted body, a
// validation 4xx is not evidence of breakage.
func authedRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method, role string) agent.TestCase {
	// One capture step per :param, in path order: GET the param's list
	// route, pick the dot-path out of the first record, expose it as a
	// per-case param for the target step's URL placeholder. The capture
	// step authenticates as the LIST route's own role (roleForRoute on the
	// list path), falling back to the target's role when the list route
	// has none — an admin-prefixed target chaining to a web-carried list
	// must not send the admin JWT there, and the web list is scoped to the
	// web user's data.
	steps := make([]agent.TestStep, 0, len(r.ParamSources)+1)
	for _, p := range routeParams(r.Path) {
		ps := r.ParamSources[p]
		_, listPath, _ := strings.Cut(ps.Route, " ")
		listRole := roleForRoute(svc, listPath)
		if listRole == "" {
			listRole = role
		}
		steps = append(steps, agent.TestStep{
			Action:            "http_request",
			URL:               host + fillRouteParams(listPath),
			Method:            "GET",
			AuthRole:          listRole,
			ExpectStatusClass: "2xx",
			Capture:           map[string]string{ps.Pick: "p_" + strings.TrimPrefix(p, ":")},
		})
	}
	body, class, expectation := "", "2xx",
		"authenticated request succeeds (2xx); real :param values from list-route chaining"
	targetURL := host + fillRouteParamsFromSources(r.Path, r.ParamSources)
	if len(r.MinBody) > 0 {
		if b, err := json.Marshal(r.MinBody); err == nil {
			body = string(b)
		}
		class = "2xx_4xx"
		expectation = "authenticated mutation returns success or client-error, never 5xx; minimal legal body from zod vocab"
	} else if method != "GET" && method != "DELETE" {
		class = "2xx_4xx"
	}
	if method == "DELETE" {
		// Destructive-chain safety (run32 lesson): an authed DELETE with the
		// real captured id would destroy the live record — and one such route
		// (delete-account) took the whole environment with it. The capture
		// steps still run (the list routes stay covered), but the target
		// DELETE uses a guaranteed-nonexistent sentinel id and asserts the
		// 4xx rejection: routing + auth + error handling proven, nothing
		// destroyed.
		targetURL = host + fillRouteParamsWith(r.Path, "cerberus_nonexistent")
		class = "4xx"
		expectation = "authenticated delete rejects a nonexistent id (4xx) — routing, auth and error handling proven without destroying live records"
	}
	steps = append(steps, agent.TestStep{
		Action:            "http_request",
		URL:               targetURL,
		Method:            method,
		AuthRole:          role,
		Body:              body,
		ExpectStatusClass: class,
	})
	return agent.TestCase{
		ID:          routeBaseID(svc, method, r.Path) + "-authed",
		Name:        fmt.Sprintf("%s %s authed", method, fillRouteParams(r.Path)),
		Service:     svc.Name,
		Target:      host,
		Action:      "ws_flow",
		Expectation: expectation,
		Priority:    0.5,
		Steps:       steps,
	}
}

// roleForRoute picks the protocol role whose JWT the authed tier injects,
// from the vocabulary's declarable role map (spec §3): the longest
// http_role_routes prefix matching the path wins (path == prefix or path
// starts with prefix+"/"), else http_default_role — each only when the role
// carries a CredentialRef. When the vocab declares no mapping at all, the
// honest fallback is the single credentialed protocol role, if exactly one
// exists; multiple candidates without a map yield "" (no guessing).
func roleForRoute(svc project.Service, path string) string {
	if svc.Protocol == nil {
		return ""
	}
	credentialed := func(name string) bool {
		r := svc.Protocol.Roles[name]
		return r != nil && r.CredentialRef != ""
	}
	if svc.Vocabulary != nil && (len(svc.Vocabulary.HTTPRoleRoutes) > 0 || svc.Vocabulary.HTTPDefaultRole != "") {
		best, bestLen := "", -1
		for _, rr := range svc.Vocabulary.HTTPRoleRoutes {
			if (path == rr.Prefix || strings.HasPrefix(path, rr.Prefix+"/")) && len(rr.Prefix) > bestLen {
				best, bestLen = rr.Role, len(rr.Prefix)
			}
		}
		if bestLen >= 0 {
			if credentialed(best) {
				return best
			}
		}
		if credentialed(svc.Vocabulary.HTTPDefaultRole) {
			return svc.Vocabulary.HTTPDefaultRole
		}
		return ""
	}
	// No declared mapping: single credentialed role or honest degradation.
	var sole string
	for name := range svc.Protocol.Roles {
		if !credentialed(name) {
			continue
		}
		if sole != "" {
			return "" // multiple candidates, no map — refuse to guess
		}
		sole = name
	}
	return sole
}

// routeParams lists a route path's :name segments in order.
func routeParams(path string) []string {
	var params []string
	for _, seg := range strings.Split(path, "/") {
		if len(seg) > 1 && strings.HasPrefix(seg, ":") {
			params = append(params, seg)
		}
	}
	return params
}

// paramResolvable reports whether the authed tier can run: the path is
// param-free, or every :param has a param source whose own path is param-free
// too — a source route with :params would need its own guessed id, which the
// degradation rule forbids.
func paramResolvable(r project.VocabHTTPRoute) bool {
	params := routeParams(r.Path)
	for _, p := range params {
		ps, ok := r.ParamSources[p]
		if !ok {
			return false
		}
		_, listPath, _ := strings.Cut(ps.Route, " ")
		if len(routeParams(listPath)) > 0 {
			return false
		}
	}
	return true
}

// routeRoleMapped reports whether the path matches any declared
// http_role_routes prefix — used to keep the "auth not penetrated" honesty
// note when the mapped role has no credential to inject.
func routeRoleMapped(svc project.Service, path string) bool {
	if svc.Vocabulary == nil {
		return false
	}
	for _, rr := range svc.Vocabulary.HTTPRoleRoutes {
		if path == rr.Prefix || strings.HasPrefix(path, rr.Prefix+"/") {
			return true
		}
	}
	return false
}

// fillRouteParams replaces :param segments with a stable placeholder and a
// trailing * with a fixed tail so the request path matches the route pattern.
func fillRouteParams(path string) string {
	return fillRouteParamsWith(path, "1")
}

// fillRouteParamsWith is fillRouteParams with an explicit :param value.
func fillRouteParamsWith(path, paramValue string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = paramValue
		} else if s == "*" {
			segs[i] = "x"
		}
	}
	return strings.Join(segs, "/")
}

// fillRouteParamsFromSources is fillRouteParams with param chaining: a
// :param that has a param source is substituted with its {{case.p_<name>}}
// placeholder (resolved by the executor from the capture step) instead of
// the static "1".
func fillRouteParamsFromSources(path string, srcs map[string]project.VocabParamSource) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		switch {
		case strings.HasPrefix(s, ":"):
			if _, ok := srcs[s]; ok {
				segs[i] = "{{case.p_" + strings.TrimPrefix(s, ":") + "}}"
			} else {
				segs[i] = "1"
			}
		case s == "*":
			segs[i] = "x"
		}
	}
	return strings.Join(segs, "/")
}

// routeExpectation renders the honesty tier for one route. The wording is
// SUT-agnostic: which role a prefix takes comes from the vocab role map
// (authRole already resolved by the caller), never from path literals.
func routeExpectation(svc project.Service, path, authRole string) string {
	exp := "route reachable over HTTP (any status response; no transport error)"
	if strings.Contains(path, ":") {
		exp += "; placeholder param values — handler semantics unverified"
	}
	switch {
	case authRole != "":
		exp += "; role JWT injected via vocab role map — authenticated routing"
	case routeRoleMapped(svc, path):
		exp += "; auth not penetrated — reachability only"
	}
	return exp
}
