package reconcile

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration with YAML/JSON marshaling support.
// It accepts Go duration strings ("5s", "2m30s") and bare seconds ("5", "30").
type Duration struct {
	Duration time.Duration
}

// IsZero returns true if the duration is zero, supporting omitempty in YAML/JSON.
func (d Duration) IsZero() bool {
	return d.Duration == 0
}

// UnmarshalJSON handles both string ("5s") and number (5 as seconds) formats.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return d.parse(s)
	}

	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		d.Duration = time.Duration(n * float64(time.Second))
		return nil
	}

	return fmt.Errorf("duration must be a string (\"5s\") or number (seconds): %s", string(b))
}

// MarshalJSON writes the duration as a Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalYAML handles string values like "5s" or bare seconds "5".
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("duration must be a string (\"5s\" or \"5\"): %w", err)
	}
	return d.parse(s)
}

// MarshalYAML writes the duration as a Go duration string.
func (d Duration) MarshalYAML() (any, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Duration.String(), nil
}

// parse interprets a string as a Go duration or bare seconds.
func (d *Duration) parse(s string) error {
	if s == "" {
		d.Duration = 0
		return nil
	}

	if parsed, err := time.ParseDuration(s); err == nil {
		d.Duration = parsed
		return nil
	}

	// Treat as bare seconds.
	if parsed, err := time.ParseDuration(s + "s"); err == nil {
		d.Duration = parsed
		return nil
	}

	return fmt.Errorf("invalid duration %q: use Go duration (\"5s\", \"2m\") or bare seconds (\"5\")", s)
}
