package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPI represents the structure of the OpenAPI specification documents.
// It contains all the main sections of the OpenAPI documents according to the specification.
type OpenAPI struct {
	OpenAPI    string                 `yaml:"openapi"`
	Info       map[string]interface{} `yaml:"info"`
	Servers    []interface{}          `yaml:"servers,omitempty"`
	Paths      map[string]interface{} `yaml:"paths"`
	Components map[string]interface{} `yaml:"components,omitempty"`
	Security   []interface{}          `yaml:"security,omitempty"`
	Tags       []interface{}          `yaml:"tags,omitempty"`
}

// OapiYaml merges OpenAPI specifications from multiple files, preserving field order.
// It reads the main OpenAPI file, processes all references, and outputs a single merged file.
//
// Parameters:
//   - inputFile: Path to the main OpenAPI specification file
//   - outputFile: Path where the merged specification will be written
//
// Returns:
//   - error: Any error that occurred during merging
func OapiYaml(inputFile, outputFile string) error {
	// Read the source file as a YAML node to preserve field order
	rootNode, err := readYAMLNode(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read input YAML: %v", err)
	}

	// Convert YAML node to OpenAPI structure for processing
	var mainAPI OpenAPI
	if err := rootNode.Decode(&mainAPI); err != nil {
		return fmt.Errorf("failed to decode YAML: %v", err)
	}

	// Initialize components if they are missing
	if mainAPI.Components == nil {
		mainAPI.Components = make(map[string]interface{})
	}

	// Process paths and collect references to external files
	urlsToParse := make(map[string]bool)
	if err := processPaths(rootNode.Content[0].Content[3].Content[1], mainAPI.Paths, urlsToParse, inputFile); err != nil {
		return fmt.Errorf("failed to process paths: %v", err)
	}

	// Process all external file references
	if err := processNestedFiles(urlsToParse, &mainAPI); err != nil {
		return fmt.Errorf("failed to process nested files: %v", err)
	}

	// Create a new YAML node for output with preserved field order
	outputNode := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{
			{
				Kind: yaml.MappingNode,
			},
		},
	}

	// Encode OpenAPI structure back to YAML while preserving field order
	if err := encodeOpenAPI(outputNode.Content[0], mainAPI); err != nil {
		return fmt.Errorf("failed to encode OpenAPI: %v", err)
	}

	// Write the result to a file
	f, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer f.Close()

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(2)
	if err := encoder.Encode(outputNode); err != nil {
		return fmt.Errorf("failed to write YAML: %v", err)
	}

	return nil
}

// encodeOpenAPI encodes the OpenAPI structure into a YAML node while preserving field order.
// This function processes each section of the OpenAPI specification separately to ensure
// that field order is retained in the output YAML.
//
// Parameters:
//   - node: The target YAML node where the OpenAPI structure will be encoded
//   - api: The OpenAPI structure to encode
//
// Returns:
//   - error: Any error that occurred during encoding
func encodeOpenAPI(node *yaml.Node, api OpenAPI) error {
	// Add OpenAPI version field
	if err := addMapEntry(node, "openapi", api.OpenAPI); err != nil {
		return err
	}

	// Add info section
	if api.Info != nil {
		if err := addMapEntry(node, "info", api.Info); err != nil {
			return err
		}
	}

	// Add servers section
	if api.Servers != nil {
		if err := addMapEntry(node, "servers", api.Servers); err != nil {
			return err
		}
	}

	// Add paths section
	if api.Paths != nil {
		if err := addMapEntry(node, "paths", api.Paths); err != nil {
			return err
		}
	}

	// Add components section
	if api.Components != nil {
		if err := addMapEntry(node, "components", api.Components); err != nil {
			return err
		}
	}

	// Add security section
	if api.Security != nil {
		if err := addMapEntry(node, "security", api.Security); err != nil {
			return err
		}
	}

	// Add tags section
	if api.Tags != nil {
		if err := addMapEntry(node, "tags", api.Tags); err != nil {
			return err
		}
	}

	return nil
}

