package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
)

// ErrInvalidPreDeployHooks identifies pre_deploy_hooks configuration that
// cannot run as declared. Config load and Run() both reject it, so a bad
// gate blocks the deploy instead of being skipped.
var ErrInvalidPreDeployHooks = errors.New("invalid pre-deploy hooks")

// ErrPreDeployHookFailed is wrapped by every error that means a pre-deploy
// hook blocked the deploy: a non-zero exit, a timeout, or a start failure.
var ErrPreDeployHookFailed = errors.New("pre-deploy hook blocked the deploy")

// DefaultPreDeployHookTimeout bounds one pre-deploy hook when its timeout is
// not set. The reconcile cycle deadline still applies on top of it.
const DefaultPreDeployHookTimeout = 2 * time.Minute

// preDeployHookCancelGrace is how long a hook's process group gets to exit
// after SIGTERM before it is killed.
const preDeployHookCancelGrace = 5 * time.Second

// preDeployHookOutputLimit caps the hook output kept for the log line.
const preDeployHookOutputLimit = 4096

// preDeployFallbackPath is the PATH a hook gets when bosun's own PATH is empty.
const preDeployFallbackPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// PreDeployHook is one validation command run against the rendered staging
// tree before anything is deployed (#477). A non-zero exit, a timeout, or a
// failure to start aborts the deploy.
//
// Command is the exact argv. It is not passed through a shell and no part of
// it is templated, so repository content cannot change what runs. A relative
// Command[0] containing a path separator resolves against the staging tree;
// a bare name is looked up on PATH.
type PreDeployHook struct {
	// Name labels the hook in logs and alerts. Defaults to Command[0].
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// Command is the argv to run, e.g.
	// ["sh", "-c", "! grep -rlF --exclude=bosun.yaml '<no value>' ."]. The
	// staging tree holds a copy of the render source, bosun.yaml included,
	// so a search must skip the config that names its own pattern.
	Command []string `json:"command" yaml:"command"`
	// Timeout bounds this hook. Zero means DefaultPreDeployHookTimeout.
	Timeout Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// preDeployHookLabelLimit caps the hook name shown in terminal lines and alerts.
const preDeployHookLabelLimit = 64

// label returns the name used for the hook in logs and alerts, with control
// and format characters removed so a name cannot inject terminal escapes or
// extra lines into an alert.
func (h PreDeployHook) label() string {
	raw := h.Name
	if raw == "" && len(h.Command) > 0 {
		raw = h.Command[0]
	}
	if label := log.SanitizeForOutputCapped(raw, preDeployHookLabelLimit); label != "" {
		return label
	}
	return "unnamed"
}

// timeout returns the effective bound for one run of the hook.
func (h PreDeployHook) timeout() time.Duration {
	if h.Timeout.Duration > 0 {
		return h.Timeout.Duration
	}
	return DefaultPreDeployHookTimeout
}

// ValidatePreDeployHooks rejects hooks that cannot run as declared: an empty
// command, an empty program name, a NUL byte in any argument or the name, or
// a negative timeout.
func ValidatePreDeployHooks(hooks []PreDeployHook) error {
	for i, h := range hooks {
		switch {
		case len(h.Command) == 0:
			return fmt.Errorf("pre_deploy_hooks[%d]: command must be a non-empty list: %w", i, ErrInvalidPreDeployHooks)
		case strings.TrimSpace(h.Command[0]) == "":
			return fmt.Errorf("pre_deploy_hooks[%d]: command[0] must name a program: %w", i, ErrInvalidPreDeployHooks)
		case h.Timeout.Duration < 0:
			return fmt.Errorf("pre_deploy_hooks[%d]: timeout %s must not be negative: %w", i, h.Timeout.Duration, ErrInvalidPreDeployHooks)
		case strings.ContainsRune(h.Name, 0):
			return fmt.Errorf("pre_deploy_hooks[%d]: name contains a NUL byte: %w", i, ErrInvalidPreDeployHooks)
		}
		for j, arg := range h.Command {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("pre_deploy_hooks[%d]: command[%d] contains a NUL byte: %w", i, j, ErrInvalidPreDeployHooks)
			}
		}
	}
	return nil
}

