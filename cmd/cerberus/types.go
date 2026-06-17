package main

// Flag variables for CLI commands
var (
	urlFlag            string
	goalFlag           string
	actorFlags         []string
	dbFlag             string
	configFlag         string
	portFlag           string
	deepPlanFlag       bool
	dirFlag            string
	sessionFlag        string
	formatFlag         string
	outputFlag         string
	parallelFlag       bool
	workersFlag        int
	resumeFlag         string
	autoTestSafetyFlag string
)

// Version information (set via -ldflags at build time)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
