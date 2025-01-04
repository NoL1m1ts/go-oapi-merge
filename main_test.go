package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMainFile(t *testing.T) {
	// Save original args and restore them after test
	originalArgs := os.Args
	defer func() {
		os.Args = originalArgs
	}()

	tmpDir := t.TempDir()

	// Create a test input file
	inputFile := filepath.Join(tmpDir, "test_api.yaml")
	err := os.WriteFile(inputFile, []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      summary: Test endpoint
`), 0644)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "output.yaml")

	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{
			name:      "successful merge",
			args:      []string{"cmd", "-input", inputFile, "-output", outputFile},
			wantError: false,
		},
		{
			name:      "nonexistent input file",
			args:      []string{"cmd", "-input", "nonexistent.yaml", "-output", outputFile},
			wantError: true,
		},
		{
			name:      "default parameters",
			args:      []string{"cmd"},
			wantError: true, // Will fail because default api.yaml doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test args
			os.Args = tt.args

			// Capture panic if any
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantError {
						t.Errorf("main() panicked: %v", r)
					}
				}
			}()

			// Run main
			if tt.wantError {
				// If we expect an error, main should panic with log.Fatal
				assert.Panics(t, func() { main() })
			} else {
				assert.NotPanics(t, func() { main() })

				// Check if output file exists
				_, err := os.Stat(outputFile)
				assert.NoError(t, err)
			}
		})
	}
}
