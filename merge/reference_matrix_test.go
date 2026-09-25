package merge

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestReferenceInputMatrix(t *testing.T) {
	cases := []struct {
		name, ref, file, content, want string
	}{
		{"bare path file", "./path.yaml", "path.yaml", "get: {responses: {'200': {description: OK}}}\n", "OK"},
		{"encoded filename", "./path%20file.yaml#/item", "path file.yaml", "item: {get: {responses: {'200': {description: OK}}}}\n", "OK"},
		{"encoded fragment", "./path.yaml#/my%20path", "path.yaml", "my path: {get: {responses: {'200': {description: OK}}}}\n", "OK"},
		{"literal percent", "./path.yaml#/item%2520", "path.yaml", "item%20: {get: {responses: {'200': {description: OK}}}}\n", "OK"},
		{"encoded hash filename", "./path%23file.yaml#/item", "path#file.yaml", "item: {get: {responses: {'200': {description: OK}}}}\n", "OK"},
		{"unicode", "./путь.yaml#/запрос", "путь.yaml", "запрос: {get: {responses: {'200': {description: Привет}}}}\n", "Привет"},
		{"json dependency", "./path.json#/item", "path.json", `{"item":{"get":{"responses":{"200":{"description":"OK"}}}}}`, "OK"},
		{"parent directory", "../path.yaml#/item", "../path.yaml", "item: {get: {responses: {'200': {description: OK}}}}\n", "OK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "specs")
			if err := os.Mkdir(dir, 0750); err != nil {
				t.Fatal(err)
			}
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, filepath.Join(dir, tc.file), tc.content)
			writeFile(t, input, fmt.Sprintf("openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n  /test: {$ref: %q}\n", tc.ref))
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			doc := unmarshalDoc(t, output)
			if got := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "description"); got != tc.want {
				t.Errorf("description = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestStandaloneSchemaMatrix(t *testing.T) {
	for _, target := range []string{"true", "false", "{type: string}", "{$ref: './next.yaml#/Value'}"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, filepath.Join(dir, "schema.yaml"), target+"\n")
			writeFile(t, filepath.Join(dir, "next.yaml"), "Value: {type: string}\n")
			writeFile(t, input, "openapi: 3.1.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Value: {$ref: './schema.yaml'}\n")
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			doc := unmarshalDoc(t, output)
			value := mapValueAt(t, doc, "components", "schemas", "Value")
			if target == "true" || target == "false" {
				if value != (target == "true") {
					t.Errorf("boolean schema = %#v, want %s", value, target)
				}
			} else if got := mapValueAt(t, value.(yaml.MapSlice), "type"); got != "string" {
				t.Errorf("schema type = %v", got)
			}
		})
	}
}

func TestJSONPointerArrayIndices(t *testing.T) {
	doc := yaml.MapSlice{{Key: "items", Value: []any{"first", "second"}}}
	for _, index := range []string{"00", "01", "+0", "-0", "-", "2", "999999999999999999999", ""} {
		t.Run(index, func(t *testing.T) {
			if got, err := navigateToFragment(doc, "/items/"+index, "input.yaml"); err == nil {
				t.Fatalf("invalid array index %q resolved to %v", index, got)
			}
		})
	}
}

func TestYAMLVersionDirective(t *testing.T) {
	for _, version := range []string{"1.1", "1.2"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, "%YAML "+version+" # version\n---\nopenapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\nx-text: \"line\n%YAML 1.2\nend\"\n")
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			doc := unmarshalDoc(t, output)
			if got := mapValueAt(t, doc, "x-text"); got != "line %YAML 1.2 end" {
				t.Fatalf("directive-like literal changed: %q", got)
			}
		})
	}
}

