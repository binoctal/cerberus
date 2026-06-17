package autotest

// MochaProjectDetector detects Mocha + nyc projects
type MochaProjectDetector struct{}

func (d *MochaProjectDetector) Type() ProjectType { return ProjectTypeMocha }

func (d *MochaProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Phase 1: Base check (0.5) - package.json exists
	if ok, score, _ := checkBaseRequirements(projectDir); !ok {
		return false, score, ""
	}

	// Phase 2: Loose check (0.7) - node_modules exists
	if ok, score, _ := checkNodeModulesExists(projectDir, 0.5); !ok {
		return false, score, ""
	}

	// Phase 3: Read and parse package.json
	pkg, score, _ := readPackageJson(projectDir, 0.7)
	if pkg == nil {
		return false, score, ""
	}

	// Phase 4: Check if Jest exists (don't interfere with Jest projects)
	if hasJestDependency(pkg) {
		return false, 0, ""
	}

	// Phase 5: Strict check (0.9) - mocha or nyc in dependencies
	if !hasMochaDependency(pkg) {
		return false, 0.7, ""
	}

	// Phase 6: Validation check (1.0) - find executable
	return findMochaExecutable(projectDir)
}
