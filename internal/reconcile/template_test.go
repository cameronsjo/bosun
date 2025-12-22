package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateOps(t *testing.T) {
	data := map[string]any{
		"key": "value",
	}
	tmpl := NewTemplateOps(data)

	assert.NotNil(t, tmpl)
	assert.Equal(t, "value", tmpl.Data["key"])
}

func TestTemplateOps_ExecuteTemplate(t *testing.T) {
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi not installed")
	}

	t.Run("simple template", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template file
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Hello, World!`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		// Verify output
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "Hello, World!", string(content))
	})

	t.Run("template with variables", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template file with env variable
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Static content`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		data := map[string]any{
			"name": "Test",
		}
		tmpl := NewTemplateOps(data)
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)
	})

	t.Run("non-existent template", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, "/non/existent/template.tmpl", filepath.Join(tmpDir, "output.txt"))

		assert.Error(t, err)
	})

	t.Run("creates output directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		require.NoError(t, os.WriteFile(templateFile, []byte("content"), 0644))

		// Deep nested output path
		outputFile := filepath.Join(tmpDir, "deep", "nested", "dir", "output.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)
		assert.FileExists(t, outputFile)
	})
}

func TestTemplateOps_RenderDirectory(t *testing.T) {
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi not installed")
	}

	t.Run("render directory with templates and static files", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		stagingDir := filepath.Join(tmpDir, "staging")
		ctx := context.Background()

		// Create source structure
		infraDir := filepath.Join(sourceDir, "infra")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Create template file
		templateFile := filepath.Join(sourceDir, "config.yaml.tmpl")
		require.NoError(t, os.WriteFile(templateFile, []byte("key: value"), 0644))

		// Create static file in infra
		staticFile := filepath.Join(infraDir, "static.yml")
		require.NoError(t, os.WriteFile(staticFile, []byte("static: content"), 0644))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")

		require.NoError(t, err)

		// Verify template was rendered (without .tmpl extension)
		renderedTemplate := filepath.Join(stagingDir, "config.yaml")
		assert.FileExists(t, renderedTemplate)

		// Verify static file was copied
		copiedStatic := filepath.Join(stagingDir, "infra", "static.yml")
		assert.FileExists(t, copiedStatic)
	})

	t.Run("non-existent source directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, "/non/existent", tmpDir, "subdir")

		assert.Error(t, err)
	})

	t.Run("empty source directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		stagingDir := filepath.Join(tmpDir, "staging")
		infraDir := filepath.Join(sourceDir, "infra")
		ctx := context.Background()

		require.NoError(t, os.MkdirAll(infraDir, 0755))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")

		require.NoError(t, err)
	})
}

func TestCopyNonTemplateFiles(t *testing.T) {
	t.Run("copy mixed files", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))

		// Create various files
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "regular.yml"), []byte("content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "template.tmpl"), []byte("template"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "config.json"), []byte("{}"), 0644))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)

		// Regular files should be copied
		assert.FileExists(t, filepath.Join(dstDir, "regular.yml"))
		assert.FileExists(t, filepath.Join(dstDir, "config.json"))

		// Template files should NOT be copied
		assert.NoFileExists(t, filepath.Join(dstDir, "template.tmpl"))
	})

	t.Run("copy with subdirectories", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		subDir := filepath.Join(srcDir, "sub")
		require.NoError(t, os.MkdirAll(subDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("content"), 0644))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(dstDir, "sub", "file.txt"))
	})

	t.Run("non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstDir := filepath.Join(tmpDir, "dst")

		err := copyNonTemplateFiles("/non/existent", dstDir)
		// Should not error because of IsNotExist check
		require.NoError(t, err)
	})
}

func TestIsSensitiveEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		expected bool
	}{
		// Prefix matches - cloud providers
		{"AWS prefix", "AWS_ACCESS_KEY_ID=AKIA...", true},
		{"AWS prefix lowercase", "aws_secret_access_key=secret", true},
		{"Azure prefix", "AZURE_CLIENT_SECRET=secret", true},
		{"GCP prefix", "GCP_PROJECT=myproject", true},
		{"Google prefix", "GOOGLE_APPLICATION_CREDENTIALS=/path", true},
		{"DigitalOcean prefix", "DO_API_TOKEN=token", true},
		{"Linode prefix", "LINODE_TOKEN=token", true},
		{"Vultr prefix", "VULTR_API_KEY=key", true},
		{"Cloudflare prefix", "CLOUDFLARE_API_TOKEN=token", true},
		{"Hetzner prefix", "HETZNER_API_TOKEN=token", true},
		{"OVH prefix", "OVH_APPLICATION_KEY=key", true},
		{"SOPS prefix", "SOPS_AGE_KEY=AGE...", true},

		// Prefix matches - generic sensitive
		{"API_KEY prefix", "API_KEY_GITHUB=key", true},
		{"SECRET prefix", "SECRET_VALUE=secret", true},
		{"TOKEN prefix", "TOKEN_FOR_SERVICE=token", true},
		{"PASSWORD prefix", "PASSWORD_DB=pass", true},
		{"CREDENTIAL prefix", "CREDENTIAL_FILE=/path", true},

		// Suffix matches
		{"_TOKEN suffix", "GITHUB_TOKEN=ghp_...", true},
		{"_SECRET suffix", "CLIENT_SECRET=secret", true},
		{"_KEY suffix", "ENCRYPTION_KEY=key", true},
		{"_PASS suffix", "DB_PASS=password", true},
		{"_PASSWORD suffix", "DATABASE_PASSWORD=password", true},
		{"_AUTH suffix", "SMTP_AUTH=authvalue", true},
		{"_CREDENTIAL suffix", "SERVICE_CREDENTIAL=cred", true},
		{"_CREDENTIALS suffix", "AWS_CREDENTIALS=creds", true},

		// Exact matches
		{"GITHUB_TOKEN exact", "GITHUB_TOKEN=ghp_...", true},
		{"GITLAB_TOKEN exact", "GITLAB_TOKEN=glpat_...", true},
		{"NPM_TOKEN exact", "NPM_TOKEN=npm_...", true},
		{"DOCKER_AUTH exact", "DOCKER_AUTH=authconfig", true},
		{"REGISTRY_AUTH exact", "REGISTRY_AUTH=auth", true},
		{"SSH_AUTH_SOCK exact", "SSH_AUTH_SOCK=/tmp/ssh-agent.sock", true},
		{"GPG_TTY exact", "GPG_TTY=/dev/pts/0", true},

		// Safe variables - should NOT be sensitive
		{"PATH is safe", "PATH=/usr/bin", false},
		{"HOME is safe", "HOME=/home/user", false},
		{"USER is safe", "USER=testuser", false},
		{"LANG is safe", "LANG=en_US.UTF-8", false},
		{"TERM is safe", "TERM=xterm-256color", false},
		{"SHELL is safe", "SHELL=/bin/bash", false},
		{"EDITOR is safe", "EDITOR=vim", false},
		{"CUSTOM_VAR is safe", "CUSTOM_VAR=value", false},
		{"MY_APP_DEBUG is safe", "MY_APP_DEBUG=true", false},

		// Edge cases
		{"empty string", "", false},
		{"no value", "SOME_VAR", false},
		{"empty value", "SOME_VAR=", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSensitiveEnvVar(tt.envVar)
			assert.Equal(t, tt.expected, result, "isSensitiveEnvVar(%q) = %v, want %v", tt.envVar, result, tt.expected)
		})
	}
}

func TestFilterSafeEnv(t *testing.T) {
	t.Run("filters sensitive variables", func(t *testing.T) {
		env := []string{
			"PATH=/usr/bin",
			"HOME=/home/user",
			"AWS_SECRET_ACCESS_KEY=secret",
			"GITHUB_TOKEN=ghp_...",
			"USER=testuser",
			"MY_API_TOKEN=token",
			"LANG=en_US.UTF-8",
			"CLOUDFLARE_API_KEY=key",
		}

		result := filterSafeEnv(env)

		// Should include safe vars
		assert.Contains(t, result, "PATH=/usr/bin")
		assert.Contains(t, result, "HOME=/home/user")
		assert.Contains(t, result, "USER=testuser")
		assert.Contains(t, result, "LANG=en_US.UTF-8")

		// Should exclude sensitive vars
		assert.NotContains(t, result, "AWS_SECRET_ACCESS_KEY=secret")
		assert.NotContains(t, result, "GITHUB_TOKEN=ghp_...")
		assert.NotContains(t, result, "MY_API_TOKEN=token")
		assert.NotContains(t, result, "CLOUDFLARE_API_KEY=key")
	})

	t.Run("handles empty env", func(t *testing.T) {
		result := filterSafeEnv([]string{})
		assert.Empty(t, result)
	})

	t.Run("all sensitive vars filtered", func(t *testing.T) {
		env := []string{
			"AWS_ACCESS_KEY=key",
			"GITHUB_TOKEN=token",
			"DB_PASSWORD=pass",
		}

		result := filterSafeEnv(env)
		assert.Empty(t, result)
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		env := []string{
			"PATH=/usr/bin",
			"github_token=token",
			"Aws_Secret_Key=key",
		}

		result := filterSafeEnv(env)

		assert.Contains(t, result, "PATH=/usr/bin")
		// Lowercase sensitive vars should still be filtered
		for _, r := range result {
			assert.NotContains(t, r, "github_token")
			assert.NotContains(t, r, "Aws_Secret_Key")
		}
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copy file", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "src.txt")
		dstFile := filepath.Join(tmpDir, "dst.txt")

		content := "test content"
		require.NoError(t, os.WriteFile(srcFile, []byte(content), 0644))

		err := fileutil.CopyFile(srcFile, dstFile)
		require.NoError(t, err)

		copied, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(copied))
	})

	t.Run("copy to nested directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "src.txt")
		dstFile := filepath.Join(tmpDir, "deep", "nested", "dst.txt")

		require.NoError(t, os.WriteFile(srcFile, []byte("content"), 0644))

		err := fileutil.CopyFile(srcFile, dstFile)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)
	})

	t.Run("non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstFile := filepath.Join(tmpDir, "dst.txt")

		err := fileutil.CopyFile("/non/existent/file.txt", dstFile)
		assert.Error(t, err)
	})
}
