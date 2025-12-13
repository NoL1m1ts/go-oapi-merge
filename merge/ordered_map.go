package merge

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// OrderedMap preserves key order when working with YAML maps
type OrderedMap struct {
	Keys   []string
	Values map[string]interface{}
}

// NewOrderedMap creates a new empty OrderedMap
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		Keys:   []string{},
		Values: make(map[string]interface{}),
	}
}

// Set adds or updates a key-value pair
func (om *OrderedMap) Set(key string, value interface{}) {
	if om.Values == nil {
		om.Values = make(map[string]interface{})
	}
	if _, exists := om.Values[key]; !exists {
		om.Keys = append(om.Keys, key)
	}
	om.Values[key] = value
}

// Get retrieves a value by key
func (om *OrderedMap) Get(key string) (interface{}, bool) {
	if om.Values == nil {
		return nil, false
	}
	val, ok := om.Values[key]
	return val, ok
}

// Delete removes a key-value pair
func (om *OrderedMap) Delete(key string) {
	if om.Values == nil {
		return
	}
	delete(om.Values, key)
	for i, k := range om.Keys {
		if k == key {
			om.Keys = append(om.Keys[:i], om.Keys[i+1:]...)
			break
		}
	}
}

// Len returns the number of key-value pairs
func (om *OrderedMap) Len() int {
	return len(om.Keys)
}

// Iterate calls the provided function for each key-value pair in order
func (om *OrderedMap) Iterate(fn func(key string, value interface{}) error) error {
	for _, key := range om.Keys {
		if err := fn(key, om.Values[key]); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler to preserve key order
func (om *OrderedMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping node, got %v", node.Kind)
	}

	om.Keys = []string{}
	om.Values = make(map[string]interface{})

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		key := keyNode.Value

		var value interface{}
		switch valueNode.Kind {
		case yaml.MappingNode:
			nestedMap := NewOrderedMap()
			if err := nestedMap.UnmarshalYAML(valueNode); err != nil {
				return err
			}
			value = nestedMap
		case yaml.SequenceNode:
			var seq []interface{}
			for _, itemNode := range valueNode.Content {
				var item interface{}
				if itemNode.Kind == yaml.MappingNode {
					nestedMap := NewOrderedMap()
					if err := nestedMap.UnmarshalYAML(itemNode); err != nil {
						return err
					}
					item = nestedMap
				} else {
					if err := itemNode.Decode(&item); err != nil {
						return err
					}
				}
				seq = append(seq, item)
			}
			value = seq
		default:
			if err := valueNode.Decode(&value); err != nil {
				return err
			}
		}

		om.Keys = append(om.Keys, key)
		om.Values[key] = value
	}

	return nil
}

// MarshalYAML implements yaml.Marshaler to preserve key order
func (om *OrderedMap) MarshalYAML() (interface{}, error) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	for _, key := range om.Keys {
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: key,
		}

		value := om.Values[key]
		var valueNode yaml.Node
		if err := valueNode.Encode(value); err != nil {
			return nil, err
		}

		node.Content = append(node.Content, keyNode, &valueNode)
	}

	return node, nil
}

// ToMap converts OrderedMap to a regular map[string]interface{}
func (om *OrderedMap) ToMap() map[string]interface{} {
	if om == nil {
		return nil
	}
	result := make(map[string]interface{})
	for _, key := range om.Keys {
		value := om.Values[key]
		if nestedOM, ok := value.(*OrderedMap); ok {
			result[key] = nestedOM.ToMap()
		} else if seq, ok := value.([]interface{}); ok {
			result[key] = convertSlice(seq)
		} else {
			result[key] = value
		}
	}
	return result
}

func convertSlice(seq []interface{}) []interface{} {
	result := make([]interface{}, len(seq))
	for i, item := range seq {
		if om, ok := item.(*OrderedMap); ok {
			result[i] = om.ToMap()
		} else if nestedSeq, ok := item.([]interface{}); ok {
			result[i] = convertSlice(nestedSeq)
		} else {
			result[i] = item
		}
	}
	return result
}

// FromMap creates an OrderedMap from a regular map
func FromMap(m map[string]interface{}) *OrderedMap {
	if m == nil {
		return nil
	}
	om := NewOrderedMap()
	for k, v := range m {
		if nestedMap, ok := v.(map[string]interface{}); ok {
			om.Set(k, FromMap(nestedMap))
		} else if seq, ok := v.([]interface{}); ok {
			om.Set(k, convertSliceFromMap(seq))
		} else {
			om.Set(k, v)
		}
	}
	return om
}

func convertSliceFromMap(seq []interface{}) []interface{} {
	result := make([]interface{}, len(seq))
	for i, item := range seq {
		if m, ok := item.(map[string]interface{}); ok {
			result[i] = FromMap(m)
		} else if nestedSeq, ok := item.([]interface{}); ok {
			result[i] = convertSliceFromMap(nestedSeq)
		} else {
			result[i] = item
		}
	}
	return result
}
