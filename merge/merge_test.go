package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestOapiYaml(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.yaml")
	outputFile := filepath.Join(tmpDir, "output.yaml")

	info := NewOrderedMap()
	info.Set("title", "Test API")
	info.Set("version", "1.0.0")

	responses := NewOrderedMap()
	resp200 := NewOrderedMap()
	resp200.Set("description", "OK")
	responses.Set("200", resp200)

	getOp := NewOrderedMap()
	getOp.Set("summary", "Test endpoint")
	getOp.Set("responses", responses)

	testPath := NewOrderedMap()
	testPath.Set("get", getOp)

	paths := NewOrderedMap()
	paths.Set("/test", testPath)

	input := OpenAPI{
		OpenAPI: "3.0.0",
		Info:    info,
		Paths:   paths,
	}

	inputData, err := yaml.Marshal(input)
	assert.NoError(t, err)
	err = os.WriteFile(inputFile, inputData, 0644)
	assert.NoError(t, err)

	err = OapiYaml(inputFile, outputFile)
	assert.NoError(t, err)

	_, err = os.Stat(outputFile)
	assert.NoError(t, err)

	var output OpenAPI
	outputData, err := os.ReadFile(outputFile)
	assert.NoError(t, err)
	err = yaml.Unmarshal(outputData, &output)
	assert.NoError(t, err)

	assert.Equal(t, input.OpenAPI, output.OpenAPI)
}

func TestOapiYamlWithNilComponents(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.yaml")
	outputFile := filepath.Join(tmpDir, "output.yaml")

	info := NewOrderedMap()
	info.Set("title", "Test API")
	info.Set("version", "1.0.0")

	getOp := NewOrderedMap()
	getOp.Set("summary", "Test endpoint")

	testPath := NewOrderedMap()
	testPath.Set("get", getOp)

	paths := NewOrderedMap()
	paths.Set("/test", testPath)

	input := OpenAPI{
		OpenAPI: "3.0.0",
		Info:    info,
		Paths:   paths,
	}

	inputData, err := yaml.Marshal(input)
	assert.NoError(t, err)
	err = os.WriteFile(inputFile, inputData, 0644)
	assert.NoError(t, err)

	err = OapiYaml(inputFile, outputFile)
	assert.NoError(t, err)

	var output OpenAPI
	outputData, err := os.ReadFile(outputFile)
	assert.NoError(t, err)
	err = yaml.Unmarshal(outputData, &output)
	assert.NoError(t, err)

	assert.NotNil(t, output.Components)
}

func TestOapiYamlWithNilSchemas(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.yaml")
	outputFile := filepath.Join(tmpDir, "output.yaml")

	info := NewOrderedMap()
	info.Set("title", "Test API")
	info.Set("version", "1.0.0")

	input := OpenAPI{
		OpenAPI:    "3.0.0",
		Info:       info,
		Paths:      NewOrderedMap(),
		Components: NewOrderedMap(),
	}

	inputData, err := yaml.Marshal(input)
	assert.NoError(t, err)
	err = os.WriteFile(inputFile, inputData, 0644)
	assert.NoError(t, err)

	err = OapiYaml(inputFile, outputFile)
	assert.NoError(t, err)

	var output OpenAPI
	outputData, err := os.ReadFile(outputFile)
	assert.NoError(t, err)
	err = yaml.Unmarshal(outputData, &output)
	assert.NoError(t, err)

	assert.NotNil(t, output.Components)
}

