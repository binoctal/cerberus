package project

// MergeVocabJudgment copies every judgment-layer value from prev onto fresh,
// in place. Fresh is the re-extracted vocabulary (its fact layer — edges,
// routes, middlewares — was derived from source and always wins); prev is the
// previous vocab file whose hand-curated marks (partial/unsupported, role
// map, min_query, param chains, cross marks) encode live-probe knowledge a
// re-extraction cannot know and must never silently drop.
func MergeVocabJudgment(prev, fresh *Vocabulary) {
	// Re-extraction must not drop manually-annotated marks (partial/unsupported)
	// on edges that still exist, matched by (from_role, to_role, type). A blind
	// overwrite re-admits server-only edges to the coverage denominator, which
	// timeout-fail until the executor escalates. Extraction cannot know these
	// marks — they encode live-probe knowledge about the running server.
	marks := make(map[string]VocabEdge, len(prev.Edges))
	for _, e := range prev.Edges {
		marks[e.FromRole+"|"+e.ToRole+"|"+e.Type] = e
	}
	for i := range fresh.Edges {
		if old, ok := marks[fresh.Edges[i].FromRole+"|"+fresh.Edges[i].ToRole+"|"+fresh.Edges[i].Type]; ok {
			fresh.Edges[i].Partial = old.Partial
			fresh.Edges[i].Unsupported = old.Unsupported
		}
	}
	// A hand-curated auth middleware list wins over the name heuristic.
	if len(prev.HTTPAuthMiddlewares) > 0 {
		fresh.HTTPAuthMiddlewares = prev.HTTPAuthMiddlewares
	}
	// The hand-curated UI surface is not derivable from source; a
	// re-extraction must never silently drop it.
	if prev.UI != nil {
		fresh.UI = prev.UI
	}
	// Same for the hand-curated HTTP role map (spec §3): which protocol
	// role's JWT a path prefix takes is live-probe knowledge, not a
	// source-derivable fact.
	if len(prev.HTTPRoleRoutes) > 0 {
		fresh.HTTPRoleRoutes = prev.HTTPRoleRoutes
	}
	if prev.HTTPDefaultRole != "" {
		fresh.HTTPDefaultRole = prev.HTTPDefaultRole
	}
	// http_cross_role rides the judgment layer with the default role: which
	// principal plays the cross tier is a hand-set choice, not a
	// source-derivable fact.
	if prev.HTTPCrossRole != "" {
		fresh.HTTPCrossRole = prev.HTTPCrossRole
	}
	// Route marks follow the same rule, keyed method|path. Hand-tuned
	// param chains and hand-set auth (spec §5: the judgment layer rides
	// the merge) win over re-derivation; middlewares/min_body are the
	// fact layer and always come back fresh above. Auth preservation
	// covers the judgment values none|required — "unknown" is the
	// not-determined marker (a first pass emits it everywhere), so
	// preserving it would freeze ignorance and block the curated-list
	// derivation from ever marking a route required.
	routeMarks := make(map[string]VocabHTTPRoute, len(prev.HTTPRoutes))
	for _, r := range prev.HTTPRoutes {
		routeMarks[r.Method+"|"+r.Path] = r
	}
	for i := range fresh.HTTPRoutes {
		if old, ok := routeMarks[fresh.HTTPRoutes[i].Method+"|"+fresh.HTTPRoutes[i].Path]; ok {
			fresh.HTTPRoutes[i].Partial = old.Partial
			fresh.HTTPRoutes[i].Unsupported = old.Unsupported
			if old.Auth == "none" || old.Auth == "required" {
				fresh.HTTPRoutes[i].Auth = old.Auth
			}
			// min_query rides the judgment layer with the role map:
			// handler-side manual query guards are live-probe
			// knowledge, not source-derivable facts.
			if old.MinQuery != nil {
				fresh.HTTPRoutes[i].MinQuery = old.MinQuery
			}
			// cross_exempt rides the judgment layer with min_query: whether
			// a resource is genuinely shared cross-principal is live-probe
			// knowledge, not source-derivable.
			if old.CrossExempt {
				fresh.HTTPRoutes[i].CrossExempt = old.CrossExempt
			}
			for p, ps := range old.ParamSources {
				if fresh.HTTPRoutes[i].ParamSources == nil {
					fresh.HTTPRoutes[i].ParamSources = map[string]VocabParamSource{}
				}
				fresh.HTTPRoutes[i].ParamSources[p] = ps
			}
			fresh.HTTPRoutes[i].ParamSourcesOff = old.ParamSourcesOff
		}
	}
}
