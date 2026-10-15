package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	// beforeMutationFn is an instance-scoped test seam invoked immediately
	// before each output filesystem mutation. Nil has no effect.
	beforeMutationFn func(string)
	// walkDirFn is an instance-scoped traversal seam. Nil uses filepath.WalkDir.
	walkDirFn func(string, fs.WalkDirFunc) error

	// sourceRoot and includeRoot are opened once for RenderDirectory. Keeping
	// them on the operation binds each source/include read to the namespace
	// that was selected before walking the checkout.
	sourceRoot      *os.Root
	sourceRootPath  string
	includeRoot     *os.Root
	includeRootPath string
}

// NewTemplateOps creates a new TemplateOps instance with the given data.
func NewTemplateOps(data map[string]any) *TemplateOps {
	return &TemplateOps{Data: data}
}

// ExecuteTemplate renders a single template file using Go's text/template with sprig functions.
// Template data is passed directly to the template context.
// Templates can access data via {{ .key }} syntax and use sprig functions.
func (t *TemplateOps) ExecuteTemplate(ctx context.Context, templateFile, outputFile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Open and validate one descriptor, then read from that descriptor. A
	// pathname check followed by a second open permits an ancestor or final
	// component swap to redirect the read.
	file, err := fileutil.OpenRegularNoFollow(templateFile)
	cleanup := func() {}
	if err != nil {
		switch {
		case errors.Is(err, fileutil.ErrSymlinkSkipped):
			return fmt.Errorf("template %s: %w", templateFile, fileutil.ErrSymlinkSkipped)
		case errors.Is(err, fileutil.ErrUnsupportedFileType):
			return fmt.Errorf("template %s: %w", templateFile, err)
		default:
			return fmt.Errorf("failed to read template %s: %w", templateFile, err)
		}
	}
	defer cleanup()
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templateFile, err)
	}

	// Create template with sprig functions and custom bosun functions.
	tmpl := template.New(filepath.Base(templateFile)).
		Option("missingkey=error").
		Funcs(sprig.TxtFuncMap()).
		Funcs(t.templateFuncs())

	// Parse the template.
	tmpl, err = tmpl.Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", templateFile, err)
	}

	// Ensure output directory exists.
	outputDir := filepath.Dir(outputFile)
	if err := t.checkBeforeMutation(ctx, "create_output_directory"); err != nil {
		return err
	}
	if err := mkdirAllContext(ctx, outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory for %s: %w", outputFile, err)
	}

	// Write rendered output atomically to avoid malformed files on failure.
	// Write to a temp file first, then rename.
	if err := t.checkBeforeMutation(ctx, "create_output_temp"); err != nil {
		return err
	}
	tmpFile, err := createTempContext(ctx, outputDir, ".bosun-template-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", outputFile, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // Cleanup on failure

	// Execute template with data.
	if err := tmpl.Execute(contextWriter{ctx: ctx, writer: tmpFile}, t.Data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to execute template %s: %w", templateFile, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file for %s: %w", outputFile, err)
	}

	// Keep the secret-bearing payload at CreateTemp's private 0600 mode until
	// the atomic rename. The published file can then adopt the established
	// render/deploy mode without exposing the temporary payload beforehand.
	if err := t.checkBeforeMutation(ctx, "publish_output"); err != nil {
		return err
	}
	if err := renameContext(ctx, tmpPath, outputFile); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", outputFile, err)
	}
	if err := os.Chmod(outputFile, 0644); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", outputFile, err)
	}

	return nil
}

func (t *TemplateOps) openTemplateSource(templateFile string) (*os.File, func(), error) {
	if t.sourceRoot == nil {
		absPath, err := filepath.Abs(templateFile)
		if err != nil {
			return nil, func() {}, fmt.Errorf("resolve template path %s: %w", templateFile, err)
		}
		// A bare ExecuteTemplate call has no RenderDirectory-owned root. Pin
		// the parent for the duration of this read so an ancestor replacement
		// cannot redirect the final-component no-follow open elsewhere.
		parentRoot, err := fileutil.OpenPinnedRoot(filepath.Dir(absPath), false)
		if err != nil {
			return nil, func() {}, err
		}
		file, err := fileutil.OpenRootRegular(parentRoot, filepath.Base(absPath), false)
		if err != nil {
			_ = parentRoot.Close()
			return nil, func() {}, err
		}
		return file, func() { _ = parentRoot.Close() }, nil
	}
	absPath, err := filepath.Abs(templateFile)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve template path %s: %w", templateFile, err)
	}
	rel, err := filepath.Rel(t.sourceRootPath, absPath)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, func() {}, fmt.Errorf("template %s escapes source root %s", templateFile, t.sourceRootPath)
	}
	file, err := fileutil.OpenRootRegular(t.sourceRoot, rel, false)
	return file, func() {}, err
}

