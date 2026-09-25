package merge

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

type objectKind uint16

const (
	literalKind objectKind = iota
	schemaKind
	pathItemKind
	operationKind
	responseKind
	parameterKind
	mediaTypeKind
	requestBodyKind
	exampleKind
	linkKind
	callbackKind
	encodingKind
	componentsKind
	genericKind
	dictionaryKind objectKind = 1 << 8
	extensionsKind objectKind = 1 << 9
)

func componentKind(category string) objectKind {
	switch category {
	case "schemas":
		return schemaKind
	case "pathItems":
		return pathItemKind
	case "responses":
		return responseKind
	case "parameters", "headers":
		return parameterKind
	case "mediaTypes":
		return mediaTypeKind
	case "requestBodies":
		return requestBodyKind
	case "examples":
		return exampleKind
	case "links":
		return linkKind
	case "callbacks":
		return callbackKind
	default:
		return genericKind
	}
}

func childKind(parent objectKind, key string) objectKind {
	if parent&dictionaryKind != 0 {
		if parent&extensionsKind != 0 && strings.HasPrefix(key, "x-") {
			return literalKind
		}
		return parent &^ (dictionaryKind | extensionsKind)
	}
	if strings.HasPrefix(key, "x-") {
		return literalKind
	}
	if parent == componentsKind {
		return componentKind(key) | dictionaryKind
	}
	switch parent {
	case schemaKind:
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
			return schemaKind | dictionaryKind
		case "items", "prefixItems", "additionalItems", "additionalProperties", "unevaluatedItems", "unevaluatedProperties", "propertyNames", "contains", "not", "if", "then", "else", "allOf", "anyOf", "oneOf", "contentSchema":
			return schemaKind
		}
	case pathItemKind:
		switch key {
		case "get", "put", "post", "delete", "options", "head", "patch", "trace", "query":
			return operationKind
		case "additionalOperations":
			return operationKind | dictionaryKind
		case "parameters":
			return parameterKind
		}
	case operationKind:
		switch key {
		case "parameters":
			return parameterKind
		case "requestBody":
			return requestBodyKind
		case "responses":
			return responseKind | dictionaryKind | extensionsKind
		case "callbacks":
			return callbackKind | dictionaryKind
		}
	case responseKind:
		switch key {
		case "content":
			return mediaTypeKind | dictionaryKind
		case "headers":
			return parameterKind | dictionaryKind
		case "links":
			return linkKind | dictionaryKind
		}
	case parameterKind:
		switch key {
		case "schema":
			return schemaKind
		case "content":
			return mediaTypeKind | dictionaryKind
		case "examples":
			return exampleKind | dictionaryKind
		}
	case mediaTypeKind:
		switch key {
		case "schema", "itemSchema":
			return schemaKind
		case "examples":
			return exampleKind | dictionaryKind
		case "encoding":
			return encodingKind | dictionaryKind
		case "itemEncoding", "prefixEncoding":
			return encodingKind
		}
	case requestBodyKind:
		if key == "content" {
			return mediaTypeKind | dictionaryKind
		}
	case callbackKind:
		return pathItemKind
	case encodingKind:
		switch key {
		case "headers":
			return parameterKind | dictionaryKind
		case "encoding":
			return encodingKind | dictionaryKind
		case "itemEncoding", "prefixEncoding":
			return encodingKind
		}
	}
	return literalKind
}

type referenceVisitor func(yaml.MapSlice, objectKind) (any, error)

// Visit children first to preserve their source context when replacing references.
func walkReferences(value any, kind objectKind, visit referenceVisitor) (any, error) {
	if kind == literalKind {
		return value, nil
	}
	switch v := value.(type) {
	case yaml.MapSlice:
		// Clone containers so resolving a schema cannot mutate an aliased example.
		v = slices.Clone(v)
		for i := range v {
			key, ok := v[i].Key.(string)
			if !ok || (key == "$ref" && kind&dictionaryKind == 0) {
				continue
			}
			child, err := walkReferences(v[i].Value, childKind(kind, key), visit)
			if err != nil {
				return nil, err
			}
			v[i].Value = child
		}
		if kind&dictionaryKind == 0 && kind != componentsKind && getMapSliceValue(v, "$ref") != nil {
			return visit(v, kind)
		}
		return v, nil
	case []any:
		v = slices.Clone(v)
		for i := range v {
			child, err := walkReferences(v[i], kind, visit)
			if err != nil {
				return nil, err
			}
			v[i] = child
		}
		return v, nil
	}
	return value, nil
}

type referenceResolver struct {
	inputFile string
	files     map[string]bool
	active    map[string]bool
}

func (r *referenceResolver) walk(value any, kind objectKind, source string) (any, error) {
	return walkReferences(value, kind, func(object yaml.MapSlice, kind objectKind) (any, error) {
		ref, ok := getMapSliceValue(object, "$ref").(string)
		if !ok {
			return object, nil
		}
		file, fragment, _ := strings.Cut(ref, "#")
		file, err := url.PathUnescape(file)
		if err != nil {
			return nil, &MergeError{File: source, Message: "Invalid reference URL", Cause: err}
		}
		pointer, err := decodeReferenceFragment(fragment, source)
		if err != nil {
			return nil, err
		}
		local := file == ""
		// Component references use the merged namespace; other references use their source file.
		if local && (filepath.Clean(source) == filepath.Clean(r.inputFile) || strings.HasPrefix(pointer, "/components/")) {
			return object, nil
		}
		refPath := source
		if !local {
			refPath = resolveRef(file, source)
		}
		data, err := os.ReadFile(refPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read '%s': %w", refPath, err)
		}
		var doc any
		if err := decodeYAML(data, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse '%s': %w", refPath, err)
		}
		target, err := navigateToPointer(doc, pointer, refPath)
		if err != nil {
			return nil, err
		}
		if _, mapping := doc.(yaml.MapSlice); mapping {
			r.files[refPath] = true
		}
		if strings.HasPrefix(pointer, "/components/") {
			setMapSliceValue(&object, "$ref", "#"+fragment)
			return object, nil
		}

		// Inline non-component targets relative to their source file.
		identity := refPath
		if realPath, err := filepath.EvalSymlinks(refPath); err == nil {
			identity = realPath
		}
		identity += "#" + pointer
		if r.active[identity] {
			return nil, &MergeError{File: refPath, Path: fragment, Message: "Cannot inline a cyclic reference outside components"}
		}
		if _, ok := target.(yaml.MapSlice); !ok {
			if _, boolean := target.(bool); kind != schemaKind || !boolean {
				return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference target"}
			}
		}
		r.active[identity] = true
		target, err = r.walk(target, kind, refPath)
		delete(r.active, identity)
		if err != nil {
			return nil, err
		}
		var siblings yaml.MapSlice
		for _, item := range object {
			if item.Key != "$ref" {
				siblings = append(siblings, item)
			}
		}
		if len(siblings) == 0 {
			return target, nil
		}
		if kind == schemaKind {
			// Use allOf to retain both target and sibling constraints.
			allOf, _ := getMapSliceValue(siblings, "allOf").([]any)
			setMapSliceValue(&siblings, "allOf", append(allOf, target))
			return siblings, nil
		}
		merged := target.(yaml.MapSlice)
		for _, item := range siblings {
			if key, ok := item.Key.(string); ok {
				setMapSliceValue(&merged, key, item.Value)
			}
		}
		return merged, nil
	})
}
