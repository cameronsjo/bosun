package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serviceWith builds a compose document with one service, "web", holding key.
func serviceWith(key string, value any) map[string]any {
	return map[string]any{
		"services": map[string]any{
			"web": map[string]any{key: value},
		},
	}
}

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
				"endpoints": []any{"ep1", "ep2", "ep3"},
			},
		},
		{
			name: "list extend endpoints keeps map elements",
			base: map[string]any{
				"endpoints": []any{
					map[string]any{"name": "a", "url": "https://a.example.com"},
				},
			},
			overlay: map[string]any{
				"endpoints": []any{
					map[string]any{"name": "b", "url": "https://b.example.com"},
				},
			},
			want: map[string]any{
				"endpoints": []any{
					map[string]any{"name": "a", "url": "https://a.example.com"},
					map[string]any{"name": "b", "url": "https://b.example.com"},
				},
			},
		},
		{
			name:    "list extend endpoints string slice base",
			base:    map[string]any{"endpoints": []string{"ep1"}},
			overlay: map[string]any{"endpoints": []any{"ep2"}},
			want:    map[string]any{"endpoints": []any{"ep1", "ep2"}},
		},
		{
			name:    "service environment list plus list",
			base:    serviceWith("environment", []any{"A=1"}),
			overlay: serviceWith("environment", []any{"B=2"}),
			want:    serviceWith("environment", map[string]any{"A": "1", "B": "2"}),
		},
		{
			name:    "service environment list overlay wins on shared key",
			base:    serviceWith("environment", []any{"A=base", "B=2"}),
			overlay: serviceWith("environment", []any{"A=overlay"}),
			want:    serviceWith("environment", map[string]any{"A": "overlay", "B": "2"}),
		},
		{
			name:    "service environment map plus list",
			base:    serviceWith("environment", map[string]any{"PORT": 8080, "B": "base"}),
			overlay: serviceWith("environment", []any{"B=2", "C=3"}),
			want:    serviceWith("environment", map[string]any{"PORT": "8080", "B": "2", "C": "3"}),
		},
		{
			name:    "service environment list plus map",
			base:    serviceWith("environment", []any{"A=1", "B=base"}),
			overlay: serviceWith("environment", map[string]any{"B": "2"}),
			want:    serviceWith("environment", map[string]any{"A": "1", "B": "2"}),
		},
		{
			name:    "service environment string slice plus list",
			base:    serviceWith("environment", []string{"A=1"}),
			overlay: serviceWith("environment", []any{"B=2"}),
			want:    serviceWith("environment", map[string]any{"A": "1", "B": "2"}),
		},
		{
			name:    "service environment bare key passes through as nil",
			base:    serviceWith("environment", []any{"FOO"}),
			overlay: serviceWith("environment", []any{"B=2"}),
			want:    serviceWith("environment", map[string]any{"FOO": nil, "B": "2"}),
		},
		{
			name:    "service environment map nil value stays nil",
			base:    serviceWith("environment", map[string]any{"FOO": nil}),
			overlay: serviceWith("environment", map[string]any{"B": "2"}),
			want:    serviceWith("environment", map[string]any{"FOO": nil, "B": "2"}),
		},
		{
			name:    "service labels list plus list",
			base:    serviceWith("labels", []any{"a.b=1"}),
			overlay: serviceWith("labels", []any{"c.d=2"}),
			want:    serviceWith("labels", map[string]any{"a.b": "1", "c.d": "2"}),
		},
		{
			name:    "service labels map plus list",
			base:    serviceWith("labels", map[string]any{"a.b": "1"}),
			overlay: serviceWith("labels", []any{"c.d=2"}),
			want:    serviceWith("labels", map[string]any{"a.b": "1", "c.d": "2"}),
		},
		{
			name:    "service environment on one side only stays a list",
			base:    serviceWith("image", "nginx"),
			overlay: serviceWith("environment", []any{"B=2"}),
			want: map[string]any{
				"services": map[string]any{
					"web": map[string]any{"image": "nginx", "environment": []any{"B=2"}},
				},
			},
		},
		{
			name:    "service environment scalar overlay replaces",
			base:    serviceWith("environment", []any{"A=1"}),
			overlay: serviceWith("environment", "not-a-list"),
			want:    serviceWith("environment", "not-a-list"),
		},
		{
			name: "service name containing dots",
			base: map[string]any{"services": map[string]any{
				"my.app": map[string]any{"environment": []any{"A=1"}},
			}},
			overlay: map[string]any{"services": map[string]any{
				"my.app": map[string]any{"environment": []any{"B=2"}},
			}},
			want: map[string]any{"services": map[string]any{
				"my.app": map[string]any{"environment": map[string]any{"A": "1", "B": "2"}},
			}},
		},
		{
			// Out of scope control: user config and chart values keep list replace.
			name:    "out of scope nested labels list is replaced",
			base:    map[string]any{"app": map[string]any{"labels": []any{"a", "b"}}},
			overlay: map[string]any{"app": map[string]any{"labels": []any{"c"}}},
			want:    map[string]any{"app": map[string]any{"labels": []any{"c"}}},
		},
		{
			name:    "out of scope top level environment list is replaced",
			base:    map[string]any{"environment": []any{"A=1"}},
			overlay: map[string]any{"environment": []any{"B=2"}},
			want:    map[string]any{"environment": []any{"B=2"}},
		},
		{
			// A map keyed directly by service name has trail [name], not [services, name].
			name: "out of scope service map without services key is replaced",
			base: map[string]any{
				"web": map[string]any{"environment": []any{"A=1"}},
			},
			overlay: map[string]any{
				"web": map[string]any{"environment": []any{"B=2"}},
			},
			want: map[string]any{
				"web": map[string]any{"environment": []any{"B=2"}},
			},
		},
		{
			name: "out of scope deeper service labels list is replaced",
			base: map[string]any{"services": map[string]any{"web": map[string]any{
				"deploy": map[string]any{"labels": []any{"a=1"}},
			}}},
			overlay: map[string]any{"services": map[string]any{"web": map[string]any{
				"deploy": map[string]any{"labels": []any{"b=2"}},
			}}},
			want: map[string]any{"services": map[string]any{"web": map[string]any{
				"deploy": map[string]any{"labels": []any{"b=2"}},
			}}},
		},
		{
			// Out of scope map plus map keeps the historical stringifying
			// normalization, nil included.
			name:    "out of scope environment maps are stringified",
			base:    map[string]any{"cfg": map[string]any{"environment": map[string]any{"PORT": 8080, "X": nil}}},
			overlay: map[string]any{"cfg": map[string]any{"environment": map[string]any{"Y": "1"}}},
			want: map[string]any{"cfg": map[string]any{
				"environment": map[string]any{"PORT": "8080", "X": "<nil>", "Y": "1"},
			}},
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
			name:    "nil overlay",
			base:    map[string]any{"key": "value"},
			overlay: nil,
			want:    map[string]any{"key": "value"},
		},
		{
			name:    "both nil",
			base:    nil,
			overlay: nil,
			want:    map[string]any{},
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
		{
			name:    "bare key keeps nil value",
			input:   []any{"FOO", "BAR=1"},
			keyName: "environment",
			want:    map[string]any{"FOO": nil, "BAR": "1"},
		},
		{
			name:    "bare key in string slice keeps nil value",
			input:   []string{"FOO"},
			keyName: "environment",
			want:    map[string]any{"FOO": nil},
		},
		{
			name:    "empty value after equals is kept",
			input:   []any{"FOO="},
			keyName: "environment",
			want:    map[string]any{"FOO": ""},
		},
		{
			name:    "nil list item is skipped",
			input:   []any{nil, "A=1"},
			keyName: "environment",
			want:    map[string]any{"A": "1"},
		},
		{
			name:    "map nil value stays nil",
			input:   map[string]any{"FOO": nil, "BAR": 2},
			keyName: "environment",
			want:    map[string]any{"FOO": nil, "BAR": "2"},
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
		name   string
		input  any
		want   []string
		wantOK bool
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
	base := serviceWith("environment", []any{"FOO=bar"})
	overlay := serviceWith("environment", []any{"BAZ=qux"})

	result := DeepMerge(base, overlay)

	got := result["services"].(map[string]any)["web"].(map[string]any)["environment"]
	assert.Equal(t, map[string]any{"FOO": "bar", "BAZ": "qux"}, got)
}

func TestDeepMerge_LabelsListToMap(t *testing.T) {
	base := serviceWith("labels", []any{"label.one=value1"})
	overlay := serviceWith("labels", []any{"label.two=value2"})

	result := DeepMerge(base, overlay)

	got := result["services"].(map[string]any)["web"].(map[string]any)["labels"]
	assert.Equal(t, map[string]any{"label.one": "value1", "label.two": "value2"}, got)
}

func TestDeepMerge_NoAliasingOfBase(t *testing.T) {
	t.Run("extend does not write into base backing array", func(t *testing.T) {
		// A []string with spare capacity: appending to it in place would write
		// the overlay element into the backing array the caller still holds.
		s := make([]string, 1, 8)
		s[0] = "a"
		base := map[string]any{"endpoints": s}
		overlay := map[string]any{"endpoints": []any{"b"}}

		result := DeepMerge(base, overlay)

		assert.Equal(t, []any{"a", "b"}, result["endpoints"])
		full := s[:cap(s)]
		assert.Equal(t, "", full[1], "overlay element leaked into base backing array")
		assert.Equal(t, []string{"a"}, base["endpoints"])
	})

	t.Run("untouched nested base values are not shared with result", func(t *testing.T) {
		base := map[string]any{
			"untouched": map[string]any{"inner": "original"},
			"list":      []any{"x"},
		}

		result := DeepMerge(base, map[string]any{"other": "value"})
		result["untouched"].(map[string]any)["inner"] = "modified"
		result["list"].([]any)[0] = "modified"

		assert.Equal(t, "original", base["untouched"].(map[string]any)["inner"])
		assert.Equal(t, "x", base["list"].([]any)[0])
	})

	t.Run("merged nested base maps are not shared with result", func(t *testing.T) {
		base := map[string]any{
			"outer": map[string]any{"keep": "base"},
		}
		overlay := map[string]any{
			"outer": map[string]any{"added": "overlay"},
		}

		result := DeepMerge(base, overlay)

		assert.Equal(t, map[string]any{"keep": "base"}, base["outer"])
		assert.Equal(t, map[string]any{"keep": "base", "added": "overlay"}, result["outer"])
	})
}

func TestDeepMerge_NoAliasingOfOverlay(t *testing.T) {
	overlay := map[string]any{
		"new":       map[string]any{"inner": "original"},
		"endpoints": []any{map[string]any{"name": "o"}},
	}
	base := map[string]any{"endpoints": []any{map[string]any{"name": "b"}}}

	result := DeepMerge(base, overlay)
	result["new"].(map[string]any)["inner"] = "modified"
	result["endpoints"].([]any)[1].(map[string]any)["name"] = "modified"

	assert.Equal(t, "original", overlay["new"].(map[string]any)["inner"])
	assert.Equal(t, "o", overlay["endpoints"].([]any)[0].(map[string]any)["name"])
}

func TestDeepMerge_PanicsOnExcessiveDepth(t *testing.T) {
	deep := map[string]any{}
	cursor := deep
	for i := 0; i <= MaxMergeDepth+1; i++ {
		next := map[string]any{}
		cursor["n"] = next
		cursor = next
	}

	assert.Panics(t, func() { DeepMerge(deep, nil) })
}

func TestNormalizeToDict_NoEquals(t *testing.T) {
	// A bare key (no equals sign) is a compose passthrough: the key stays,
	// with a nil value, so the container inherits it from the environment.
	input := []any{"no_equals_here", "valid=value"}
	result := normalizeToDict(input, "environment")

	assert.Equal(t, map[string]any{"no_equals_here": nil, "valid": "value"}, result)
}

func TestNormalizeToDict_EmptyKey(t *testing.T) {
	// Test items where equals is at position 0 (empty key)
	input := []any{"=empty_key"}
	result := normalizeToDict(input, "environment")

	// Should be skipped (idx > 0 check)
	assert.Empty(t, result)
}
