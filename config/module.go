package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadModuleFile reads optional YAML settings into a module-owned struct.
// Initialize destination with defaults before calling. An empty path or missing
// file returns false without changing destination. An empty file keeps defaults.
// Other read errors, unknown fields, duplicate keys, and invalid documents fail.
// Decode errors report the first problem and its decoder-provided line when
// available, without including configuration keys or values.
// Discard destination on error because decoding may have assigned some fields.
//
// Destination must be a nonnil pointer to a struct with YAML field tags. The module
// validates its settings after loading and applying explicit overrides. Paths are
// used as supplied, without searching directories or reading environment variables.
func LoadModuleFile(path string, destination any) (bool, error) {
	v := reflect.ValueOf(destination)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return false, errors.New("module configuration requires a nonnil pointer to a struct")
	}
	if path == "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load module configuration: %w", err)
	}

	// Check the document shape before decoding. A null or scalar document must
	// not silently turn a misspelled configuration file into default settings.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
		return true, nil
	} else if err != nil {
		return true, moduleConfigError(path, err, "invalid YAML")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return true, fmt.Errorf("%s: module configuration must be a YAML mapping", path)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return true, fmt.Errorf("%s: module configuration must contain one YAML document", path)
	} else if !errors.Is(err, io.EOF) {
		return true, moduleConfigError(path, err, "invalid YAML")
	}

	decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return true, moduleConfigError(path, err, "invalid value")
	}
	return true, nil
}

// moduleConfigError reports the first decoder problem without quoting keys,
// values, tags, or custom unmarshaler errors. YAML exposes locations as text,
// so retain only a validated line number and a known error category.
func moduleConfigError(path string, err error, problem string) error {
	message := err.Error()
	var typeError *yaml.TypeError
	if errors.As(err, &typeError) && len(typeError.Errors) > 0 {
		message = typeError.Errors[0]
	}
	location, detail, ok := strings.Cut(strings.TrimPrefix(message, "yaml: line "), ": ")
	location = strings.TrimPrefix(location, "line ")
	line, parseErr := strconv.Atoi(location)
	if !ok || parseErr != nil || line < 1 {
		return fmt.Errorf("%s: module configuration: %s", path, problem)
	}

	if typeError != nil {
		switch {
		case strings.HasPrefix(detail, "field ") && strings.Contains(detail, " not found in type "):
			problem = "unknown field"
		case strings.HasPrefix(detail, "mapping key ") && strings.Contains(detail, " already defined at line "):
			problem = "duplicate key"
		}
	}
	return fmt.Errorf("%s: line %d: module configuration: %s", path, line, problem)
}
