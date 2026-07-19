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

// DefaultIncludeSubdir is the subdirectory (relative to the render source
// directory) that include/fromJsonFile reads are confined to by default.
// Secrets (SOPS files, age keys) and bosun.yaml live in the infra root, NOT
// under templates/, so confining reads here keeps them unreachable.
const DefaultIncludeSubdir = "templates"

// TemplateOps provides template rendering operations using Go's text/template with sprig functions.
type TemplateOps struct {
	// Data is the template data available during rendering.
	Data map[string]any

	// IncludeDir is the allowlisted root for include/fromJsonFile reads: only
	// files located within this subtree may be read, enumerating what MAY be
	// read rather than blocklisting what may not. When empty, RenderDirectory
	// defaults it to <sourceDir>/DefaultIncludeSubdir. When still empty at
	// render time (e.g. a bare ExecuteTemplate call with no tree), validation
	// is skipped (backwards-compatible for callers that supply no root).
	IncludeDir string
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
	tmpl := template.New(filepath.Base(templateFile)).Funcs(sprig.TxtFuncMap()).Funcs(bosunTemplateFuncs(t.IncludeDir))

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

// validateIncludePath enforces a subtree ALLOWLIST: the resolved path must live
// within includeDir. It uses filepath.Rel-based containment so a ".."-escape,
// an absolute path outside the root, or a Rel error all fail closed, and it
// resolves symlinks so a symlink whose target escapes the root is rejected too.
// When includeDir is empty, no allowlist is configured and validation is skipped.
func validateIncludePath(path, includeDir string) error {
	if includeDir == "" {
		return nil
	}

	absRoot, err := filepath.Abs(includeDir)
	if err != nil {
		return fmt.Errorf("failed to resolve include directory: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve include path: %w", err)
	}

	// First check: lexical containment catches traversal and absolute-outside
	// paths even when the target file does not exist yet.
	if err := ensureWithin(filepath.Clean(absRoot), filepath.Clean(absPath), path, includeDir); err != nil {
		return err
	}

	// Second check: resolve symlinks to catch symlink-based escapes. If the
	// path does not resolve (missing file), the lexical check above stands.
	realPath, err := filepath.EvalSymlinks(filepath.Clean(absPath))
	if err != nil {
		return nil
	}
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(absRoot))
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks for include directory: %w", err)
	}
	return ensureWithin(realRoot, realPath, path, includeDir)
}

// resolveTemplateIncludeDir resolves a configured include-dir override against
// the infra directory: an absolute value is used as-is, a relative value is
// joined to infraDir. An empty configured value yields "" — the caller then
// lets RenderDirectory apply the <infraDir>/templates default.
func resolveTemplateIncludeDir(infraDir, configured string) string {
	if configured == "" {
		return ""
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(infraDir, configured)
}

// ensureWithin reports whether target is contained in root, naming the allowed
// root in the error. A Rel error, a ".."-prefixed result, or an absolute result
// all mean "outside" and fail closed.
func ensureWithin(root, target, origPath, allowedRoot string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("include path %s escapes the allowed include directory %s", origPath, allowedRoot)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("include path %s escapes the allowed include directory %s", origPath, allowedRoot)
	}
	return nil
}

// bosunTemplateFuncs returns custom template functions for bosun templates.
// includeDir confines include/fromJsonFile reads to that subtree when non-empty.
func bosunTemplateFuncs(includeDir string) template.FuncMap {
	return template.FuncMap{
		// include reads and returns the contents of a file.
		// Usage: {{ include "/path/to/file" }}
		"include": func(path string) (string, error) {
			if err := validateIncludePath(path, includeDir); err != nil {
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
			if err := validateIncludePath(path, includeDir); err != nil {
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

	// Confine include/fromJsonFile reads to the include subtree. Default to
	// <sourceDir>/templates when the caller has not set an explicit allowlist
	// root — secrets and bosun.yaml live in the source root, above this subtree,
	// so they stay unreachable.
	if t.IncludeDir == "" {
		t.IncludeDir = filepath.Join(sourceDir, DefaultIncludeSubdir)
	}

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
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source directory does not exist: %w", err)
		}
		return fmt.Errorf("failed to stat source directory %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Individual files may disappear mid-walk (race condition safety).
			if os.IsNotExist(err) {
				// Root disappearing is not a per-file race; surface it.
				if path == src {
					return fmt.Errorf("source directory disappeared during walk: %w", err)
				}
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
