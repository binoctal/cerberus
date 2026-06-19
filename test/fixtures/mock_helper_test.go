package fixtures

// MockResponses returns mock LLM responses parameterized by the fixture's
// target file. The same response set works for Scout.Plan, BuildCoverageContract,
// Agent.Steer, Examiner.Judge, and AutoTest generator — all get a permissive
// JSON that lets the real executor/coverage/generator code run.
func MockResponses(targetFile string) map[string]string {
	planAndContract := `{
		"cases": [{"id":"tc-1","name":"test","target":"` + targetFile + `","action":"file_read","expectation":"reads ok","priority":0.9}],
		"depth": "standard",
		"scope": ["` + targetFile + `"],
		"path_types": ["happy"],
		"error_scope": ["4xx"],
		"boundaries": ["empty"],
		"priorities": {},
		"coverage_gate": {"module":"` + targetFile + `","line_threshold":0.5}
	}`
	return map[string]string{
		"default": planAndContract,
	}
}
