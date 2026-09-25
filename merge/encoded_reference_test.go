package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodedPathItemFragment(t *testing.T) {
	for _, fragment := range []string{"%2Fitem", "%2fitem", "%2Fitem%2520"} {
		t.Run(fragment, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, fmt.Sprintf("openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n  /test: {$ref: './path.yaml#%s'}\n", fragment))
			key := "item"
			if strings.Contains(fragment, "%2520") {
				key = "item%20"
			}
			writeFile(t, filepath.Join(dir, "path.yaml"), key+": {get: {responses: {'200': {description: OK}}}}\n")
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			if got := mapValueAt(t, unmarshalDoc(t, output), "paths", "/test", "get", "responses", "200", "description"); got != "OK" {
				t.Fatalf("wrong path target: %v", got)
			}
		})
	}
}

func TestEncodedRecursiveComponentFragment(t *testing.T) {
	for _, fragment := range []string{"%2Fcomponents%2Fschemas%2FNode", "/%63omponents/schemas/Node", "%2Fcomponents%2Fschemas%2FNode%2520"} {
		t.Run(fragment, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, fmt.Sprintf("openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Result: {$ref: './defs.yaml#%s'}\n", fragment))
			name := "Node"
			if strings.Contains(fragment, "%2520") {
				name = "Node%20"
			}
			writeFile(t, filepath.Join(dir, "defs.yaml"), fmt.Sprintf("components:\n  schemas:\n    %s:\n      type: object\n      properties:\n        next: {$ref: '#%s'}\n", name, fragment))
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			doc := unmarshalDoc(t, output)
			for _, path := range [][]string{{"components", "schemas", "Result", "$ref"}, {"components", "schemas", name, "properties", "next", "$ref"}} {
				if got := mapValueAt(t, doc, path...); got != "#"+fragment {
					t.Errorf("reference spelling changed: %v", got)
				}
			}
			if err := OapiYaml(output, filepath.Join(dir, "rebundled.yaml")); err != nil {
				t.Fatalf("rebundling encoded local refs: %v", err)
			}
		})
	}
}

func TestEncodedReferenceErrors(t *testing.T) {
	for _, ref := range []string{
		"#%2Fcomponents%2Fschemas%2FMissing",
		"#/%63omponents/schemas/Missing",
		"#%2Gcomponents/schemas/Missing",
		"#/components/schemas/%",
		"./defs.yaml#%2GValue",
		"./bad%file.yaml#/Value",
	} {
		t.Run(ref, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, fmt.Sprintf("openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Result: {$ref: '%s'}\n", ref))
			writeFile(t, filepath.Join(dir, "defs.yaml"), "Value: {type: string}\n")
			writeFile(t, output, "existing output\n")
			if err := OapiYaml(input, output); err == nil {
				t.Fatalf("invalid reference accepted: %s", ref)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "existing output\n" {
				t.Fatalf("failed reference overwrote output: %q, %v", data, err)
			}
		})
	}
}
