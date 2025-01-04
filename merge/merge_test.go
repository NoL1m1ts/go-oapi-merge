package merge

import (
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

	input := OpenAPI{
		OpenAPI: "3.0.0",
		Info: map[string]interface{}{
			"title":   "Test API",
			"version": "1.0.0",
		},
		Paths: map[string]interface{}{
			"/test": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Test endpoint",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "OK",
						},
					},
				},
			},
		},
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
	assert.Equal(t, input.Info, output.Info)
	assert.Equal(t, input.Paths, output.Paths)
}

func TestProcessPaths(t *testing.T) {
	paths := map[string]interface{}{
		"/users": map[string]interface{}{
			"get": map[string]interface{}{
				"summary": "Get users",
			},
		},
	}

	urlsToParse := make(map[string]bool)
	err := processPaths(paths, urlsToParse, "test.yaml")
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"/users": map[string]interface{}{
			"get": map[string]interface{}{
				"summary": "Get users",
			},
		},
	}, paths)
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

	testAPI := OpenAPI{
		OpenAPI: "3.0.0",
		Info: map[string]interface{}{
			"title":   "Test API",
			"version": "1.0.0",
		},
		Paths: map[string]interface{}{},
	}

	data, err := yaml.Marshal(testAPI)
	assert.NoError(t, err)
	err = os.WriteFile(testFile, data, 0644)
	assert.NoError(t, err)

	result, err := readApiYAML(testFile)
	assert.NoError(t, err)
	assert.Equal(t, testAPI.OpenAPI, result.OpenAPI)
	assert.Equal(t, testAPI.Info, result.Info)
	assert.Equal(t, testAPI.Paths, result.Paths)

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

	mainAPI := &OpenAPI{
		Components: map[string]interface{}{
			"schemas": map[string]interface{}{},
		},
	}

	urlsToParse := map[string]bool{
		nestedFile: true,
	}

	err = processNestedFiles(urlsToParse, mainAPI)
	assert.NoError(t, err)

	schemas := mainAPI.Components["schemas"].(map[string]interface{})
	assert.Contains(t, schemas, "User")

	responses := mainAPI.Components["responses"].(map[string]interface{})
	assert.Contains(t, responses, "NotFound")

	examples := mainAPI.Components["examples"].(map[string]interface{})
	assert.Contains(t, examples, "UserExample")
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

	mainAPI := &OpenAPI{
		Components: map[string]interface{}{
			"schemas": map[string]interface{}{},
		},
	}

	err := mergeComponents(nestedComponents, mainAPI)
	assert.NoError(t, err)

	schemas := mainAPI.Components["schemas"].(map[string]interface{})
	assert.Contains(t, schemas, "Pet")

	responses := mainAPI.Components["responses"].(map[string]interface{})
	assert.Contains(t, responses, "Error")

	examples := mainAPI.Components["examples"].(map[string]interface{})
	assert.Contains(t, examples, "PetExample")
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

func TestProcessPathsWithErrors(t *testing.T) {
	tests := []struct {
		name          string
		paths         map[string]interface{}
		urlsToParse   map[string]bool
		currentPath   string
		expectedError string
	}{
		{
			name: "invalid path type",
			paths: map[string]interface{}{
				"/users": "invalid",
			},
			urlsToParse:   make(map[string]bool),
			currentPath:   "test.yaml",
			expectedError: "",
		},
		{
			name: "invalid ref path",
			paths: map[string]interface{}{
				"/users": map[string]interface{}{
					"$ref": "nonexistent.yaml#/paths/~1users",
				},
			},
			urlsToParse:   make(map[string]bool),
			currentPath:   "test.yaml",
			expectedError: "failed to read YAML file",
		},
		{
			name: "invalid ref format",
			paths: map[string]interface{}{
				"/users": map[string]interface{}{
					"$ref": "invalid_ref",
				},
			},
			urlsToParse:   make(map[string]bool),
			currentPath:   "test.yaml",
			expectedError: "failed to read YAML file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processPaths(tt.paths, tt.urlsToParse, tt.currentPath)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMergeComponentsWithDuplicates(t *testing.T) {
	// Подготовка начального состояния globalComponents
	globalComponents = make(map[string]interface{})
	globalResponses = make(map[string]interface{})
	globalExamples = make(map[string]interface{})

	// Создаем компоненты с дубликатами
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

	mainAPI := &OpenAPI{
		Components: map[string]interface{}{
			"schemas": map[string]interface{}{
				"User": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}

	// Первый вызов должен добавить компоненты
	err := mergeComponents(nestedComponents, mainAPI)
	assert.NoError(t, err)

	// Проверяем, что компоненты были добавлены правильно
	schemas := mainAPI.Components["schemas"].(map[string]interface{})
	assert.Contains(t, schemas, "User")

	// Проверяем, что новые компоненты были добавлены
	responses := mainAPI.Components["responses"].(map[string]interface{})
	assert.Contains(t, responses, "Error")

	examples := mainAPI.Components["examples"].(map[string]interface{})
	assert.Contains(t, examples, "UserExample")
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