// addMapEntry adds a key-value pair into a YAML node.
// This function handles various value types and ensures they are properly
// encoded as YAML nodes with the correct structure.
//
// Parameters:
//   - node: The target YAML node where the key-value pair will be added
//   - key: The key for the map entry
//   - value: The value for the map entry, which can be of various types
//
// Returns:
//   - error: Any error that occurred during encoding
func addMapEntry(node *yaml.Node, key string, value interface{}) error {
	// Add key
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: key,
	}
	node.Content = append(node.Content, keyNode)

	// Add value based on its type
	var valueNode *yaml.Node
	switch v := value.(type) {
	case *yaml.Node:
		// If value is already a YAML node, use it directly
		valueNode = v
	case string:
		// Handle string values
		valueNode = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: v,
		}

		// Special handling for external references - ensure they are quoted
		if key == "$ref" && !strings.HasPrefix(v, "#") {
			valueNode.Style = yaml.SingleQuotedStyle
		}
	case int:
		// Handle integer values
		valueNode = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: fmt.Sprintf("%d", v),
		}
	case bool:
		// Handle boolean values
		valueNode = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: fmt.Sprintf("%t", v),
		}
	case map[string]interface{}:
		// Handle map values, recursively adding their entries
		valueNode = &yaml.Node{Kind: yaml.MappingNode}

		// Preserve order of specific OpenAPI objects based on common field patterns
		if isSchemaObject(v) {
			// For Schema objects, preserve common field order
			addOrderedSchemaFields(valueNode, v)
		} else if isParameterObject(v) {
			// For Parameter objects, preserve standard parameter field order
			addOrderedParameterFields(valueNode, v)
		} else if isResponseObject(v) {
			// For Response objects, preserve standard response field order
			addOrderedResponseFields(valueNode, v)
		} else if isPathItemObject(v) {
			// For Path Item objects, preserve standard path item field order
			addOrderedPathItemFields(valueNode, v)
		} else if isOperationObject(v) {
			// For Operation objects, preserve standard operation field order
			addOrderedOperationFields(valueNode, v)
		} else {
			// Default processing for other maps
			for k, val := range v {
				if err := addMapEntry(valueNode, k, val); err != nil {
					return err
				}
			}
		}
	case []interface{}:
		// Handle array values
		valueNode = &yaml.Node{Kind: yaml.SequenceNode}

		// Special handling for specific array types
		if key == "required" {
			// For required fields array, ensure it contains only string values and no empty objects
			for _, item := range v {
				if strValue, ok := item.(string); ok {
					itemNode := &yaml.Node{
						Kind:  yaml.ScalarNode,
						Value: strValue,
					}
					valueNode.Content = append(valueNode.Content, itemNode)
				}
			}
		} else if key == "parameters" {
			// For parameters array, ensure each parameter follows the standard order
			for _, item := range v {
				if paramMap, ok := item.(map[string]interface{}); ok {
					// Create a mapping node for the parameter
					paramNode := &yaml.Node{Kind: yaml.MappingNode}

					// Add parameter fields in the standard order
					if isParameterObject(paramMap) {
						addOrderedParameterFields(paramNode, paramMap)
					} else {
						// Fallback for non-standard parameters
						for k, v := range paramMap {
							if err := addMapEntry(paramNode, k, v); err != nil {
								return err
							}
						}
					}

					valueNode.Content = append(valueNode.Content, paramNode)
				} else {
					// Handle non-map parameters (shouldn't happen in valid OpenAPI)
					itemNode := &yaml.Node{Kind: yaml.ScalarNode}
					if err := itemNode.Encode(item); err != nil {
						return err
					}
					valueNode.Content = append(valueNode.Content, itemNode)
				}
			}
		} else {
			// Default processing for other arrays
			for _, item := range v {
				switch itemValue := item.(type) {
				case map[string]interface{}:
					// Handle objects in array
					itemNode := &yaml.Node{Kind: yaml.MappingNode}
					for k, v := range itemValue {
						if err := addMapEntry(itemNode, k, v); err != nil {
							return err
						}
					}
					valueNode.Content = append(valueNode.Content, itemNode)
				case string:
					// Handle string values in array
					itemNode := &yaml.Node{
						Kind:  yaml.ScalarNode,
						Value: itemValue,
					}
					valueNode.Content = append(valueNode.Content, itemNode)
				default:
					// Handle other types of values in array
					itemNode := &yaml.Node{Kind: yaml.ScalarNode}
					if err := itemNode.Encode(item); err != nil {
						return err
					}
					valueNode.Content = append(valueNode.Content, itemNode)
				}
			}
		}
	default:
		// Handle any other types of values using YAML encoder
		valueNode = &yaml.Node{Kind: yaml.ScalarNode}
		if err := valueNode.Encode(v); err != nil {
			return err
		}
	}

	node.Content = append(node.Content, valueNode)
	return nil
}

