package config

import (
	"fmt"
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
	parsed, ok := ParseBoolStrict(v)
	if !ok {
		log.Debug().
			Str("env", src).
			Str("value", v).
			Bool("default", defaultVal).
			Msg("Unrecognized boolean value; using default")
		return defaultVal
	}
	return parsed
}

// ParseBoolStrict is the one boolean grammar: every caller in this package and
// ParseBoolValue go through it, so no second spelling can appear. The second
// return distinguishes "parsed as false" from "not a boolean", which a caller
// needs to log a rejected value rather than silently taking its default.
func ParseBoolStrict(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// ParseBoolValue parses an already-read string as a boolean, with the same
// spellings as BosunEnvBool:
//
//	"1", "true", "yes", "on"      -> true  (case-insensitive)
//	"0", "false", "no", "off"     -> false (case-insensitive)
//	anything else                 -> defaultVal
//
// It exists so the daemon and the one-shot CLI cannot drift on a variable
// they both read directly, such as DRY_RUN.
func ParseBoolValue(v string, defaultVal bool) bool {
	if parsed, ok := ParseBoolStrict(v); ok {
		return parsed
	}
	return defaultVal
}

// ParseDurationValue parses an already-read string as a duration, accepting a
// bare integer as seconds (legacy POLL_INTERVAL spelling). The bool reports
// whether it parsed. Shared for the same reason as ParseBoolValue.
func ParseDurationValue(v string) (time.Duration, bool) {
	if d, err := time.ParseDuration(v); err == nil {
		return d, true
	}
	if d, err := time.ParseDuration(v + "s"); err == nil {
		return d, true
	}
	return 0, false
}

// SplitAndTrim splits a comma-separated list, trims each entry and drops the
// empty ones. Shared by the daemon and the one-shot CLI so a list-valued
// variable parses the same on both paths.
func SplitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// SecretsFilesFromEnv resolves the secrets list from the environment. It
// prefers BOSUN_SECRETS_FILE and falls back to the legacy SECRETS_FILES,
// matching BosunEnv's precedence, and reads only the effective one: a
// malformed legacy value must not reject a valid preferred one.
//
// A variable that exists but names no file is an error, empty string
// included. An empty list skips SOPS entirely, so templates render with blank
// secret values and deploy; "no secrets" is expressed by leaving the variable
// unset, not by setting it empty. ok is false when neither is set.
func SecretsFilesFromEnv(lookup func(string) (string, bool)) (files []string, ok bool, err error) {
	for _, name := range []string{"BOSUN_SECRETS_FILE", "SECRETS_FILES"} {
		raw, present := lookup(name)
		if !present {
			continue
		}
		parsed := SplitAndTrim(raw)
		if len(parsed) == 0 {
			return nil, true, fmt.Errorf("%s is set but names no secrets file; unset it to run without secrets", name)
		}
		return parsed, true, nil
	}
	return nil, false, nil
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
	// Bare-integer fallback: "3600" is interpreted as seconds for backward
	// compat with legacy POLL_INTERVAL config that used raw seconds.
	if d, ok := ParseDurationValue(v); ok {
		return d
	}
	log.Debug().
		Str("env", src).
		Str("value", v).
		Msg("Unrecognized duration value; using default")
	return defaultVal
}