// ClonePreDeployHooks deep-copies hooks so per-target configs never share a
// command slice. nil stays nil, which means "not set" for target overrides.
func ClonePreDeployHooks(hooks []PreDeployHook) []PreDeployHook {
	if hooks == nil {
		return nil
	}
	out := make([]PreDeployHook, len(hooks))
	for i, h := range hooks {
		out[i] = h
		out[i].Command = cloneSlice(h.Command)
	}
	return out
}

// preDeployHooksEqual reports whether two hook lists are identical.
func preDeployHooksEqual(a, b []PreDeployHook) bool {
	return slices.EqualFunc(a, b, func(x, y PreDeployHook) bool {
		return x.Name == y.Name && x.Timeout == y.Timeout && slices.Equal(x.Command, y.Command)
	})
}

// preDeployHookEnv builds the minimal environment a hook runs with. bosun's
// own environment carries the Git token, the age key, and alert credentials,
// so none of it is inherited except PATH (and the variables Windows needs to
// start a process).
func preDeployHookEnv(stagingDir, target, commit string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = preDeployFallbackPath
	}
	env := []string{
		"PATH=" + path,
		"BOSUN_STAGING_DIR=" + stagingDir,
		"BOSUN_TARGET=" + target,
		"BOSUN_COMMIT=" + commit,
	}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"SystemRoot", "PATHEXT", "TEMP", "TMP"} {
			if v, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+v)
			}
		}
	}
	return env
}

// cappedBuffer keeps the last limit bytes written to it. A hook that prints
// a lot cannot grow bosun's memory, and the tail is where a failure message
// usually is.
type cappedBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.limit; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
		b.truncated = true
	}
	return len(p), nil
}

// String returns the kept output with control characters removed. Line
// breaks become " | " so the output stays one readable log field.
func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := strings.Split(strings.ReplaceAll(string(b.buf), "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if clean := log.SanitizeForOutput(line); clean != "" {
			kept = append(kept, clean)
		}
	}
	out := strings.Join(kept, " | ")
	if b.truncated {
		return "[truncated] " + out
	}
	return out
}

// runPreDeployHooks runs each hook in order with cwd set to stagingDir and
// stops at the first failure. ctx is the reconcile cycle context, so the
// cycle deadline bounds the hooks too; each hook also has its own timeout.
// Every failure, timeout included, returns an error wrapping
// ErrPreDeployHookFailed (and the context error when the caller canceled).
func runPreDeployHooks(ctx context.Context, hooks []PreDeployHook, stagingDir, target, commit string) error {
	if err := ValidatePreDeployHooks(hooks); err != nil {
		return err
	}
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	env := preDeployHookEnv(stagingDir, target, commit)

	for i, h := range hooks {
		name := h.label()
		timeout := h.timeout()
		logger.Info().
			Str("hook", name).
			Int("index", i).
			Int("command_args", len(h.Command)).
			Dur("timeout", timeout).
			Str(log.FieldPath, stagingDir).
			Msg("Running pre-deploy hook against the staging tree")
		ui.Info("  Pre-deploy hook %s...", name)

		start := time.Now()
		err := runOnePreDeployHook(ctx, h, timeout, stagingDir, env, &logger)
		if err != nil {
			return err
		}
		logger.Info().
			Str("hook", name).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Pre-deploy hook passed")
		ui.Success("  Pre-deploy hook %s passed", name)
	}
	return nil
}