// isSchemaObject checks if a map represents an OpenAPI Schema Object
func isSchemaObject(m map[string]interface{}) bool {
	// Schema objects typically have type, properties, etc.
	_, hasType := m["type"]
	_, hasProperties := m["properties"]
	_, hasItems := m["items"]
	_, hasRequired := m["required"]
	_, hasFormat := m["format"]

	return (hasType || hasProperties || hasItems || hasRequired || hasFormat)
}

// addOrderedSchemaFields adds Schema Object fields in a standardized order
func addOrderedSchemaFields(node *yaml.Node, schema map[string]interface{}) error {
	// Standard order for Schema Object properties
	orderedFields := []string{
		"type", "format", "title", "description", "default", "multipleOf",
		"maximum", "exclusiveMaximum", "minimum", "exclusiveMinimum",
		"maxLength", "minLength", "pattern", "maxItems", "minItems",
		"uniqueItems", "maxProperties", "minProperties", "required",
		"enum", "properties", "items", "allOf", "oneOf", "anyOf", "not",
		"additionalProperties", "nullable", "discriminator", "readOnly",
		"writeOnly", "xml", "externalDocs", "example", "deprecated",
	}

	// First add fields in standard order
	for _, field := range orderedFields {
		if value, exists := schema[field]; exists {
			// Special handling for properties to preserve preferred order
			if field == "properties" && isMapOfInterfaces(value) {
				propertiesNode := &yaml.Node{Kind: yaml.MappingNode}
				if err := addMapEntry(node, field, propertiesNode); err != nil {
					return err
				}

				// Preferred order for common property names
				preferredOrder := []string{
					"id", "name", "title", "description", "type", "format",
					"username", "email", "password", "firstName", "lastName",
					"createdAt", "updatedAt", "deletedAt",
				}

				// Add properties in preferred order first
				propertiesMap := value.(map[string]interface{})
				processedProps := make(map[string]bool)

				for _, propName := range preferredOrder {
					if propValue, exists := propertiesMap[propName]; exists {
						if err := addMapEntry(propertiesNode, propName, propValue); err != nil {
							return err
						}
						processedProps[propName] = true
					}
				}

				// Add remaining properties
				for propName, propValue := range propertiesMap {
					if !processedProps[propName] {
						if err := addMapEntry(propertiesNode, propName, propValue); err != nil {
							return err
						}
					}
				}
			} else {
				if err := addMapEntry(node, field, value); err != nil {
					return err
				}
			}
			delete(schema, field)
		}
	}

	// Then add any remaining fields
	for field, value := range schema {
		if err := addMapEntry(node, field, value); err != nil {
			return err
		}
	}

	return nil
}

// isParameterObject checks if a map represents an OpenAPI Parameter Object
func isParameterObject(m map[string]interface{}) bool {
	// Parameter objects typically have name, in, etc.
	_, hasName := m["name"]
	_, hasIn := m["in"]

	return (hasName && hasIn)
}

