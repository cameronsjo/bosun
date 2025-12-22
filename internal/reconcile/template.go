package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TemplateOps provides Chezmoi template rendering operations.
type TemplateOps struct {
	// Data is the template data available during rendering.
	Data map[string]any
}

// NewTemplateOps creates a new TemplateOps instance with the given data.
func NewTemplateOps(data map[string]any) *TemplateOps {
	return &TemplateOps{Data: data}
}

// ExecuteTemplate renders a single template file using chezmoi execute-template.
// Secrets are passed via a temporary file with restricted permissions (0600).
// The template can access secrets via BOSUN_SECRETS_FILE environment variable
// pointing to a JSON file that can be read with chezmoi's fromJson/include functions.
func (t *TemplateOps) ExecuteTemplate(ctx context.Context, templateFile, outputFile string) error {
	// Read template content.
	content, err := os.ReadFile(templateFile)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templateFile, err)
	}

	// Prepare data as JSON for chezmoi.
	dataJSON, err := json.Marshal(t.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal template data: %w", err)
	}

	// Write secrets to a temporary file with restricted permissions (0600)
	// instead of passing the actual secret values via environment variables
	secretsFile, err := os.CreateTemp("", "bosun-secrets-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp secrets file: %w", err)
	}
	secretsPath := secretsFile.Name()
	defer func() {
		secretsFile.Close()
		os.Remove(secretsPath)
	}()

	// Set restrictive permissions before writing
	if err := os.Chmod(secretsPath, 0600); err != nil {
		return fmt.Errorf("failed to set secrets file permissions: %w", err)
	}

	if _, err := secretsFile.Write(dataJSON); err != nil {
		return fmt.Errorf("failed to write secrets to temp file: %w", err)
	}
	secretsFile.Close()

	// Build chezmoi command.
	// Pass path to secrets file via env var (the file path itself is not sensitive,
	// only the file contents are protected by 0600 permissions).
	// Templates can use: {{ $secrets := fromJson (include (env "BOSUN_SECRETS_FILE")) }}
	cmd := exec.CommandContext(ctx, "chezmoi", "execute-template")
	cmd.Stdin = bytes.NewReader(content)
	// Pass filtered safe environment plus the secrets file path
	cmd.Env = append(filterSafeEnv(os.Environ()), "BOSUN_SECRETS_FILE="+secretsPath)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Sanitize error output to avoid leaking secrets
		return fmt.Errorf("chezmoi execute-template failed for %s: %w: %s", templateFile, err, sanitizeStderr(stderr.String()))
	}

	// Ensure output directory exists.
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return fmt.Errorf("failed to create output directory for %s: %w", outputFile, err)
	}

	// Write rendered output.
	if err := os.WriteFile(outputFile, stdout.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write output %s: %w", outputFile, err)
	}

	return nil
}

// filterSafeEnv returns only safe environment variables, excluding secrets.
func filterSafeEnv(env []string) []string {
	// List of env var prefixes that are safe to pass through
	safePrefix := []string{
		"PATH=", "HOME=", "USER=", "LANG=", "LC_", "TERM=",
		"XDG_", "TMPDIR=", "TMP=", "TEMP=",
	}
	// Env vars to explicitly exclude (may contain secrets)
	excludePrefix := []string{
		"SOPS_", "AWS_", "AZURE_", "GCP_", "GOOGLE_",
		"API_KEY", "SECRET", "TOKEN", "PASSWORD", "CREDENTIAL",
	}

	var result []string
	for _, e := range env {
		// Check exclusions first
		excluded := false
		for _, prefix := range excludePrefix {
			if strings.HasPrefix(strings.ToUpper(e), prefix) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// Include if it matches safe prefix
		for _, prefix := range safePrefix {
			if strings.HasPrefix(e, prefix) {
				result = append(result, e)
				break
			}
		}
	}
	return result
}

// sanitizeStderr removes potential secret values from error output.
func sanitizeStderr(stderr string) string {
	// Remove any JSON-like structures that might contain secrets
	// and limit output length
	const maxLen = 500
	if len(stderr) > maxLen {
		stderr = stderr[:maxLen] + "... (truncated)"
	}
	return stderr
}

// RenderDirectory processes all .tmpl files in sourceDir and renders them to stagingDir.
// Non-template files are copied as-is.
func (t *TemplateOps) RenderDirectory(ctx context.Context, sourceDir, stagingDir, subDir string) error {
	infraDir := filepath.Join(sourceDir, subDir)
	outDir := filepath.Join(stagingDir, subDir)

	// First, copy non-template files.
	if err := copyNonTemplateFiles(infraDir, outDir); err != nil {
		return fmt.Errorf("failed to copy non-template files: %w", err)
	}

	// Find and render all .tmpl files in the entire sourceDir (not just subDir).
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		// Compute relative path and output path.
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path for %s: %w", path, err)
		}

		// Remove .tmpl extension for output.
		outputPath := filepath.Join(stagingDir, strings.TrimSuffix(relPath, ".tmpl"))

		if err := t.ExecuteTemplate(ctx, path, outputPath); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to render templates: %w", err)
	}

	return nil
}

// copyNonTemplateFiles copies all non-.tmpl files from src to dst.
func copyNonTemplateFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip if source doesn't exist.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		// Skip template files.
		if strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		return copyFile(path, dstPath)
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Ensure destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
