package cmd

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
	"github.com/spf13/cobra"

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
  - All sprig template functions
  - Custom functions: include, fromJsonFile

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
		ui.Yellow.Println("\nTry one of:")
		ui.Yellow.Println("  bosun render -s /path/to/secrets.sops.yaml template.tmpl")
		ui.Yellow.Println("  export BOSUN_SECRETS_FILE=/path/to/secrets.sops.yaml")
		os.Exit(1)
	}

	// Decrypt secrets
	sops := reconcile.NewSOPSOps()
	secrets, err := sops.DecryptToMap(ctx, secretsFile)
	if err != nil {
		ui.Error("Failed to decrypt secrets: %v", err)
		os.Exit(1)
	}
	ui.Green.Printf("✓ Decrypted secrets from %s\n", secretsFile)

	// Find templates to render
	var templates []string
	if len(args) == 0 {
		// Find all .tmpl files in current directory recursively
		if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".tmpl") {
				templates = append(templates, path)
			}
			return nil
		}); err != nil {
			ui.Error("Failed to find templates: %v", err)
			os.Exit(1)
		}
	} else {
		// Expand globs and collect files
		for _, arg := range args {
			// Check if it's a directory
			info, err := os.Stat(arg)
			if err == nil && info.IsDir() {
				// Walk directory for .tmpl files
				if walkErr := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() && strings.HasSuffix(path, ".tmpl") {
						templates = append(templates, path)
					}
					return nil
				}); walkErr != nil {
					ui.Error("Failed to walk directory %s: %v", arg, walkErr)
					os.Exit(1)
				}
			} else if err == nil {
				templates = append(templates, arg)
			} else {
				// Try glob expansion
				matches, globErr := filepath.Glob(arg)
				if globErr != nil {
					ui.Error("Invalid pattern %s: %v", arg, globErr)
					os.Exit(1)
				}
				templates = append(templates, matches...)
			}
		}
	}

	if len(templates) == 0 {
		ui.Yellow.Println("No .tmpl files found")
		os.Exit(0)
	}

	ui.Info("Rendering %d template(s)...\n", len(templates))

	// Render each template
	errors := 0
	for _, tmplPath := range templates {
		if err := renderTemplate(ctx, tmplPath, secrets, renderOutput); err != nil {
			ui.Error("%s: %v", tmplPath, err)
			errors++
		}
	}

	if errors > 0 {
		ui.Red.Printf("\n✗ %d template(s) failed\n", errors)
		os.Exit(1)
	}
	ui.Green.Printf("\n✓ All templates rendered successfully\n")
}

func renderTemplate(ctx context.Context, tmplPath string, secrets map[string]any, outputDir string) error {
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	// Create template with sprig and bosun functions
	tmpl := template.New(filepath.Base(tmplPath)).
		Funcs(sprig.TxtFuncMap()).
		Funcs(bosunRenderFuncs())

	tmpl, err = tmpl.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Determine output destination
	if outputDir == "" {
		// Print to stdout with separator
		ui.Blue.Printf("--- %s ---\n", tmplPath)
		if err := tmpl.Execute(os.Stdout, secrets); err != nil {
			return fmt.Errorf("render error: %w", err)
		}
		fmt.Println() // Blank line after
		return nil
	}

	// Write to output directory
	outputPath := filepath.Join(outputDir, strings.TrimSuffix(tmplPath, ".tmpl"))
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if err := tmpl.Execute(outFile, secrets); err != nil {
		return fmt.Errorf("render error: %w", err)
	}

	ui.Green.Printf("  ✓ %s → %s\n", tmplPath, outputPath)
	return nil
}

// bosunRenderFuncs returns custom template functions for bosun templates.
// These match the functions available during reconciliation.
func bosunRenderFuncs() template.FuncMap {
	return template.FuncMap{
		"include": func(path string) (string, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("include %s: %w", path, err)
			}
			return string(data), nil
		},
		"fromJsonFile": func(path string) (any, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("fromJsonFile %s: %w", path, err)
			}
			var result any
			if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
				return nil, fmt.Errorf("fromJsonFile %s: invalid JSON: %w", path, jsonErr)
			}
			return result, nil
		},
	}
}
