package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		variables map[string]any
		want      string
		wantErr   bool
	}{
		{
			name:     "simple variable replacement",
			template: "Hello, ${name}!",
			variables: map[string]any{
				"name": "World",
			},
			want:    "Hello, World!",
			wantErr: false,
		},
		{
			name:     "multiple variables in one string",
			template: "image: ${registry}/${image}:${tag}",
			variables: map[string]any{
				"registry": "ghcr.io",
				"image":    "myapp",
				"tag":      "latest",
			},
			want:    "image: ghcr.io/myapp:latest",
			wantErr: false,
		},
		{
			name:     "missing variable returns error",
			template: "Hello, ${name}! Welcome to ${place}.",
			variables: map[string]any{
				"name": "World",
			},
			want:    "",
			wantErr: true,
		},
		{
			name:      "no variables returns unchanged",
			template:  "No variables here",
			variables: map[string]any{},
			want:      "No variables here",
			wantErr:   false,
		},
		{
			name:     "nested braces do not break",
			template: "${outer} with ${inner} and extra ${outer}",
			variables: map[string]any{
				"outer": "foo",
				"inner": "bar",
			},
			want:    "foo with bar and extra foo",
			wantErr: false,
		},
		{
			name:     "integer variable",
			template: "port: ${port}",
			variables: map[string]any{
				"port": 8080,
			},
			want:    "port: 8080",
			wantErr: false,
		},
		{
			name:     "boolean variable",
			template: "enabled: ${enabled}",
			variables: map[string]any{
				"enabled": true,
			},
			want:    "enabled: true",
			wantErr: false,
		},
		{
			name:     "float variable",
			template: "ratio: ${ratio}",
			variables: map[string]any{
				"ratio": 3.14,
			},
			want:    "ratio: 3.14",
			wantErr: false,
		},
		{
			name:      "empty template",
			template:  "",
			variables: map[string]any{},
			want:      "",
			wantErr:   false,
		},
		{
			name:     "variable at start and end",
			template: "${prefix}middle${suffix}",
			variables: map[string]any{
				"prefix": "start-",
				"suffix": "-end",
			},
			want:    "start-middle-end",
			wantErr: false,
		},
		{
			name:     "dollar sign without braces preserved",
			template: "$name and ${actual}",
			variables: map[string]any{
				"actual": "value",
			},
			want:    "$name and value",
			wantErr: false,
		},
		{
			name:     "multiple missing variables",
			template: "${foo} and ${bar} and ${baz}",
			variables: map[string]any{
				"bar": "present",
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Interpolate(tt.template, tt.variables)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInterpolateMap(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string]any
		variables map[string]any
		want      map[string]any
		wantErr   bool
	}{
		{
			name: "simple map interpolation",
			data: map[string]any{
				"image": "${image}",
				"name":  "${name}",
			},
			variables: map[string]any{
				"image": "myapp:latest",
				"name":  "myapp",
			},
			want: map[string]any{
				"image": "myapp:latest",
				"name":  "myapp",
			},
			wantErr: false,
		},
		{
			name: "nested map interpolation",
			data: map[string]any{
				"service": map[string]any{
					"name":  "${name}",
					"image": "${image}",
				},
			},
			variables: map[string]any{
				"name":  "test",
				"image": "test:v1",
			},
			want: map[string]any{
				"service": map[string]any{
					"name":  "test",
					"image": "test:v1",
				},
			},
			wantErr: false,
		},
		{
			name: "list interpolation",
			data: map[string]any{
				"items": []any{"${a}", "${b}", "${c}"},
			},
			variables: map[string]any{
				"a": "first",
				"b": "second",
				"c": "third",
			},
			want: map[string]any{
				"items": []any{"first", "second", "third"},
			},
			wantErr: false,
		},
		{
			name: "non-string values preserved",
			data: map[string]any{
				"count": 42,
				"rate":  3.14,
				"flag":  true,
			},
			variables: map[string]any{},
			want: map[string]any{
				"count": 42,
				"rate":  3.14,
				"flag":  true,
			},
			wantErr: false,
		},
		{
			name: "missing variable in nested map",
			data: map[string]any{
				"outer": map[string]any{
					"inner": "${missing}",
				},
			},
			variables: map[string]any{},
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InterpolateMap(tt.data, tt.variables)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(100), "100"},
		{"uint", uint(50), "50"},
		{"uint8", uint8(8), "8"},
		{"uint16", uint16(16), "16"},
		{"uint32", uint32(32), "32"},
		{"uint64", uint64(64), "64"},
		{"float32", float32(3.14), "3.14"},
		{"float64", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "<nil>"},
		{"slice", []string{"a", "b"}, "[a b]"},
		{"struct", struct{ Name string }{"test"}, "{test}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInterpolateMap_ErrorInList(t *testing.T) {
	// Test error propagation from list interpolation
	data := map[string]any{
		"items": []any{"${missing}"},
	}
	variables := map[string]any{}

	_, err := InterpolateMap(data, variables)
	require.Error(t, err)
}

func TestInterpolateValue_NonStringNonMapNonList(t *testing.T) {
	// Test passthrough of non-string, non-map, non-list values
	data := map[string]any{
		"number": 42,
		"float":  3.14,
		"bool":   true,
		"nil":    nil,
	}
	variables := map[string]any{}

	result, err := InterpolateMap(data, variables)
	require.NoError(t, err)
	assert.Equal(t, 42, result["number"])
	assert.Equal(t, 3.14, result["float"])
	assert.Equal(t, true, result["bool"])
	assert.Nil(t, result["nil"])
}