func TestOapiYamlErrors(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("invalid input file", func(t *testing.T) {
		err := OapiYaml("nonexistent.yaml", "output.yaml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to read input file")
	})

	t.Run("invalid output path", func(t *testing.T) {
		inputFile := filepath.Join(tmpDir, "input.yaml")
		info := NewOrderedMap()
		info.Set("title", "Test API")
		info.Set("version", "1.0.0")

		input := OpenAPI{
			OpenAPI: "3.0.0",
			Info:    info,
			Paths:   NewOrderedMap(),
		}
		inputData, err := yaml.Marshal(input)
		assert.NoError(t, err)
		err = os.WriteFile(inputFile, inputData, 0644)
		assert.NoError(t, err)

		invalidPath := filepath.Join(tmpDir, "nonexistent", "output.yaml")
		err = OapiYaml(inputFile, invalidPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create output file")
	})
}

func TestDecodeRef(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with encoded slash",
			input:    "path/to~1resource",
			expected: "path/to/resource",
		},
		{
			name:     "with encoded tilde",
			input:    "path~0resource",
			expected: "path~resource",
		},
		{
			name:     "with both encodings",
			input:    "path~0to~1resource",
			expected: "path~to/resource",
		},
		{
			name:     "without encoding",
			input:    "path/to/resource",
			expected: "path/to/resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeRef(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveRef(t *testing.T) {
	tests := []struct {
		name          string
		ref           string
		currentPath   string
		expectedPath  string
		expectedError bool
	}{
		{
			name:          "local reference",
			ref:           "#/components/schemas/User",
			currentPath:   "api.yaml",
			expectedPath:  "",
			expectedError: false,
		},
		{
			name:          "relative path reference",
			ref:           "./schemas/user.yaml#/User",
			currentPath:   "api.yaml",
			expectedPath:  "schemas/user.yaml",
			expectedError: false,
		},
		{
			name:          "invalid reference format",
			ref:           "invalid_reference",
			currentPath:   "api.yaml",
			expectedPath:  "invalid_reference",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := resolveRef(tt.ref, tt.currentPath)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedPath != "" {
					assert.Contains(t, path, tt.expectedPath)
				} else {
					assert.Empty(t, path)
				}
			}
		})
	}
}

func TestReadApiYAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yaml")

	info := NewOrderedMap()
	info.Set("title", "Test API")
	info.Set("version", "1.0.0")

	testAPI := OpenAPI{
		OpenAPI: "3.0.0",
		Info:    info,
		Paths:   NewOrderedMap(),
	}

	data, err := yaml.Marshal(testAPI)
	assert.NoError(t, err)
	err = os.WriteFile(testFile, data, 0644)
	assert.NoError(t, err)

	result, err := readApiYAML(testFile)
	assert.NoError(t, err)
	assert.Equal(t, testAPI.OpenAPI, result.OpenAPI)

	_, err = readApiYAML("nonexistent.yaml")
	assert.Error(t, err)
}

func TestReadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yaml")

	testData := map[string]interface{}{
		"key": "value",
		"nested": map[string]interface{}{
			"subkey": "subvalue",
		},
	}

	data, err := yaml.Marshal(testData)
	assert.NoError(t, err)
	err = os.WriteFile(testFile, data, 0644)
	assert.NoError(t, err)

	result, err := readYAML(testFile)
	assert.NoError(t, err)
	assert.Equal(t, testData, result)

	_, err = readYAML("nonexistent.yaml")
	assert.Error(t, err)
}

