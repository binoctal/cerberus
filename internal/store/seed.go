package store

import (
	"context"

	"go.uber.org/zap"
)

// SeedStrategies writes default L3 procedural memory strategies.
// Called during `cerberus init` to bootstrap the strategy library.
func SeedStrategies(ctx context.Context, s *Store, projectName string, logger *zap.Logger) (int, error) {
	strategies := defaultStrategies()
	count := 0

	for _, st := range strategies {
		// Skip if a strategy with same name already exists.
		existing, _ := s.GetProceduralByMatch(ctx, st.condition, 1)
		for _, e := range existing {
			if e.Name == st.name {
				continue
			}
		}

		_, err := s.StoreProceduralWithType(ctx, st.name, st.condition, st.action, projectName, st.category, st.refType)
		if err != nil {
			logger.Warn("seed strategy failed", zap.String("name", st.name), zap.Error(err))
			continue
		}
		count++
	}

	logger.Info("seeded default strategies", zap.Int("count", count))
	return count, nil
}

type seedStrategy struct {
	name      string
	condition string
	action    string
	category  string
	refType   string // "failure" or "success"
}

func defaultStrategies() []seedStrategy {
	return []seedStrategy{
		// Auth & session failures.
		{
			name:      "auth-token-expiry",
			condition: "*/auth/*, */login, */session*",
			action:    "When auth endpoint returns 401, check token expiry first. Re-authenticate with stored credentials before retrying the original request. If re-auth fails, skip remaining auth-dependent tests.",
			category:  "auth",
			refType:   "failure",
		},
		{
			name:      "auth-rate-limiting",
			condition: "*/auth/*, */login",
			action:    "When login returns 429, add exponential backoff (1s, 2s, 4s). If still rate-limited after 3 retries, skip the test case and flag for manual review.",
			category:  "auth",
			refType:   "failure",
		},

		// CRUD API patterns.
		{
			name:      "crud-not-found",
			condition: "*/api/*/users/*, */api/*/items/*, */api/*/products/*",
			action:    "When GET /resource/{id} returns 404, the resource may have been deleted by a prior test. Create a new resource first, then retry with the new ID.",
			category:  "crud",
			refType:   "failure",
		},
		{
			name:      "crud-validation-error",
			condition: "*/api/*",
			action:    "When POST/PUT returns 422, inspect the response body for validation error details. Fix the request payload based on the error fields and retry once.",
			category:  "crud",
			refType:   "failure",
		},
		{
			name:      "crud-duplicate-key",
			condition: "*/api/*",
			action:    "When POST returns 409 Conflict, the resource already exists. Use GET to fetch the existing resource and continue with update operations instead.",
			category:  "crud",
			refType:   "failure",
		},

		// Infrastructure patterns.
		{
			name:      "server-timeout",
			condition: "*",
			action:    "When request times out (no response within 30s), check if the server is healthy via the health endpoint. If health check passes, retry with a longer timeout. If health check fails, skip and report server issue.",
			category:  "infra",
			refType:   "failure",
		},
		{
			name:      "server-500-internal",
			condition: "*/api/*",
			action:    "When receiving 500, retry once after 2 seconds. If still 500, check if other endpoints return 500 too (systemic issue). If isolated, skip this test and report as server bug.",
			category:  "infra",
			refType:   "failure",
		},

		// Success patterns.
		{
			name:      "api-health-verify",
			condition: "*/health*, */status*, */ping*",
			action:    "Always verify health endpoints first before running API tests. If health fails, skip all API tests. Use health check as a dependency for all test cases.",
			category:  "infra",
			refType:   "success",
		},
		{
			name:      "crud-create-then-verify",
			condition: "*/api/*",
			action:    "For CRUD test flows: create resource → verify GET returns created data → update → verify update → delete → verify 404. This ensures full lifecycle coverage.",
			category:  "crud",
			refType:   "success",
		},
		{
			name:      "pagination-baseline",
			condition: "*/api/*/list*, */api/*s",
			action:    "When testing list endpoints, first create 3+ resources, then verify pagination parameters (page, limit, offset) work correctly. Check response includes total count.",
			category:  "crud",
			refType:   "success",
		},
	}
}
