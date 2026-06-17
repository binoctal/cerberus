package agent

// containsCheck returns true if the check name is in the list.
func containsCheck(checks []string, name string) bool {
	for _, c := range checks {
		if c == name {
			return true
		}
	}
	return false
}
