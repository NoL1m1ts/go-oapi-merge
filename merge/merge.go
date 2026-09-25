package merge

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// OpenAPI models root fields and preserves nested content as ordered YAML.
type OpenAPI struct {
	OpenAPI    string        `yaml:"openapi"`
	Info       yaml.MapSlice `yaml:"info"`
	Servers    []any         `yaml:"servers,omitempty"`
	Paths      yaml.MapSlice `yaml:"paths"`
	Webhooks   yaml.MapSlice `yaml:"webhooks,omitempty"`
	Components yaml.MapSlice `yaml:"components,omitempty"`
	Security   []any         `yaml:"security,omitempty"`
	Tags       []any         `yaml:"tags,omitempty"`
}

func OapiYaml(inputFile, outputFile string) error {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return &MergeError{File: inputFile, Message: "Failed to read input file", Cause: err}
	}

	var mainAPI OpenAPI
	if err := decodeYAML(data, &mainAPI); err != nil {
		return &MergeError{File: inputFile, Message: "Invalid OpenAPI YAML structure", Cause: err}
	}

	if mainAPI.OpenAPI == "" {
		return &MergeError{File: inputFile, Message: "Missing required field 'openapi'"}
	}
	if len(mainAPI.Info) == 0 {
		return &MergeError{File: inputFile, Message: "Missing required field 'info'"}
	}

	extra, err := extraRootFields(data)
	if err != nil {
		return &MergeError{File: inputFile, Message: "Invalid OpenAPI YAML structure", Cause: err}
	}

	resolver := &referenceResolver{inputFile: inputFile, files: make(map[string]bool), active: make(map[string]bool)}
	if err := processPathItemMap(&mainAPI.Paths, resolver, inputFile, true); err != nil {
		return err
	}
	if err := processPathItemMap(&mainAPI.Webhooks, resolver, inputFile, false); err != nil {
		return err
	}

	components, err := resolver.walk(mainAPI.Components, componentsKind, inputFile)
	if err != nil {
		return err
	}
	mainAPI.Components = components.(yaml.MapSlice)
	if err := processNestedFiles(resolver, &mainAPI.Components); err != nil {
		return err
	}

	if err := validateNoDanglingLocalRefs(&mainAPI, inputFile); err != nil {
		return err
	}

	data, err = marshalYAML(&mainAPI)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if len(extra) > 0 {
		data, err = appendExtraRootFields(data, extra)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
	}

	return os.WriteFile(outputFile, data, 0644)
}

var namedExtraRootFields = map[string]bool{
	"jsonSchemaDialect": true,
	"$self":             true,
	"summary":           true,
	"externalDocs":      true,
}

// extraRootFields preserves supported extra fields and extensions in source order.
func extraRootFields(data []byte) (yaml.MapSlice, error) {
	var raw yaml.MapSlice
	if err := decodeYAML(data, &raw); err != nil {
		return nil, err
	}

	var extra yaml.MapSlice
	for _, item := range raw {
		key, ok := item.Key.(string)
		if !ok {
			continue
		}
		if namedExtraRootFields[key] || strings.HasPrefix(key, "x-") {
			extra = append(extra, item)
		}
	}
	return extra, nil
}

func appendExtraRootFields(marshaled []byte, extra yaml.MapSlice) ([]byte, error) {
	var doc yaml.MapSlice
	if err := decodeYAML(marshaled, &doc); err != nil {
		return nil, err
	}
	doc = append(doc, extra...)
	return marshalYAML(doc)
}

func processPathItemMap(paths *yaml.MapSlice, resolver *referenceResolver, currentFilePath string, hasExtensions bool) error {
	*paths = append(yaml.MapSlice(nil), (*paths)...)
	for i := range *paths {
		pathKey := (*paths)[i].Key.(string)
		if hasExtensions && strings.HasPrefix(pathKey, "x-") {
			continue
		}
		pathValue := (*paths)[i].Value

		pathMap, ok := pathValue.(yaml.MapSlice)
		if !ok {
			continue
		}

		refValue := getMapSliceValue(pathMap, "$ref")
		refStr, isExternalRef := refValue.(string)
		isExternalRef = isExternalRef && !strings.HasPrefix(refStr, "#")

		if !isExternalRef {
			resolved, err := resolver.walk(pathMap, pathItemKind, currentFilePath)
			if err != nil {
				return err
			}
			(*paths)[i].Value = resolved
			continue
		}

		file, fragment, _ := strings.Cut(refStr, "#")
		file, err := url.PathUnescape(file)
		if err != nil {
			return &MergeError{File: currentFilePath, Path: pathKey, Message: "Invalid reference URL", Cause: err}
		}

		refPath := resolveRef(file, currentFilePath)
		resolver.files[refPath] = true

		pointer, err := decodeReferenceFragment(fragment, refPath)
		if err != nil {
			return err
		}
		if pointer != "" && !strings.HasPrefix(pointer, "/") {
			pointer = "/" + pointer
		}

		data, err := os.ReadFile(refPath)
		if err != nil {
			return &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Cannot read referenced file '%s'", refPath), Cause: err}
		}

		var nested yaml.MapSlice
		if err := decodeYAML(data, &nested); err != nil {
			return &MergeError{File: refPath, Message: "Invalid YAML syntax", Cause: err}
		}

		current, err := navigateToPointer(nested, pointer, refPath)
		if err != nil {
			return err
		}

		resolvedPathItem, ok := current.(yaml.MapSlice)
		if !ok {
			return &MergeError{File: refPath, Path: fragment, Message: "Invalid reference target"}
		}

		resolved, err := resolver.walk(resolvedPathItem, pathItemKind, refPath)
		if err != nil {
			return err
		}
		(*paths)[i].Value = resolved
	}
	return nil
}

