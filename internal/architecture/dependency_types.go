package architecture

// DependencyGraph represents import dependencies between packages
type DependencyGraph struct {
	Nodes map[string][]string // package -> list of imported packages
}
