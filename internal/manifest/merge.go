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
//   - ExtendKeys (endpoints): append lists; elements are kept as-is, so
//     endpoint maps survive intact
//   - Default: replace lists, recursive merge for dicts
//   - environment/labels of a compose service (services.<name>.environment,
//     services.<name>.labels) are normalized from list to map before merging,
//     so list and map forms merge with each other. Anywhere else (user config,
//     chart values) the values are merged like any other key.
//
// The result never aliases base or overlay: base is deep-copied once up front
// and merged into in place, and overlay values are deep-copied as they are
// taken.
//
// Panics if recursion depth exceeds MaxMergeDepth to prevent OOM. That includes
// a base nested deeper than MaxMergeDepth, which the up-front copy visits even
// where the overlay does not touch it.
func DeepMerge(base, overlay map[string]any) map[string]any {
	result, _ := deepCopyWithDepth(base, 0).(map[string]any)
	return deepMergeInternal(result, overlay, nil, 0)
}

// deepMergeInternal merges overlay into result in place and returns result.
// The caller owns result (it is a private copy); trail is the key path from
// the DeepMerge root to result.
func deepMergeInternal(result, overlay map[string]any, trail []string, depth int) map[string]any {
	if depth > MaxMergeDepth {
		panic(fmt.Sprintf("DeepMerge: %v at path %q", ErrMergeDepthExceeded, strings.Join(trail, ".")))
	}

	// Handle nil overlay - nothing to merge
	if overlay == nil {
		return result
	}

	for key, overlayValue := range overlay {
		baseValue, exists := result[key]
		if !exists {
			result[key] = deepCopyWithDepth(overlayValue, depth+1)
			continue
		}

		childTrail := make([]string, len(trail)+1)
		copy(childTrail, trail)
		childTrail[len(trail)] = key

		// Compose service environment/labels: accept list or dict on either side
		if isServiceDictKey(trail, key) && isDictOrList(baseValue) && isDictOrList(overlayValue) {
			result[key] = deepMergeInternal(
				normalizeToDict(baseValue, key),
				normalizeToDict(overlayValue, key),
				childTrail, depth+1)
			continue
		}

		// Both are maps - recursive merge
		baseMap, baseIsMap := baseValue.(map[string]any)
		overlayMap, overlayIsMap := overlayValue.(map[string]any)
		if baseIsMap && overlayIsMap {
			// Outside compose services environment/labels keep their historical
			// stringifying normalization
			if key == "environment" || key == "labels" {
				baseMap = stringifyDict(baseMap)
				overlayMap = stringifyDict(overlayMap)
			}
			result[key] = deepMergeInternal(baseMap, overlayMap, childTrail, depth+1)
			continue
		}

		// Both are lists - apply merge strategy
		switch {
		case UnionKeys[key]:
			baseList, baseOK := toStringSlice(baseValue)
			overlayList, overlayOK := toStringSlice(overlayValue)
			if baseOK && overlayOK {
				// Set union - no duplicates
				result[key] = stringSliceUnion(baseList, overlayList)
				continue
			}
		case ExtendKeys[key]:
			baseList, baseOK := toAnySlice(baseValue, depth+1)
			overlayList, overlayOK := toAnySlice(overlayValue, depth+1)
			if baseOK && overlayOK {
				// Extend - append, keeping elements untouched
				result[key] = append(baseList, overlayList...)
				continue
			}
		}

		// Default: replace
		result[key] = deepCopyWithDepth(overlayValue, depth+1)
	}

	return result
}

// isServiceDictKey reports whether key is an environment or labels entry of a
// compose service, that is, trail is exactly ["services", <name>].
func isServiceDictKey(trail []string, key string) bool {
	if key != "environment" && key != "labels" {
		return false
	}
	return len(trail) == 2 && trail[0] == "services"
}

// isDictOrList reports whether value is a shape normalizeToDict understands.
func isDictOrList(value any) bool {
	switch value.(type) {
	case map[string]any, []any, []string:
		return true
	default:
		return false
	}
}

// normalizeToDict converts list-style environment/labels to dict format.
// Input: ["FOO=bar", "BAZ=qux"] -> {"FOO": "bar", "BAZ": "qux"}
// Input: ["FOO"] -> {"FOO": nil} (compose passthrough: value comes from the shell)
// Input: {"FOO": "bar"} -> {"FOO": "bar"} (values stringified; nil stays nil)
// The result is always a new map.
func normalizeToDict(value any, keyName string) map[string]any {
	if value == nil {
		return make(map[string]any)
	}

	// Already a map
	if m, ok := value.(map[string]any); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			if v == nil {
				result[k] = nil
				continue
			}
			result[k] = fmt.Sprintf("%v", v)
		}
		return result
	}

	// Convert list to map
	result := make(map[string]any)
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if item == nil {
				continue
			}
			addListEntry(result, fmt.Sprintf("%v", item))
		}
	case []string:
		for _, item := range v {
			addListEntry(result, item)
		}
	}

	return result
}

// addListEntry parses one "KEY=value" or bare "KEY" list entry into result.
// An entry with an empty key ("=value") or no text is skipped.
func addListEntry(result map[string]any, entry string) {
	idx := strings.Index(entry, "=")
	switch {
	case idx > 0:
		result[entry[:idx]] = entry[idx+1:]
	case idx < 0 && entry != "":
		result[entry] = nil
	}
}

// stringifyDict converts every value of a dict to its string form, nil
// included ("<nil>"). It is the pre-existing normalization for environment and
// labels maps outside compose services, kept unchanged so user config and
// chart values merge as before.
func stringifyDict(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", v)
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

// toAnySlice converts a []any or []string to a fresh []any whose elements are
// deep copies, so the result can be appended to without touching the input.
// Elements keep their type: endpoint maps stay maps.
func toAnySlice(value any, depth int) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		copied, _ := deepCopyWithDepth(v, depth).([]any)
		return copied, true
	case []string:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
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
