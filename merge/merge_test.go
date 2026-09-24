package merge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
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
        $ref: './responses.yaml#/components/responses/OK'

components:
  schemas:
    User:
      type: object
`)
	writeFile(t, filepath.Join(tmpDir, "responses.yaml"), `
components:
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

func TestOapiYamlMergesTransitivelyReferencedComponents(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")
	pathsDir := filepath.Join(tmpDir, "paths")
	componentsDir := filepath.Join(tmpDir, "components")
	schemasDir := filepath.Join(componentsDir, "schemas")

	for _, dir := range []string{pathsDir, schemasDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
	}

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /users:
    $ref: './paths/users.yaml#/users'
`)
	writeFile(t, filepath.Join(pathsDir, "users.yaml"), `
users:
  get:
    responses:
      "200":
        $ref: '../components/responses.yaml#/components/responses/OK'
`)
	writeFile(t, filepath.Join(componentsDir, "responses.yaml"), `
components:
  responses:
    OK:
      description: Success
      content:
        application/json:
          schema:
            $ref: './schemas/user.yaml#/components/schemas/User'
`)
	writeFile(t, filepath.Join(schemasDir, "user.yaml"), `
components:
  schemas:
    User:
      type: object
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}

	var doc yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &doc, yaml.UseOrderedMap()); err != nil {
		t.Fatalf("parse merged output: %v", err)
	}
	paths, components := docPathsAndComponents(t, doc)

	pathResponseRef := mapValueAt(
		t,
		paths,
		"/users",
		"get",
		"responses",
		"200",
		"$ref",
	)
	if pathResponseRef != "#/components/responses/OK" {
		t.Errorf("path response ref = %v, want #/components/responses/OK", pathResponseRef)
	}

	responseSchemaRef := mapValueAt(
		t,
		components,
		"responses",
		"OK",
		"content",
		"application/json",
		"schema",
		"$ref",
	)
	if responseSchemaRef != "#/components/schemas/User" {
		t.Errorf("response schema ref = %v, want #/components/schemas/User", responseSchemaRef)
	}

	if got := mapValueAt(
		t,
		components,
		"schemas",
		"User",
		"type",
	); got != "object" {
		t.Errorf("transitively referenced schema type = %v, want object", got)
	}
}

func TestOapiYamlIgnoresReferencesFromDiscardedComponents(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /users:
    $ref: './paths.yaml#/users'
components:
  schemas:
    User:
      type: object
`)
	writeFile(t, filepath.Join(tmpDir, "paths.yaml"), `
users:
  get:
    responses:
      "200":
        description: Success
components:
  schemas:
    User:
      $ref: './missing.yaml#/components/schemas/Missing'
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error from discarded component reference: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}

	var doc yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &doc, yaml.UseOrderedMap()); err != nil {
		t.Fatalf("parse merged output: %v", err)
	}
	_, components := docPathsAndComponents(t, doc)

	if got := mapValueAt(
		t,
		components,
		"schemas",
		"User",
		"type",
	); got != "object" {
		t.Errorf("main component type = %v, want object", got)
	}
}

