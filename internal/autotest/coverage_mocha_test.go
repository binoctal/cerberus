package autotest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMochaProjectDetector_Detect(t *testing.T) {
	detector := &MochaProjectDetector{}

	tests := []struct {
		name           string
		setup          func() (string, func())
		wantSupported  bool
		wantConfidence float64
	}{
		{
			name: "Mocha + nyc project",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()

				// Create package.json with mocha and nyc
				pkgJson := filepath.Join(tmpDir, "package.json")
				pkgContent := `{"devDependencies": {"mocha": "^10.0.0", "nyc": "^15.0.0"}}`
				if err := os.WriteFile(pkgJson, []byte(pkgContent), 0644); err != nil {
					t.Fatal(err)
				}

				// Create node_modules/.bin/nyc
				nodeModules := filepath.Join(tmpDir, "node_modules", ".bin")
				if err := os.MkdirAll(nodeModules, 0755); err != nil {
					t.Fatal(err)
				}
				nycPath := filepath.Join(nodeModules, "nyc")
				if err := os.WriteFile(nycPath, []byte("#!/bin/sh\n"), 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported:  true,
			wantConfidence: 1.0,
		},
		{
			name: "Mocha without nyc",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()

				pkgJson := filepath.Join(tmpDir, "package.json")
				pkgContent := `{"devDependencies": {"mocha": "^10.0.0"}}`
				if err := os.WriteFile(pkgJson, []byte(pkgContent), 0644); err != nil {
					t.Fatal(err)
				}

				nodeModules := filepath.Join(tmpDir, "node_modules")
				if err := os.MkdirAll(nodeModules, 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported:  true,
			wantConfidence: 0.9,
		},
		{
			name: "Jest project (not Mocha)",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()

				pkgJson := filepath.Join(tmpDir, "package.json")
				pkgContent := `{"devDependencies": {"jest": "^29.0.0"}}`
				if err := os.WriteFile(pkgJson, []byte(pkgContent), 0644); err != nil {
					t.Fatal(err)
				}

				nodeModules := filepath.Join(tmpDir, "node_modules")
				if err := os.MkdirAll(nodeModules, 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported:  false,
			wantConfidence: 0,
		},
		{
			name: "no package.json",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				return tmpDir, func() {}
			},
			wantSupported:  false,
			wantConfidence: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, cleanup := tt.setup()
			defer cleanup()

			supported, confidence, _ := detector.Detect(dir)

			if supported != tt.wantSupported {
				t.Errorf("Detect() supported = %v, want %v", supported, tt.wantSupported)
			}
			if confidence != tt.wantConfidence {
				t.Errorf("Detect() confidence = %v, want %v", confidence, tt.wantConfidence)
			}
		})
	}
}

func TestMochaTestFilePath(t *testing.T) {
	tests := []struct {
		name          string
		sourceFile    string
		projectDir    string
		createTestDir bool
		want          string
	}{
		{
			name:          "same-directory mode",
			sourceFile:    "src/api/users.js",
			projectDir:    "",
			createTestDir: false,
			want:          "src/api/users.test.js",
		},
		{
			name:          "jsx file",
			sourceFile:    "components/Button.jsx",
			projectDir:    "",
			createTestDir: false,
			want:          "components/Button.test.jsx",
		},
		{
			name:          "test directory mode",
			sourceFile:    "src/calculator.js",
			createTestDir: true,
			want:          "", // Will be set in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var projectDir string
			// Setup test directory if needed
			if tt.createTestDir {
				projectDir = t.TempDir()
				testDir := filepath.Join(projectDir, "test")
				if err := os.MkdirAll(testDir, 0755); err != nil {
					t.Fatal(err)
				}
				tt.projectDir = projectDir
				tt.sourceFile = filepath.Join(projectDir, "src", "calculator.js")
			}

			got := MochaTestFilePath(tt.sourceFile, tt.projectDir)
			// For test directory mode, check that it ends with the expected path
			if tt.createTestDir {
				if filepath.Base(got) != "calculator.test.js" {
					t.Errorf("MochaTestFilePath() = %s, want ending with calculator.test.js", got)
				}
				// Also check that it contains /test/ directory
				if !strings.Contains(got, "/test/") {
					t.Errorf("MochaTestFilePath() = %s, want path containing /test/", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("MochaTestFilePath() = %s, want %s", got, tt.want)
				}
			}
		})
	}
}
