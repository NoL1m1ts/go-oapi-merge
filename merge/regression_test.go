package merge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestOapiYamlPreservesLiteralPayloads(t *testing.T) {
	for _, location := range []string{"inline", "external"} {
		t.Run(location, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			pathItem := `
    x-data: {$ref: 'missing.yaml#/Extension'}
    get:
      parameters:
        - name: query
          in: query
          example: {$ref: 'missing.yaml#/Parameter'}
          schema:
            type: object
            default: {$ref: '#/NotAReference'}
            const: {$ref: 'missing.yaml#/Constant'}
            enum: [{$ref: 'missing.yaml#/Enum'}]
            examples: [{$ref: 'missing.yaml#/SchemaExample'}]
            properties:
              default: {$ref: './shared.yaml#/components/schemas/Text'}
              x-value: {$ref: './shared.yaml#/components/schemas/Text'}
              $ref: {$ref: './shared.yaml#/components/schemas/Text'}
      responses:
        '200':
          description: OK
          content:
            application/json:
              example: {$ref: 'missing.yaml#/MediaExample'}
              examples:
                inline:
                  value: {$ref: 'missing.yaml#/ExampleValue'}
                example: {$ref: './shared.yaml#/components/examples/Sample'}
`
			paths := "  /test:" + pathItem
			if location == "external" {
				writeFile(t, filepath.Join(dir, "path.yaml"), "test:"+pathItem)
				paths = "  /test: {$ref: './path.yaml#/test'}\n"
			}
			writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n"+paths)
			writeFile(t, filepath.Join(dir, "shared.yaml"), `
components:
  schemas:
    Text: {type: string}
  examples:
    Sample:
      value: {$ref: 'missing.yaml#/ImportedExample'}
`)
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			doc := unmarshalDoc(t, output)
			path := mapValueAt(t, doc, "paths", "/test").(yaml.MapSlice)
			if got := mapValueAt(t, path, "x-data", "$ref"); got != "missing.yaml#/Extension" {
				t.Fatalf("extension mutated: %v", got)
			}
			parameters := mapValueAt(t, path, "get", "parameters").([]any)
			parameter := parameters[0].(yaml.MapSlice)
			if got := mapValueAt(t, parameter, "schema", "default", "$ref"); got != "#/NotAReference" {
				t.Fatalf("default mutated: %v", got)
			}
			for _, name := range []string{"default", "x-value", "$ref"} {
				if got := mapValueAt(t, parameter, "schema", "properties", name, "$ref"); got != "#/components/schemas/Text" {
					t.Errorf("property %s was not resolved: %v", name, got)
				}
			}
			if got := mapValueAt(t, doc, "components", "examples", "Sample", "value", "$ref"); got != "missing.yaml#/ImportedExample" {
				t.Fatalf("imported example mutated: %v", got)
			}
			data, _ := os.ReadFile(output)
			for _, fragment := range []string{"Parameter", "Constant", "Enum", "SchemaExample", "MediaExample", "ExampleValue"} {
				if !bytes.Contains(data, []byte("missing.yaml#/"+fragment)) {
					t.Errorf("literal %s changed or disappeared", fragment)
				}
			}
		})
	}
}

