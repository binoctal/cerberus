package architecture

// InterfaceInfo represents information about a Go interface
type InterfaceInfo struct {
	Name         string
	FilePath     string
	LineNumber   int
	Implementations int // Number of concrete implementations
	Methods       int
}