func TestOapiYamlHandlesSymlinkedComponentCycle(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")
	componentsDir := filepath.Join(tmpDir, "components")

	if err := os.MkdirAll(componentsDir, 0750); err != nil {
		t.Fatalf("create components directory: %v", err)
	}
	if err := os.Symlink(".", filepath.Join(componentsDir, "alias")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/test'
`)
	writeFile(t, filepath.Join(tmpDir, "paths.yaml"), `
test:
  get:
    responses:
      "200":
        description: Success
        content:
          application/json:
            schema:
              $ref: './components/a.yaml#/components/schemas/A'
`)
	writeFile(t, filepath.Join(componentsDir, "a.yaml"), `
components:
  schemas:
    A:
      type: object
      properties:
        b:
          $ref: './alias/b.yaml#/components/schemas/B'
`)
	writeFile(t, filepath.Join(componentsDir, "b.yaml"), `
components:
  schemas:
    B:
      type: object
      properties:
        a:
          $ref: './alias/a.yaml#/components/schemas/A'
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error from symlinked component cycle: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}

	var doc yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &doc, yaml.UseOrderedMap()); err != nil {
		t.Fatalf("parse merged output: %v", err)
	}
	_, components := docPathsAndComponents(t, doc)

	aRef := mapValueAt(
		t,
		components,
		"schemas",
		"A",
		"properties",
		"b",
		"$ref",
	)
	if aRef != "#/components/schemas/B" {
		t.Errorf("A.b ref = %v, want #/components/schemas/B", aRef)
	}

	bRef := mapValueAt(
		t,
		components,
		"schemas",
		"B",
		"properties",
		"a",
		"$ref",
	)
	if bRef != "#/components/schemas/A" {
		t.Errorf("B.a ref = %v, want #/components/schemas/A", bRef)
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

func TestOapiYamlOpenAPI32Version(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.2.0
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

	doc := unmarshalDoc(t, output)
	version, _ := getMapSliceValue(doc, "openapi").(string)
	if version != "3.2.0" {
		t.Errorf("openapi version = %q, want %q", version, "3.2.0")
	}
}

func TestOapiYamlVersionCompatibility(t *testing.T) {
	versions := []string{"3.0.0", "3.0.3", "3.1.0", "3.1.1", "3.2.0", "3.2.5"}

	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			tmpDir := t.TempDir()
			input := filepath.Join(tmpDir, "api.yaml")
			output := filepath.Join(tmpDir, "out.yaml")

			writeFile(t, input, `
openapi: "`+version+`"
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
				t.Fatalf("unexpected error for version %s: %v", version, err)
			}

			doc := unmarshalDoc(t, output)
			got, _ := getMapSliceValue(doc, "openapi").(string)
			if got != version {
				t.Errorf("openapi version = %q, want %q", got, version)
			}
		})
	}
}

func TestOapiYamlHierarchicalTags(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.2.0
info:
  title: Test
  version: "1.0"
tags:
  - name: Catalog

  - name: Categories
    description: Category management
    parent: Catalog

  - name: Items
    parent: Catalog

  - name: Bundles
    parent: Catalog

  - name: FeaturedItems
    parent: Items

  - name: Legacy
    description: Deprecated endpoints
    externalDocs:
      description: Migration guide
      url: https://example.com/migration
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

	doc := unmarshalDoc(t, output)
	tags := tagsByName(t, doc)

	if len(tags) != 6 {
		t.Fatalf("expected 6 tags, got %d", len(tags))
	}

	if v := getMapSliceValue(tags["Catalog"], "parent"); v != nil {
		t.Errorf("Catalog should not have a parent, got %v", v)
	}

	if got := getMapSliceValue(tags["Categories"], "parent"); got != "Catalog" {
		t.Errorf("Categories.parent = %v, want Catalog", got)
	}
	if got := getMapSliceValue(tags["Categories"], "description"); got != "Category management" {
		t.Errorf("Categories.description = %v, want %q", got, "Category management")
	}

	if got := getMapSliceValue(tags["Items"], "parent"); got != "Catalog" {
		t.Errorf("Items.parent = %v, want Catalog", got)
	}
	if got := getMapSliceValue(tags["Bundles"], "parent"); got != "Catalog" {
		t.Errorf("Bundles.parent = %v, want Catalog", got)
	}

	if got := getMapSliceValue(tags["FeaturedItems"], "parent"); got != "Items" {
		t.Errorf("FeaturedItems.parent = %v, want Items", got)
	}

	if v := getMapSliceValue(tags["Legacy"], "parent"); v != nil {
		t.Errorf("Legacy should not have a parent, got %v", v)
	}
	externalDocs, ok := getMapSliceValue(tags["Legacy"], "externalDocs").(yaml.MapSlice)
	if !ok {
		t.Fatalf("Legacy.externalDocs missing or wrong type")
	}
	if got := getMapSliceValue(externalDocs, "url"); got != "https://example.com/migration" {
		t.Errorf("Legacy.externalDocs.url = %v, want %q", got, "https://example.com/migration")
	}
}

func TestOapiYamlMixed30And32Tags(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.2.0
info:
  title: Test
  version: "1.0"
tags:
  - name: PlainTag
    description: A plain OpenAPI 3.0-style tag with no hierarchy
  - name: Catalog
  - name: Categories
    parent: Catalog
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

	doc := unmarshalDoc(t, output)
	tags := tagsByName(t, doc)

	if v := getMapSliceValue(tags["PlainTag"], "parent"); v != nil {
		t.Errorf("PlainTag should not have a parent, got %v", v)
	}
	if got := getMapSliceValue(tags["Categories"], "parent"); got != "Catalog" {
		t.Errorf("Categories.parent = %v, want Catalog", got)
	}
}

func TestOapiYamlMultiFileHierarchicalTags(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root.yaml")
	categories := filepath.Join(tmpDir, "categories.yaml")
	items := filepath.Join(tmpDir, "items.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, root, `
openapi: 3.2.0
info:
  title: Catalog API
  version: "1.0"
tags:
  - name: Catalog
  - name: Categories
    parent: Catalog
  - name: Items
    parent: Catalog
paths:
  /categories:
    $ref: './categories.yaml#/categories'
  /items:
    $ref: './items.yaml#/items'
`)
	writeFile(t, categories, `
categories:
  get:
    tags:
      - Categories
    responses:
      "200":
        description: OK
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Category'

components:
  schemas:
    Category:
      type: object
      properties:
        name:
          type: string
`)
	writeFile(t, items, `
items:
  get:
    tags:
      - Items
    responses:
      "200":
        description: OK
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Item'

components:
  schemas:
    Item:
      type: object
      properties:
        name:
          type: string
`)

	if err := OapiYaml(root, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)

	version, _ := getMapSliceValue(doc, "openapi").(string)
	if version != "3.2.0" {
		t.Errorf("openapi version = %q, want 3.2.0", version)
	}

	tags := tagsByName(t, doc)
	if got := getMapSliceValue(tags["Categories"], "parent"); got != "Catalog" {
		t.Errorf("Categories.parent = %v, want Catalog", got)
	}
	if got := getMapSliceValue(tags["Items"], "parent"); got != "Catalog" {
		t.Errorf("Items.parent = %v, want Catalog", got)
	}

	paths, components := docPathsAndComponents(t, doc)
	if got := mapValueAt(t, paths, "/categories", "get", "responses", "200", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/Category" {
		t.Errorf("categories schema ref = %v, want #/components/schemas/Category", got)
	}
	if got := mapValueAt(t, components, "schemas", "Category", "type"); got != "object" {
		t.Errorf("Category.type = %v, want object", got)
	}
	if got := mapValueAt(t, components, "schemas", "Item", "type"); got != "object" {
		t.Errorf("Item.type = %v, want object", got)
	}
}

func TestOapiYamlPreservesExtraRootFields(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.2.0
summary: Root level summary
info:
  title: Test
  version: "1.0"
jsonSchemaDialect: https://json-schema.org/draft/2020-12/schema
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
externalDocs:
  description: Find out more
  url: https://example.com/docs
x-custom-extension: hello
$self: https://example.com/api
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)

	if got := getMapSliceValue(doc, "summary"); got != "Root level summary" {
		t.Errorf("summary = %v, want %q", got, "Root level summary")
	}
	if got := getMapSliceValue(doc, "jsonSchemaDialect"); got != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("jsonSchemaDialect = %v", got)
	}
	if got := getMapSliceValue(doc, "x-custom-extension"); got != "hello" {
		t.Errorf("x-custom-extension = %v, want %q", got, "hello")
	}
	if got := getMapSliceValue(doc, "$self"); got != "https://example.com/api" {
		t.Errorf("$self = %v, want %q", got, "https://example.com/api")
	}
	externalDocs, ok := getMapSliceValue(doc, "externalDocs").(yaml.MapSlice)
	if !ok {
		t.Fatalf("externalDocs missing or wrong type")
	}
	if got := getMapSliceValue(externalDocs, "url"); got != "https://example.com/docs" {
		t.Errorf("externalDocs.url = %v", got)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(data)
	idxPaths := strings.Index(content, "paths:")
	idxSummary := strings.Index(content, "summary:")
	idxJSONSchemaDialect := strings.Index(content, "jsonSchemaDialect:")
	idxExternalDocs := strings.Index(content, "externalDocs:")
	idxExtension := strings.Index(content, "x-custom-extension:")
	idxSelf := strings.Index(content, "$self:")
	if idxPaths == -1 || idxPaths > idxSummary {
		t.Error("paths should come before the extra root fields")
	}
	if !(idxSummary < idxJSONSchemaDialect && idxJSONSchemaDialect < idxExternalDocs && idxExternalDocs < idxExtension && idxExtension < idxSelf) {
		t.Error("extra root fields should preserve their original relative order")
	}
}

func TestOapiYamlOmitsAbsentExtraRootFields(t *testing.T) {
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

	doc := unmarshalDoc(t, output)
	for _, key := range []string{"summary", "jsonSchemaDialect", "$self", "externalDocs"} {
		if got := getMapSliceValue(doc, key); got != nil {
			t.Errorf("%s should be absent, got %v", key, got)
		}
	}
}

func TestOapiYamlResolvesWebhookReferences(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	webhookDefs := filepath.Join(tmpDir, "webhook-defs.yaml")
	schemas := filepath.Join(tmpDir, "schemas.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.1.0
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
webhooks:
  newItem:
    $ref: './webhook-defs.yaml#/newItem'
`)
	writeFile(t, webhookDefs, `
newItem:
  post:
    summary: New item webhook
    requestBody:
      content:
        application/json:
          schema:
            $ref: './schemas.yaml#/components/schemas/Item'
    responses:
      "200":
        description: OK
`)
	writeFile(t, schemas, `
components:
  schemas:
    Item:
      type: object
      properties:
        name:
          type: string
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	webhooks, ok := getMapSliceValue(doc, "webhooks").(yaml.MapSlice)
	if !ok {
		t.Fatalf("webhooks missing or wrong type")
	}

	if got := mapValueAt(t, webhooks, "newItem", "post", "summary"); got != "New item webhook" {
		t.Errorf("webhooks.newItem.post.summary = %v, want %q", got, "New item webhook")
	}

	schemaRef := mapValueAt(t, webhooks, "newItem", "post", "requestBody", "content", "application/json", "schema", "$ref")
	if schemaRef != "#/components/schemas/Item" {
		t.Errorf("webhook schema ref = %v, want #/components/schemas/Item", schemaRef)
	}
	_, components := docPathsAndComponents(t, doc)
	if got := mapValueAt(t, components, "schemas", "Item", "type"); got != "object" {
		t.Errorf("Item.type = %v, want object", got)
	}
}

func TestOapiYamlResolvesNestedRefsInInlinePaths(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	schemas := filepath.Join(tmpDir, "schemas.yaml")
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
          content:
            application/json:
              schema:
                $ref: './schemas.yaml#/components/schemas/Item'
`)
	writeFile(t, schemas, `
components:
  schemas:
    Item:
      type: object
      properties:
        name:
          type: string
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths, components := docPathsAndComponents(t, doc)

	schemaRef := mapValueAt(t, paths, "/test", "get", "responses", "200", "content", "application/json", "schema", "$ref")
	if schemaRef != "#/components/schemas/Item" {
		t.Errorf("inline path schema ref = %v, want #/components/schemas/Item", schemaRef)
	}
	if got := mapValueAt(t, components, "schemas", "Item", "type"); got != "object" {
		t.Errorf("Item.type = %v, want object", got)
	}
}

func TestOapiYamlResolvesNestedRefsInInlineWebhooks(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	schemas := filepath.Join(tmpDir, "schemas.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.1.0
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
webhooks:
  newItem:
    post:
      summary: New item webhook
      requestBody:
        content:
          application/json:
            schema:
              $ref: './schemas.yaml#/components/schemas/Item'
      responses:
        "200":
          description: OK
`)
	writeFile(t, schemas, `
components:
  schemas:
    Item:
      type: object
      properties:
        name:
          type: string
`)

	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	webhooks, ok := getMapSliceValue(doc, "webhooks").(yaml.MapSlice)
	if !ok {
		t.Fatalf("webhooks missing or wrong type")
	}

	schemaRef := mapValueAt(t, webhooks, "newItem", "post", "requestBody", "content", "application/json", "schema", "$ref")
	if schemaRef != "#/components/schemas/Item" {
		t.Errorf("inline webhook schema ref = %v, want #/components/schemas/Item", schemaRef)
	}
	_, components := docPathsAndComponents(t, doc)
	if got := mapValueAt(t, components, "schemas", "Item", "type"); got != "object" {
		t.Errorf("Item.type = %v, want object", got)
	}
}

func TestOapiYamlOmitsAbsentWebhooks(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.0.0
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

	doc := unmarshalDoc(t, output)
	if getMapSliceValue(doc, "webhooks") != nil {
		t.Error("webhooks should not be synthesized when absent from input")
	}
}

func unmarshalDoc(t *testing.T, path string) yaml.MapSlice {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var doc yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &doc, yaml.UseOrderedMap()); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	return doc
}