func TestOapiYamlStableDependencyOrder(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	root := "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n"
	for _, name := range []string{"z", "a", "m"} {
		root += fmt.Sprintf("  /%s: {$ref: './%s.yaml#/item'}\n", name, name)
		writeFile(t, filepath.Join(dir, name+".yaml"), fmt.Sprintf(`
item:
  get:
    responses:
      '200': {description: OK}
components:
  schemas:
    %[1]s:
      type: object
      properties:
        zebra: {type: string}
        alpha: {$ref: './nested-%[1]s.yaml#/components/schemas/Nested%[1]s'}
    Duplicate: {description: %[1]s}
`, name))
		writeFile(t, filepath.Join(dir, "nested-"+name+".yaml"), fmt.Sprintf("components:\n  schemas:\n    Nested%s: {type: string}\n", name))
	}
	root += "components:\n  schemas:\n    Root: {type: string}\n"
	writeFile(t, input, root)
	var baseline []byte
	for run := 0; run < 40; run++ {
		if err := OapiYaml(input, output); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			baseline = data
		} else if !bytes.Equal(data, baseline) {
			t.Fatalf("output changed on identical run %d", run)
		}
	}
	doc := unmarshalDoc(t, output)
	schemas := mapValueAt(t, doc, "components", "schemas").(yaml.MapSlice)
	want := []string{"Root", "a", "Duplicate", "m", "z", "Nesteda", "Nestedm", "Nestedz"}
	if got := mapSliceKeys(schemas); !reflect.DeepEqual(got, want) {
		t.Errorf("component order = %v, want %v", got, want)
	}
	if got := mapValueAt(t, schemas, "Duplicate", "description"); got != "a" {
		t.Errorf("duplicate winner = %v, want lexical first dependency a", got)
	}
	properties := mapValueAt(t, schemas, "a", "properties").(yaml.MapSlice)
	if got := mapSliceKeys(properties); !reflect.DeepEqual(got, []string{"zebra", "alpha"}) {
		t.Errorf("source property order changed: %v", got)
	}
}

func TestOapiYamlResolvesNestedArbitraryTargets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "defs"), 0750); err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.2.0
info: {title: Test, version: '1'}
paths:
  /test:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: './defs/pet.yaml#/Pet'}
`)
	writeFile(t, filepath.Join(dir, "defs", "pet.yaml"), `
Pet:
  type: object
  properties:
    name: {$ref: '#/Name'}
    age: {$ref: './age.yaml#/Age'}
Name: {type: string}
`)
	writeFile(t, filepath.Join(dir, "defs", "age.yaml"), "Age: {type: integer}\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	properties := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json", "schema", "properties").(yaml.MapSlice)
	if got := mapValueAt(t, properties, "name", "type"); got != "string" {
		t.Errorf("name type = %v", got)
	}
	if got := mapValueAt(t, properties, "age", "type"); got != "integer" {
		t.Errorf("age type = %v", got)
	}
}

func TestOapiYamlRejectsActualDanglingReference(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n  /test: {$ref: '#/components/pathItems/Missing'}\n")
	if err := OapiYaml(input, output); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("expected dangling reference error, got %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failure must not write an output file: %v", err)
	}
}

func TestOapiYamlPreservesPathAndComponentExtensions(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.2.0
info: {title: Test, version: '1'}
paths:
  x-payload: {$ref: 'missing.yaml#/Literal'}
  /test: {$ref: './paths.yaml#/test'}
webhooks:
  x-hook: {$ref: './paths.yaml#/test'}
`)
	writeFile(t, filepath.Join(dir, "paths.yaml"), `
test:
  post:
    responses:
      '200': {description: OK}
      x-payload: {$ref: 'missing.yaml#/ResponseExtension'}
components:
  x-payload:
    nested: {$ref: 'missing.yaml#/Literal'}
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "x-payload", "$ref"); got != "missing.yaml#/Literal" {
		t.Errorf("paths extension changed: %v", got)
	}
	if got := mapValueAt(t, doc, "components", "x-payload", "nested", "$ref"); got != "missing.yaml#/Literal" {
		t.Errorf("components extension changed: %v", got)
	}
	if got := mapValueAt(t, doc, "webhooks", "x-hook", "post", "responses", "200", "description"); got != "OK" {
		t.Errorf("webhook named x-hook not resolved: %v", got)
	}
	if got := mapValueAt(t, doc, "paths", "/test", "post", "responses", "x-payload", "$ref"); got != "missing.yaml#/ResponseExtension" {
		t.Errorf("responses extension changed: %v", got)
	}
}

func TestOapiYamlArbitrarySchemaReferenceSiblings(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.1.0
info: {title: Test, version: '1'}
paths: {}
components:
  schemas:
    Choice:
      $ref: './schema.yaml#/Never'
      description: Retain the rejecting target and sibling constraints
      allOf:
        - {type: string}
`)
	writeFile(t, filepath.Join(dir, "schema.yaml"), "Never: false\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	allOf := mapValueAt(t, doc, "components", "schemas", "Choice", "allOf").([]any)
	if len(allOf) != 2 || allOf[1] != false {
		t.Fatalf("referenced false schema was lost: %#v", allOf)
	}
	if got := mapValueAt(t, allOf[0].(yaml.MapSlice), "type"); got != "string" {
		t.Fatalf("original allOf constraint lost: %v", got)
	}
}