// addOrderedParameterFields adds Parameter Object fields in a standardized order
func addOrderedParameterFields(node *yaml.Node, param map[string]interface{}) error {
	// Standard order for Parameter Object properties
	orderedFields := []string{
		"name", "in", "description", "required", "deprecated",
		"allowEmptyValue", "style", "explode", "allowReserved",
		"schema", "example", "examples", "content",
	}

	// First add fields in standard order
	for _, field := range orderedFields {
		if value, exists := param[field]; exists {
			if err := addMapEntry(node, field, value); err != nil {
				return err
			}
			delete(param, field)
		}
	}

	// Then add any remaining fields
	for field, value := range param {
		if err := addMapEntry(node, field, value); err != nil {
			return err
		}
	}

	return nil
}

// isResponseObject checks if a map represents an OpenAPI Response Object
func isResponseObject(m map[string]interface{}) bool {
	// Response objects typically have description
	_, hasDescription := m["description"]
	_, hasContent := m["content"]

	return (hasDescription || hasContent)
}

// addOrderedResponseFields adds Response Object fields in a standardized order
func addOrderedResponseFields(node *yaml.Node, response map[string]interface{}) error {
	// Standard order for Response Object properties
	orderedFields := []string{
		"description", "headers", "content", "links",
	}

	// First add fields in standard order
	for _, field := range orderedFields {
		if value, exists := response[field]; exists {
			if err := addMapEntry(node, field, value); err != nil {
				return err
			}
			delete(response, field)
		}
	}

	// Then add any remaining fields
	for field, value := range response {
		if err := addMapEntry(node, field, value); err != nil {
			return err
		}
	}

	return nil
}

// isPathItemObject checks if a map represents an OpenAPI Path Item Object
func isPathItemObject(m map[string]interface{}) bool {
	// Path Item objects typically have HTTP methods like get, post, etc.
	httpMethods := []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	for _, method := range httpMethods {
		if _, exists := m[method]; exists {
			return true
		}
	}
	_, hasParameters := m["parameters"]
	_, hasRef := m["$ref"]

	return hasParameters || hasRef
}

// addOrderedPathItemFields adds Path Item Object fields in a standardized order
func addOrderedPathItemFields(node *yaml.Node, pathItem map[string]interface{}) error {
	// Handle $ref specially
	if ref, exists := pathItem["$ref"]; exists {
		if err := addMapEntry(node, "$ref", ref); err != nil {
			return err
		}
		delete(pathItem, "$ref")
		// If there's a $ref, it typically replaces all other fields
		// but we'll process them anyway in case they're used for something
	}

	// Standard order for Path Item Object HTTP methods
	httpMethods := []string{
		"get", "put", "post", "delete", "options", "head", "patch", "trace",
	}

	// Add HTTP methods in standard order
	for _, method := range httpMethods {
		if operation, exists := pathItem[method]; exists {
			if err := addMapEntry(node, method, operation); err != nil {
				return err
			}
			delete(pathItem, method)
		}
	}

	// Other fields
	otherFields := []string{
		"summary", "description", "servers", "parameters",
	}

	// Add other fields in standard order
	for _, field := range otherFields {
		if value, exists := pathItem[field]; exists {
			if err := addMapEntry(node, field, value); err != nil {
				return err
			}
			delete(pathItem, field)
		}
	}

	// Add any remaining fields
	for field, value := range pathItem {
		if err := addMapEntry(node, field, value); err != nil {
			return err
		}
	}

	return nil
}

// isOperationObject checks if a map represents an OpenAPI Operation Object
func isOperationObject(m map[string]interface{}) bool {
	operations := []string{"summary", "description", "operationId", "responses", "parameters", "requestBody"}
	for _, field := range operations {
		if _, exists := m[field]; exists {
			return true
		}
	}
	return false
}

