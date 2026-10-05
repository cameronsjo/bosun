package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// A failing reconcile sends a real alert: dispatch has no dry-run gate. These
// tests drive the CLI's own config and option building into a real Reconciler
// and count what reaches the webhook. Each case runs its control arm first —
// without it, "zero requests" proves nothing.
func TestReconcileNoAlertsSuppressesEveryAlertSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		// configure points the alert at the receiver, through the environment
		// or through bosun.yaml in the working directory.
		configure func(t *testing.T, dir, webhook string)
	}{
		{
			name: "environment",
			configure: func(t *testing.T, dir, webhook string) {
				t.Setenv("DISCORD_WEBHOOK_URL", webhook)
				// Anchor config.FindRoot's upward walk here: without a file in
				// the temp dir it could reach a bosun.yaml above TMPDIR and
				// take its alert destination.
				require.NoError(t, os.WriteFile(filepath.Join(dir, "bosun.yaml"), []byte("alerts: {}\n"), 0o600))
			},
		},
		{
			name: "project config",
			configure: func(t *testing.T, dir, webhook string) {
				t.Setenv("DISCORD_WEBHOOK_URL", "")
				body := "alerts:\n  discord_webhook_url: " + webhook + "\n  on_failure: true\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "bosun.yaml"), []byte(body), 0o600))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFlag := runFailingReconcile(t, tc.configure, true)
			require.Zero(t, withFlag, "--no-alerts must send nothing")

			control := runFailingReconcile(t, tc.configure, false)
			require.Equal(t, int32(1), control,
				"control arm: without the flag this run must alert, or the case above proves nothing")
		})
	}
}

// runFailingReconcile builds the config and options the CLI would build, runs
// one failing reconciliation, and returns how many requests the alert
// receiver saw.
func runFailingReconcile(t *testing.T, configure func(t *testing.T, dir, webhook string), noAlerts bool) int32 {
	t.Helper()
	var hits int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)

	dir := t.TempDir()
	t.Chdir(dir)
	// Clear every alert credential bosun reads. Without this, an operator's
	// own exported webhook or Twilio token would take part in the run: the
	// control arm's alert would go somewhere real, and the count would be
	// wrong in both directions.
	for _, name := range []string{
		"BOSUN_DISCORD_WEBHOOK_URL", "DISCORD_WEBHOOK_URL",
		"BOSUN_SLACK_WEBHOOK_URL", "SLACK_WEBHOOK_URL",
		"BOSUN_SENDGRID_API_KEY", "SENDGRID_API_KEY", "BOSUN_SENDGRID_TO_EMAILS", "SENDGRID_TO_EMAILS",
		"BOSUN_TWILIO_ACCOUNT_SID", "TWILIO_ACCOUNT_SID", "BOSUN_TWILIO_AUTH_TOKEN", "TWILIO_AUTH_TOKEN",
		"BOSUN_TWILIO_TO_NUMBERS", "TWILIO_TO_NUMBERS",
		"BOSUN_WEBHOOK_URL", "WEBHOOK_URL",
	} {
		t.Setenv(name, "")
	}
	configure(t, dir, receiver.URL)

	// A URL that PASSES authentication validation but cannot be cloned: one
	// that fails validation exits before the pipeline and would send nothing
	// in either arm.
	t.Setenv("BOSUN_REPO_URL", "https://127.0.0.1:1/unreachable.git")
	t.Setenv("REPO_DIR", filepath.Join(dir, "repo"))
	t.Setenv("STAGING_DIR", filepath.Join(dir, "staging"))
	t.Setenv("BACKUP_DIR", filepath.Join(dir, "backups"))
	t.Setenv("LOG_DIR", filepath.Join(dir, "logs"))
	t.Setenv("BOSUN_STATE_DIR", filepath.Join(dir, "state"))

	// Every flag global, not just this one: the builder reads --remote and
	// --local too, and a prior command test leaves them set.
	resetReconcileFlags(t)
	reconcileNoAlerts = noAlerts

	cfg, err := buildReconcileConfigFromEnv()
	require.NoError(t, err)
	require.NoError(t, reconcile.ValidateGitAuthentication(cfg.RepoURL),
		"the fixture URL must pass validation, or the control arm is vacuous")
	// The only value the CLI cannot express: its default lock path is not
	// writable by a test process.
	cfg.LockFile = filepath.Join(dir, "reconcile.lock")
	cfg.DryRun = true

	r := reconcile.NewReconciler(cfg, reconcilerOptionsForCLI()...)
	require.Error(t, r.Run(context.Background()), "the fixture repository must fail to sync")
	return atomic.LoadInt32(&hits)
}

func TestReconcileNoAlertsIsNotImpliedByDryRun(t *testing.T) {
	resetReconcileFlags(t)
	// Away from the repo's own bosun.yaml: otherwise an ambient provider, not
	// the variable set below, could satisfy the assertion.
	t.Chdir(t.TempDir())

	reconcileNoAlerts = false
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.example/hook")
	require.Len(t, reconcilerOptionsForCLI(), 1, "without the flag a configured provider is attached")

	reconcileNoAlerts = true
	require.Empty(t, reconcilerOptionsForCLI(), "with the flag no alerter is attached")
}
