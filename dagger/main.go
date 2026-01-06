// Package main provides Dagger CI/CD pipelines for Bosun.
//
// Run locally with: dagger call ci --source .
// Or use make: make ci
package main

import (
	"context"
	"fmt"
)

// Bosun provides CI/CD pipelines for the Bosun project.
type Bosun struct{}

// buildTarget represents a target platform for multi-platform builds.
type buildTarget struct {
	os   string
	arch string
}

var buildTargets = []buildTarget{
	{os: "linux", arch: "amd64"},
	{os: "linux", arch: "arm64"},
	{os: "darwin", arch: "amd64"},
	{os: "darwin", arch: "arm64"},
}

// goVersion is extracted from go.mod.
const goVersion = "1.24"

// Base returns a Go container configured for building Bosun.
// It sets up the Go toolchain and caches module downloads.
func (m *Bosun) Base(source *Directory) *Container {
	goCache := dag.CacheVolume("go-mod")
	goBuildCache := dag.CacheVolume("go-build")

	return dag.Container().
		From(fmt.Sprintf("golang:%s-alpine", goVersion)).
		WithMountedCache("/go/pkg/mod", goCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithEnvVariable("CGO_ENABLED", "0").
		WithMountedDirectory("/src", source).
		WithWorkdir("/src")
}

// Test runs the Go test suite with coverage.
// Note: -race flag is omitted because it requires CGO which isn't available in Alpine.
func (m *Bosun) Test(ctx context.Context, source *Directory) *Container {
	return m.Base(source).
		// Install git for tests that need it (e.g., reconcile/git_test.go)
		WithExec([]string{"apk", "add", "--no-cache", "git"}).
		WithExec([]string{"go", "mod", "download"}).
		WithExec([]string{
			"go", "test",
			"-v",
			"-coverprofile=coverage.out",
			"./...",
		})
}

// Lint runs golangci-lint on the codebase.
func (m *Bosun) Lint(ctx context.Context, source *Directory) *Container {
	goCache := dag.CacheVolume("go-mod")
	goBuildCache := dag.CacheVolume("go-build")
	lintCache := dag.CacheVolume("golangci-lint")

	return dag.Container().
		From("golangci/golangci-lint:v1.64-alpine").
		WithMountedCache("/go/pkg/mod", goCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithMountedCache("/root/.cache/golangci-lint", lintCache).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithExec([]string{
			"golangci-lint", "run",
			"--timeout=10m",
			"./...",
		})
}

// Build compiles Bosun for all target platforms.
// Returns a directory containing the compiled binaries.
func (m *Bosun) Build(
	ctx context.Context,
	source *Directory,
	// Version string for ldflags (optional, defaults to "dev")
	// +optional
	version string,
	// Git commit SHA for ldflags (optional, defaults to "none")
	// +optional
	commit string,
) *Directory {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "none"
	}

	ldflags := fmt.Sprintf(
		"-s -w -X github.com/cameronsjo/bosun/internal/cmd.version=%s -X github.com/cameronsjo/bosun/internal/cmd.commit=%s",
		version, commit,
	)

	outputs := dag.Directory()

	for _, target := range buildTargets {
		binary := fmt.Sprintf("bosun-%s-%s", target.os, target.arch)

		built := m.Base(source).
			WithEnvVariable("GOOS", target.os).
			WithEnvVariable("GOARCH", target.arch).
			WithExec([]string{"go", "mod", "download"}).
			WithExec([]string{
				"go", "build",
				"-ldflags", ldflags,
				"-o", binary,
				"./cmd/bosun",
			})

		outputs = outputs.WithFile(binary, built.File(binary))
	}

	return outputs
}

// CI runs the complete CI pipeline: test, lint, and build.
// Returns a directory containing the build artifacts.
func (m *Bosun) CI(
	ctx context.Context,
	source *Directory,
	// Version string for ldflags (optional, defaults to "dev")
	// +optional
	version string,
	// Git commit SHA for ldflags (optional, defaults to "none")
	// +optional
	commit string,
) (*Directory, error) {
	// Run test and lint in parallel for faster CI
	testCtr := m.Test(ctx, source)
	lintCtr := m.Lint(ctx, source)

	// Wait for test to complete (this forces execution)
	_, err := testCtr.Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("tests failed: %w", err)
	}

	// Wait for lint to complete
	_, err = lintCtr.Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("lint failed: %w", err)
	}

	// Build all platforms
	return m.Build(ctx, source, version, commit), nil
}

// Release runs GoReleaser to create a release.
// This should be called from the release workflow after release-please creates a tag.
func (m *Bosun) Release(
	ctx context.Context,
	source *Directory,
	// GitHub token for publishing releases
	githubToken *Secret,
) *Container {
	goCache := dag.CacheVolume("go-mod")
	goBuildCache := dag.CacheVolume("go-build")

	return dag.Container().
		From("goreleaser/goreleaser:v2").
		WithMountedCache("/go/pkg/mod", goCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithSecretVariable("GITHUB_TOKEN", githubToken).
		WithExec([]string{
			"goreleaser", "release", "--clean",
		})
}

// ReleaseDryRun runs GoReleaser in snapshot mode for testing.
func (m *Bosun) ReleaseDryRun(
	ctx context.Context,
	source *Directory,
) *Container {
	goCache := dag.CacheVolume("go-mod")
	goBuildCache := dag.CacheVolume("go-build")

	return dag.Container().
		From("goreleaser/goreleaser:v2").
		WithMountedCache("/go/pkg/mod", goCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithExec([]string{
			"goreleaser", "release", "--snapshot", "--clean", "--skip=publish",
		})
}

// Coverage runs tests and returns the coverage file.
func (m *Bosun) Coverage(ctx context.Context, source *Directory) *File {
	return m.Test(ctx, source).File("coverage.out")
}

// nodeVersion for WebUI builds.
const nodeVersion = "22"

// WebUI runs the WebUI CI pipeline: install, type check, and build.
func (m *Bosun) WebUI(ctx context.Context, source *Directory) *Container {
	npmCache := dag.CacheVolume("npm-cache")

	return dag.Container().
		From(fmt.Sprintf("node:%s-alpine", nodeVersion)).
		WithMountedCache("/root/.npm", npmCache).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src/webui").
		WithExec([]string{"npm", "ci"}).
		WithExec([]string{"npx", "tsc", "--noEmit"}).
		WithExec([]string{"npm", "run", "build"})
}

// WebUIBuild returns the built WebUI dist directory.
func (m *Bosun) WebUIBuild(ctx context.Context, source *Directory) *Directory {
	return m.WebUI(ctx, source).Directory("/src/webui/dist")
}

// All runs the complete CI pipeline for both Go and WebUI.
func (m *Bosun) All(
	ctx context.Context,
	source *Directory,
	// Version string for ldflags (optional, defaults to "dev")
	// +optional
	version string,
	// Git commit SHA for ldflags (optional, defaults to "none")
	// +optional
	commit string,
) (*Directory, error) {
	// Run Go CI and WebUI in parallel
	goCIResult, err := m.CI(ctx, source, version, commit)
	if err != nil {
		return nil, fmt.Errorf("go ci failed: %w", err)
	}

	// Run WebUI CI
	webUICtr := m.WebUI(ctx, source)
	_, err = webUICtr.Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("webui ci failed: %w", err)
	}

	return goCIResult, nil
}
