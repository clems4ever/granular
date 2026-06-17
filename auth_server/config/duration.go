package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from a YAML string such as "30s".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string (e.g. "2m") into a Duration.
//
// @arg value The YAML node holding the duration string.
// @error error when the node is not a string or not a valid Go duration.
//
// @testcase TestDurationUnmarshalInvalid rejects a malformed duration.
// @testcase TestLoadParsesYAML parses a duration from a config file.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the value as a standard time.Duration.
//
// @return time.Duration The duration value.
//
// @testcase TestDurationStd converts a Duration back to time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}
