package merge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOapiYaml(t *testing.T) {
	t.Run("basic merge", func(t *testing.T) {
		tmpDir := t.TempDir()
		input := filepath.Join(tmpDir, "api.yaml")
		output := filepath.Join(tmpDir, "out.yaml")

		writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
`)
		if err := OapiYaml(input, output); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(output); err != nil {
			t.Fatal("output file not created")
		}
	})

	t.Run("with external ref", func(t *testing.T) {
		tmpDir := t.TempDir()
		input := filepath.Join(tmpDir, "api.yaml")
		paths := filepath.Join(tmpDir, "paths.yaml")
		output := filepath.Join(tmpDir, "out.yaml")

		writeFile(t, paths, `
test:
  get:
    responses:
      "200":
        description: OK
      "400":
        description: Bad
      "500":
        description: Error
`)
		writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/test'
`)
		if err := OapiYaml(input, output); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(output)
		content := string(data)

		idx200 := strings.Index(content, "200")
		idx400 := strings.Index(content, "400")
		idx500 := strings.Index(content, "500")

		if idx200 > idx400 || idx400 > idx500 {
			t.Error("response order not preserved")
		}
	})

	t.Run("missing input file", func(t *testing.T) {
		err := OapiYaml("nonexistent.yaml", "out.yaml")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "Failed to read") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing openapi field", func(t *testing.T) {
		tmpDir := t.TempDir()
		input := filepath.Join(tmpDir, "api.yaml")

		writeFile(t, input, `
info:
  title: Test
  version: "1.0"
paths: {}
`)
		err := OapiYaml(input, filepath.Join(tmpDir, "out.yaml"))
		if err == nil || !strings.Contains(err.Error(), "openapi") {
			t.Errorf("expected openapi error, got: %v", err)
		}
	})

	t.Run("missing info field", func(t *testing.T) {
		tmpDir := t.TempDir()
		input := filepath.Join(tmpDir, "api.yaml")

		writeFile(t, input, `
openapi: "3.0.0"
paths: {}
`)
		err := OapiYaml(input, filepath.Join(tmpDir, "out.yaml"))
		if err == nil || !strings.Contains(err.Error(), "info") {
			t.Errorf("expected info error, got: %v", err)
		}
	})
}

func TestResolveRef(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		current  string
		expected string
	}{
		{"empty", "", "api.yaml", ""},
		{"relative", "./schemas/user.yaml", "api.yaml", "schemas/user.yaml"},
		{"parent dir", "../common.yaml", "sub/api.yaml", "common.yaml"},
		{"absolute", "/abs/path.yaml", "api.yaml", "/abs/path.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRef(tt.path, tt.current)
			if !strings.HasSuffix(got, tt.expected) && got != tt.expected {
				t.Errorf("resolveRef(%q, %q) = %q, want suffix %q", tt.path, tt.current, got, tt.expected)
			}
		})
	}
}

func TestMergeComponents(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `
users:
  get:
    responses:
      "200":
        $ref: './responses.yaml#/responses/OK'

components:
  schemas:
    User:
      type: object
`)
	writeFile(t, filepath.Join(tmpDir, "responses.yaml"), `
responses:
  OK:
    description: Success
`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /users:
    $ref: './paths.yaml#/users'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(output)
	content := string(data)

	if !strings.Contains(content, "schemas:") {
		t.Error("schemas not merged")
	}
	if !strings.Contains(content, "User:") {
		t.Error("User schema not found")
	}
}

func TestOutputStructure(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
tags:
  - name: test
security:
  - bearerAuth: []
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(output)
	content := string(data)

	idxOpenapi := strings.Index(content, "openapi:")
	idxInfo := strings.Index(content, "info:")
	idxPaths := strings.Index(content, "paths:")
	idxComponents := strings.Index(content, "components:")
	idxSecurity := strings.Index(content, "security:")
	idxTags := strings.Index(content, "tags:")

	if idxOpenapi > idxInfo {
		t.Error("openapi should come before info")
	}
	if idxInfo > idxPaths {
		t.Error("info should come before paths")
	}
	if idxPaths > idxComponents {
		t.Error("paths should come before components")
	}
	if idxComponents > idxSecurity {
		t.Error("components should come before security")
	}
	if idxSecurity > idxTags {
		t.Error("security should come before tags")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
