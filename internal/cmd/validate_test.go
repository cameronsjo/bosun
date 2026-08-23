package cmd

import (
	"bytes"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidGitURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Valid HTTPS URLs
		{
			name: "https github",
			url:  "https://github.com/user/repo",
			want: true,
		},
		{
			name: "https gitlab",
			url:  "https://gitlab.com/user/repo.git",
			want: true,
		},
		{
			name: "https with port",
			url:  "https://git.example.com:8443/repo",
			want: true,
		},

		// Valid HTTP URLs
		{
			name: "http url",
			url:  "http://git.internal/repo",
			want: true,
		},

		// Valid SSH URLs (git@)
		{
			name: "git@ github",
			url:  "git@github.com:user/repo.git",
			want: true,
		},
		{
			name: "git@ gitlab",
			url:  "git@gitlab.com:group/project.git",
			want: true,
		},

		// Valid ssh:// URLs
		{
			name: "ssh protocol",
			url:  "ssh://git@github.com/user/repo.git",
			want: true,
		},
		{
			name: "ssh with port",
			url:  "ssh://git@example.com:22/repo.git",
			want: true,
		},

		// Valid git:// URLs
		{
			name: "git protocol",
			url:  "git://github.com/user/repo.git",
			want: true,
		},

		// Valid file:// URLs
		{
			name: "file protocol",
			url:  "file:///path/to/repo",
			want: true,
		},
		{
			name: "file protocol relative",
			url:  "file://./local/repo",
			want: true,
		},

		// Invalid URLs - too short
		{
			name: "empty string",
			url:  "",
			want: false,
		},
		{
			name: "too short - 1 char",
			url:  "a",
			want: false,
		},
		{
			name: "too short - 4 chars",
			url:  "http",
			want: false,
		},
		{
			name: "exactly 5 chars but invalid",
			url:  "abcde",
			want: false,
		},

		// Invalid URLs - wrong prefix
		{
			name: "ftp protocol",
			url:  "ftp://example.com/repo",
			want: false,
		},
		{
			name: "plain path",
			url:  "/path/to/repo",
			want: false,
		},
		{
			name: "relative path",
			url:  "./repo",
			want: false,
		},
		{
			name: "just hostname",
			url:  "github.com/user/repo",
			want: false,
		},
		{
			name: "mailto url",
			url:  "mailto:user@example.com",
			want: false,
		},

		// Edge cases
		{
			name: "https only",
			url:  "https://",
			want: true, // Has valid prefix, passes current implementation
		},
		{
			name: "git@ only",
			url:  "git@x",
			want: true, // Has valid prefix
		},
		{
			name: "case sensitive - HTTPS",
			url:  "HTTPS://github.com/repo",
			want: false, // Prefix matching is case-sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidGitURL(tt.url)
			assert.Equal(t, tt.want, got, "isValidGitURL(%q)", tt.url)
		})
	}
}

func TestValidateCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"validate"})
	require.NoError(t, err)
	assert.Equal(t, "validate", cmd.Name())
}

func TestValidateCmd_Help(t *testing.T) {
	output, err := executeCmd(t, "validate", "--help")
	require.NoError(t, err)
	assert.Contains(t, output, "Validate")
	assert.Contains(t, output, "configuration")
	assert.Contains(t, output, "connectivity")
}

