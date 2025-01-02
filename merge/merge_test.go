package merge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestOapiYaml(t *testing.T) {
	// Create a temporary directory for the tests
	tmpDir := t.TempDir()

	// Create a subdirectory 'paths'
	pathsDir := filepath.Join(tmpDir, "paths")
	err := os.Mkdir(pathsDir, 0755)
	assert.NoError(t, err)

	// Create a subdirectory 'components'
	componentsDir := filepath.Join(tmpDir, "components")
	err = os.Mkdir(componentsDir, 0755)
	assert.NoError(t, err)

	// Create test files
	apiYAML := `
openapi: 3.0.0
info:
  title: Sample API
  version: 1.0.0
paths:
  /users:
    $ref: './paths/users.yaml#/~1users'
`
	usersYAML := `
/users:
  get:
    description: Get all users
    responses:
      "200":
        $ref: '#/components/schemas/User'
`
	schemasYAML := `
components:
  schemas:
    User:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
`

	// Write test files
	writeFile(t, tmpDir, "api.yaml", apiYAML)
	writeFile(t, pathsDir, "users.yaml", usersYAML)          // Write to 'paths' subdirectory
	writeFile(t, componentsDir, "schemas.yaml", schemasYAML) // Write to 'components' subdirectory

	// Run OapiYaml function
	inputFile := filepath.Join(tmpDir, "api.yaml")
	outputFile := filepath.Join(tmpDir, "merged_api.yaml")
	err = OapiYaml(inputFile, outputFile)
	assert.NoError(t, err)

	// Read the result
	mergedContent, err := os.ReadFile(outputFile)
	assert.NoError(t, err)

	// Verify the result
	var mergedAPI OpenAPI
	err = yaml.Unmarshal(mergedContent, &mergedAPI)
	assert.NoError(t, err)

	assert.NotNil(t, mergedAPI.Paths)
	assert.Contains(t, mergedAPI.Paths, "/users")
}

func TestParseRef(t *testing.T) {
	filePath, refPath, err := ParseRef("./file.yaml#/path/to/resource")
	assert.NoError(t, err)
	assert.Equal(t, "./file.yaml", filePath)
	assert.Equal(t, "/path/to/resource", refPath)

	_, _, err = ParseRef("invalid-ref")
	assert.Error(t, err)
}

func TestResolveRefPath(t *testing.T) {
	data := map[string]interface{}{
		"path": map[string]interface{}{
			"to": map[string]interface{}{
				"resource": "value",
			},
		},
	}

	value, err := ResolveRefPath(data, "/path/to/resource")
	assert.NoError(t, err)
	assert.Equal(t, "value", value)

	_, err = ResolveRefPath(data, "/invalid/path")
	assert.Error(t, err)
}

func TestProcessRefs(t *testing.T) {
	baseDir := "/base/dir"
	currentFilePath := "./file.yaml"

	data := map[string]interface{}{
		"$ref": "#/components/schemas/User",
	}

	processed := ProcessRefs(data, baseDir, currentFilePath)
	processedMap, ok := processed.(map[string]interface{})
	assert.True(t, ok, "Processed result should be a map")
	assert.Equal(t, "./file.yaml#/components/schemas/User", processedMap["$ref"])
}

func TestNormalizeRef(t *testing.T) {
	baseDir := "/base/dir"
	currentFilePath := "./file.yaml"

	// Reference with "#/"
	ref := "#/components/schemas/User"
	normalized := NormalizeRef(ref, baseDir, currentFilePath)
	assert.Equal(t, "./file.yaml#/components/schemas/User", normalized)

	// Relative reference
	ref = "../components/responses.yaml#/responses/200"
	normalized = NormalizeRef(ref, baseDir, currentFilePath)
	assert.Equal(t, "./components/responses.yaml#/responses/200", normalized)
}

func writeFile(t *testing.T, dir, name, content string) {
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	assert.NoError(t, err)
}