// addOrderedOperationFields adds Operation Object fields in a standardized order
func addOrderedOperationFields(node *yaml.Node, operation map[string]interface{}) error {
	// Standard order for Operation Object fields
	orderedFields := []string{
		"tags", "summary", "description", "externalDocs", "operationId", "parameters",
		"requestBody", "responses", "callbacks", "deprecated", "security", "servers",
	}

	// First add fields in standard order
	for _, field := range orderedFields {
		if value, exists := operation[field]; exists {
			// Special handling for responses to order by status code
			if field == "responses" && isMapOfInterfaces(value) {
				responsesNode := &yaml.Node{Kind: yaml.MappingNode}
				if err := addMapEntry(node, field, responsesNode); err != nil {
					return err
				}

				// Order responses by status code
				responsesMap := value.(map[string]interface{})

				// Preferred order for common response status codes
				statusCodes := []string{
					"default", "200", "201", "202", "204",
					"400", "401", "403", "404", "405", "409", "422",
					"500", "501", "503",
				}

				// Add responses in preferred order first
				processedResponses := make(map[string]bool)

				for _, statusCode := range statusCodes {
					if responseValue, exists := responsesMap[statusCode]; exists {
						if err := addMapEntry(responsesNode, statusCode, responseValue); err != nil {
							return err
						}
						processedResponses[statusCode] = true
					}
				}

				// Add remaining responses
				for statusCode, responseValue := range responsesMap {
					if !processedResponses[statusCode] {
						if err := addMapEntry(responsesNode, statusCode, responseValue); err != nil {
							return err
						}
					}
				}
			} else {
				if err := addMapEntry(node, field, value); err != nil {
					return err
				}
			}
			delete(operation, field)
		}
	}

	// Then add any remaining fields
	for field, value := range operation {
		if err := addMapEntry(node, field, value); err != nil {
			return err
		}
	}

	return nil
}

// processOperation processes an operation object (GET, POST, etc.)
func processOperation(operationNode *yaml.Node, operation map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	// Check for references in the operation
	if err := checkForRefs(operation); err != nil {
		return fmt.Errorf("failed to check for references in operation: %v", err)
	}

	// Create a new node to hold the ordered fields
	newNode := &yaml.Node{Kind: yaml.MappingNode}

	// Add operation fields in a standardized order
	if err := addOrderedOperationFields(newNode, operation); err != nil {
		return fmt.Errorf("failed to add ordered operation fields: %v", err)
	}

	// Copy the content from the new node to the operation node
	operationNode.Kind = newNode.Kind
	operationNode.Style = newNode.Style
	operationNode.Content = newNode.Content

	return nil
}

// readYAMLNode reads a YAML file and returns its root node.
// This preserves the structure and order of the original YAML document.
//
// Parameters:
//   - filePath: The path to the YAML file to be read
//
// Returns:
//   - *yaml.Node: The root node of the YAML document
//   - error: Any error that occurred during reading or parsing
func readYAMLNode(filePath string) (*yaml.Node, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file '%s': %v", filePath, err)
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %v", err)
	}

	return &rootNode, nil
}

// decodeRef decodes a JSON Pointer reference by replacing escape sequences.
// This is used for proper handling of references in OpenAPI specifications.
//
// Parameters:
//   - ref: The reference to decode
//
// Returns:
//   - string: The decoded reference
func decodeRef(ref string) string {
	ref = strings.ReplaceAll(ref, "~1", "/")
	ref = strings.ReplaceAll(ref, "~0", "~")
	return ref
}