func TestReadYAMLErrors(t *testing.T) {
	tmpDir := t.TempDir()
	currentDir, err := os.Getwd()
	assert.NoError(t, err)
	defer func(dir string) {
		err := os.Chdir(dir)
		if err != nil {
			t.Error(err)
		}
	}(currentDir)

	err = os.Chdir(tmpDir)
	assert.NoError(t, err)

	// Test invalid YAML
	invalidYAML := `
openapi: 3.0.0
  invalid:
    - not valid yaml
      wrong indentation
`
	err = os.WriteFile("invalid.yaml", []byte(invalidYAML), 0644)
	assert.NoError(t, err)

	// Test non-object YAML
	arrayYAML := `
- item1
- item2
`
	err = os.WriteFile("array.yaml", []byte(arrayYAML), 0644)
	assert.NoError(t, err)

	tests := []struct {
		name          string
		filePath      string
		expectedError string
	}{
		{
			name:          "non-existent file",
			filePath:      "nonexistent.yaml",
			expectedError: "failed to read file",
		},
		{
			name:          "invalid YAML",
			filePath:      "invalid.yaml",
			expectedError: "failed to parse YAML",
		},
		{
			name:          "non-object YAML",
			filePath:      "array.yaml",
			expectedError: "YAML is not an object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readYAML(tt.filePath)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestProcessNestedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	nestedFile := filepath.Join(tmpDir, "nested.yaml")

	nestedContent := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"User": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":   map[string]interface{}{"type": "string"},
						"name": map[string]interface{}{"type": "string"},
					},
				},
			},
			"responses": map[string]interface{}{
				"NotFound": map[string]interface{}{
					"description": "Not Found",
				},
			},
			"examples": map[string]interface{}{
				"UserExample": map[string]interface{}{
					"value": map[string]interface{}{
						"id":   "123",
						"name": "John Doe",
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(nestedContent)
	assert.NoError(t, err)
	err = os.WriteFile(nestedFile, data, 0644)
	assert.NoError(t, err)

	schemas := NewOrderedMap()
	components := NewOrderedMap()
	components.Set("schemas", schemas)

	mainAPI := &OpenAPI{
		Components: components,
	}

	urlsToParse := map[string]bool{
		nestedFile: true,
	}

	err = processNestedFiles(urlsToParse, mainAPI)
	assert.NoError(t, err)

	schemasVal, _ := mainAPI.Components.Get("schemas")
	schemasOM := schemasVal.(*OrderedMap)
	_, hasUser := schemasOM.Get("User")
	assert.True(t, hasUser)

	responsesVal, _ := mainAPI.Components.Get("responses")
	responsesOM := responsesVal.(*OrderedMap)
	_, hasNotFound := responsesOM.Get("NotFound")
	assert.True(t, hasNotFound)

	examplesVal, _ := mainAPI.Components.Get("examples")
	examplesOM := examplesVal.(*OrderedMap)
	_, hasUserExample := examplesOM.Get("UserExample")
	assert.True(t, hasUserExample)
}

func TestMergeComponents(t *testing.T) {
	nestedComponents := map[string]interface{}{
		"schemas": map[string]interface{}{
			"Pet": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		"responses": map[string]interface{}{
			"Error": map[string]interface{}{
				"description": "Error response",
			},
		},
		"examples": map[string]interface{}{
			"PetExample": map[string]interface{}{
				"value": map[string]interface{}{
					"name": "Fluffy",
				},
			},
		},
	}

	schemas := NewOrderedMap()
	components := NewOrderedMap()
	components.Set("schemas", schemas)

	mainAPI := &OpenAPI{
		Components: components,
	}

	err := mergeComponents(nestedComponents, mainAPI)
	assert.NoError(t, err)

	schemasVal, _ := mainAPI.Components.Get("schemas")
	schemasOM := schemasVal.(*OrderedMap)
	_, hasPet := schemasOM.Get("Pet")
	assert.True(t, hasPet)

	responsesVal, _ := mainAPI.Components.Get("responses")
	responsesOM := responsesVal.(*OrderedMap)
	_, hasError := responsesOM.Get("Error")
	assert.True(t, hasError)

	examplesVal, _ := mainAPI.Components.Get("examples")
	examplesOM := examplesVal.(*OrderedMap)
	_, hasPetExample := examplesOM.Get("PetExample")
	assert.True(t, hasPetExample)
}

func TestFindRef(t *testing.T) {
	api := map[string]interface{}{
		"schema": map[string]interface{}{
			"$ref": "./components.yaml#/components/schemas/User",
		},
		"responses": map[string]interface{}{
			"200": map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"$ref": "./components.yaml#/components/schemas/Error",
						},
					},
				},
			},
		},
	}

	urlsToParse := make(map[string]bool)
	err := findRef(api, urlsToParse, "api.yaml")
	assert.NoError(t, err)
	assert.Contains(t, urlsToParse, "components.yaml")
}

