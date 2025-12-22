# Migration Guide: Bash/Python to Go

This guide covers migrating from the legacy bash/Python implementation to the new Go-based bosun CLI.

## Background

Bosun was rewritten in Go for several reasons:

- **Single binary distribution** - No Python, uv, or bash dependencies
- **Better testing** - Unit tests and golden file tests
- **Type safety** - Catch errors at compile time
- **Native Docker SDK** - First-party Docker integration
- **Concurrency** - Goroutines for watch mode and parallel operations

See [ADR-0010: Go Rewrite](adr/0010-go-rewrite.md) for the full decision rationale.

## What Changed

### Command Differences

| Old (Bash) | New (Go) | Notes |
|------------|----------|-------|
| `bin/bosun yacht up` | `bosun yacht up` | Same syntax |
| `bin/bosun crew list` | `bosun crew list` | Same syntax |
| `cd manifest && uv run manifest.py render` | `bosun provision` | Integrated |
| `cd manifest && uv run manifest.py provisions` | `bosun provisions` | Integrated |
| Manual reconcile.sh | `bosun reconcile` | Integrated |

### File Structure Changes

**Before (Bash/Python):**

```
bin/
  bosun                    # 1400-line bash script
manifest/
  manifest.py              # Python renderer
  pyproject.toml           # Python dependencies
bosun/
  scripts/
    reconcile.sh           # GitOps workflow
    entrypoint.sh
    healthcheck.sh
```

**After (Go):**

```
cmd/bosun/
  main.go                  # Entry point
internal/
  cmd/                     # Cobra commands
  manifest/                # YAML rendering (ported from Python)
  docker/                  # Docker SDK wrapper
  reconcile/               # GitOps engine (ported from bash)
  snapshot/                # Rollback system
  ui/                      # Colored output
build/
  bosun                    # Compiled binary
```

### Breaking Changes

1. **No Python dependency** - The `uv run manifest.py` command is replaced by `bosun provision`

2. **Config file location** - Bosun now looks for config in:
   - `./bosun.yaml`
   - `./.bosun.yaml`
   - `$HOME/.config/bosun/config.yaml`

3. **Environment variables** - Some variable names standardized:
   - `REPO_URL` (unchanged)
   - `REPO_BRANCH` (unchanged)
   - `DEPLOY_TARGET` (previously `TARGET_HOST` in some contexts)

4. **Manifest syntax** - Unchanged. The Go version reads the same YAML manifests.

## How to Update

### Step 1: Build the Go Version

```bash
cd /path/to/bosun
make build
```

This creates `./build/bosun`.

### Step 2: Verify Commands Work

Test that the new binary works correctly:

```bash
# Check version
./build/bosun --version

# Run doctor to verify environment
./build/bosun doctor

# Test manifest rendering (dry-run)
./build/bosun provision core --dry-run

# Compare output if needed
```

### Step 3: Update PATH or Aliases

Option A - Add build directory to PATH:

```bash
export PATH="$PATH:/path/to/bosun/build"
```

Option B - Create alias:

```bash
alias bosun='/path/to/bosun/build/bosun'
```

Option C - Install to GOPATH:

```bash
make install
```

### Step 4: Remove Legacy Files (If Present)

The legacy bash/Python files may have already been removed. If they still exist, remove them:

```bash
# Check if legacy files exist
ls -la bin/bosun manifest/manifest.py manifest/pyproject.toml 2>/dev/null

# If they exist, create backup first (optional)
mkdir -p legacy-backup
cp bin/bosun legacy-backup/ 2>/dev/null
cp manifest/manifest.py manifest/pyproject.toml legacy-backup/ 2>/dev/null

# Remove legacy files
rm -f bin/bosun
rm -f manifest/manifest.py
rm -f manifest/pyproject.toml

# Remove empty directories if applicable
rmdir bin 2>/dev/null || true
```

**Files to remove:**

| File | Description |
|------|-------------|
| `bin/bosun` | Original bash script (~1400 lines) |
| `manifest/manifest.py` | Python YAML renderer (~330 lines) |
| `manifest/pyproject.toml` | Python dependencies |

## Verify Migration

Run these commands to verify the migration worked:

```bash
# 1. Check version
bosun --version
# Expected: bosun version 0.2.0

# 2. Run doctor
bosun doctor
# Expected: All checks pass (or show expected warnings)

# 3. List containers
bosun crew list
# Expected: Shows running containers

# 4. Check yacht status
bosun yacht status
# Expected: Shows compose services

# 5. Test manifest rendering
bosun provision core --dry-run
# Expected: YAML output matching previous behavior
```

## Troubleshooting

### "command not found: bosun"

The binary isn't in your PATH. Either:

- Run with full path: `./build/bosun`
- Add to PATH: `export PATH="$PATH:$(pwd)/build"`
- Install: `make install`

### "failed to load config"

Create a `bosun.yaml` in your project root:

```yaml
root: .
manifest_dir: manifest
```

### "Docker not available"

Ensure Docker is running:

```bash
docker ps
```

### Manifest output differs

The Go version should produce identical output. If you see differences:

1. Run both versions with `--dry-run`
2. Compare outputs with `diff`
3. Report any discrepancies as bugs

## Rollback

If you need to rollback to the bash version:

```bash
# Restore from backup
cp legacy-backup/bosun bin/
cp legacy-backup/manifest.py legacy-backup/pyproject.toml manifest/

# Or restore from git
git checkout HEAD -- bin/bosun manifest/manifest.py manifest/pyproject.toml
```

## Getting Help

- Run `bosun --help` for command documentation
- See [docs/commands.md](commands.md) for full reference
- Check [docs/adr/0010-go-rewrite.md](adr/0010-go-rewrite.md) for design decisions
