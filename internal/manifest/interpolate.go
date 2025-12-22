package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

// varPattern matches ${varname} placeholders.
var varPattern = regexp.MustCompile(`\$\{(\w+)\}`)

// Interpolate replaces ${var} placeholders with values from the variables map.
// Returns an error if any referenced variable is missing.
// This function operates on raw strings BEFORE YAML parsing.
func Interpolate(template string, variables map[string]any) (string, error) {
	var missingVars []string

	result := varPattern.ReplaceAllStringFunc(template, func(match string) string {
		// Extract variable name from ${varname}
		key := varPattern.FindStringSubmatch(match)[1]

		value, ok := variables[key]
		if !ok {
			missingVars = append(missingVars, key)
			return match // Keep original if missing
		}

		return toString(value)
	})

	if len(missingVars) > 0 {
		return "", fmt.Errorf("missing variables: ${%s}", strings.Join(missingVars, "}, ${"))
	}

	return result, nil
}

// toString converts any value to its string representation.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// InterpolateMap applies interpolation to all string values in a map recursively.
// This is useful for post-processing parsed YAML when needed.
func InterpolateMap(data map[string]any, variables map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(data))

	for k, v := range data {
		interpolated, err := interpolateValue(v, variables)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		result[k] = interpolated
	}

	return result, nil
}

// interpolateValue recursively interpolates string values.
func interpolateValue(value any, variables map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		return Interpolate(v, variables)
	case map[string]any:
		return InterpolateMap(v, variables)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			interpolated, err := interpolateValue(item, variables)
			if err != nil {
				return nil, err
			}
			result[i] = interpolated
		}
		return result, nil
	default:
		return value, nil
	}
}
