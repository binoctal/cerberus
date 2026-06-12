package sandbox

import "path/filepath"

// DefaultProcessPolicy returns a restrictive policy for process execution.
func DefaultProcessPolicy(workDir string) Policy {
	absWork, _ := filepath.Abs(workDir)
	return Policy{
		FS: FSPolicy{
			ReadOnly:  []string{"/usr", "/lib", "/go", "/tmp"},
			ReadWrite: []string{absWork},
			Denied:    []string{"/etc/shadow", "/root/.ssh", "/.env"},
		},
		Network: NetPolicy{AllowOutbound: false},
		Resources: ResPolicy{
			MaxMemoryMB: 512,
			Timeout:     60,
		},
	}
}

// DefaultFilePolicy returns a policy for file operations within a project.
func DefaultFilePolicy(projectDir string) Policy {
	abs, _ := filepath.Abs(projectDir)
	return Policy{
		FS:      FSPolicy{ReadWrite: []string{abs}},
		Network: NetPolicy{AllowOutbound: false},
	}
}

// DefaultHTTPPolicy returns a policy for HTTP actions.
func DefaultHTTPPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 30},
	}
}

// DefaultMCPPolicy returns a policy for MCP calls.
func DefaultMCPPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 10},
	}
}

// DefaultCodePolicy returns a policy for code analysis operations.
func DefaultCodePolicy(projectDir string) Policy {
	abs, _ := filepath.Abs(projectDir)
	return Policy{
		FS:      FSPolicy{ReadOnly: []string{abs}},
		Network: NetPolicy{AllowOutbound: false},
	}
}

// DefaultBrowserPolicy returns a policy for browser automation.
// Allows outbound network (to reach target URLs) and generous timeout.
func DefaultBrowserPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 60},
	}
}

// DefaultDBPolicy returns a policy for database operations.
// Network access for remote DBs; local SQLite needs no network.
func DefaultDBPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 30},
	}
}

// DefaultGraphQLPolicy returns a policy for GraphQL queries.
func DefaultGraphQLPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 30},
	}
}

// DefaultWSPolicy returns a policy for WebSocket connections.
func DefaultWSPolicy() Policy {
	return Policy{
		Network:   NetPolicy{AllowOutbound: true},
		Resources: ResPolicy{Timeout: 60},
	}
}
