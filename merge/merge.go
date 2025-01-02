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

type RefString string

func (r RefString) MarshalYAML() (interface{}, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: string(r),
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
	}, nil
}

const (
	DefaultInputFile  = "api.yaml"
	DefaultOutputFile = "merged_api.yaml"
)

func OapiYaml(inputFile, outputFile string) error {
	if inputFile == "" {
		inputFile = DefaultInputFile
	}
	if outputFile == "" {
		outputFile = DefaultOutputFile
	}

	mainContent, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", inputFile, err)
	}

	var mainAPI OpenAPI
	if err := yaml.Unmarshal(mainContent, &mainAPI); err != nil {
		return fmt.Errorf("failed to parse YAML file %s: %v", inputFile, err)
	}

	if err := validateOpenAPISpec(&mainAPI); err != nil {
		return fmt.Errorf("invalid OpenAPI specification in %s: %v", inputFile, err)
	}

	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %v", outputDir, err)
	}

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

	ensureRefQuotes(&mainAPI)

	mergedYAML, err := yaml.Marshal(&mainAPI)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %v", err)
	}

	if err := os.WriteFile(outputFile, mergedYAML, 0644); err != nil {
		return fmt.Errorf("failed to write to file %s: %v", outputFile, err)
	}

	return nil
}

func ensureRefQuotes(api *OpenAPI) {
	if api.Paths != nil {
		for path, pathItem := range api.Paths {
			if pathItemMap, ok := pathItem.(map[string]interface{}); ok {
				api.Paths[path] = ensureRefQuotesInValue(pathItemMap)
			}
		}
	}

	if api.Components != nil {
		api.Components = ensureRefQuotesInValue(api.Components).(map[string]interface{})
	}
}

func ensureRefQuotesInValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if key == "$ref" {
				if ref, ok := val.(string); ok {
					v[key] = RefString(ref)
				}
			} else {
				v[key] = ensureRefQuotesInValue(val)
			}
		}
		return v
	case []interface{}:
		for i, item := range v {
			v[i] = ensureRefQuotesInValue(item)
		}
		return v
	default:
		return v
	}
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
	if api.OpenAPI == "" {
		return fmt.Errorf("missing OpenAPI version")
	}
	if !strings.HasPrefix(api.OpenAPI, "3.") {
		return fmt.Errorf("unsupported OpenAPI version: %s (only 3.x versions are supported)", api.OpenAPI)
	}

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
