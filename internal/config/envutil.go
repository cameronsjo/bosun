package config

import (
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// BosunEnv returns the value of the BOSUN_<name> env var, falling back to
// the unprefixed legacy <name> if the BOSUN_ form is unset or empty.
// Returns an empty string if neither is set.
func BosunEnv(name string) string {
	v, _ := bosunEnvWithSource(name)
	return v
}

// bosunEnvWithSource returns the resolved value and the name of the env var
// that actually provided it ("BOSUN_<name>" or "<name>"), so callers can log
// the correct source rather than always attributing the value to BOSUN_<name>.
// Returns ("", "") when neither var is set.
func bosunEnvWithSource(name string) (value, source string) {
	bosunKey := "BOSUN_" + name
	if v := getEnvOrDefault(bosunKey, ""); v != "" {
		return v, bosunKey
	}
	if v := getEnvOrDefault(name, ""); v != "" {
		return v, name
	}
	return "", ""
}

// BosunEnvBool parses BosunEnv(name) as a boolean with consistent semantics:
//
//	"1", "true", "yes", "on"          -> true  (case-insensitive)
//	"0", "false", "no", "off", ""     -> false (case-insensitive)
//	anything else                     -> defaultVal, with a debug-level warning
func BosunEnvBool(name string, defaultVal bool) bool {
	v, src := bosunEnvWithSource(name)
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Debug().
			Str("env", src).
			Str("value", v).
			Bool("default", defaultVal).
			Msg("Unrecognized boolean value; using default")
		return defaultVal
	}
}

// BosunEnvDuration parses BosunEnv(name) as a time.Duration.
// Accepts bare integers (treated as seconds) for backward compatibility with
// legacy POLL_INTERVAL config that used raw seconds — equivalent to appending
// "s" before parsing (e.g. "3600" -> "3600s" -> 1h0m0s).
// Returns defaultVal on parse error or when the var is unset.
func BosunEnvDuration(name string, defaultVal time.Duration) time.Duration {
	v, src := bosunEnvWithSource(name)
	if v == "" {
		return defaultVal
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// Bare-integer fallback: "3600" is interpreted as seconds for backward
	// compat with legacy POLL_INTERVAL config that used raw seconds.
	// This is the same convention as time.ParseDuration("3600s").
	if d, err := time.ParseDuration(v + "s"); err == nil {
		return d
	}
	log.Debug().
		Str("env", src).
		Str("value", v).
		Msg("Unrecognized duration value; using default")
	return defaultVal
}
