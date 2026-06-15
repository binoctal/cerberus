package autotest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoProjectDetector_Detect(t *testing.T) {
	detector := &GoProjectDetector{}

	tests := []struct {
		name     string
		setup    func() (string, func())
		wantSupported bool
		wantConfidence float64
	}{
		{
			name: "go.mod exists",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				goMod := filepath.Join(tmpDir, "go.mod")
				if err := os.WriteFile(goMod, []byte("module test\n"), 0644); err != nil {
					t.Fatal(err)
				}
				return tmpDir, func() {}
			},
			wantSupported: true,
			wantConfidence: 1.0,
		},
		{
			name: "no go.mod",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				return tmpDir, func() {}
			},
			wantSupported: false,
			wantConfidence: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, cleanup := tt.setup()
			defer cleanup()

			supported, confidence, toolPath := detector.Detect(dir)

			if supported != tt.wantSupported {
				t.Errorf("Detect() supported = %v, want %v", supported, tt.wantSupported)
			}
			if confidence != tt.wantConfidence {
				t.Errorf("Detect() confidence = %v, want %v", confidence, tt.wantConfidence)
			}
			if tt.wantSupported && toolPath == "" {
				t.Errorf("Detect() toolPath = empty, want non-empty")
			}
		})
	}
}

func TestNodeProjectDetector_Detect(t *testing.T) {
	detector := &NodeProjectDetector{}

	tests := []struct {
		name          string
		setup         func() (string, func())
		wantSupported bool
		wantConfidence float64
	}{
		{
			name: "Jest project with node_modules",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()

				// Create package.json with Jest
				pkgJson := filepath.Join(tmpDir, "package.json")
				pkgContent := `{"devDependencies": {"jest": "^29.0.0"}}`
				if err := os.WriteFile(pkgJson, []byte(pkgContent), 0644); err != nil {
					t.Fatal(err)
				}

				// Create node_modules/.bin/jest
				nodeModules := filepath.Join(tmpDir, "node_modules", ".bin")
				if err := os.MkdirAll(nodeModules, 0755); err != nil {
					t.Fatal(err)
				}
				jestPath := filepath.Join(nodeModules, "jest")
				if err := os.WriteFile(jestPath, []byte("#!/bin/sh\n"), 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported: true,
			wantConfidence: 1.0,
		},
		{
			name: "Node project without Jest",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				pkgJson := filepath.Join(tmpDir, "package.json")
				pkgContent := `{"dependencies": {"express": "^4.0.0"}}`
				if err := os.WriteFile(pkgJson, []byte(pkgContent), 0644); err != nil {
					t.Fatal(err)
				}

				// Create node_modules to simulate installed dependencies
				nodeModules := filepath.Join(tmpDir, "node_modules")
				if err := os.MkdirAll(nodeModules, 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported: false,
			wantConfidence: 0,
		},
		{
			name: "no package.json",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				return tmpDir, func() {}
			},
			wantSupported: false,
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

func TestPythonProjectDetector_Detect(t *testing.T) {
	detector := &PythonProjectDetector{}

	tests := []struct {
		name          string
		setup         func() (string, func())
		wantSupported bool
		wantConfidence float64
	}{
		{
			name: "Python project with pytest and coverage",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()

				// Create requirements.txt
				reqPath := filepath.Join(tmpDir, "requirements.txt")
				reqContent := "pytest\ncoverage\n"
				if err := os.WriteFile(reqPath, []byte(reqContent), 0644); err != nil {
					t.Fatal(err)
				}

				// Create virtual environment marker
				venvDir := filepath.Join(tmpDir, "venv", "bin")
				if err := os.MkdirAll(venvDir, 0755); err != nil {
					t.Fatal(err)
				}
				pythonPath := filepath.Join(venvDir, "python")
				if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0755); err != nil {
					t.Fatal(err)
				}

				return tmpDir, func() {}
			},
			wantSupported: true,
			wantConfidence: 1.0,
		},
		{
			name: "Python project without pytest",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				reqPath := filepath.Join(tmpDir, "requirements.txt")
				if err := os.WriteFile(reqPath, []byte("flask\n"), 0644); err != nil {
					t.Fatal(err)
				}
				return tmpDir, func() {}
			},
			wantSupported: false,
			wantConfidence: 0.7,
		},
		{
			name: "no Python markers",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				return tmpDir, func() {}
			},
			wantSupported: false,
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

func TestDetectProjectType(t *testing.T) {
	tests := []struct {
		name          string
		setup         func() (string, func())
		wantType      ProjectType
		wantConfidence float64
		wantErr       bool
	}{
		{
			name: "Go project",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				goMod := filepath.Join(tmpDir, "go.mod")
				if err := os.WriteFile(goMod, []byte("module test\n"), 0644); err != nil {
					t.Fatal(err)
				}
				return tmpDir, func() {}
			},
			wantType: ProjectTypeGo,
			wantConfidence: 1.0,
			wantErr: false,
		},
		{
			name: "no recognized project",
			setup: func() (string, func()) {
				tmpDir := t.TempDir()
				return tmpDir, func() {}
			},
			wantType: "",
			wantConfidence: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, cleanup := tt.setup()
			defer cleanup()

			typ, confidence, err := DetectProjectType(dir)

			if tt.wantErr && err == nil {
				t.Errorf("DetectProjectType() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("DetectProjectType() unexpected error: %v", err)
			}
			if typ != tt.wantType {
				t.Errorf("DetectProjectType() type = %v, want %v", typ, tt.wantType)
			}
			if confidence != tt.wantConfidence {
				t.Errorf("DetectProjectType() confidence = %v, want %v", confidence, tt.wantConfidence)
			}
		})
	}
}