// processPaths handles all paths in the OpenAPI specification and resolves references.
// It collects URLs of external files that need to be processed.
//
// Parameters:
//   - pathsNode: The YAML node for the paths section
//   - paths: The paths section of the OpenAPI specification
//   - urlsToParse: A map for collecting URLs of external files to be processed
//   - currentFilePath: The path of the current file being processed
//
// Returns:
//   - error: Any error that occurred during processing
func processPaths(pathsNode *yaml.Node, paths map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	// Temporary map to store resolved paths
	resolvedPaths := make(map[string]interface{})

	for pathKey, pathValue := range paths {
		// Check if the path value is a reference
		pathMap, ok := pathValue.(map[string]interface{})
		if !ok {
			continue
		}

		// Check for the presence of a $ref key
		refValue, hasRef := pathMap["$ref"]
		if !hasRef {
			// If there's no reference, save the path as is
			resolvedPaths[pathKey] = pathValue
			continue
		}

		// Get the reference string
		refStr, ok := refValue.(string)
		if !ok {
			return fmt.Errorf("invalid reference format for path '%s': expected string, got %T", pathKey, refValue)
		}

		// If it's a reference to an internal component, save as is
		if strings.HasPrefix(refStr, "#") {
			resolvedPaths[pathKey] = pathValue
			continue
		}

		// Resolve the reference path
		refPath, err := resolveRef(refStr, currentFilePath)
		if err != nil {
			return fmt.Errorf("failed to resolve reference for path '%s': %v", pathKey, err)
		}

		// Add the file to the list for processing
		urlsToParse[refPath] = true

		// Split the reference into parts: file and internal path
		parts := strings.SplitN(refStr, "#", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid reference format for path '%s': missing fragment in '%s'", pathKey, refStr)
		}

		// Get the fragment (part after #)
		fragment := parts[1]
		if !strings.HasPrefix(fragment, "/") {
			fragment = "/" + fragment
		}

		// Read the YAML file referenced by the reference
		data, err := os.ReadFile(refPath)
		if err != nil {
			return fmt.Errorf("failed to read YAML file '%s': %v", refPath, err)
		}

		// Parse the YAML content
		var nested map[string]interface{}
		if err := yaml.Unmarshal(data, &nested); err != nil {
			return fmt.Errorf("failed to unmarshal YAML file '%s': %v", refPath, err)
		}

		// Split the fragment into path parts
		fragmentParts := strings.Split(strings.TrimPrefix(fragment, "/"), "/")

		// Follow the fragment path sequentially
		var current interface{} = nested
		for _, part := range fragmentParts {
			if part == "" {
				continue
			}

			// Convert the current node to a map
			currentMap, ok := current.(map[string]interface{})
			if !ok {
				return fmt.Errorf("invalid reference path for path '%s': expected map at '%s', got %T", pathKey, fragment, current)
			}

			// Get the next node along the path
			current, ok = currentMap[part]
			if !ok {
				return fmt.Errorf("invalid reference path for path '%s': key '%s' not found at '%s'", pathKey, part, fragment)
			}
		}

		// Convert the found node to a map
		resolvedPathItem, ok := current.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid reference target for path '%s': expected map, got %T", pathKey, current)
		}

		// Process any nested references in the resolved path item
		if err := findRef(resolvedPathItem, urlsToParse, refPath); err != nil {
			return fmt.Errorf("failed to process nested references: %v", err)
		}

		// Save the resolved path
		resolvedPaths[pathKey] = resolvedPathItem
	}

	// Replace the original paths with the resolved ones
	for k := range paths {
		delete(paths, k)
	}
	for k, v := range resolvedPaths {
		paths[k] = v
	}

	return nil
}

// processNestedFiles processes all external files referenced by the OpenAPI specification.
// It merges components from these files into the main specification.
//
// Parameters:
//   - urlsToParse: A map of URLs of external files to be processed
//   - mainAPI: The main OpenAPI specification being built
//
// Returns:
//   - error: Any error that occurred during processing
func processNestedFiles(urlsToParse map[string]bool, mainAPI *OpenAPI) error {
	for url := range urlsToParse {
		// Read the YAML file
		data, err := os.ReadFile(url)
		if err != nil {
			return fmt.Errorf("failed to read nested YAML file '%s': %v", url, err)
		}

		// Parse the YAML content
		var nested map[string]interface{}
		if err := yaml.Unmarshal(data, &nested); err != nil {
			return fmt.Errorf("failed to unmarshal nested YAML file '%s': %v", url, err)
		}

		// Process components in the nested file
		if nestedComponents, ok := nested["components"].(map[string]interface{}); ok {
			if err := mergeComponents(nestedComponents, mainAPI); err != nil {
				return fmt.Errorf("failed to merge components from '%s': %v", url, err)
			}
		}

		// Also check for components directly at the root level (for files that only contain components)
		// This is important for files like responses.yaml that might have components at the root
		componentTypes := []string{
			"schemas", "responses", "parameters", "examples", "requestBodies",
			"headers", "securitySchemes", "links", "callbacks",
		}

		// Check if this file has components at the root level
		hasRootComponents := false
		for _, componentType := range componentTypes {
			if _, ok := nested[componentType].(map[string]interface{}); ok {
				hasRootComponents = true
				break
			}
		}

		// If there are components at the root level, merge them
		if hasRootComponents {
			if err := mergeComponents(nested, mainAPI); err != nil {
				return fmt.Errorf("failed to merge root components from '%s': %v", url, err)
			}
		}
	}
	return nil
}

