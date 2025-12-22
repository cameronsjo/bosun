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

	// Build chezmoi command.
	// chezmoi execute-template reads from stdin and writes to stdout.
	// Data is passed via --init --promptString.
	// However, chezmoi execute-template can use environment variables.
	cmd := exec.CommandContext(ctx, "chezmoi", "execute-template")
	cmd.Stdin = bytes.NewReader(content)
	cmd.Env = append(os.Environ(), "SOPS_SECRETS="+string(dataJSON))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chezmoi execute-template failed for %s: %w: %s", templateFile, err, stderr.String())
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
