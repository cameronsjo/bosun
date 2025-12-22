package manifest

import (
	"errors"
	"fmt"
	"strings"
)

// MaxMergeDepth is the maximum recursion depth for DeepMerge operations.
// This prevents stack overflow from deeply nested or circular structures.
const MaxMergeDepth = 100

// ErrMergeDepthExceeded indicates the merge operation exceeded the maximum depth.
var ErrMergeDepthExceeded = errors.New("merge depth exceeded maximum")

// UnionKeys are keys where lists use set-union merge (no duplicates).
var UnionKeys = map[string]bool{
	"networks":   true,
	"depends_on": true,
}

// ExtendKeys are keys where lists are extended (appended) instead of replaced.
var ExtendKeys = map[string]bool{
	"endpoints": true,
}

// DeepMerge recursively merges overlay into base and returns a new map.
// Merge semantics:
//   - UnionKeys (networks, depends_on): set union for lists
//   - ExtendKeys (endpoints): append lists
//   - Default: replace lists, recursive merge for dicts
//   - environment/labels are normalized from list to map before merging
//
// Panics if recursion depth exceeds MaxMergeDepth to prevent OOM.
func DeepMerge(base, overlay map[string]any) map[string]any {
	return deepMergeInternal(base, overlay, "", 0)
}

func deepMergeInternal(base, overlay map[string]any, path string, depth int) map[string]any {
	if depth > MaxMergeDepth {
		panic(fmt.Sprintf("DeepMerge: %v at path %q", ErrMergeDepthExceeded, path))
	}

	result := copyMap(base)

	for key, overlayValue := range overlay {
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		baseValue, exists := result[key]
		if !exists {
			result[key] = deepCopyWithDepth(overlayValue, depth+1)
			continue
		}

		// Both are maps - recursive merge
		baseMap, baseIsMap := baseValue.(map[string]any)
		overlayMap, overlayIsMap := overlayValue.(map[string]any)
		if baseIsMap && overlayIsMap {
			// Normalize environment/labels before dict merge
			if key == "environment" || key == "labels" {
				baseMap = normalizeToDict(baseMap, key)
				overlayMap = normalizeToDict(overlayMap, key)
			}
			result[key] = deepMergeInternal(baseMap, overlayMap, currentPath, depth+1)
			continue
		}

		// Both are lists - apply merge strategy
		baseList, baseIsList := toStringSlice(baseValue)
		overlayList, overlayIsList := toStringSlice(overlayValue)
		if baseIsList && overlayIsList {
			if UnionKeys[key] {
				// Set union - no duplicates
				result[key] = stringSliceUnion(baseList, overlayList)
			} else if ExtendKeys[key] {
				// Extend - append
				result[key] = append(baseList, overlayList...)
			} else {
				// Replace
				result[key] = deepCopyWithDepth(overlayValue, depth+1)
			}
			continue
		}

		// Default: replace
		result[key] = deepCopyWithDepth(overlayValue, depth+1)
	}

	return result
}

// normalizeToDict converts list-style environment/labels to dict format.
// Input: ["FOO=bar", "BAZ=qux"] -> {"FOO": "bar", "BAZ": "qux"}
// Input: {"FOO": "bar"} -> {"FOO": "bar"} (unchanged)
func normalizeToDict(value any, keyName string) map[string]any {
	if value == nil {
		return make(map[string]any)
	}

	// Already a map
	if m, ok := value.(map[string]any); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			result[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", v)
		}
		return result
	}

	// Convert list to map
	result := make(map[string]any)
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			s := fmt.Sprintf("%v", item)
			if idx := strings.Index(s, "="); idx > 0 {
				result[s[:idx]] = s[idx+1:]
			}
		}
	case []string:
		for _, item := range v {
			if idx := strings.Index(item, "="); idx > 0 {
				result[item[:idx]] = item[idx+1:]
			}
		}
	}

	return result
}

// toStringSlice attempts to convert a value to []string.
// Returns the slice and true if successful, nil and false otherwise.
func toStringSlice(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []any:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result, true
	default:
		return nil, false
	}
}

// stringSliceUnion returns the union of two string slices (no duplicates).
func stringSliceUnion(a, b []string) []any {
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]any, 0, len(a)+len(b))

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// deepCopy creates a deep copy of any value.
func deepCopy(value any) any {
	return deepCopyWithDepth(value, 0)
}

// deepCopyWithDepth creates a deep copy with depth tracking.
func deepCopyWithDepth(value any, depth int) any {
	if value == nil {
		return nil
	}

	if depth > MaxMergeDepth {
		panic(fmt.Sprintf("deepCopy: %v", ErrMergeDepthExceeded))
	}

	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			result[k] = deepCopyWithDepth(val, depth+1)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = deepCopyWithDepth(val, depth+1)
		}
		return result
	case []string:
		result := make([]string, len(v))
		copy(result, v)
		return result
	default:
		// Primitive types are immutable, return as-is
		return value
	}
}