func runOnePreDeployHook(ctx context.Context, h PreDeployHook, timeout time.Duration, stagingDir string, env []string, logger *zerolog.Logger) error {
	name := h.label()
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- the argv is operator configuration validated by
	// ValidatePreDeployHooks; it runs without a shell and is never templated.
	cmd := exec.CommandContext(hookCtx, h.Command[0], h.Command[1:]...)
	cmd.Dir = stagingDir
	cmd.Env = env
	out := &cappedBuffer{limit: preDeployHookOutputLimit}
	cmd.Stdout = out
	cmd.Stderr = out
	// Run the hook in its own process group so a timeout reaches any child
	// it started, then kill the group if it ignores SIGTERM (same mechanism
	// as compose up cancellation).
	configureComposeCancellation(cmd, preDeployHookCancelGrace)

	start := time.Now()
	runErr := cmd.Run()
	// Kill whatever is left in the hook's process group, pass or fail, so a
	// background child cannot keep running while the deploy proceeds. A
	// child that called setsid has left the group and is not reached.
	if cmd.Process != nil {
		killPreDeployHookGroup(cmd.Process.Pid)
	}
	if runErr == nil {
		return nil
	}
	elapsed := time.Since(start)
	// A start failure's text can carry the raw program path (fs.PathError
	// does not quote it), so clean it before it reaches any output.
	safeRunErr := log.SanitizeForOutputCapped(runErr.Error(), preDeployHookOutputLimit)

	var reason string
	var cause error
	switch {
	case ctx.Err() != nil:
		// The cycle itself ended (caller cancellation or cycle deadline).
		cause = ctx.Err()
		reason = fmt.Sprintf("pre-deploy hook %q stopped: reconcile cycle ended (%v)", name, ctx.Err())
	case errors.Is(hookCtx.Err(), context.DeadlineExceeded):
		cause = context.DeadlineExceeded
		reason = fmt.Sprintf("pre-deploy hook %q timed out after %s", name, timeout)
	default:
		var exitErr *exec.ExitError
		if errors.Is(runErr, exec.ErrWaitDelay) {
			reason = fmt.Sprintf("pre-deploy hook %q left a background process holding its output open", name)
		} else if errors.As(runErr, &exitErr) {
			reason = fmt.Sprintf("pre-deploy hook %q exited with status %d", name, exitErr.ExitCode())
		} else {
			reason = fmt.Sprintf("pre-deploy hook %q could not run: %s", name, safeRunErr)
		}
		cause = runErr
	}

	logger.Error().
		Str(log.FieldError, safeRunErr).
		Str("hook", name).
		Int64(log.FieldDurationMS, elapsed.Milliseconds()).
		Dur("timeout", timeout).
		Str("output", out.String()).
		Msg("Pre-deploy hook failed, expected exit status 0; aborting deploy before any file sync or compose up")
	ui.Error("  %s", reason)

	return &preDeployHookError{reason: reason, causes: []error{ErrPreDeployHookFailed, cause}}
}

// preDeployHookError is a blocked deploy. Its text is the one-line,
// sanitized reason that goes into the failure alert; the sentinel and the
// underlying cause stay reachable through errors.Is and errors.As.
type preDeployHookError struct {
	reason string
	causes []error
}

func (e *preDeployHookError) Error() string   { return e.reason }
func (e *preDeployHookError) Unwrap() []error { return e.causes }

// undoDryRunAttempt restores the circuit-breaker counters a dry run bumped
// before its pre-deploy hooks blocked it, so a preview never counts as a
// failed deploy attempt.
func (r *Reconciler) undoDryRunAttempt(ctx context.Context, state *DeployState, snapshot *attemptBudgetSnapshot) {
	if snapshot == nil {
		return
	}
	state.LastAttemptedCommit = snapshot.lastAttemptedCommit
	state.AttemptCount = snapshot.attemptCount
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).
			Msg("Failed to restore attempt counters after a dry run blocked by pre-deploy hooks")
		return
	}
	logger.Warn().
		Int("attempt_count", state.AttemptCount).
		Msg("Dry run blocked by pre-deploy hooks; no alert sent and the attempt is not counted")
}