func TestOapiYamlRejectsArbitraryReferenceCycle(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Pet: {$ref: './schema.yaml#/Pet'}\n")
	writeFile(t, filepath.Join(dir, "schema.yaml"), "Pet:\n  type: object\n  properties:\n    child: {$ref: '#/Pet'}\n")
	if err := OapiYaml(input, output); err == nil || !strings.Contains(err.Error(), "cyclic reference") {
		t.Fatalf("expected explicit cycle error, got %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("cycle must not write output: %v", err)
	}
}

func TestOapiYamlEscapedAndArrayReferenceTargets(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.2.0
info: {title: Test, version: '1'}
paths:
  /pets: {$ref: './paths.yaml#/paths/~1pets'}
components:
  schemas:
    Alias: {$ref: '#/components/schemas/Array/prefixItems/0'}
    Array:
      prefixItems:
        - {type: string}
`)
	writeFile(t, filepath.Join(dir, "paths.yaml"), `
paths:
  /pets:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: './schema.yaml#/Pet~0Type'}
`)
	writeFile(t, filepath.Join(dir, "schema.yaml"), "Pet~Type: {type: string}\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "/pets", "get", "responses", "200", "content", "application/json", "schema", "type"); got != "string" {
		t.Errorf("escaped schema target = %v", got)
	}
}

func TestOapiYamlDoesNotMutateAliasedLiteralData(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.2.0
info: {title: Test, version: '1'}
paths:
  /test:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: &shared
                allOf:
                  - {$ref: './defs.yaml#/components/schemas/Text'}
              example: *shared
`)
	writeFile(t, filepath.Join(dir, "defs.yaml"), "components:\n  schemas:\n    Text: {type: string}\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	media := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json").(yaml.MapSlice)
	for _, field := range []string{"schema", "example"} {
		allOf := mapValueAt(t, media, field, "allOf").([]any)
		want := "#/components/schemas/Text"
		if field == "example" {
			want = "./defs.yaml#/components/schemas/Text"
		}
		if got := mapValueAt(t, allOf[0].(yaml.MapSlice), "$ref"); got != want {
			t.Errorf("%s ref = %v, want %s", field, got, want)
		}
	}
}

func TestOapiYamlResolvesEmptyPropertyPointer(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    Empty: {$ref: './defs.yaml#/properties/'}\n")
	writeFile(t, filepath.Join(dir, "defs.yaml"), "type: object\nproperties:\n  '': {type: string}\n  other: {type: integer}\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "components", "schemas", "Empty", "type"); got != "string" {
		t.Errorf("empty-name property target = %v", got)
	}
}

func TestOapiYamlPreservesEmptyContainers(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, `
openapi: 3.2.0
info: {title: Test, version: '1'}
paths:
  /test:
    parameters: []
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                properties: {}
                allOf: []
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	if _, ok := mapValueAt(t, doc, "paths", "/test", "parameters").([]any); !ok {
		t.Fatal("empty parameters array changed type")
	}
	schema := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json", "schema").(yaml.MapSlice)
	if _, ok := mapValueAt(t, schema, "allOf").([]any); !ok {
		t.Fatal("empty allOf array changed type")
	}
	if _, ok := mapValueAt(t, schema, "properties").(yaml.MapSlice); !ok {
		t.Fatal("empty properties object changed type")
	}
}

func TestOapiYamlResolvesWholePathItemDocument(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths:\n  /test: {$ref: './path.yaml#'}\n")
	writeFile(t, filepath.Join(dir, "path.yaml"), "get:\n  responses:\n    '200': {description: OK}\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("whole path item not imported: %v", got)
	}
}
