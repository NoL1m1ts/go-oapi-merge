package merge

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
	yamlv3 "gopkg.in/yaml.v3"
)

func decodeYAML(data []byte, destination any) error {
	data = normalizeYAMLDirective(data)
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	var document, extra yamlv3.Node
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("expected one YAML document, found multiple documents")
	}
	var validated any
	if err := document.Decode(&validated); err != nil {
		return err
	}
	value, err := orderedYAMLValue(&document)
	if err != nil {
		return err
	}
	switch dst := destination.(type) {
	case *any:
		*dst = value
		return nil
	case *yaml.MapSlice:
		if value == nil {
			*dst = nil
			return nil
		}
		mapping, ok := value.(yaml.MapSlice)
		if !ok {
			return fmt.Errorf("expected YAML mapping, got %T", value)
		}
		*dst = mapping
		return nil
	default:
		// Keep struct type validation while normalizing YAML-specific syntax.
		normalized, err := yaml.MarshalWithOptions(value, yaml.JSON())
		if err != nil {
			return err
		}
		return yaml.UnmarshalWithOptions(normalized, destination, yaml.UseOrderedMap())
	}
}

func normalizeYAMLDirective(data []byte) []byte {
	lines := bytes.SplitAfter(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), []byte("\n"))
	for i, line := range lines {
		text := strings.TrimSpace(string(line))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if !bytes.HasPrefix(line, []byte("%")) {
			break
		}
		fields := strings.Fields(text)
		if len(fields) >= 2 && fields[0] == "%YAML" && fields[1] == "1.2" {
			// yaml.v3 supports these values but only accepts the 1.1 directive.
			lines[i] = bytes.Replace(line, []byte("1.2"), []byte("1.1"), 1)
		}
	}
	return bytes.Join(lines, nil)
}

func orderedYAMLValue(node *yamlv3.Node) (any, error) {
	return (&yamlConversion{active: make(map[*yamlv3.Node]bool)}).value(node)
}

type yamlConversion struct {
	active     map[*yamlv3.Node]bool
	aliasNodes int
	aliasDepth int
}

func (c *yamlConversion) value(node *yamlv3.Node) (any, error) {
	if c.active[node] {
		return nil, fmt.Errorf("cyclic YAML alias")
	}
	c.active[node] = true
	defer delete(c.active, node)
	if c.aliasDepth > 0 {
		c.aliasNodes++
		if c.aliasNodes > 100000 {
			return nil, fmt.Errorf("excessive YAML alias expansion")
		}
	}
	switch node.Kind {
	case yamlv3.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return c.value(node.Content[0])
	case yamlv3.AliasNode:
		c.aliasDepth++
		defer func() { c.aliasDepth-- }()
		return c.value(node.Alias)
	case yamlv3.SequenceNode:
		values := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := c.value(child)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case yamlv3.MappingNode:
		explicit := make(map[string]bool)
		keys := make([]string, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Tag == "!!merge" {
				continue
			}
			if key.Kind == yamlv3.AliasNode {
				key = key.Alias
			}
			if key.Kind != yamlv3.ScalarNode {
				return nil, fmt.Errorf("YAML mapping keys must be scalars")
			}
			name := key.Value
			if explicit[name] {
				return nil, fmt.Errorf("duplicate YAML mapping key %q", name)
			}
			explicit[name] = true
			keys[i/2] = name
		}
		result := make(yaml.MapSlice, 0, len(node.Content)/2)
		inherited := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			value, err := c.value(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			if key.Tag != "!!merge" {
				result = append(result, yaml.MapItem{Key: keys[i/2], Value: value})
				continue
			}
			maps := []any{value}
			if sequence, ok := value.([]any); ok {
				maps = sequence
			}
			for _, source := range maps {
				mapping, ok := source.(yaml.MapSlice)
				if !ok {
					return nil, fmt.Errorf("YAML merge source must be a mapping")
				}
				for _, item := range mapping {
					name := item.Key.(string)
					if !explicit[name] && !inherited[name] {
						result = append(result, item)
						inherited[name] = true
					}
				}
			}
		}
		return result, nil
	default:
		if node.Tag == "!!timestamp" {
			return node.Value, nil
		}
		var value any
		err := node.Decode(&value)
		return value, err
	}
}

func marshalYAML(value any) ([]byte, error) {
	quoted, err := yaml.MarshalWithOptions(value, yaml.JSON())
	if err != nil {
		return nil, err
	}
	var node yamlv3.Node
	if err := yamlv3.Unmarshal(quoted, &node); err != nil {
		return nil, err
	}
	var blockStyle func(*yamlv3.Node)
	blockStyle = func(node *yamlv3.Node) {
		node.Style = 0
		if node.Kind == yamlv3.ScalarNode && node.Tag == "!!str" && strings.ContainsAny(node.Value, "\n\r\u2028\u2029") {
			node.Style = yamlv3.DoubleQuotedStyle
		}
		for _, child := range node.Content {
			blockStyle(child)
		}
	}
	blockStyle(&node)
	var output bytes.Buffer
	encoder := yamlv3.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
