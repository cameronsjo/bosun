package reconcile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		file    string
		want    bool
	}{
		{"exact match", "config.yaml", "config.yaml", true},
		{"exact no match", "config.yaml", "other.yaml", false},
		{"star glob", "*.yml", "compose.yml", true},
		{"star glob no match", "*.yml", "compose.yaml", false},
		{"doublestar prefix", "traefik/conf.d/**", "traefik/conf.d/dynamic.yml", true},
		{"doublestar nested", "traefik/conf.d/**", "traefik/conf.d/sub/deep.yml", true},
		{"doublestar no match", "traefik/conf.d/**", "gatus/config.yaml", false},
		{"doublestar root", "**", "anything/at/all.txt", true},
		{"simple dir match", "traefik/**", "traefik/traefik.yml", true},
		{"dir boundary", "traefik/**", "traefik-other/file.yml", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchGlob(tc.pattern, tc.file))
		})
	}
}

func TestEvaluatePostSyncHooks(t *testing.T) {
	hooks := []PostSyncHook{
		{Paths: []string{"traefik/conf.d/**"}, Action: "restart", Container: "traefik"},
		{Paths: []string{"gatus/config.yaml"}, Action: "restart", Container: "gatus"},
		{Paths: []string{"prometheus/**"}, Action: "restart", Container: "prometheus"},
	}

	t.Run("matches traefik hook", func(t *testing.T) {
		changed := []string{"traefik/conf.d/dynamic.yml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 1)
		assert.Equal(t, "traefik", matched[0].Container)
	})

	t.Run("matches multiple hooks", func(t *testing.T) {
		changed := []string{"traefik/conf.d/routers.yml", "gatus/config.yaml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 2)
	})

	t.Run("no matches for unrelated files", func(t *testing.T) {
		changed := []string{"docker-compose.yml", "README.md"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Empty(t, matched)
	})

	t.Run("empty changed files returns nil", func(t *testing.T) {
		matched := EvaluatePostSyncHooks(nil, hooks)
		assert.Nil(t, matched)
	})

	t.Run("empty hooks returns nil", func(t *testing.T) {
		matched := EvaluatePostSyncHooks([]string{"anything.txt"}, nil)
		assert.Nil(t, matched)
	})

	t.Run("deduplicates by container", func(t *testing.T) {
		dupeHooks := []PostSyncHook{
			{Paths: []string{"traefik/conf.d/**"}, Action: "restart", Container: "traefik"},
			{Paths: []string{"traefik/traefik.yml"}, Action: "restart", Container: "traefik"},
		}
		changed := []string{"traefik/conf.d/x.yml", "traefik/traefik.yml"}
		matched := EvaluatePostSyncHooks(changed, dupeHooks)
		assert.Len(t, matched, 1)
	})

	t.Run("preserves delay field on matched hooks", func(t *testing.T) {
		hooksWithDelay := []PostSyncHook{
			{
				Paths:     []string{"traefik/conf.d/**"},
				Action:    "restart",
				Container: "traefik",
				Delay:     Duration{Duration: 5 * time.Second},
			},
			{
				Paths:     []string{"gatus/config.yaml"},
				Action:    "restart",
				Container: "gatus",
			},
		}
		changed := []string{"traefik/conf.d/dynamic.yml", "gatus/config.yaml"}
		matched := EvaluatePostSyncHooks(changed, hooksWithDelay)
		assert.Len(t, matched, 2)
		assert.Equal(t, 5*time.Second, matched[0].Delay.Duration)
		assert.Equal(t, time.Duration(0), matched[1].Delay.Duration)
	})
}
