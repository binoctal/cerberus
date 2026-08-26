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
	// An "admin" protocol role (HTTP-only, never WS-connects) upgrades the
	// admin route block's reachability tier from bare 401-probe to
	// authenticated routing via AuthRole token injection.
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
		if r.Auth == "required" {
			cases = append(cases,
				unauthRouteCase(svc, host, r, method),
				reachabilityRouteCase(svc, host, r, method, adminRole))
			// The authed 2xx tier needs a credential to inject and real
			// :param values — a guessed id would 404 and lie red.
			if role := roleForRoute(svc, r.Path); role != "" && paramResolvable(r) {
				cases = append(cases, authedRouteCase(svc, host, r, method, role))
			}
			continue
		}
		// none | unknown | "" (pre-v2 vocab): reachability honesty fallback.
		cases = append(cases, reachabilityRouteCase(svc, host, r, method, adminRole))
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
// transport errors fail, proving the route is reachable, nothing more.
func reachabilityRouteCase(svc project.Service, host string, r project.VocabHTTPRoute, method, adminRole string) agent.TestCase {
	path := fillRouteParams(r.Path)
	authRole := ""
	if adminRole != "" && isAdminPath(r.Path) {
		authRole = adminRole
	}
	return agent.TestCase{
		ID:          routeBaseID(svc, method, r.Path),
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
	// per-case param for the target step's URL placeholder.
	steps := make([]agent.TestStep, 0, len(r.ParamSources)+1)
	for _, p := range routeParams(r.Path) {
		ps := r.ParamSources[p]
		_, listPath, _ := strings.Cut(ps.Route, " ")
		steps = append(steps, agent.TestStep{
			Action:            "http_request",
			URL:               host + fillRouteParams(listPath),
			Method:            "GET",
			AuthRole:          role,
			ExpectStatusClass: "2xx",
			Capture:           map[string]string{ps.Pick: "p_" + strings.TrimPrefix(p, ":")},
		})
	}
	body, class, expectation := "", "2xx",
		"authenticated request succeeds (2xx); real :param values from list-route chaining"
	if len(r.MinBody) > 0 {
		if b, err := json.Marshal(r.MinBody); err == nil {
			body = string(b)
		}
		class = "2xx_4xx"
		expectation = "authenticated mutation returns success or client-error, never 5xx; minimal legal body from zod vocab"
	} else if method != "GET" && method != "DELETE" {
		class = "2xx_4xx"
	}
	steps = append(steps, agent.TestStep{
		Action:            "http_request",
		URL:               host + fillRouteParamsFromSources(r.Path, r.ParamSources),
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

// roleForRoute picks the protocol role whose JWT the authed tier injects:
// the admin role for the admin route block, the web role elsewhere. "" when
// no candidate carries a credential (nothing to inject). Generalizes the v1
// adminRole special case without new SUT knowledge.
func roleForRoute(svc project.Service, path string) string {
	if svc.Protocol == nil {
		return ""
	}
	prefer := []string{"web"}
	if isAdminPath(path) {
		prefer = []string{"admin", "web"}
	}
	for _, name := range prefer {
		if r := svc.Protocol.Roles[name]; r != nil && r.CredentialRef != "" {
			return name
		}
	}
	return ""
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
