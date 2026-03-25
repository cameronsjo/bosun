package reconcile

// ConfigSource tracks where a configuration value originated.
// Environment variables always take precedence over config file values
// during hot-reload — fields with SourceEnv are never overwritten.
type ConfigSource int

const (
	// SourceDefault indicates the field holds its zero value or a hardcoded default.
	SourceDefault ConfigSource = iota
	// SourceFile indicates the field was loaded from bosun.yaml.
	SourceFile
	// SourceEnv indicates the field was set via an environment variable.
	SourceEnv
)

// ConfigField tracks a configuration value alongside its origin.
// The zero value is safe: Value is T's zero value and Source is SourceDefault.
type ConfigField[T any] struct {
	Value  T
	Source ConfigSource
}

// FromEnv returns true if the field was set by an environment variable.
func (f ConfigField[T]) FromEnv() bool { return f.Source == SourceEnv }

// SetFromEnv sets the value and marks its source as environment.
func (f *ConfigField[T]) SetFromEnv(v T) {
	f.Value = v
	f.Source = SourceEnv
}

// SetFromFile sets the value and marks its source as config file.
func (f *ConfigField[T]) SetFromFile(v T) {
	f.Value = v
	f.Source = SourceFile
}

// NewConfigField creates a ConfigField with SourceDefault.
func NewConfigField[T any](v T) ConfigField[T] {
	return ConfigField[T]{Value: v}
}

// EnvConfigField creates a ConfigField with SourceEnv.
func EnvConfigField[T any](v T) ConfigField[T] {
	return ConfigField[T]{Value: v, Source: SourceEnv}
}

// FileConfigField creates a ConfigField with SourceFile.
func FileConfigField[T any](v T) ConfigField[T] {
	return ConfigField[T]{Value: v, Source: SourceFile}
}

// reloadField updates a ConfigField from a reloaded value if:
// 1. The field was not set from an environment variable, AND
// 2. The reloaded value passes the isSet predicate (non-nil for slices, non-zero for scalars).
//
// Returns true if the field was updated.
func reloadField[T any](field *ConfigField[T], reloaded T, isSet func(T) bool) bool {
	if field.FromEnv() || !isSet(reloaded) {
		return false
	}
	field.Value = reloaded
	field.Source = SourceFile
	return true
}
