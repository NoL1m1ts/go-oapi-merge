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
	Servers    []interface{}          `yaml:"servers,omitempty"`
	Paths      map[string]interface{} `yaml:"paths"`
	Components map[string]interface{} `yaml:"components,omitempty"`
	Security   []interface{}          `yaml:"security,omitempty"`
	Tags       []interface{}          `yaml:"tags,omitempty"`
}

var (
	globalComponents = make(map[string]interface{})
	globalResponses  = make(map[string]interface{})
)

func OapiYaml(inputFile, outputFile string) error {
	mainAPI, err := readApiYAML(inputFile)
	if err != nil {
		return err
	}

	if mainAPI.Components == nil {
		mainAPI.Components = make(map[string]interface{})
	}
	if mainAPI.Components["schemas"] == nil {
		mainAPI.Components["schemas"] = make(map[string]interface{})
	}

	urlsToParse := make(map[string]bool)
	if err := processPaths(mainAPI.Paths, urlsToParse, inputFile); err != nil {
		return err
	}

	if err := processNestedFiles(urlsToParse, mainAPI); err != nil {
		return err
	}

	outputData, err := yaml.Marshal(mainAPI)
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, outputData, 0644)
}

func processPaths(paths map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	for pathKey, path := range paths {
		pathMap, ok := path.(map[string]interface{})
		if !ok {
			continue
		}

		if ref, ok := pathMap["$ref"].(string); ok {
			refPath, err := resolveRef(ref, currentFilePath)
			if err != nil {
				return err
			}

			if refPath == "" {
				continue
			}

			nested, err := readYAML(refPath)
			if err != nil {
				return err
			}

			_, after, _ := strings.Cut(ref, "#/")
			if nestedAPI, ok := nested[after].(map[string]interface{}); ok {
				paths[pathKey] = nestedAPI
			}

			urlsToParse[refPath] = true

			if err := findRef(nested[after].(map[string]interface{}), urlsToParse, refPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func processNestedFiles(urlsToParse map[string]bool, mainAPI *OpenAPI) error {
	for url := range urlsToParse {
		nested, err := readYAML(url)
		if err != nil {
			return err
		}

		if nestedComponents, ok := nested["components"].(map[string]interface{}); ok {
			mergeComponents(nestedComponents, mainAPI)
		}
	}
	return nil
}

func mergeComponents(nestedComponents map[string]interface{}, mainAPI *OpenAPI) {
	if nestedSchemas, ok := nestedComponents["schemas"].(map[string]interface{}); ok {
		for key, value := range nestedSchemas {
			if _, exists := globalComponents[key]; !exists {
				globalComponents[key] = value
				mainAPI.Components["schemas"].(map[string]interface{})[key] = value
			}
		}
	}

	if nestedResponses, ok := nestedComponents["responses"].(map[string]interface{}); ok {
		if mainAPI.Components["responses"] == nil {
			mainAPI.Components["responses"] = make(map[string]interface{})
		}
		for key, value := range nestedResponses {
			if _, exists := globalResponses[key]; !exists {
				globalResponses[key] = value
				mainAPI.Components["responses"].(map[string]interface{})[key] = value
			}
		}
	}
}

func findRef(api map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	for _, value := range api {
		if val, ok := value.(map[string]interface{}); ok {
			if ref, ok := val["$ref"].(string); ok {
				s, err := resolveRef(ref, currentFilePath)
				if err != nil {
					return err
				}
				if s != "" {
					urlsToParse[s] = true
					val["$ref"] = "#/" + strings.Split(ref, "#/")[1]
				}
			} else {
				if err := findRef(val, urlsToParse, currentFilePath); err != nil {
					return err
				}
			}
		} else if arr, ok := value.([]interface{}); ok {
			for _, item := range arr {
				if v, ok := item.(map[string]interface{}); ok {
					if err := findRef(v, urlsToParse, currentFilePath); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func readApiYAML(filePath string) (*OpenAPI, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var api OpenAPI
	if err := yaml.Unmarshal(data, &api); err != nil {
		return nil, err
	}

	return &api, nil
}

func readYAML(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var api interface{}
	if err := yaml.Unmarshal(data, &api); err != nil {
		return nil, err
	}

	apiMap, ok := api.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("yaml должен быть объектом")
	}

	return apiMap, nil
}

func resolveRef(ref, currentFilePath string) (string, error) {
	if strings.HasPrefix(ref, "#") {
		return "", nil
	}

	refParts := strings.SplitN(ref, "#", 2)
	relativePath := refParts[0]
	basePath := filepath.Dir(currentFilePath)
	absolutePath := filepath.Join(basePath, relativePath)

	return filepath.Clean(absolutePath), nil
}