// mergeComponents merges components from external files into the main specification.
// It handles schemas, responses, and examples, avoiding duplication.
//
// Parameters:
//   - nestedComponents: The components from the external file
//   - mainAPI: The main OpenAPI specification being built
//
// Returns:
//   - error: Any error that occurred during merging
func mergeComponents(nestedComponents map[string]interface{}, mainAPI *OpenAPI) error {
	if err := checkForRefs(nestedComponents); err != nil {
		return fmt.Errorf("failed to check for references in components: %v", err)
	}

	// Initialize components if they don't exist
	if mainAPI.Components == nil {
		mainAPI.Components = make(map[string]interface{})
	}

	// Process schemas
	if schemas, ok := nestedComponents["schemas"].(map[string]interface{}); ok {
		if mainAPI.Components["schemas"] == nil {
			mainAPI.Components["schemas"] = make(map[string]interface{})
		}
		mainSchemas := mainAPI.Components["schemas"].(map[string]interface{})

		for name, schema := range schemas {
			// Add schema to main API if it doesn't already exist
			if _, exists := mainSchemas[name]; !exists {
				mainSchemas[name] = schema
			}
		}
	}

	// Process responses
	if responses, ok := nestedComponents["responses"].(map[string]interface{}); ok {
		if mainAPI.Components["responses"] == nil {
			mainAPI.Components["responses"] = make(map[string]interface{})
		}
		mainResponses := mainAPI.Components["responses"].(map[string]interface{})

		for name, response := range responses {
			// Add response to main API if it doesn't already exist
			if _, exists := mainResponses[name]; !exists {
				mainResponses[name] = response
			}
		}
	}

	// Process examples
	if examples, ok := nestedComponents["examples"].(map[string]interface{}); ok {
		if mainAPI.Components["examples"] == nil {
			mainAPI.Components["examples"] = make(map[string]interface{})
		}
		mainExamples := mainAPI.Components["examples"].(map[string]interface{})

		for name, example := range examples {
			// Add example to main API if it doesn't already exist
			if _, exists := mainExamples[name]; !exists {
				mainExamples[name] = example
			}
		}
	}

	// Process other component types
	componentTypes := []string{
		"parameters", "requestBodies", "headers",
		"securitySchemes", "links", "callbacks",
	}

	for _, compType := range componentTypes {
		if components, ok := nestedComponents[compType].(map[string]interface{}); ok {
			if mainAPI.Components[compType] == nil {
				mainAPI.Components[compType] = make(map[string]interface{})
			}
			mainComponents := mainAPI.Components[compType].(map[string]interface{})

			for name, component := range components {
				// Add component to main API if it doesn't already exist
				if _, exists := mainComponents[name]; !exists {
					mainComponents[name] = component
				}
			}
		}
	}

	return nil
}