func (t *TemplateOps) templateFuncs() template.FuncMap {
	return templateFuncsWithRoot(t.IncludeDir, t.includeRoot, t.includeRootPath)
}

func (t *TemplateOps) checkBeforeMutation(ctx context.Context, operation string) error {
	if t.beforeMutationFn != nil {
		t.beforeMutationFn(operation)
	}
	return ctx.Err()
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func createTempContext(ctx context.Context, dir, pattern string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, pattern)
}

func renameContext(ctx context.Context, oldPath, newPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
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

// ResolveTemplateIncludeDir resolves the include allowlist root against the
// infra directory. An absolute override is used as-is, a relative override is
// joined to infraDir, and an empty override selects <infraDir>/templates.
func ResolveTemplateIncludeDir(infraDir, configured string) string {
	if configured == "" {
		return filepath.Join(infraDir, DefaultIncludeSubdir)
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

// TemplateFuncs returns custom template functions for bosun templates.
// includeDir confines include/fromJsonFile reads to that subtree when non-empty.
func TemplateFuncs(includeDir string) template.FuncMap {
	return templateFuncsWithRoot(includeDir, nil, "")
}

func templateFuncsWithRoot(includeDir string, includeRoot *os.Root, includeRootPath string) template.FuncMap {
	read := func(path string) ([]byte, error) {
		if err := validateIncludePath(path, includeDir); err != nil {
			return nil, err
		}
		root := includeRoot
		rootPath := includeRootPath
		closeRoot := func() {}
		if root == nil && includeDir != "" {
			absRoot, err := filepath.Abs(includeDir)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve include directory: %w", err)
			}
			root, err = fileutil.OpenPinnedRoot(absRoot, false)
			if err != nil {
				return nil, err
			}
			rootPath = absRoot
			closeRoot = func() { _ = root.Close() }
		}
		defer closeRoot()
		if root == nil {
			return os.ReadFile(path)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve include path: %w", err)
		}
		rel, err := filepath.Rel(rootPath, absPath)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("include path %s escapes the allowed include directory %s", path, includeDir)
		}
		file, err := fileutil.OpenRootRegular(root, rel, true)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		return io.ReadAll(file)
	}

	return template.FuncMap{
		// include reads and returns the contents of a file.
		// Usage: {{ include "/path/to/file" }}
		"include": func(path string) (string, error) {
			data, err := read(path)
			if err != nil {
				return "", fmt.Errorf("include %s: %w", path, err)
			}
			return string(data), nil
		},

		// fromJsonFile reads a JSON file and returns the parsed data.
		// This is a convenience function that combines include + fromJson.
		// Usage: {{ $data := fromJsonFile "/path/to/file.json" }}
		"fromJsonFile": func(path string) (any, error) {
			data, err := read(path)
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
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)
	outDir := filepath.Join(stagingDir, subDir)

	// Confine include/fromJsonFile reads to the include subtree. Default to
	// <sourceDir>/templates when the caller has not set an explicit allowlist
	// root — secrets and bosun.yaml live in the source root, above this subtree,
	// so they stay unreachable.
	if t.IncludeDir == "" {
		t.IncludeDir = filepath.Join(sourceDir, DefaultIncludeSubdir)
	}
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve template source directory: %w", err)
	}
	sourceRoot, err := fileutil.OpenPinnedRoot(absSource, false)
	if err != nil {
		return fmt.Errorf("open template source root: %w", err)
	}
	defer func() { _ = sourceRoot.Close() }()
	absInclude, err := filepath.Abs(t.IncludeDir)
	if err != nil {
		return fmt.Errorf("resolve template include directory: %w", err)
	}
	var includeRoot *os.Root
	if root, rootErr := fileutil.OpenPinnedRoot(absInclude, false); rootErr == nil {
		includeRoot = root
		defer func() { _ = includeRoot.Close() }()
	} else if !errors.Is(rootErr, fs.ErrNotExist) {
		return fmt.Errorf("open template include root: %w", rootErr)
	}
	previousSourceRoot, previousSourcePath := t.sourceRoot, t.sourceRootPath
	previousIncludeRoot, previousIncludePath := t.includeRoot, t.includeRootPath
	t.sourceRoot, t.sourceRootPath = sourceRoot, absSource
	t.includeRoot, t.includeRootPath = includeRoot, absInclude
	defer func() {
		t.sourceRoot, t.sourceRootPath = previousSourceRoot, previousSourcePath
		t.includeRoot, t.includeRootPath = previousIncludeRoot, previousIncludePath
	}()

	// Copy non-template files from the already pinned source root. Discovery and
	// the later source open must share that descriptor-bound namespace.
	if err := copyNonTemplateFilesFromRoot(ctx, absSource, outDir, sourceRoot); err != nil {
		return fmt.Errorf("failed to copy non-template files: %w", err)
	}

	// Find and render all .tmpl files in the entire sourceDir (not just subDir).
	templatesRendered := 0
	walkDir := filepath.WalkDir
	if t.walkDirFn != nil {
		walkDir = t.walkDirFn
	}
	visitTemplate := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		// WalkDir uses Lstat semantics, so skip symlinked templates before
		// ExecuteTemplate can follow one out of the repository, mirroring how
		// the non-template copy path treats them.
		if d.Type()&os.ModeSymlink != 0 {
			logger.Warn().
				Str(log.FieldPath, path).
				Msg("Skipping symlink during template render")
			return nil
		}

		// Compute relative path and output path.
		relPath := filepath.FromSlash(path)
		templatePath := path
		if t.walkDirFn != nil {
			var relErr error
			relPath, relErr = filepath.Rel(sourceDir, path)
			if relErr != nil {
				return fmt.Errorf("failed to compute relative path for %s: %w", path, relErr)
			}
			templatePath = path
		} else {
			templatePath = filepath.Join(absSource, relPath)
		}

		// Remove .tmpl extension for output. Use outDir (stagingDir/subDir)
		// so rendered files land where deployLocal expects them.
		outputPath := filepath.Join(outDir, strings.TrimSuffix(relPath, ".tmpl"))

		if err := t.ExecuteTemplate(ctx, templatePath, outputPath); err != nil {
			// The entry can become a symlink after WalkDir captured its DirEntry.
			// Treat only the typed skip as benign; every other failure must abort
			// staging rather than produce a partially rendered tree.
			if errors.Is(err, fileutil.ErrSymlinkSkipped) {
				logger.Warn().
					Str(log.FieldPath, path).
					Msg("Skipping symlink during template render")
				return nil
			}
			return err
		}
		templatesRendered++

		return nil
	}
	var err error
	if t.walkDirFn != nil {
		err = walkDir(sourceDir, visitTemplate)
	} else {
		// The source root was selected and pinned above. Traverse that namespace
		// too, so discovery cannot be redirected by a source ancestor swap after
		// the root was opened; ExecuteTemplate then reads the same root-relative
		// entry selected here.
		err = fs.WalkDir(sourceRoot.FS(), ".", visitTemplate)
	}

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
func copyNonTemplateFiles(ctx context.Context, src, dst string) error {
	return copyNonTemplateFilesWith(ctx, src, dst, fileutil.CopyFile)
}

func copyNonTemplateFilesFromRoot(ctx context.Context, src, dst string, root *os.Root) error {
	if root == nil {
		return fmt.Errorf("source directory %s has no pinned root", src)
	}
	info, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("failed to stat pinned source directory %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", src)
	}
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)
	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path != "." {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			logger.Warn().Str(log.FieldPath, filepath.Join(src, filepath.FromSlash(path))).Msg("Skipping symlink during template file copy")
			return nil
		}
		relPath := filepath.FromSlash(path)
		dstPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			return mkdirAllContext(ctx, dstPath, 0755)
		}
		if strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		if err := fileutil.CopyFileFromRoot(ctx, root, relPath, dstPath); err != nil {
			if errors.Is(err, fileutil.ErrSymlinkSkipped) {
				logger.Warn().Str(log.FieldPath, filepath.Join(src, relPath)).Msg("Skipping symlink during template file copy")
				return nil
			}
			return err
		}
		return nil
	})
}

