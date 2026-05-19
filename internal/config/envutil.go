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
	if v := getEnvOrDefault("BOSUN_"+name, ""); v != "" {
		return v
	}
	return getEnvOrDefault(name, "")
}

// BosunEnvBool parses BosunEnv(name) as a boolean with consistent semantics:
//
//	"1", "true", "yes", "on"          -> true  (case-insensitive)
//	"0", "false", "no", "off", ""     -> false (case-insensitive)
//	anything else                     -> defaultVal, with a debug-level warning
func BosunEnvBool(name string, defaultVal bool) bool {
	v := BosunEnv(name)
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
			Str("env", "BOSUN_"+name).
			Str("value", v).
			Bool("default", defaultVal).
			Msg("Unrecognized boolean value; using default")
		return defaultVal
	}
}

// BosunEnvDuration parses BosunEnv(name) as a time.Duration.
// Accepts bare integers (treated as seconds) for backward compatibility.
// Returns defaultVal on parse error or when the var is unset.
func BosunEnvDuration(name string, defaultVal time.Duration) time.Duration {
	v := BosunEnv(name)
	if v == "" {
		return defaultVal
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// Bare integer treated as seconds.
	if d, err := time.ParseDuration(v + "s"); err == nil {
		return d
	}
	log.Debug().
		Str("env", "BOSUN_"+name).
		Str("value", v).
		Msg("Unrecognized duration value; using default")
	return defaultVal
}
