package cmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	renderSecrets string
	renderOutput  string
)

// renderCmd represents the render command.
var renderCmd = &cobra.Command{
	Use:   "render [file.tmpl...]",
	Short: "Render templates with SOPS secrets",
	Long: `Render Go templates with decrypted SOPS secrets.

This command renders .tmpl files using the same template engine as bosun
reconciliation. Use it to preview rendered configs before pushing.

Templates have access to:
  - Secrets data via {{ . }} (the root context)
  - sprig template functions, except env, expandenv and getHostByName
  - Custom functions: include, fromJsonFile

include and fromJsonFile may only read from the configured template include
directory (default: <infraDir>/templates).

If no files are specified, renders all .tmpl files in current directory.

Examples:
  # Render a single template to stdout
  bosun render config.yml.tmpl

  # Render all templates in a directory to stdout
  bosun render unraid/compose/*.tmpl

  # Render with specific secrets file
  bosun render -s secrets.sops.yaml config.yml.tmpl

  # Render to output directory (preserves structure, strips .tmpl)
  bosun render -o /tmp/rendered unraid/`,
	Run: runRender,
}

func init() {
	renderCmd.Flags().StringVarP(&renderSecrets, "secrets", "s", "", "SOPS secrets file (auto-detected from BOSUN_SECRETS_FILE if not set)")
	renderCmd.Flags().StringVarP(&renderOutput, "output", "o", "", "Output directory (prints to stdout if not set)")

	rootCmd.AddCommand(renderCmd)
}

func runRender(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// Find secrets file
	secretsFile := renderSecrets
	if secretsFile == "" {
		secretsFile = os.Getenv("BOSUN_SECRETS_FILE")
	}
	if secretsFile == "" {
		secretsFile = "secrets.sops.yaml"
	}

	// Check if secrets file exists
	if _, err := os.Stat(secretsFile); os.IsNotExist(err) {
		ui.Error("Secrets file not found: %s", secretsFile)
		_, _ = ui.Yellow.Println("\nTry one of:")
		_, _ = ui.Yellow.Println("  bosun render -s /path/to/secrets.sops.yaml template.tmpl")
		_, _ = ui.Yellow.Println("  export BOSUN_SECRETS_FILE=/path/to/secrets.sops.yaml")
		os.Exit(1)
	}

	// Decrypt secrets
	sops := reconcile.NewSOPSOps()
	secrets, err := sops.DecryptToMap(ctx, secretsFile)
	if err != nil {
		ui.Error("Failed to decrypt secrets: %v", err)
		os.Exit(1)
	}
	_, _ = ui.Green.Printf("✓ Decrypted secrets from %s\n", secretsFile)

	// Find templates to render
	templates, err := resolveRenderTemplates(args)
	if err != nil {
		ui.Error("%v", err)
		os.Exit(1)
	}

	if len(templates) == 0 {
		_, _ = ui.Yellow.Println("No .tmpl files found")
		os.Exit(0)
	}

	ui.Info("Rendering %d template(s)...\n", len(templates))

	includeDir := mustRenderTemplateIncludeDir()

	// Render each template
	errors := 0
	for _, tmplPath := range templates {
		if err := renderTemplate(ctx, tmplPath, secrets, renderOutput, includeDir); err != nil {
			ui.Error("%s: %v", tmplPath, err)
			errors++
		}
	}

	if errors > 0 {
		_, _ = ui.Red.Printf("\n✗ %d template(s) failed\n", errors)
		os.Exit(1)
	}
	_, _ = ui.Green.Printf("\n✓ All templates rendered successfully\n")
}