// copyNonTemplateFilesWith makes the copy operation explicit so the
// discovery-to-copy race can be tested without a package-global fault seam.
func copyNonTemplateFilesWith(ctx context.Context, src, dst string, copyFile func(context.Context, string, string) error) error {
	return copyNonTemplateFilesWithOps(ctx, src, dst, copyFile, mkdirAllContext)
}

func copyNonTemplateFilesWithOps(
	ctx context.Context,
	src, dst string,
	copyFile func(context.Context, string, string) error,
	mkdirAll func(context.Context, string, os.FileMode) error,
) error {
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)
	if err := ctx.Err(); err != nil {
		return err
	}

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
		if err := ctx.Err(); err != nil {
			return err
		}

		// WalkDir uses Lstat semantics, so skip symlinks before CopyFile can
		// return ErrSymlinkSkipped and abort the rest of the directory walk.
		if d.Type()&os.ModeSymlink != 0 {
			logger.Warn().
				Str(log.FieldPath, path).
				Msg("Skipping symlink during template file copy")
			return nil
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			if err := ctx.Err(); err != nil {
				return err
			}
			return mkdirAll(ctx, dstPath, 0755)
		}

		// Skip template files.
		if strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		if err := copyFile(ctx, path, dstPath); err != nil {
			// The entry can become a symlink after WalkDir captured its DirEntry.
			// Treat only CopyFile's typed skip as benign; I/O and destination
			// errors must still abort staging rather than produce a partial tree.
			if errors.Is(err, fileutil.ErrSymlinkSkipped) {
				logger.Warn().
					Str(log.FieldPath, path).
					Msg("Skipping symlink during template file copy")
				return nil
			}
			return err
		}
		return nil
	})
}