func TestCheckForRefs(t *testing.T) {
	tests := []struct {
		name    string
		data    interface{}
		wantErr bool
	}{
		{
			name: "valid nested refs",
			data: map[string]interface{}{
				"schema": map[string]interface{}{
					"$ref": "#/components/schemas/User",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid ref type",
			data: map[string]interface{}{
				"schema": map[string]interface{}{
					"$ref": 123,
				},
			},
			wantErr: false,
		},
		{
			name: "array with refs",
			data: []interface{}{
				map[string]interface{}{
					"$ref": "#/components/schemas/User",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkForRefs(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMergeComponentsWithDuplicates(t *testing.T) {
	nestedComponents := map[string]interface{}{
		"schemas": map[string]interface{}{
			"User": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		"responses": map[string]interface{}{
			"Error": map[string]interface{}{
				"description": "Error response",
			},
		},
		"examples": map[string]interface{}{
			"UserExample": map[string]interface{}{
				"value": map[string]interface{}{
					"name": "John",
				},
			},
		},
	}

	schemas := NewOrderedMap()
	schemas.Set("User", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"},
		},
	})
	components := NewOrderedMap()
	components.Set("schemas", schemas)

	mainAPI := &OpenAPI{
		Components: components,
	}

	err := mergeComponents(nestedComponents, mainAPI)
	assert.NoError(t, err)

	schemasVal, _ := mainAPI.Components.Get("schemas")
	schemasOM := schemasVal.(*OrderedMap)
	_, hasUser := schemasOM.Get("User")
	assert.True(t, hasUser)

	responsesVal, _ := mainAPI.Components.Get("responses")
	responsesOM := responsesVal.(*OrderedMap)
	_, hasError := responsesOM.Get("Error")
	assert.True(t, hasError)

	examplesVal, _ := mainAPI.Components.Get("examples")
	examplesOM := examplesVal.(*OrderedMap)
	_, hasUserExample := examplesOM.Get("UserExample")
	assert.True(t, hasUserExample)
}

func TestFindRefWithNestedStructures(t *testing.T) {
	api := map[string]interface{}{
		"paths": map[string]interface{}{
			"/users": map[string]interface{}{
				"get": map[string]interface{}{
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "./schemas/user.yaml#/User",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Error": map[string]interface{}{
					"$ref": "./schemas/error.yaml#/Error",
				},
			},
			"parameters": []interface{}{
				map[string]interface{}{
					"$ref": "./parameters/common.yaml#/Parameters/Limit",
				},
			},
		},
	}

	urlsToParse := make(map[string]bool)
	err := findRef(api, urlsToParse, "api.yaml")
	assert.NoError(t, err)

	// Проверяем, что все файлы со ссылками были добавлены
	assert.Contains(t, urlsToParse, "schemas/user.yaml")
	assert.Contains(t, urlsToParse, "schemas/error.yaml")
}

func TestResolveRefWithVariousPaths(t *testing.T) {
	tests := []struct {
		name          string
		ref           string
		currentPath   string
		expectedPath  string
		expectedError bool
	}{
		{
			name:          "absolute path",
			ref:           "/absolute/path/schema.yaml#/components/schemas/User",
			currentPath:   "api.yaml",
			expectedPath:  "absolute/path/schema.yaml",
			expectedError: false,
		},
		{
			name:          "relative path with parent directory",
			ref:           "../schemas/user.yaml#/User",
			currentPath:   "api/openapi.yaml",
			expectedPath:  "schemas/user.yaml",
			expectedError: false,
		},
		{
			name:          "local reference",
			ref:           "#/components/schemas/User",
			currentPath:   "api.yaml",
			expectedPath:  "",
			expectedError: false,
		},
		{
			name:          "invalid reference format",
			ref:           "invalid_reference",
			currentPath:   "api.yaml",
			expectedPath:  "invalid_reference",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := resolveRef(tt.ref, tt.currentPath)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedPath != "" {
					assert.Contains(t, path, tt.expectedPath)
				} else {
					assert.Empty(t, path)
				}
			}
		})
	}
}

func TestCheckForRefsWithComplexStructures(t *testing.T) {
	tests := []struct {
		name    string
		data    interface{}
		wantErr bool
	}{
		{
			name: "nested maps with refs",
			data: map[string]interface{}{
				"components": map[string]interface{}{
					"schemas": map[string]interface{}{
						"User": map[string]interface{}{
							"$ref": "#/definitions/User",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "array of objects with refs",
			data: []interface{}{
				map[string]interface{}{
					"$ref": "#/components/parameters/Limit",
				},
				map[string]interface{}{
					"$ref": "#/components/parameters/Offset",
				},
			},
			wantErr: false,
		},
		{
			name: "mixed types",
			data: map[string]interface{}{
				"string": "value",
				"number": 123,
				"bool":   true,
				"object": map[string]interface{}{
					"$ref": "#/components/schemas/Type",
				},
				"array": []interface{}{
					map[string]interface{}{
						"$ref": "#/components/schemas/Item",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkForRefs(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProcessNestedFilesErrors(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("nonexistent file", func(t *testing.T) {
		urlsToParse := map[string]bool{
			"nonexistent.yaml": true,
		}
		mainAPI := &OpenAPI{}
		err := processNestedFiles(urlsToParse, mainAPI)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read nested YAML file")
	})

	t.Run("invalid components type", func(t *testing.T) {
		inputFile := filepath.Join(tmpDir, "invalid.yaml")
		yamlContent := []byte(`
components: "invalid"
`)
		err := os.WriteFile(inputFile, yamlContent, 0644)
		assert.NoError(t, err)

		urlsToParse := map[string]bool{
			inputFile: true,
		}
		mainAPI := &OpenAPI{
			Components: NewOrderedMap(),
		}
		err = processNestedFiles(urlsToParse, mainAPI)
		assert.NoError(t, err) // Should not error as invalid components are skipped
	})

	t.Run("no components", func(t *testing.T) {
		inputFile := filepath.Join(tmpDir, "no_components.yaml")
		yamlContent := []byte(`
paths:
  /test:
    get:
      summary: Test endpoint
`)
		err := os.WriteFile(inputFile, yamlContent, 0644)
		assert.NoError(t, err)

		urlsToParse := map[string]bool{
			inputFile: true,
		}
		mainAPI := &OpenAPI{
			Components: NewOrderedMap(),
		}
		err = processNestedFiles(urlsToParse, mainAPI)
		assert.NoError(t, err)
	})
}

func TestCheckForRefsComplex(t *testing.T) {
	tests := []struct {
		name          string
		data          interface{}
		expectedError string
	}{
		{
			name: "array with nested refs",
			data: []interface{}{
				map[string]interface{}{
					"$ref": "test.yaml#/components/schemas/Test",
				},
				map[string]interface{}{
					"nested": map[string]interface{}{
						"$ref": "test.yaml#/components/schemas/Other",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "array with non-string ref",
			data: []interface{}{
				map[string]interface{}{
					"$ref": 123, // Invalid ref type
				},
			},
			expectedError: "",
		},
		{
			name: "map with non-string ref",
			data: map[string]interface{}{
				"$ref": []interface{}{}, // Invalid ref type
			},
			expectedError: "",
		},
		{
			name: "deeply nested non-string ref",
			data: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"$ref": true, // Invalid ref type
					},
				},
			},
			expectedError: "",
		},
		{
			name: "mixed valid and non-string refs",
			data: map[string]interface{}{
				"valid": map[string]interface{}{
					"$ref": "test.yaml#/components/schemas/Test",
				},
				"invalid": map[string]interface{}{
					"$ref": 42.0, // Invalid ref type
				},
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkForRefs(tt.data)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFindRefComplex(t *testing.T) {
	tests := []struct {
		name          string
		api           map[string]interface{}
		currentFile   string
		expectedError string
		expectedURLs  map[string]bool
	}{
		{
			name: "deeply nested refs",
			api: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"$ref": "test.yaml#/components/schemas/Test",
					},
					"array": []interface{}{
						map[string]interface{}{
							"$ref": "other.yaml#/components/schemas/Other",
						},
					},
				},
			},
			currentFile:   "main.yaml",
			expectedError: "",
			expectedURLs: map[string]bool{
				"test.yaml":  true,
				"other.yaml": true,
			},
		},
		{
			name: "invalid ref path",
			api: map[string]interface{}{
				"test": map[string]interface{}{
					"$ref": "test.yaml#invalid/path",
				},
			},
			currentFile:   "main.yaml",
			expectedError: "",
			expectedURLs: map[string]bool{
				"test.yaml": true,
			},
		},
		{
			name: "array with mixed content",
			api: map[string]interface{}{
				"items": []interface{}{
					"string item",
					42,
					map[string]interface{}{
						"$ref": "test.yaml#/components/schemas/Test",
					},
					[]interface{}{
						map[string]interface{}{
							"$ref": "other.yaml#/components/schemas/Other",
						},
					},
				},
			},
			currentFile:   "main.yaml",
			expectedError: "",
			expectedURLs: map[string]bool{
				"test.yaml":  true,
				"other.yaml": true,
			},
		},
		{
			name: "invalid ref resolution",
			api: map[string]interface{}{
				"test": map[string]interface{}{
					"$ref": "../../invalid.yaml#/components/schemas/Test",
				},
			},
			currentFile:   "main.yaml",
			expectedError: "",
			expectedURLs: map[string]bool{
				"../../invalid.yaml": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlsToParse := make(map[string]bool)
			err := findRef(tt.api, urlsToParse, tt.currentFile)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				// Check that all expected URLs are present
				for url := range tt.expectedURLs {
					assert.True(t, urlsToParse[url], "Expected URL %s not found in urlsToParse", url)
				}
				// Check that no unexpected URLs are present
				for url := range urlsToParse {
					assert.True(t, tt.expectedURLs[url], "Unexpected URL %s found in urlsToParse", url)
				}
			}
		})
	}
}

// readYAML reads a YAML file and parses it into a map.
// This function is used only in tests.
func readYAML(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	var api interface{}
	if err := yaml.Unmarshal(data, &api); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %v", err)
	}

	apiMap, ok := api.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("YAML is not an object")
	}

	return apiMap, nil
}

// readApiYAML reads an OpenAPI specification from a file and parses it into the OpenAPI structure.
// This function is used only in tests.
func readApiYAML(filePath string) (*OpenAPI, error) {
	// Read the YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	// Parse the YAML into the OpenAPI structure
	var api OpenAPI
	if err := yaml.Unmarshal(data, &api); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %v", err)
	}

	return &api, nil
}