func renderTemplate(ctx context.Context, tmplPath string, secrets map[string]any, outputDir, includeDir string) error {
	content, err := readRenderTemplateSource(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	// Create template with the allowlisted sprig functions and bosun functions
	tmpl := template.New(filepath.Base(tmplPath)).
		Option("missingkey=error").
		Funcs(reconcile.SafeSprigFuncMap()).
		Funcs(reconcile.TemplateFuncs(includeDir))

	tmpl, err = tmpl.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Determine output destination
	if outputDir == "" {
		// Print to stdout with separator
		_, _ = ui.Blue.Printf("--- %s ---\n", tmplPath)
		if err := tmpl.Execute(os.Stdout, secrets); err != nil {
			return fmt.Errorf("render error: %w", err)
		}
		fmt.Println() // Blank line after
		return nil
	}

	// Write to output directory with path containment and atomic 0600 permissions
	// (rendered files may contain decrypted SOPS secrets).
	outputPath, err := resolveRenderOutputPath(outputDir, tmplPath)
	if err != nil {
		return err
	}
	targetDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(targetDir, ".bosun-render-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp output file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpl.Execute(tmpFile, secrets); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("render error: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close output file: %w", err)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("failed to finalize output file: %w", err)
	}
	cleanupTmp = false

	_, _ = ui.Green.Printf("  ✓ %s → %s\n", tmplPath, outputPath)
	return nil
}

func resolveRenderTemplates(args []string) ([]string, error) {
	var templates []string
	if len(args) == 0 {
		if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".tmpl") {
				templates = append(templates, path)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("failed to find templates: %w", err)
		}
		return templates, nil
	}

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err == nil && info.IsDir() {
			if walkErr := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(path, ".tmpl") {
					templates = append(templates, path)
				}
				return nil
			}); walkErr != nil {
				return nil, fmt.Errorf("failed to walk directory %s: %w", arg, walkErr)
			}
			continue
		}
		if err == nil {
			templates = append(templates, arg)
			continue
		}
		if !strings.ContainsAny(arg, "*?[") {
			return nil, fmt.Errorf("template not found: %s", arg)
		}
		matches, globErr := filepath.Glob(arg)
		if globErr != nil {
			return nil, fmt.Errorf("invalid pattern %s: %w", arg, globErr)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no templates matched pattern: %s", arg)
		}
		templates = append(templates, matches...)
	}
	return templates, nil
}

func resolveRenderOutputPath(outputDir, tmplPath string) (string, error) {
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	outputPath := filepath.Join(absOutputDir, strings.TrimSuffix(tmplPath, ".tmpl"))
	absOutputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	rel, err := filepath.Rel(absOutputDir, absOutputPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path %q escapes output directory %q", tmplPath, outputDir)
	}
	return outputPath, nil
}

func readRenderTemplateSource(tmplPath string) ([]byte, error) {
	absPath, err := filepath.Abs(tmplPath)
	if err != nil {
		return nil, fmt.Errorf("resolve template path: %w", err)
	}

	parentRoot, err := fileutil.OpenPinnedRoot(filepath.Dir(absPath), false)
	if err != nil {
		return nil, fmt.Errorf("pin template parent: %w", err)
	}
	defer func() { _ = parentRoot.Close() }()

	source, err := fileutil.OpenRootRegular(parentRoot, filepath.Base(absPath), false)
	if err != nil {
		return nil, fmt.Errorf("open template source: %w", err)
	}
	defer func() { _ = source.Close() }()

	content, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("read template source: %w", err)
	}
	return content, nil
}

// renderTemplateIncludeDir resolves the render command's include allowlist
// with the same defaults and precedence as reconciliation. In a Bosun project,
// relative paths are rooted at the project infra directory; standalone renders
// use the current directory as their infra root.
func renderTemplateIncludeDir() (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	infraRoot := workingDir
	configured := ""
	if projectRoot, findErr := config.FindRoot(); findErr == nil {
		projectCfg, loadErr := config.LoadFrom(projectRoot)
		if loadErr != nil {
			return "", fmt.Errorf("load project config: %w", loadErr)
		}
		infraRoot = projectRoot
		configured = projectCfg.TemplateIncludeDir()
	}

	if infraSubDir := os.Getenv("BOSUN_INFRA_DIR"); infraSubDir != "" {
		infraRoot = filepath.Join(infraRoot, infraSubDir)
	}
	configured = templateIncludeDirForCLI(configured, os.Getenv("BOSUN_TEMPLATE_INCLUDE_DIR"))

	return reconcile.ResolveTemplateIncludeDir(infraRoot, configured), nil
}

func mustRenderTemplateIncludeDir() string {
	includeDir, err := renderTemplateIncludeDir()
	if err != nil {
		ui.Fatal("Failed to resolve template include directory: %v", err)
	}
	return includeDir
}
