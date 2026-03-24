package reconcile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigField_ZeroValue(t *testing.T) {
	var f ConfigField[string]
	assert.Equal(t, "", f.Value)
	assert.Equal(t, SourceDefault, f.Source)
	assert.False(t, f.FromEnv())
}

func TestConfigField_SetFromEnv(t *testing.T) {
	var f ConfigField[time.Duration]
	f.SetFromEnv(5 * time.Second)
	assert.Equal(t, 5*time.Second, f.Value)
	assert.Equal(t, SourceEnv, f.Source)
	assert.True(t, f.FromEnv())
}

func TestConfigField_SetFromFile(t *testing.T) {
	var f ConfigField[[]string]
	f.SetFromFile([]string{"a", "b"})
	assert.Equal(t, []string{"a", "b"}, f.Value)
	assert.Equal(t, SourceFile, f.Source)
	assert.False(t, f.FromEnv())
}

func TestConfigField_Constructors(t *testing.T) {
	t.Run("NewConfigField", func(t *testing.T) {
		f := NewConfigField(42)
		assert.Equal(t, 42, f.Value)
		assert.Equal(t, SourceDefault, f.Source)
	})

	t.Run("EnvConfigField", func(t *testing.T) {
		f := EnvConfigField(true)
		assert.True(t, f.Value)
		assert.Equal(t, SourceEnv, f.Source)
	})

	t.Run("FileConfigField", func(t *testing.T) {
		f := FileConfigField([]string{"x"})
		assert.Equal(t, []string{"x"}, f.Value)
		assert.Equal(t, SourceFile, f.Source)
	})
}

func TestConfigField_ShallowCopy(t *testing.T) {
	orig := ConfigField[[]string]{Value: []string{"a", "b"}, Source: SourceEnv}
	cp := orig
	assert.Equal(t, orig.Value, cp.Value)
	assert.Equal(t, orig.Source, cp.Source)

	// Shallow copy shares backing array — mutation visible in both.
	cp.Value[0] = "changed"
	assert.Equal(t, "changed", orig.Value[0], "shallow copy shares backing array")
}

func TestReloadField_EnvBlocksReload(t *testing.T) {
	f := EnvConfigField([]string{"from-env"})
	updated := reloadField(&f, []string{"from-file"}, func(v []string) bool { return v != nil })
	assert.False(t, updated)
	assert.Equal(t, []string{"from-env"}, f.Value)
	assert.Equal(t, SourceEnv, f.Source)
}

func TestReloadField_NilSkipsReload(t *testing.T) {
	f := FileConfigField([]string{"existing"})
	updated := reloadField(&f, nil, func(v []string) bool { return v != nil })
	assert.False(t, updated)
	assert.Equal(t, []string{"existing"}, f.Value)
}

func TestReloadField_AppliesValue(t *testing.T) {
	f := NewConfigField([]string{"old"})
	updated := reloadField(&f, []string{"new"}, func(v []string) bool { return v != nil })
	assert.True(t, updated)
	assert.Equal(t, []string{"new"}, f.Value)
	assert.Equal(t, SourceFile, f.Source)
}

func TestReloadField_Duration(t *testing.T) {
	f := NewConfigField(time.Duration(0))
	updated := reloadField(&f, 5*time.Second, func(v time.Duration) bool { return v > 0 })
	assert.True(t, updated)
	assert.Equal(t, 5*time.Second, f.Value)
	assert.Equal(t, SourceFile, f.Source)
}

func TestReloadField_DurationZeroSkips(t *testing.T) {
	f := NewConfigField(3 * time.Second)
	updated := reloadField(&f, time.Duration(0), func(v time.Duration) bool { return v > 0 })
	assert.False(t, updated)
	assert.Equal(t, 3*time.Second, f.Value)
}