func decodeReferenceFragment(fragment, refPath string) (string, error) {
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		return "", &MergeError{File: refPath, Path: fragment, Message: "Invalid reference fragment", Cause: err}
	}
	return decoded, nil
}

func navigateToFragment(nested any, fragment, refPath string) (any, error) {
	pointer, err := decodeReferenceFragment(fragment, refPath)
	if err != nil {
		return nil, err
	}
	return navigateToPointer(nested, pointer, refPath)
}

func navigateToPointer(nested any, fragment, refPath string) (any, error) {
	var current any = nested
	if fragment == "" {
		return current, nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case yaml.MapSlice:
			current = getMapSliceValue(value, part)
			if current == nil {
				return nil, &MergeError{File: refPath, Path: fragment, Message: fmt.Sprintf("Key '%s' not found", part)}
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) || strconv.Itoa(index) != part {
				return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference array index"}
			}
			current = value[index]
		default:
			return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference structure"}
		}
	}
	return current, nil
}

func processNestedFiles(resolver *referenceResolver, components *yaml.MapSlice) error {
	componentTypes := []string{
		"schemas",
		"responses",
		"parameters",
		"examples",
		"requestBodies",
		"headers",
		"securitySchemes",
		"links",
		"callbacks",
		"pathItems",
		"mediaTypes",
	}

	processed := make(map[string]bool)

	for {
		var pending []string
		for url := range resolver.files {
			if !processed[url] {
				pending = append(pending, url)
			}
		}

		if len(pending) == 0 {
			break
		}
		// Stable dependency order also makes duplicate precedence deterministic.
		sort.Strings(pending)

		for _, url := range pending {
			processed[url] = true

			data, err := os.ReadFile(url)
			if err != nil {
				return fmt.Errorf("failed to read '%s': %w", url, err)
			}

			var nested yaml.MapSlice
			if err := decodeYAML(data, &nested); err != nil {
				return fmt.Errorf("failed to parse '%s': %w", url, err)
			}

			if nestedComponents := getMapSliceValue(nested, "components"); nestedComponents != nil {
				if compMap, ok := nestedComponents.(yaml.MapSlice); ok {
					if err := mergeComponents(
						compMap,
						components,
						mapSliceKeys(compMap),
						resolver,
						url,
					); err != nil {
						return err
					}
				}
			}

			for _, ct := range componentTypes {
				if getMapSliceValue(nested, ct) != nil {
					if err := mergeComponents(
						nested,
						components,
						componentTypes,
						resolver,
						url,
					); err != nil {
						return err
					}
					break
				}
			}
		}
	}
	return nil
}

func mapSliceKeys(m yaml.MapSlice) []string {
	keys := make([]string, 0, len(m))
	for _, item := range m {
		if key, ok := item.Key.(string); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func mergeComponents(
	nestedComponents yaml.MapSlice,
	components *yaml.MapSlice,
	componentTypes []string,
	resolver *referenceResolver,
	currentFilePath string,
) error {
	for _, compType := range componentTypes {
		nestedComp, ok := getMapSliceValue(nestedComponents, compType).(yaml.MapSlice)
		if !ok {
			continue
		}

		mainComp, _ := getMapSliceValue(*components, compType).(yaml.MapSlice)
		componentsToMerge := make(yaml.MapSlice, 0, len(nestedComp))
		for _, item := range nestedComp {
			if getMapSliceValue(mainComp, item.Key.(string)) == nil {
				componentsToMerge = append(componentsToMerge, item)
			}
		}

		if !strings.HasPrefix(compType, "x-") {
			resolved, err := resolver.walk(componentsToMerge, componentKind(compType)|dictionaryKind, currentFilePath)
			if err != nil {
				return err
			}
			componentsToMerge = resolved.(yaml.MapSlice)
		}
		mainComp = append(mainComp, componentsToMerge...)
		setMapSliceValue(components, compType, mainComp)
	}
	return nil
}

func getMapSliceValue(m yaml.MapSlice, key string) any {
	for _, item := range m {
		if item.Key == key {
			return item.Value
		}
	}
	return nil
}

func setMapSliceValue(m *yaml.MapSlice, key string, value any) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, yaml.MapItem{Key: key, Value: value})
}

func validateNoDanglingLocalRefs(mainAPI *OpenAPI, inputFile string) error {
	root := yaml.MapSlice{
		{Key: "paths", Value: mainAPI.Paths},
		{Key: "webhooks", Value: mainAPI.Webhooks},
		{Key: "components", Value: mainAPI.Components},
	}
	validate := func(object yaml.MapSlice, _ objectKind) (any, error) {
		ref, ok := getMapSliceValue(object, "$ref").(string)
		if ok && strings.HasPrefix(ref, "#") {
			fragment := strings.TrimPrefix(ref, "#")
			pointer, err := decodeReferenceFragment(fragment, inputFile)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(pointer, "/") {
				if _, err := navigateToPointer(root, pointer, inputFile); err != nil {
					return nil, &MergeError{File: inputFile, Path: fragment, Message: fmt.Sprintf("Reference '#%s' does not resolve to a merged value", fragment)}
				}
			}
		}
		return object, nil
	}
	for _, section := range []struct {
		value any
		kind  objectKind
	}{
		{mainAPI.Paths, pathItemKind | dictionaryKind | extensionsKind},
		{mainAPI.Webhooks, pathItemKind | dictionaryKind},
		{mainAPI.Components, componentsKind},
	} {
		if _, err := walkReferences(section.value, section.kind, validate); err != nil {
			return err
		}
	}
	return nil
}

func resolveRef(relativePath, currentFilePath string) string {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return relativePath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), relativePath))
}