// findRef locates and processes all references in the OpenAPI specification.
// It updates references to external files and gathers URLs that need to be processed.
//
// Parameters:
//   - api: The API object in which references need to be found
//   - urlsToParse: A map collecting URLs of external files to be processed
//   - currentFilePath: The path of the current file being processed
//
// Returns:
//   - error: Any error that occurred during processing
func findRef(api map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	var processValue func(interface{}) error

	processMap := func(m map[string]interface{}) error {
		// Process keys and values first
		for k, v := range m {
			// If key is $ref, then process the reference
			if k == "$ref" {
				if refStr, ok := v.(string); ok {
					// If it's a reference to an external file, then process it
					if !strings.HasPrefix(refStr, "#") {
						refPath, err := resolveRef(refStr, currentFilePath)
						if err != nil {
							return err
						}
						if refPath != "" {
							urlsToParse[refPath] = true

							// Update the reference to point to the merged document
							// This will be replaced with the actual content during merging
							parts := strings.SplitN(refStr, "#", 2)
							if len(parts) > 1 {
								// Convert external reference to internal reference
								m[k] = "#" + parts[1]
							}
						}
					}
				}
			} else {
				// For other keys, recursively process values
				if err := processValue(v); err != nil {
					return err
				}
			}
		}

		return nil
	}

	// Define the function for recursive value processing
	processValue = func(v interface{}) error {
		switch vt := v.(type) {
		case map[string]interface{}:
			return processMap(vt)
		case []interface{}:
			for _, item := range vt {
				if err := processValue(item); err != nil {
					return err
				}
			}
		}
		return nil
	}

	return processMap(api)
}

// resolveRef resolves a reference relative to the current file.
// It processes both absolute and relative references.
//
// Parameters:
//   - ref: The reference to resolve
//   - currentFilePath: The path of the current file
//
// Returns:
//   - string: The resolved absolute path to the referenced file
//   - error: Any error that occurred during resolution
func resolveRef(ref, currentFilePath string) (string, error) {
	if strings.HasPrefix(ref, "#") {
		return "", nil
	}

	var relativePath string
	if strings.Contains(ref, "#") {
		refParts := strings.SplitN(ref, "#", 2)
		relativePath = refParts[0]
	} else {
		relativePath = ref
	}

	if relativePath == "" {
		return "", nil
	}

	if !filepath.IsAbs(relativePath) {
		basePath := filepath.Dir(currentFilePath)
		absolutePath := filepath.Join(basePath, relativePath)
		return filepath.Clean(absolutePath), nil
	}

	return relativePath, nil
}

// checkForRefs checks and processes references in components.
// It updates references to use the correct format.
//
// Parameters:
//   - data: The data to check for references
//
// Returns:
//   - error: Any error that occurred during processing
func checkForRefs(data interface{}) error {
	switch v := data.(type) {
	case map[string]interface{}:
		if ref, ok := v["$ref"].(string); ok {
			if strings.Contains(ref, "#") {
				v["$ref"] = "#" + strings.SplitN(ref, "#", 2)[1]
			}
		}

		for _, value := range v {
			if err := checkForRefs(value); err != nil {
				return err
			}
		}

	case []interface{}:
		for _, item := range v {
			if err := checkForRefs(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// isMapOfInterfaces checks if a value is a map[string]interface{}
func isMapOfInterfaces(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

// processPathItem processes a path item object and its operations
func processPathItem(pathItemNode *yaml.Node, pathItem map[string]interface{}, urlsToParse map[string]bool, currentFilePath string) error {
	// Check for references in the path item
	if err := checkForRefs(pathItem); err != nil {
		return fmt.Errorf("failed to check for references in path item: %v", err)
	}

	// Process operations (HTTP methods)
	httpMethods := []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	for _, method := range httpMethods {
		if operation, ok := pathItem[method].(map[string]interface{}); ok {
			// Find the operation node in the pathItemNode
			var operationNode *yaml.Node
			for i := 0; i < len(pathItemNode.Content); i += 2 {
				if i+1 < len(pathItemNode.Content) && pathItemNode.Content[i].Value == method {
					operationNode = pathItemNode.Content[i+1]
					break
				}
			}

			if operationNode != nil {
				if err := processOperation(operationNode, operation, urlsToParse, currentFilePath); err != nil {
					return fmt.Errorf("failed to process %s operation: %v", method, err)
				}
			}
		}
	}

	// Process parameters at the path level
	if parameters, ok := pathItem["parameters"].([]interface{}); ok {
		for _, param := range parameters {
			if paramMap, ok := param.(map[string]interface{}); ok {
				if err := checkForRefs(paramMap); err != nil {
					return fmt.Errorf("failed to check for references in path parameter: %v", err)
				}
			}
		}
	}

	return nil
}
