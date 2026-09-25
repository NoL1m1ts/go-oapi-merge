package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "go-oapi-merge")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		name, input        string
		missing, wantError bool
	}{
		{"valid", "openapi: 3.2.0\ninfo: {title: CLI, version: '1'}\npaths: {}\n", false, false},
		{"missing file", "", true, true},
		{"invalid syntax", "openapi: [unfinished\n", false, true},
		{"invalid security", "openapi: 3.2.0\ninfo: {title: CLI, version: '1'}\npaths: {}\nsecurity: {Bearer: []}\n", false, true},
		{"missing reference", "openapi: 3.2.0\ninfo: {title: CLI, version: '1'}\npaths:\n  /test: {$ref: './missing.yaml#/item'}\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "input.yaml"), filepath.Join(dir, "output.yaml")
			if !tc.missing {
				if err := os.WriteFile(input, []byte(tc.input), 0600); err != nil {
					t.Fatal(err)
				}
			}
			const sentinel = "existing output\n"
			if err := os.WriteFile(output, []byte(sentinel), 0600); err != nil {
				t.Fatal(err)
			}
			log, err := exec.Command(binary, "-input", input, "-output", output).CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("CLI error = %v, want error %v\n%s", err, tc.wantError, log)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError {
				if string(data) != sentinel {
					t.Fatal("failed CLI invocation overwrote existing output")
				}
				if !bytes.Contains(log, []byte("Error:")) || bytes.Contains(log, []byte("panic:")) {
					t.Fatalf("expected a handled error, got %s", log)
				}
			} else if !bytes.Contains(data, []byte("title: CLI")) {
				t.Fatalf("unexpected merged output: %s", data)
			}
		})
	}
}