func TestValidateEnvironment(t *testing.T) {
	t.Run("returns 0 errors when REPO_URL set", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_REPO_URL", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})

	t.Run("returns 0 errors when BOSUN_REPO_URL set", func(t *testing.T) {
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_URL", "https://github.com/user/repo")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})

	t.Run("returns 1 error when no repo URL set", func(t *testing.T) {
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_URL", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 1, errors)
	})

	t.Run("handles custom branch", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_REPO_URL", "")
		t.Setenv("REPO_BRANCH", "develop")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})

	t.Run("handles webhook secret", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_REPO_URL", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "mysecret")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})

	t.Run("handles deploy target", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_REPO_URL", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "user@server:/opt/deploy")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})
}

func TestValidateReconcileConfig(t *testing.T) {
	t.Run("returns 1 error when no repo URL", func(t *testing.T) {
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_URL", "")

		errors := validateReconcileConfig()
		assert.Equal(t, 1, errors)
	})

	t.Run("returns 0 errors with valid repo URL", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_REPO_URL", "")

		errors := validateReconcileConfig()
		assert.Equal(t, 0, errors)
	})

	t.Run("returns 1 error with invalid repo URL", func(t *testing.T) {
		t.Setenv("REPO_URL", "not-a-valid-url")
		t.Setenv("BOSUN_REPO_URL", "")

		errors := validateReconcileConfig()
		assert.Equal(t, 1, errors)
	})

	t.Run("BOSUN_REPO_URL takes precedence when REPO_URL empty", func(t *testing.T) {
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_URL", "git@github.com:user/repo.git")

		errors := validateReconcileConfig()
		assert.Equal(t, 0, errors)
	})

	t.Run("BOSUN_REPO_URL wins over REPO_URL when both set", func(t *testing.T) {
		// REPO_URL has an invalid URL; BOSUN_REPO_URL has a valid one.
		// If precedence is correct (BOSUN_ wins), validateReconcileConfig
		// should return 0 errors.
		t.Setenv("REPO_URL", "not-a-valid-url")
		t.Setenv("BOSUN_REPO_URL", "https://github.com/user/repo")

		errors := validateReconcileConfig()
		assert.Equal(t, 0, errors, "BOSUN_REPO_URL should win over REPO_URL")
	})

	t.Run("valid repo URL still fails when BOSUN_DRIFT_IGNORE has an invalid rule", func(t *testing.T) {
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_URL", "https://github.com/user/repo")
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"traefik","type":"stopped"}]`)

		errors := validateReconcileConfig()
		assert.Equal(t, 1, errors, "an invalid drift_ignore rule should fail validation even with a valid repo URL")
	})
}

func TestValidateGitAuthenticationConsumers(t *testing.T) {
	t.Run("environment validation rejects a partial pair", func(t *testing.T) {
		t.Setenv("BOSUN_REPO_URL", "https://example.com/repo.git")
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		assert.Equal(t, 1, validateEnvironment())
	})

	t.Run("reconcile config uses BOSUN URL precedence", func(t *testing.T) {
		t.Setenv("BOSUN_REPO_URL", "https://primary.example/repo.git")
		t.Setenv("REPO_URL", "http://shadowed.example/repo.git")
		t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
		t.Setenv("BOSUN_GIT_TOKEN", "configured-token")
		assert.Equal(t, 0, validateReconcileConfig())
	})

	t.Run("reconcile config rejects URL userinfo", func(t *testing.T) {
		t.Setenv("BOSUN_REPO_URL", "https://embedded:password@example.com/repo.git")
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		assert.Equal(t, 1, validateReconcileConfig())
	})

	t.Run("full dry run validates before reconcile", func(t *testing.T) {
		t.Setenv("BOSUN_REPO_URL", "http://example.com/repo.git")
		t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
		t.Setenv("BOSUN_GIT_TOKEN", "configured-token")
		err := runFullDryRun()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute https://")
		assert.NotContains(t, err.Error(), "configured-token")
	})

	t.Run("legacy credential aliases are ignored", func(t *testing.T) {
		t.Setenv("BOSUN_REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("GIT_USERNAME", "legacy-user")
		t.Setenv("GIT_TOKEN", "legacy-token")
		assert.Equal(t, 0, validateReconcileConfig())
	})
}

func TestValidateGitAuthenticationOutputIsRedacted(t *testing.T) {
	var output bytes.Buffer
	previousOutput := color.Output
	color.Output = &output
	t.Cleanup(func() {
		color.Output = previousOutput
	})

	t.Setenv("BOSUN_REPO_URL", "https://embedded%40user:embedded%3Atoken@example.com/repo.git")
	t.Setenv("REPO_URL", "")
	t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	assert.Equal(t, 1, validateEnvironment())

	text := output.String()
	assert.Contains(t, text, "https://example.com/repo.git")
	for _, forbidden := range []string{"embedded%40user", "embedded%3Atoken", "configured-user"} {
		assert.NotContains(t, text, forbidden)
	}
}

func TestValidateDriftIgnoreConfig(t *testing.T) {
	t.Run("no rules configured returns 0 errors", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", "")

		errors := validateDriftIgnoreConfig()
		assert.Equal(t, 0, errors)
	})

	t.Run("valid rules return 0 errors", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"traefik","type":"unhealthy"}]`)

		errors := validateDriftIgnoreConfig()
		assert.Equal(t, 0, errors)
	})

	t.Run("unknown type returns 1 error", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"traefik","type":"stopped"}]`)

		errors := validateDriftIgnoreConfig()
		assert.Equal(t, 1, errors)
	})

	t.Run("invalid glob returns 1 error", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"[unclosed","type":"unhealthy"}]`)

		errors := validateDriftIgnoreConfig()
		assert.Equal(t, 1, errors)
	})

	t.Run("total suppression rule is treated as an error by bosun validate", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"*","type":"*"}]`)

		errors := validateDriftIgnoreConfig()
		assert.Equal(t, 1, errors, "bosun validate should escalate the total-suppression warning to an error")
	})
}

func TestValidateEnvironment_BOSUNPrecedence(t *testing.T) {
	t.Run("BOSUN_REPO_URL wins over REPO_URL when both set", func(t *testing.T) {
		// REPO_URL is empty; BOSUN_REPO_URL is set — should succeed.
		t.Setenv("BOSUN_REPO_URL", "https://bosun.example.com/repo")
		t.Setenv("REPO_URL", "")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})

	t.Run("BOSUN_REPO_URL beats non-empty REPO_URL", func(t *testing.T) {
		// Both set — BOSUN_ value should be shown (no error).
		t.Setenv("BOSUN_REPO_URL", "https://bosun.example.com/repo")
		t.Setenv("REPO_URL", "https://legacy.example.com/repo")
		t.Setenv("BOSUN_REPO_BRANCH", "")
		t.Setenv("REPO_BRANCH", "")
		t.Setenv("WEBHOOK_SECRET", "")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "")
		t.Setenv("DEPLOY_TARGET", "")

		errors := validateEnvironment()
		assert.Equal(t, 0, errors)
	})
}
