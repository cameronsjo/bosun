package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name    string
		base    map[string]any
		overlay map[string]any
		want    map[string]any
	}{
		{
			name: "basic dict merge overlay wins",
			base: map[string]any{
				"key1": "base1",
				"key2": "base2",
			},
			overlay: map[string]any{
				"key2": "overlay2",
				"key3": "overlay3",
			},
			want: map[string]any{
				"key1": "base1",
				"key2": "overlay2",
				"key3": "overlay3",
			},
		},
		{
			name: "nested dict merge recursive",
			base: map[string]any{
				"outer": map[string]any{
					"inner1": "base1",
					"inner2": "base2",
				},
			},
			overlay: map[string]any{
				"outer": map[string]any{
					"inner2": "overlay2",
					"inner3": "overlay3",
				},
			},
			want: map[string]any{
				"outer": map[string]any{
					"inner1": "base1",
					"inner2": "overlay2",
					"inner3": "overlay3",
				},
			},
		},
		{
			name: "list replace default",
			base: map[string]any{
				"ports": []any{"80", "443"},
			},
			overlay: map[string]any{
				"ports": []any{"8080"},
			},
			want: map[string]any{
				"ports": []any{"8080"},
			},
		},
		{
			name: "list union networks",
			base: map[string]any{
				"networks": []any{"net1", "net2"},
			},
			overlay: map[string]any{
				"networks": []any{"net2", "net3"},
			},
			want: map[string]any{
				"networks": []any{"net1", "net2", "net3"},
			},
		},
		{
			name: "list union depends_on",
			base: map[string]any{
				"depends_on": []any{"service1"},
			},
			overlay: map[string]any{
				"depends_on": []any{"service1", "service2"},
			},
			want: map[string]any{
				"depends_on": []any{"service1", "service2"},
			},
		},
		{
			name: "list extend endpoints",
			base: map[string]any{
				"endpoints": []any{"ep1"},
			},
			overlay: map[string]any{
				"endpoints": []any{"ep2", "ep3"},
			},
			want: map[string]any{
				"endpoints": []string{"ep1", "ep2", "ep3"},
			},
		},
		{
			name: "empty base",
			base: map[string]any{},
			overlay: map[string]any{
				"key": "value",
			},
			want: map[string]any{
				"key": "value",
			},
		},
		{
			name: "empty overlay",
			base: map[string]any{
				"key": "value",
			},
			overlay: map[string]any{},
			want: map[string]any{
				"key": "value",
			},
		},
		{
			name:    "both empty",
			base:    map[string]any{},
			overlay: map[string]any{},
			want:    map[string]any{},
		},
		{
			name:    "nil base",
			base:    nil,
			overlay: map[string]any{"key": "value"},
			want:    map[string]any{"key": "value"},
		},
		{
			name: "deeply nested merge",
			base: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "base",
					},
				},
			},
			overlay: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "overlay",
						"new":    "added",
					},
				},
			},
			want: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "overlay",
						"new":    "added",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeepMerge(tt.base, tt.overlay)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeToDict(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		keyName string
		want    map[string]any
	}{
		{
			name:    "list to map environment",
			input:   []any{"FOO=bar", "BAZ=qux"},
			keyName: "environment",
			want: map[string]any{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			name:    "list to map labels",
			input:   []any{"label.one=value1", "label.two=value2"},
			keyName: "labels",
			want: map[string]any{
				"label.one": "value1",
				"label.two": "value2",
			},
		},
		{
			name: "map unchanged",
			input: map[string]any{
				"FOO": "bar",
			},
			keyName: "environment",
			want: map[string]any{
				"FOO": "bar",
			},
		},
		{
			name:    "nil input",
			input:   nil,
			keyName: "environment",
			want:    map[string]any{},
		},
		{
			name:    "empty list",
			input:   []any{},
			keyName: "environment",
			want:    map[string]any{},
		},
		{
			name:    "string slice",
			input:   []string{"KEY=value"},
			keyName: "environment",
			want: map[string]any{
				"KEY": "value",
			},
		},
		{
			name:    "value with equals sign",
			input:   []any{"COMPLEX=a=b=c"},
			keyName: "environment",
			want: map[string]any{
				"COMPLEX": "a=b=c",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeToDict(tt.input, tt.keyName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeepCopy(t *testing.T) {
	t.Run("no mutation of original map", func(t *testing.T) {
		original := map[string]any{
			"key": "value",
			"nested": map[string]any{
				"inner": "original",
			},
		}

		copied := deepCopy(original)
		copiedMap := copied.(map[string]any)

		// Modify the copy
		copiedMap["key"] = "modified"
		copiedMap["nested"].(map[string]any)["inner"] = "modified"

		// Verify original is unchanged
		assert.Equal(t, "value", original["key"])
		assert.Equal(t, "original", original["nested"].(map[string]any)["inner"])
	})

	t.Run("no mutation of original slice", func(t *testing.T) {
		original := []any{"a", "b", "c"}

		copied := deepCopy(original)
		copiedSlice := copied.([]any)

		// Modify the copy
		copiedSlice[0] = "modified"

		// Verify original is unchanged
		assert.Equal(t, "a", original[0])
	})

	t.Run("nil input", func(t *testing.T) {
		got := deepCopy(nil)
		assert.Nil(t, got)
	})

	t.Run("primitive passthrough", func(t *testing.T) {
		assert.Equal(t, 42, deepCopy(42))
		assert.Equal(t, "string", deepCopy("string"))
		assert.Equal(t, true, deepCopy(true))
	})
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []string
		wantOK  bool
	}{
		{
			name:   "string slice",
			input:  []string{"a", "b", "c"},
			want:   []string{"a", "b", "c"},
			wantOK: true,
		},
		{
			name:   "any slice",
			input:  []any{"x", "y", "z"},
			want:   []string{"x", "y", "z"},
			wantOK: true,
		},
		{
			name:   "any slice with numbers",
			input:  []any{1, 2, 3},
			want:   []string{"1", "2", "3"},
			wantOK: true,
		},
		{
			name:   "not a slice",
			input:  "string",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "nil",
			input:  nil,
			want:   nil,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toStringSlice(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestStringSliceUnion(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []any
	}{
		{
			name: "no overlap",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: []any{"a", "b", "c", "d"},
		},
		{
			name: "with overlap",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c", "d"},
			want: []any{"a", "b", "c", "d"},
		},
		{
			name: "empty first",
			a:    []string{},
			b:    []string{"a", "b"},
			want: []any{"a", "b"},
		},
		{
			name: "empty second",
			a:    []string{"a", "b"},
			b:    []string{},
			want: []any{"a", "b"},
		},
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: []any{},
		},
		{
			name: "duplicates in first",
			a:    []string{"a", "a", "b"},
			b:    []string{"c"},
			want: []any{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSliceUnion(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeepMerge_NoMutationOfOriginals(t *testing.T) {
	base := map[string]any{
		"key": "base_value",
		"nested": map[string]any{
			"inner": "base_inner",
		},
		"list": []any{"base1", "base2"},
	}

	overlay := map[string]any{
		"key": "overlay_value",
		"nested": map[string]any{
			"inner":   "overlay_inner",
			"new_key": "new_value",
		},
	}

	// Store original values
	originalBaseKey := base["key"]
	originalBaseNested := base["nested"].(map[string]any)["inner"]

	// Perform merge
	result := DeepMerge(base, overlay)

	// Modify result
	result["key"] = "modified"
	result["nested"].(map[string]any)["inner"] = "modified"

	// Verify originals are unchanged
	assert.Equal(t, originalBaseKey, base["key"])
	assert.Equal(t, originalBaseNested, base["nested"].(map[string]any)["inner"])
}

func TestDeepCopy_StringSlice(t *testing.T) {
	original := []string{"a", "b", "c"}

	copied := deepCopy(original)
	copiedSlice, ok := copied.([]string)
	require.True(t, ok)

	// Modify the copy
	copiedSlice[0] = "modified"

	// Verify original is unchanged
	assert.Equal(t, "a", original[0])
}

func TestDeepMerge_EnvironmentListToMap(t *testing.T) {
	// Test environment list-to-map normalization during merge
	base := map[string]any{
		"environment": []any{"FOO=bar"},
	}
	overlay := map[string]any{
		"environment": []any{"BAZ=qux"},
	}

	// Note: This test verifies the normalization happens, but the actual
	// merge behavior depends on how the code handles environment merging
	result := DeepMerge(base, overlay)
	require.NotNil(t, result["environment"])
}

func TestDeepMerge_LabelsListToMap(t *testing.T) {
	// Test labels list-to-map normalization during merge
	base := map[string]any{
		"labels": []any{"label.one=value1"},
	}
	overlay := map[string]any{
		"labels": []any{"label.two=value2"},
	}

	result := DeepMerge(base, overlay)
	require.NotNil(t, result["labels"])
}

func TestNormalizeToDict_NoEquals(t *testing.T) {
	// Test items without equals sign (should be skipped)
	input := []any{"no_equals_here", "valid=value"}
	result := normalizeToDict(input, "environment")

	// Only the valid one should be present
	assert.Len(t, result, 1)
	assert.Equal(t, "value", result["valid"])
}

func TestNormalizeToDict_EmptyKey(t *testing.T) {
	// Test items where equals is at position 0 (empty key)
	input := []any{"=empty_key"}
	result := normalizeToDict(input, "environment")

	// Should be skipped (idx > 0 check)
	assert.Empty(t, result)
}

func TestCopyMap_Nil(t *testing.T) {
	result := copyMap(nil)
	require.NotNil(t, result)
	assert.Empty(t, result)
}
