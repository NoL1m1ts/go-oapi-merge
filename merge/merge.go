package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

type MergeError struct {
	File    string
	Path    string
	Message string
	Cause   error
}

func (e *MergeError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.File != "" {
		b.WriteString(" (in ")
		b.WriteString(e.File)
		if e.Path != "" {
			b.WriteString(" at ")
			b.WriteString(e.Path)
		}
		b.WriteString(")")
	}
	return b.String()
}

func (e *MergeError) Unwrap() error {
	return e.Cause
}

type OpenAPI struct {
	OpenAPI    string        `yaml:"openapi"`
	Info       yaml.MapSlice `yaml:"info"`
	Servers    []interface{} `yaml:"servers,omitempty"`
	Paths      yaml.MapSlice `yaml:"paths"`
	Components yaml.MapSlice `yaml:"components,omitempty"`
	Security   []interface{} `yaml:"security,omitempty"`
	Tags       []interface{} `yaml:"tags,omitempty"`
}

func OapiYaml(inputFile, outputFile string) error {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return &MergeError{File: inputFile, Message: "Failed to read input file", Cause: err}
	}

	var mainAPI OpenAPI
	if err := yaml.UnmarshalWithOptions(data, &mainAPI, yaml.UseOrderedMap()); err != nil {
		return &MergeError{File: inputFile, Message: "Invalid OpenAPI YAML structure", Cause: err}
	}

	if mainAPI.OpenAPI == "" {
		return &MergeError{File: inputFile, Message: "Missing required field 'openapi'"}
	}
	if len(mainAPI.Info) == 0 {
		return &MergeError{File: inputFile, Message: "Missing required field 'info'"}
	}

	urlsToParse := make(map[string]bool)
	if err := processPaths(&mainAPI.Paths, urlsToParse, inputFile); err != nil {
		return err
	}

	if err := processNestedFiles(urlsToParse, &mainAPI); err != nil {
		return err
	}

	data, err = yaml.MarshalWithOptions(&mainAPI, yaml.Indent(2))
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	return os.WriteFile(outputFile, data, 0644)
}

func processPaths(paths *yaml.MapSlice, urlsToParse map[string]bool, currentFilePath string) error {
	for i := range *paths {
		pathKey := (*paths)[i].Key.(string)
		pathValue := (*paths)[i].Value

		pathMap, ok := pathValue.(yaml.MapSlice)
		if !ok {
			continue
		}

		refValue := getMapSliceValue(pathMap, "$ref")
		if refValue == nil {
			continue
		}

		refStr, ok := refValue.(string)
		if !ok || strings.HasPrefix(refStr, "#") {
			continue
		}

		parts := strings.SplitN(refStr, "#", 2)
		if len(parts) < 2 {
			return &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Invalid $ref format '%s': missing fragment", refStr)}
		}

		refPath := resolveRef(parts[0], currentFilePath)
		urlsToParse[refPath] = true

		fragment := parts[1]
		if !strings.HasPrefix(fragment, "/") {
			fragment = "/" + fragment
		}

		data, err := os.ReadFile(refPath)
		if err != nil {
			return &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Cannot read referenced file '%s'", refPath), Cause: err}
		}

		var nested yaml.MapSlice
		if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
			return &MergeError{File: refPath, Message: "Invalid YAML syntax", Cause: err}
		}

		current, err := navigateToFragment(nested, fragment, refPath)
		if err != nil {
			return err
		}

		resolvedPathItem, ok := current.(yaml.MapSlice)
		if !ok {
			return &MergeError{File: refPath, Path: fragment, Message: "Invalid reference target"}
		}

		findRefs(&resolvedPathItem, urlsToParse, refPath)
		(*paths)[i].Value = resolvedPathItem
	}
	return nil
}

func navigateToFragment(nested yaml.MapSlice, fragment, refPath string) (interface{}, error) {
	var current interface{} = nested
	for _, part := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		if part == "" {
			continue
		}
		currentMap, ok := current.(yaml.MapSlice)
		if !ok {
			return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference structure"}
		}
		value := getMapSliceValue(currentMap, part)
		if value == nil {
			return nil, &MergeError{File: refPath, Path: fragment, Message: fmt.Sprintf("Key '%s' not found", part)}
		}
		current = value
	}
	return current, nil
}

func processNestedFiles(urlsToParse map[string]bool, mainAPI *OpenAPI) error {
	componentTypes := []string{"schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks"}

	for url := range urlsToParse {
		data, err := os.ReadFile(url)
		if err != nil {
			return fmt.Errorf("failed to read '%s': %w", url, err)
		}

		var nested yaml.MapSlice
		if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
			return fmt.Errorf("failed to parse '%s': %w", url, err)
		}

		if nestedComponents := getMapSliceValue(nested, "components"); nestedComponents != nil {
			if compMap, ok := nestedComponents.(yaml.MapSlice); ok {
				mergeComponents(compMap, mainAPI, componentTypes)
			}
		}

		for _, ct := range componentTypes {
			if getMapSliceValue(nested, ct) != nil {
				mergeComponents(nested, mainAPI, componentTypes)
				break
			}
		}
	}
	return nil
}

func mergeComponents(nestedComponents yaml.MapSlice, mainAPI *OpenAPI, componentTypes []string) {
	findRefs(&nestedComponents, nil, "")

	for _, compType := range componentTypes {
		nestedComp, ok := getMapSliceValue(nestedComponents, compType).(yaml.MapSlice)
		if !ok {
			continue
		}

		mainComp, _ := getMapSliceValue(mainAPI.Components, compType).(yaml.MapSlice)
		for _, item := range nestedComp {
			if getMapSliceValue(mainComp, item.Key.(string)) == nil {
				mainComp = append(mainComp, item)
			}
		}
		setMapSliceValue(&mainAPI.Components, compType, mainComp)
	}
}

func findRefs(api *yaml.MapSlice, urlsToParse map[string]bool, currentFilePath string) {
	for i := range *api {
		key := (*api)[i].Key.(string)
		value := (*api)[i].Value

		if key == "$ref" {
			if refStr, ok := value.(string); ok && strings.Contains(refStr, "#") && !strings.HasPrefix(refStr, "#") {
				parts := strings.SplitN(refStr, "#", 2)
				if urlsToParse != nil {
					urlsToParse[resolveRef(parts[0], currentFilePath)] = true
				}
				(*api)[i].Value = "#" + parts[1]
			}
		} else {
			processValue(value, urlsToParse, currentFilePath)
		}
	}
}

func processValue(v interface{}, urlsToParse map[string]bool, currentFilePath string) {
	switch vt := v.(type) {
	case yaml.MapSlice:
		findRefs(&vt, urlsToParse, currentFilePath)
	case []interface{}:
		for _, item := range vt {
			processValue(item, urlsToParse, currentFilePath)
		}
	}
}

func getMapSliceValue(m yaml.MapSlice, key string) interface{} {
	for _, item := range m {
		if item.Key == key {
			return item.Value
		}
	}
	return nil
}

func setMapSliceValue(m *yaml.MapSlice, key string, value interface{}) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, yaml.MapItem{Key: key, Value: value})
}

func resolveRef(relativePath, currentFilePath string) string {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return relativePath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), relativePath))
}
