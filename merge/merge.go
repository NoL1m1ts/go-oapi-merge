package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type OpenAPI struct {
	OpenAPI    string                 `yaml:"openapi"`
	Info       map[string]interface{} `yaml:"info"`
	Components map[string]interface{} `yaml:"components,omitempty"`
	Paths      map[string]interface{} `yaml:"paths,omitempty"`
}

// DefaultInputFile is the default input file name
const DefaultInputFile = "api.yaml"

// DefaultOutputFile is the default output file name
const DefaultOutputFile = "merged_api.yaml"

// OapiYaml merges OpenAPI YAML files.
func OapiYaml(inputFile, outputFile string) error {
	// Set default values if empty
	if inputFile == "" {
		inputFile = DefaultInputFile
	}
	if outputFile == "" {
		outputFile = DefaultOutputFile
	}

	// Check if input file exists and is accessible
	mainContent, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", inputFile, err)
	}

	// Validate YAML format
	var mainAPI OpenAPI
	if err := yaml.Unmarshal(mainContent, &mainAPI); err != nil {
		return fmt.Errorf("failed to parse YAML file %s: %v", inputFile, err)
	}

	// Validate OpenAPI specification format
	if err := validateOpenAPISpec(&mainAPI); err != nil {
		return fmt.Errorf("invalid OpenAPI specification in %s: %v", inputFile, err)
	}

	// Check if output directory exists and is writable
	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %v", outputDir, err)
	}

	// Try to create a temporary file to check if directory is writable
	tmpFile := filepath.Join(outputDir, ".tmp_write_test")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		return fmt.Errorf("output directory %s is not writable: %v", outputDir, err)
	}
	os.Remove(tmpFile)

	baseDir := filepath.Dir(inputFile)

	if mainAPI.Paths != nil {
		for path, ref := range mainAPI.Paths {
			refMap, ok := ref.(map[string]interface{})
			if !ok {
				continue
			}

			refValue, ok := refMap["$ref"].(string)
			if !ok {
				continue
			}

			filePath, refPath, err := ParseRef(refValue)
			if err != nil {
				return fmt.Errorf("failed to parse reference %s: %v", refValue, err)
			}

			absFilePath := filepath.Join(baseDir, filePath)
			if _, err := os.Stat(absFilePath); os.IsNotExist(err) {
				return fmt.Errorf("file %s does not exist", absFilePath)
			}

			componentContent, err := os.ReadFile(absFilePath)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %v", absFilePath, err)
			}

			var component map[string]interface{}
			if err := yaml.Unmarshal(componentContent, &component); err != nil {
				return fmt.Errorf("failed to parse YAML file %s: %v", absFilePath, err)
			}

			pathValue, err := ResolveRefPath(component, refPath)
			if err != nil {
				return fmt.Errorf("failed to resolve path %s in file %s: %v", refPath, absFilePath, err)
			}

			pathValue = ProcessRefs(pathValue, baseDir, filePath)

			if mainAPI.Paths == nil {
				mainAPI.Paths = make(map[string]interface{})
			}
			mainAPI.Paths[path] = pathValue
		}
	}

	mergedYAML, err := yaml.Marshal(&mainAPI)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %v", err)
	}

	if err := os.WriteFile(outputFile, mergedYAML, 0644); err != nil {
		return fmt.Errorf("failed to write to file %s: %v", outputFile, err)
	}

	return nil
}

func ParseRef(ref string) (filePath string, refPath string, err error) {
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid reference format: %s", ref)
	}
	return parts[0], parts[1], nil
}

func ResolveRefPath(data map[string]interface{}, refPath string) (interface{}, error) {
	parts := strings.Split(refPath, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid path format: %s", refPath)
	}

	current := data
	for _, part := range parts[1:] {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")

		value, ok := current[part]
		if !ok {
			return nil, fmt.Errorf("path %s not found", part)
		}

		if next, ok := value.(map[string]interface{}); ok {
			current = next
		} else {
			return value, nil
		}
	}

	return current, nil
}

func ProcessRefs(data interface{}, baseDir string, currentFilePath string) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					v[key] = NormalizeRef(ref, baseDir, currentFilePath)
				}
			} else {
				v[key] = ProcessRefs(value, baseDir, currentFilePath)
			}
		}
		return v
	case []interface{}:
		for i, item := range v {
			v[i] = ProcessRefs(item, baseDir, currentFilePath)
		}
		return v
	default:
		return data
	}
}

func NormalizeRef(ref string, baseDir string, currentFilePath string) string {
	if strings.HasPrefix(ref, "#/") {
		return fmt.Sprintf("%s%s", currentFilePath, ref)
	}

	filePath, refPath, err := ParseRef(ref)
	if err != nil {
		return ref
	}

	absFilePath := filepath.Join(baseDir, filePath)
	relFilePath, err := filepath.Rel(baseDir, absFilePath)
	if err != nil {
		return ref
	}

	relFilePath = filepath.ToSlash(relFilePath)
	relFilePath = strings.TrimPrefix(relFilePath, "../")

	return fmt.Sprintf("./%s#%s", relFilePath, refPath)
}

func validateOpenAPISpec(api *OpenAPI) error {
	// Check OpenAPI version
	if api.OpenAPI == "" {
		return fmt.Errorf("missing OpenAPI version")
	}
	if !strings.HasPrefix(api.OpenAPI, "3.") {
		return fmt.Errorf("unsupported OpenAPI version: %s (only 3.x versions are supported)", api.OpenAPI)
	}

	// Check required Info fields
	if api.Info == nil {
		return fmt.Errorf("missing required 'info' field")
	}
	if _, ok := api.Info["title"].(string); !ok {
		return fmt.Errorf("missing required 'info.title' field")
	}
	if _, ok := api.Info["version"].(string); !ok {
		return fmt.Errorf("missing required 'info.version' field")
	}

	return nil
}
