package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
)

// TemplateOps provides template rendering operations using Go's text/template with sprig functions.
type TemplateOps struct {
	// Data is the template data available during rendering.
	Data map[string]any

	// SourceDir restricts include/fromJsonFile to paths within this directory.
	// When empty, path validation is skipped (backwards-compatible).
	SourceDir string
}

// NewTemplateOps creates a new TemplateOps instance with the given data.
func NewTemplateOps(data map[string]any) *TemplateOps {
	return &TemplateOps{Data: data}
}

// ExecuteTemplate renders a single template file using Go's text/template with sprig functions.
// Template data is passed directly to the template context.
// Templates can access data via {{ .key }} syntax and use sprig functions.
func (t *TemplateOps) ExecuteTemplate(_ context.Context, templateFile, outputFile string) error {
	// Read template content.
	content, err := os.ReadFile(templateFile)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templateFile, err)
	}

	// Create template with sprig functions and custom bosun functions.
	tmpl := template.New(filepath.Base(templateFile)).Funcs(sprig.TxtFuncMap()).Funcs(bosunTemplateFuncs(t.SourceDir))

	// Parse the template.
	tmpl, err = tmpl.Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", templateFile, err)
	}

	// Ensure output directory exists.
	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory for %s: %w", outputFile, err)
	}

	// Write rendered output atomically to avoid malformed files on failure.
	// Write to a temp file first, then rename.
	tmpFile, err := os.CreateTemp(outputDir, ".bosun-template-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", outputFile, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // Cleanup on failure

	// Execute template with data.
	if err := tmpl.Execute(tmpFile, t.Data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to execute template %s: %w", templateFile, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file for %s: %w", outputFile, err)
	}

	// Set permissions before rename
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", outputFile, err)
	}

	// Atomic rename - either succeeds completely or fails, no partial writes
	if err := os.Rename(tmpPath, outputFile); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", outputFile, err)
	}

	return nil
}

// validateIncludePath checks that the resolved path is within the allowed source directory.
// Returns an error if the path escapes the source tree via traversal or symlinks.
func validateIncludePath(path, sourceDir string) error {
	if sourceDir == "" {
		return nil
	}

	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to resolve source directory: %w", err)
	}

	// Clean the path first to resolve ".." segments lexically.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve include path: %w", err)
	}
	cleanPath := filepath.Clean(absPath)
	cleanSource := filepath.Clean(absSource)

	// First check: lexical containment catches traversal even for non-existent paths.
	if !strings.HasPrefix(cleanPath, cleanSource+string(filepath.Separator)) && cleanPath != cleanSource {
		return fmt.Errorf("include path %s escapes source directory %s", path, sourceDir)
	}

	// Second check: resolve symlinks to catch symlink-based escapes (only if the file exists).
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		// File doesn't exist — the lexical check above is sufficient.
		return nil
	}

	realSource, err := filepath.EvalSymlinks(cleanSource)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks for source directory: %w", err)
	}

	if !strings.HasPrefix(realPath, realSource+string(filepath.Separator)) && realPath != realSource {
		return fmt.Errorf("include path %s escapes source directory %s", path, sourceDir)
	}

	return nil
}

// bosunTemplateFuncs returns custom template functions for bosun templates.
// sourceDir restricts file reads to within the source tree when non-empty.
func bosunTemplateFuncs(sourceDir string) template.FuncMap {
	return template.FuncMap{
		// include reads and returns the contents of a file.
		// Usage: {{ include "/path/to/file" }}
		"include": func(path string) (string, error) {
			if err := validateIncludePath(path, sourceDir); err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("include %s: %w", path, err)
			}
			return string(data), nil
		},

		// fromJsonFile reads a JSON file and returns the parsed data.
		// This is a convenience function that combines include + fromJson.
		// Usage: {{ $data := fromJsonFile "/path/to/file.json" }}
		"fromJsonFile": func(path string) (any, error) {
			if err := validateIncludePath(path, sourceDir); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("fromJsonFile %s: %w", path, err)
			}
			var result any
			if err := json.Unmarshal(data, &result); err != nil {
				return nil, fmt.Errorf("fromJsonFile %s: invalid JSON: %w", path, err)
			}
			return result, nil
		},
	}
}

// RenderDirectory processes all .tmpl files in sourceDir and renders them to stagingDir.
// Non-template files are copied as-is.
func (t *TemplateOps) RenderDirectory(ctx context.Context, sourceDir, stagingDir, subDir string) error {
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)
	outDir := filepath.Join(stagingDir, subDir)

	// Restrict include/fromJsonFile to the source directory tree.
	t.SourceDir = sourceDir

	// Copy non-template files from sourceDir (caller already joined InfraSubDir).
	if err := copyNonTemplateFiles(sourceDir, outDir); err != nil {
		return fmt.Errorf("failed to copy non-template files: %w", err)
	}

	// Find and render all .tmpl files in the entire sourceDir (not just subDir).
	templatesRendered := 0
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

		// Remove .tmpl extension for output. Use outDir (stagingDir/subDir)
		// so rendered files land where deployLocal expects them.
		outputPath := filepath.Join(outDir, strings.TrimSuffix(relPath, ".tmpl"))

		if err := t.ExecuteTemplate(ctx, path, outputPath); err != nil {
			return err
		}
		templatesRendered++

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to render templates: %w", err)
	}

	logger.Debug().
		Str(log.FieldOperation, "render_directory").
		Str("source", sourceDir).
		Str("sub_dir", subDir).
		Int("templates_rendered", templatesRendered).
		Msg("Successfully rendered templates")

	return nil
}

// copyNonTemplateFiles copies all non-.tmpl files from src to dst.
func copyNonTemplateFiles(src, dst string) error {
	// Validate the source directory exists before walking. A missing root
	// indicates an InfraSubDir misconfiguration and must surface as an error.
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("source directory does not exist: %w", err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Individual files may disappear mid-walk (race condition safety).
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

		return fileutil.CopyFile(path, dstPath)
	})
}
