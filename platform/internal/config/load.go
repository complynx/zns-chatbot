package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v4"
)

const maxConfigBytes = 1 << 20
const stringTag = "!!str"
const trueValue = "true"

// Load applies defaults, one YAML document, legacy environment aliases, then
// canonical ZNS_ variables. It never reads a file or the process environment.
func Load(command string, data []byte, environ []string) (Config, error) {
	result := defaults(command)
	if len(data) > maxConfigBytes {
		return Config{}, errors.New("configuration exceeds 1 MiB")
	}
	if err := decodeYAML(data, &result); err != nil {
		return Config{}, err
	}
	if err := applyEnvironment(&result, environ); err != nil {
		return Config{}, err
	}
	if result.Orders.ActiveEvent == "" && result.Env == sandboxMode {
		result.Orders.ActiveEvent = "sandbox-festival"
	}
	if err := result.Validate(command); err != nil {
		return Config{}, err
	}
	return result, nil
}

func decodeYAML(data []byte, result *Config) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	parser := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := parser.Decode(&document); err != nil {
		return errors.New("invalid configuration YAML")
	}
	if !errors.Is(parser.Decode(new(yaml.Node)), io.EOF) {
		return errors.New("configuration requires exactly one YAML document")
	}
	if len(document.Content) != 1 {
		return errors.New("configuration requires a YAML mapping")
	}
	if err := checkMapping(document.Content[0], reflect.TypeFor[Config]()); err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(result); err != nil {
		return errors.New("configuration YAML has an invalid field or value")
	}
	return nil
}

func checkMapping(node *yaml.Node, shape reflect.Type) error {
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" || node.Anchor != "" {
		return errors.New("configuration requires plain YAML mappings")
	}
	fields := map[string]reflect.Type{}
	for field := range shape.Fields() {
		fields[field.Tag.Get("yaml")] = field.Type
	}
	seen := map[string]bool{}
	const entrySize = 2
	for i := 0; i < len(node.Content); i += entrySize {
		key, value := node.Content[i], node.Content[i+1]
		field, known := fields[key.Value]
		if key.Kind != yaml.ScalarNode || key.Tag != stringTag || key.Anchor != "" || !known {
			return errors.New("unknown YAML configuration field")
		}
		if seen[key.Value] {
			return errors.New("duplicate YAML configuration field")
		}
		seen[key.Value] = true
		if field.Kind() == reflect.Struct {
			if err := checkMapping(value, field); err != nil {
				return err
			}
			continue
		}
		if !validScalar(value, field) {
			return fmt.Errorf("configuration field %s has an invalid YAML type", key.Value)
		}
	}
	return nil
}

func validScalar(node *yaml.Node, field reflect.Type) bool {
	if node.Kind != yaml.ScalarNode || node.Anchor != "" {
		return false
	}
	if isDuration(field) {
		return node.Tag == stringTag
	}
	switch {
	case field.Kind() == reflect.String:
		return node.Tag == stringTag
	case field.Kind() == reflect.Bool:
		return node.Tag == "!!bool" && (node.Value == trueValue || node.Value == "false")
	case field.Kind() == reflect.Int:
		return node.Tag == "!!int"
	case field.Kind() == reflect.Float64:
		return node.Tag == "!!float" || node.Tag == "!!int"
	default:
		return false
	}
}

func fieldsOf(value reflect.Value, prefix string, result map[string]reflect.Value) {
	for i := range value.NumField() {
		field := value.Type().Field(i)
		name := prefix + strings.ToUpper(field.Tag.Get("yaml"))
		if field.Type.Kind() == reflect.Struct {
			fieldsOf(value.Field(i), name+"__", result)
		} else {
			result["ZNS_"+name] = value.Field(i)
		}
	}
}