func tagsByName(t *testing.T, doc yaml.MapSlice) map[string]yaml.MapSlice {
	t.Helper()
	rawTags, ok := getMapSliceValue(doc, "tags").([]interface{})
	if !ok {
		t.Fatalf("tags missing or wrong type")
	}
	byName := make(map[string]yaml.MapSlice, len(rawTags))
	for _, rt := range rawTags {
		tag, ok := rt.(yaml.MapSlice)
		if !ok {
			t.Fatalf("tag entry has type %T, want yaml.MapSlice", rt)
		}
		name, _ := getMapSliceValue(tag, "name").(string)
		if name == "" {
			t.Fatalf("tag entry missing name: %+v", tag)
		}
		byName[name] = tag
	}
	return byName
}

func docPathsAndComponents(t *testing.T, doc yaml.MapSlice) (yaml.MapSlice, yaml.MapSlice) {
	t.Helper()
	paths, _ := getMapSliceValue(doc, "paths").(yaml.MapSlice)
	components, _ := getMapSliceValue(doc, "components").(yaml.MapSlice)
	return paths, components
}

func mapValueAt(t *testing.T, root yaml.MapSlice, keys ...string) any {
	t.Helper()

	var value any = root
	for _, key := range keys {
		current, ok := value.(yaml.MapSlice)
		if !ok {
			t.Fatalf("value at %q has type %T, want yaml.MapSlice", strings.Join(keys, "."), value)
		}

		value = getMapSliceValue(current, key)
		if value == nil {
			t.Fatalf("key %q not found at %q", key, strings.Join(keys, "."))
		}
	}

	return value
}

