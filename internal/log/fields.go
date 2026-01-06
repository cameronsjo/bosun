package log

import (
	"time"

	"github.com/rs/zerolog"
)

// Standard field names for structured logging.
const (
	// FieldComponent identifies the subsystem (daemon, reconcile, git, deploy).
	FieldComponent = "component"

	// FieldRequestID is the HTTP request correlation ID.
	FieldRequestID = "request_id"

	// FieldReconcileID is the reconcile run correlation ID.
	FieldReconcileID = "reconcile_id"

	// FieldSource identifies the trigger source (webhook, poll, manual).
	FieldSource = "source"

	// FieldDurationMS is the operation duration in milliseconds.
	FieldDurationMS = "duration_ms"

	// FieldError contains the error message.
	FieldError = "error"

	// FieldOperation identifies the current operation.
	FieldOperation = "operation"

	// FieldTarget identifies the deployment target.
	FieldTarget = "target"

	// FieldPath is a file or directory path.
	FieldPath = "path"

	// FieldCommit is a git commit hash.
	FieldCommit = "commit"

	// FieldBranch is a git branch name.
	FieldBranch = "branch"

	// FieldContainer is a container name.
	FieldContainer = "container"

	// FieldMethod is an HTTP method.
	FieldMethod = "method"

	// FieldURL is an HTTP URL or path.
	FieldURL = "url"

	// FieldStatus is an HTTP status code.
	FieldStatus = "status"
)

// Component values for FieldComponent.
const (
	ComponentDaemon    = "daemon"
	ComponentReconcile = "reconcile"
	ComponentGit       = "git"
	ComponentDeploy    = "deploy"
	ComponentSOPS      = "sops"
	ComponentTemplate  = "template"
	ComponentDocker    = "docker"
	ComponentManifest  = "manifest"
	ComponentWebhook   = "webhook"
	ComponentHTTP      = "http"
)

// Source values for FieldSource.
const (
	SourceWebhook = "webhook"
	SourcePoll    = "poll"
	SourceManual  = "manual"
	SourceStartup = "startup"
	SourceGitHub  = "github"
	SourceGitLab  = "gitlab"
	SourceGitea   = "gitea"
)

// Component returns a logger with the component field set.
func Component(component string) zerolog.Logger {
	return logger.With().Str(FieldComponent, component).Logger()
}

// DurationMS calculates and returns the duration in milliseconds from a start time.
func DurationMS(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// WithDuration adds a duration_ms field to a log event.
func WithDuration(e *zerolog.Event, start time.Time) *zerolog.Event {
	return e.Int64(FieldDurationMS, DurationMS(start))
}

// WithError adds an error field to a log event.
func WithError(e *zerolog.Event, err error) *zerolog.Event {
	if err == nil {
		return e
	}
	return e.Err(err)
}