func TestGeneratedDependencyGraphs(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			random := rand.New(rand.NewSource(seed))
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			const count = 9
			root := "openapi: 3.2.0\ninfo: {title: Generated, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"
			for _, n := range random.Perm(count) {
				root += fmt.Sprintf("    Root%d: {$ref: './file%d.yaml#/components/schemas/Node%d'}\n", n, n, n)
			}
			for n := 0; n < count; n++ {
				next := random.Intn(count)
				writeFile(t, filepath.Join(dir, fmt.Sprintf("file%d.yaml", n)), fmt.Sprintf("components:\n  schemas:\n    Node%d:\n      type: object\n      properties:\n        z: {type: string}\n        next: {$ref: './file%d.yaml#/components/schemas/Node%d'}\n        a: {type: integer}\n", n, next, next))
			}
			writeFile(t, input, root)
			var baseline []byte
			for run := 0; run < 6; run++ {
				if err := OapiYaml(input, output); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if run == 0 {
					baseline = data
				} else if !bytes.Equal(baseline, data) {
					t.Fatalf("output changed on run %d", run)
				}
			}
			doc := unmarshalDoc(t, output)
			schemas := mapValueAt(t, doc, "components", "schemas").(yaml.MapSlice)
			if len(schemas) != 2*count {
				t.Fatalf("lost components: got %d, want %d", len(schemas), 2*count)
			}
			for n := 0; n < count; n++ {
				properties := mapValueAt(t, schemas, fmt.Sprintf("Node%d", n), "properties").(yaml.MapSlice)
				if got := strings.Join(mapSliceKeys(properties), ","); got != "z,next,a" {
					t.Fatalf("property order changed: %s", got)
				}
			}
			second := filepath.Join(dir, "second.yaml")
			if err := OapiYaml(output, second); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(second)
			if !bytes.Equal(baseline, data) {
				t.Fatal("rebundling already bundled output changed the document")
			}
		})
	}
}

func FuzzLiteralPayloadRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "hello", "a\u2028b", "a\u2029b", "true", "null", "01", "Привет 🌍", "line 1\nline 2\n", "a: b # comment", "./missing.yaml#/Pet", "&anchor *alias", "\tquoted\r\nline", "\"'\\\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 2048 || !utf8.ValidString(value) {
			t.Skip()
		}
		dir := t.TempDir()
		input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
		encoded := strconv.Quote(value)
		writeFile(t, input, fmt.Sprintf("openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Value:\n      type: object\n      default: {$ref: %s}\n", encoded))
		if err := OapiYaml(input, output); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Components struct {
				Schemas map[string]struct {
					Default map[string]any `yaml:"default"`
				} `yaml:"schemas"`
			} `yaml:"components"`
		}
		if err := yamlv3.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parser rejected output: %v\n%s", err, data)
		}
		secondary := unmarshalDoc(t, output)
		if got := mapValueAt(t, secondary, "components", "schemas", "Value", "default", "$ref"); got != value {
			t.Fatalf("secondary decoder literal changed: got %#v, want %q", got, value)
		}
		if got := doc.Components.Schemas["Value"].Default["$ref"]; got != value {
			t.Fatalf("literal changed: got %#v, want %q\n%s", got, value, data)
		}
	})
}

func TestRepositoryExamples(t *testing.T) {
	for _, name := range []string{"api.yaml", "openapi3.2/root.yaml"} {
		t.Run(name, func(t *testing.T) {
			input := filepath.Join("..", "example", filepath.FromSlash(name))
			dir := t.TempDir()
			output, repeated, rebundled := filepath.Join(dir, "out.yaml"), filepath.Join(dir, "repeated.yaml"), filepath.Join(dir, "rebundled.yaml")
			for _, target := range []string{output, repeated} {
				if err := OapiYaml(input, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := OapiYaml(output, rebundled); err != nil {
				t.Fatal(err)
			}
			baseline, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{repeated, rebundled} {
				data, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(baseline, data) {
					t.Fatalf("example output changed: %s", target)
				}
			}
			doc := unmarshalDoc(t, output)
			if len(doc) == 0 {
				t.Fatal("empty output")
			}
		})
	}
}