func TestMergeErrorMessageFormatting(t *testing.T) {
	tests := []struct {
		name string
		err  *MergeError
		want string
	}{
		{
			name: "message only",
			err:  &MergeError{Message: "something went wrong"},
			want: "something went wrong",
		},
		{
			name: "message and file",
			err:  &MergeError{Message: "something went wrong", File: "api.yaml"},
			want: "something went wrong (in api.yaml)",
		},
		{
			name: "message, file, and path",
			err:  &MergeError{Message: "something went wrong", File: "api.yaml", Path: "/users"},
			want: "something went wrong (in api.yaml at /users)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeErrorUnwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	err := &MergeError{Message: "wrapped", Cause: cause}

	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

func TestOapiYamlInvalidYAMLSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info: [this is not valid
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "Invalid OpenAPI YAML structure") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlInvalidRefFormatInPaths(t *testing.T) {
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
    $ref: './missing-fragment.yaml'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for $ref missing a fragment")
	}
	if !strings.Contains(err.Error(), "missing fragment") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlInvalidRefFormatInWebhooks(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: 3.1.0
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
webhooks:
  newItem:
    $ref: './missing-fragment.yaml'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for $ref missing a fragment")
	}
	if !strings.Contains(err.Error(), "missing fragment") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlUnreadableRefFile(t *testing.T) {
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
    $ref: './missing.yaml#/test'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for unreadable ref target")
	}
	if !strings.Contains(err.Error(), "Cannot read referenced file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlRefTargetInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `test: [this is not valid`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/test'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for malformed ref target YAML")
	}
	if !strings.Contains(err.Error(), "Invalid YAML syntax") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlRefTargetNotAnObject(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `test: "just a string, not a path item"`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/test'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for a ref target that isn't an object")
	}
	if !strings.Contains(err.Error(), "Invalid reference target") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlRefFragmentKeyNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `test:
  get:
    responses:
      "200":
        description: OK
`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/nonexistent'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for a fragment key that doesn't exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlRefFragmentInvalidStructure(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `test: "a scalar, not a map"`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#/test/deeper'
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error navigating past a scalar")
	}
	if !strings.Contains(err.Error(), "Invalid reference structure") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlRefFragmentWithoutLeadingSlash(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `test:
  get:
    responses:
      "200":
        description: OK
`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#test'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error resolving a fragment without a leading slash: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths_, _ := docPathsAndComponents(t, doc)
	if got := mapValueAt(t, paths_, "/test", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("description = %v, want OK", got)
	}
}

func TestOapiYamlRefFragmentWithDoubleSlash(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `"":
  test:
    get:
      responses:
        "200":
          description: OK
`)
	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /test:
    $ref: './paths.yaml#//test'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error resolving a fragment with an empty segment: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths_, _ := docPathsAndComponents(t, doc)
	if got := mapValueAt(t, paths_, "/test", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("description = %v, want OK", got)
	}
}

func TestOapiYamlSkipsNonObjectPathItem(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
paths:
  /weird: "not an object"
  /test:
    get:
      responses:
        "200":
          description: OK
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths, _ := docPathsAndComponents(t, doc)
	if got := getMapSliceValue(paths, "/weird"); got != "not an object" {
		t.Errorf("/weird = %v, want unchanged string", got)
	}
	if got := mapValueAt(t, paths, "/test", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("description = %v, want OK", got)
	}
}

func TestOapiYamlNestedFileReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, paths, `
users:
  get:
    responses:
      "200":
        description: OK

components:
  schemas:
    User:
      $ref: './missing.yaml#/components/schemas/User'
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
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for a transitively referenced file that doesn't exist")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlNestedFileInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	paths := filepath.Join(tmpDir, "paths.yaml")
	broken := filepath.Join(tmpDir, "broken.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, broken, `components: [this is not valid`)
	writeFile(t, paths, `
users:
  get:
    responses:
      "200":
        description: OK

components:
  schemas:
    User:
      $ref: './broken.yaml#/components/schemas/User'
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
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for a transitively referenced file with invalid YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOapiYamlServersRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com/v1
    description: Production
  - url: https://staging.example.com/v1
    description: Staging
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

	doc := unmarshalDoc(t, output)
	servers, ok := getMapSliceValue(doc, "servers").([]any)
	if !ok {
		t.Fatalf("servers missing or wrong type")
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	first, ok := servers[0].(yaml.MapSlice)
	if !ok {
		t.Fatalf("servers[0] has wrong type: %T", servers[0])
	}
	if got := getMapSliceValue(first, "url"); got != "https://api.example.com/v1" {
		t.Errorf("servers[0].url = %v", got)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(data)
	if idxServers, idxPaths := strings.Index(content, "servers:"), strings.Index(content, "paths:"); idxServers == -1 || idxServers > idxPaths {
		t.Error("servers should appear before paths in the output")
	}
}

func TestOapiYamlEmptyServersSecurityTagsOmitted(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
servers: []
security: []
tags: []
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

	doc := unmarshalDoc(t, output)
	for _, key := range []string{"servers", "security", "tags"} {
		if getMapSliceValue(doc, key) != nil {
			t.Errorf("%s should be omitted from output when empty in input", key)
		}
	}
}

func TestOpenAPITypeRoundTrips(t *testing.T) {
	data := []byte(`
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
webhooks:
  newItem:
    post:
      responses:
        "200":
          description: OK
components:
  schemas:
    User:
      type: object
security:
  - bearerAuth: []
tags:
  - name: test
`)

	var doc OpenAPI
	if err := yaml.UnmarshalWithOptions(data, &doc, yaml.UseOrderedMap()); err != nil {
		t.Fatalf("unmarshal into OpenAPI type: %v", err)
	}

	if doc.OpenAPI != "3.0.0" {
		t.Errorf("OpenAPI = %q, want 3.0.0", doc.OpenAPI)
	}
	if len(doc.Servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(doc.Servers))
	}
	if got := mapValueAt(t, doc.Paths, "/test", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("description = %v, want OK", got)
	}
	if got := mapValueAt(t, doc.Webhooks, "newItem", "post", "responses", "200", "description"); got != "OK" {
		t.Errorf("webhooks description = %v, want OK", got)
	}
	if got := mapValueAt(t, doc.Components, "schemas", "User", "type"); got != "object" {
		t.Errorf("User.type = %v, want object", got)
	}
	if len(doc.Security) != 1 {
		t.Errorf("expected 1 security requirement, got %d", len(doc.Security))
	}
	if len(doc.Tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(doc.Tags))
	}

	out, err := yaml.MarshalWithOptions(&doc, yaml.Indent(2))
	if err != nil {
		t.Fatalf("marshal OpenAPI type: %v", err)
	}
	if !strings.Contains(string(out), "openapi: 3.0.0") {
		t.Errorf("marshaled output missing openapi version:\n%s", out)
	}
}

func TestOapiYamlRejectsMistypedSecurity(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
security:
  BearerAuth: []
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
`)
	err := OapiYaml(input, output)
	if err == nil {
		t.Fatal("expected error for mistyped security field")
	}
	cause := errors.Unwrap(err)
	if cause == nil || !strings.Contains(cause.Error(), "sequence") {
		t.Errorf("unexpected error: %v (cause: %v)", err, cause)
	}
}

func TestOapiYamlRejectsMistypedServersAndTags(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"servers as mapping", "servers:\n  url: https://example.com\n"},
		{"tags as mapping", "tags:\n  name: test\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			input := filepath.Join(tmpDir, "api.yaml")
			output := filepath.Join(tmpDir, "out.yaml")

			writeFile(t, input, `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
`+tt.yaml+`
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
`)
			err := OapiYaml(input, output)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			cause := errors.Unwrap(err)
			if cause == nil || !strings.Contains(cause.Error(), "sequence") {
				t.Errorf("unexpected error: %v (cause: %v)", err, cause)
			}
		})
	}
}

func TestOapiYamlRejectsMistypedComponentsPathsWebhooks(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"components as array", "components:\n  - oops\n"},
		{"paths as array", "paths:\n  - oops\n"},
		{"webhooks as array", "webhooks:\n  - oops\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			input := filepath.Join(tmpDir, "api.yaml")
			output := filepath.Join(tmpDir, "out.yaml")

			body := `
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
` + tt.yaml
			if tt.name != "paths as array" {
				body += `
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
`
			}

			writeFile(t, input, body)
			err := OapiYaml(input, output)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			cause := errors.Unwrap(err)
			if cause == nil || !strings.Contains(cause.Error(), "mapping") {
				t.Errorf("unexpected error: %v (cause: %v)", err, cause)
			}
		})
	}
}

func TestOapiYamlPreservesLiteralExternalRefExample(t *testing.T) {
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
          content:
            application/json:
              example:
                $ref: 'not-a-file.yaml#/data'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("literal example must not be resolved: %v", err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json", "example", "$ref"); got != "not-a-file.yaml#/data" {
		t.Fatalf("literal example changed: %v", got)
	}
}

func TestOapiYamlMergesUnlistedComponentCategories(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	extra := filepath.Join(tmpDir, "extra.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, extra, `
components:
  pathItems:
    Reusable:
      get:
        responses:
          "200":
            description: OK
  widgets:
    Gadget:
      type: object
`)
	writeFile(t, input, `
openapi: 3.1.0
info:
  title: Test
  version: "1.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: './extra.yaml#/components/widgets/Gadget'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	_, components := docPathsAndComponents(t, doc)

	if got := mapValueAt(t, components, "widgets", "Gadget", "type"); got != "object" {
		t.Errorf("widgets.Gadget.type = %v, want object", got)
	}
	if got := mapValueAt(t, components, "pathItems", "Reusable", "get", "responses", "200", "description"); got != "OK" {
		t.Errorf("pathItems.Reusable... description = %v, want OK", got)
	}
}

func TestOapiYamlImportsReferenceOutsideComponents(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	definitions := filepath.Join(tmpDir, "definitions.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, definitions, `
Pet:
  type: object
  properties:
    name:
      type: string
`)
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
          content:
            application/json:
              schema:
                $ref: './definitions.yaml#/Pet'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("could not import Pet: %v", err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json", "schema", "properties", "name", "type"); got != "string" {
		t.Fatalf("Pet was not inlined: %v", got)
	}
}

func TestOapiYamlPreservesLiteralLocalRefExample(t *testing.T) {
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
          content:
            application/json:
              example:
                $ref: '#/components/schemas/DoesNotExist'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("literal local reference must not be validated: %v", err)
	}
	doc := unmarshalDoc(t, output)
	if got := mapValueAt(t, doc, "paths", "/test", "get", "responses", "200", "content", "application/json", "example", "$ref"); got != "#/components/schemas/DoesNotExist" {
		t.Fatalf("literal example changed: %v", got)
	}
}

func TestOapiYamlResolvesRefUnderPropertyNamedExample(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	schemas := filepath.Join(tmpDir, "schemas.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, schemas, `
components:
  schemas:
    Snippet:
      type: object
`)
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
          content:
            application/json:
              schema:
                type: object
                properties:
                  example:
                    $ref: './schemas.yaml#/components/schemas/Snippet'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths, components := docPathsAndComponents(t, doc)

	schemaRef := mapValueAt(t, paths, "/test", "get", "responses", "200", "content", "application/json", "schema", "properties", "example", "$ref")
	if schemaRef != "#/components/schemas/Snippet" {
		t.Errorf("properties.example.$ref = %v, want #/components/schemas/Snippet", schemaRef)
	}
	if got := mapValueAt(t, components, "schemas", "Snippet", "type"); got != "object" {
		t.Errorf("Snippet.type = %v, want object", got)
	}
}

func TestOapiYamlResolvesRefUnderComponentsExampleNamedExample(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "api.yaml")
	shared := filepath.Join(tmpDir, "shared.yaml")
	output := filepath.Join(tmpDir, "out.yaml")

	writeFile(t, shared, `
components:
  examples:
    RealExample:
      value:
        name: sample
`)
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
          content:
            application/json:
              examples:
                example:
                  $ref: './shared.yaml#/components/examples/RealExample'
`)
	if err := OapiYaml(input, output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := unmarshalDoc(t, output)
	paths, components := docPathsAndComponents(t, doc)

	ref := mapValueAt(t, paths, "/test", "get", "responses", "200", "content", "application/json", "examples", "example", "$ref")
	if ref != "#/components/examples/RealExample" {
		t.Errorf("examples.example.$ref = %v, want #/components/examples/RealExample", ref)
	}
	if got := mapValueAt(t, components, "examples", "RealExample", "value", "name"); got != "sample" {
		t.Errorf("RealExample.value.name = %v, want sample", got)
	}
}
