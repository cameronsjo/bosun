package reconcile

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"go duration string", `"5s"`, 5 * time.Second, false},
		{"go duration minutes", `"2m30s"`, 2*time.Minute + 30*time.Second, false},
		{"bare seconds string", `"30"`, 30 * time.Second, false},
		{"number as seconds", `5`, 5 * time.Second, false},
		{"fractional seconds", `2.5`, 2500 * time.Millisecond, false},
		{"zero string", `"0"`, 0, false},
		{"zero number", `0`, 0, false},
		{"empty string", `""`, 0, false},
		{"invalid string", `"not-a-duration"`, 0, true},
		{"boolean", `true`, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tc.input), &d)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, d.Duration)
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	d := Duration{Duration: 5 * time.Second}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, `"5s"`, string(data))
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"go duration string", "delay: 5s", 5 * time.Second, false},
		{"quoted duration", `delay: "2m30s"`, 2*time.Minute + 30*time.Second, false},
		{"bare seconds", "delay: 30", 30 * time.Second, false},
		{"quoted bare seconds", `delay: "30"`, 30 * time.Second, false},
		{"zero", "delay: 0", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				Delay Duration `yaml:"delay"`
			}
			err := yaml.Unmarshal([]byte(tc.input), &out)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, out.Delay.Duration)
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	t.Run("non-zero round-trips", func(t *testing.T) {
		in := struct {
			Delay Duration `yaml:"delay"`
		}{Delay: Duration{Duration: 5 * time.Second}}

		data, err := yaml.Marshal(in)
		require.NoError(t, err)
		assert.Contains(t, string(data), "5s")

		var out struct {
			Delay Duration `yaml:"delay"`
		}
		require.NoError(t, yaml.Unmarshal(data, &out))
		assert.Equal(t, 5*time.Second, out.Delay.Duration)
	})

	t.Run("zero omits with omitempty", func(t *testing.T) {
		in := struct {
			Delay Duration `yaml:"delay,omitempty"`
		}{Delay: Duration{Duration: 0}}

		data, err := yaml.Marshal(in)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "delay")
	})
}

func TestDuration_JSONRoundTrip(t *testing.T) {
	original := Duration{Duration: 2*time.Minute + 30*time.Second}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Duration
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, original.Duration, decoded.Duration)
}

func TestDuration_IsZero(t *testing.T) {
	assert.True(t, Duration{}.IsZero())
	assert.True(t, Duration{Duration: 0}.IsZero())
	assert.False(t, Duration{Duration: time.Second}.IsZero())
}

func TestDuration_UnmarshalYAML_Error(t *testing.T) {
	// YAML unmarshaling into Duration should fail for non-string types like map.
	var out struct {
		Delay Duration `yaml:"delay"`
	}
	err := yaml.Unmarshal([]byte("delay:\n  nested: value"), &out)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duration must be a string")
}

func TestDuration_MarshalYAML_ZeroReturnsNil(t *testing.T) {
	d := Duration{Duration: 0}
	result, err := d.MarshalYAML()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestDuration_MarshalYAML_NonZero(t *testing.T) {
	d := Duration{Duration: 10 * time.Second}
	result, err := d.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, "10s", result)
}

func TestDuration_YAMLRoundTrip(t *testing.T) {
	type wrapper struct {
		Delay Duration `yaml:"delay"`
	}

	original := wrapper{Delay: Duration{Duration: 3*time.Minute + 15*time.Second}}
	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var decoded wrapper
	require.NoError(t, yaml.Unmarshal(data, &decoded))
	assert.Equal(t, original.Delay.Duration, decoded.Delay.Duration)
}
