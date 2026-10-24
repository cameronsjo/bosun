package daemon

import (
	"encoding/json"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// reconcileStateString returns "reconciling" if isReconciling is true, "idle" otherwise.
func reconcileStateString(isReconciling bool) string {
	if isReconciling {
		return "reconciling"
	}
	return "idle"
}

// computeDriftStatus determines the drift status string from deploy state.
// Returns "unknown" if no commit has been deployed, "drifted" if drift items exist,
// or "clean" if the deployment matches the declared state.
func computeDriftStatus(driftItemCount int, lastDeployedCommit string) string {
	if lastDeployedCommit == "" {
		return "unknown"
	}
	if driftItemCount > 0 {
		return "drifted"
	}
	return "clean"
}

// buildAPIStatusResponse constructs an APIStatusResponse from daemon state.
// The caller is responsible for reading daemon state under appropriate locks.
func buildAPIStatusResponse(lastReconcile time.Time, lastErr error, reconciling bool, health string, uptime time.Duration, pollInterval time.Duration) APIStatusResponse {
	rounded := uptime.Round(time.Second)
	resp := APIStatusResponse{
		Health:        health,
		State:         reconcileStateString(reconciling),
		Uptime:        rounded.String(),
		UptimeSeconds: int64(rounded.Seconds()),
	}

	if !lastReconcile.IsZero() {
		resp.LastReconcile = &lastReconcile
	}

	if lastErr != nil {
		resp.LastError = reconcile.SanitizeGitText(lastErr.Error())
	}

	if pollInterval > 0 {
		resp.PollInterval = int(pollInterval.Seconds())
		if !lastReconcile.IsZero() {
			nextPoll := lastReconcile.Add(pollInterval)
			resp.NextPoll = &nextPoll
		}
	}

	return resp
}

// computeContainerSummary computes aggregate container stats from a list of containers.
func computeContainerSummary(containers []docker.ContainerInfo) ContainerSummary {
	summary := ContainerSummary{
		Total: len(containers),
	}
	for _, c := range containers {
		if c.State == "running" {
			summary.Running++
		} else {
			summary.Stopped++
		}
		if c.Health == "unhealthy" {
			summary.Unhealthy++
		}
	}
	return summary
}

// shouldProcessGitHubEvent determines how to handle a GitHub webhook event type.
// Returns an action ("process", "ping", or "ignore") and a human-readable reason.
func shouldProcessGitHubEvent(eventType string) (action string, reason string) {
	switch eventType {
	case "ping":
		return "ping", "ping event received"
	case "push":
		return "process", "push event received"
	default:
		return "ignore", "Event type '" + eventType + "' not handled"
	}
}

// shouldProcessGitHubPush determines whether a push event targets the tracking branch.
func shouldProcessGitHubPush(pushRef string, trackingBranch string) (process bool, reason string) {
	expectedRef := "refs/heads/" + trackingBranch
	if pushRef != expectedRef {
		return false, "Push to " + pushRef + " ignored (tracking " + expectedRef + ")"
	}
	return true, "push to tracked branch " + trackingBranch
}

// ShouldProcessGenericWebhook decides whether an authorized generic webhook
// starts a reconcile. A GitHub event header other than push is ignored. A
// top-level "ref" in a JSON object body must name the tracked branch, using the
// same comparison as the GitHub endpoint; tags, other branches, and non-string
// values are ignored. A body without a ref (empty, not JSON, not an object, or
// no "ref" key) still triggers, so the endpoint stays usable as a plain signed
// trigger, unless the GitHub event header is set: a GitHub push always has a
// ref, so one without a readable ref is ignored. An empty trackingBranch means
// the caller does not know the branch (the standalone receiver without
// configuration): only the event filter applies. The returned ref is the raw
// value for logging; callers must sanitize it, and reason may embed it
// unsanitized. Shared by the daemon's /webhook and the standalone receiver's.
func ShouldProcessGenericWebhook(githubEvent string, body []byte, trackingBranch string) (process bool, ref string, reason string) {
	if githubEvent != "" {
		if action, eventReason := shouldProcessGitHubEvent(githubEvent); action != "process" {
			return false, "", eventReason
		}
	}
	if trackingBranch == "" {
		return true, "", "no tracked branch configured; ref not checked"
	}

	var fields map[string]json.RawMessage
	var rawRef json.RawMessage
	present := false
	if err := json.Unmarshal(body, &fields); err == nil {
		rawRef, present = fields["ref"]
	}
	if !present {
		// Every GitHub push carries a ref. Without one in a JSON body (for
		// example a form-encoded hook), the branch is unknown, so do not deploy.
		if githubEvent != "" {
			return false, "", "GitHub push without a JSON ref ignored (set the hook content type to application/json)"
		}
		return true, "", "no ref in payload"
	}
	if err := json.Unmarshal(rawRef, &ref); err != nil {
		return false, "", "Payload ref is not a string; ignored"
	}
	// A JSON null decodes to "", which never matches and is ignored.
	process, reason = shouldProcessGitHubPush(ref, trackingBranch)
	return process, ref, reason
}

// buildConfigResponse constructs a ConfigResponse from daemon configuration.
//
// includeWebhookSecret must be set explicitly by a caller that has already
// authorized the peer. The credential is opt-in rather than default so a new
// caller cannot emit it by forgetting to strip it: the zero value of the
// parameter withholds the secret.
func buildConfigResponse(cfg *Config, includeWebhookSecret bool) ConfigResponse {
	resp := ConfigResponse{}

	if includeWebhookSecret {
		resp.WebhookSecret = cfg.WebhookSecret
	}

	if cfg.PollInterval > 0 {
		resp.PollInterval = int(cfg.PollInterval.Seconds())
	}

	if cfg.ReconcileConfig != nil {
		resp.RepoURL = reconcile.SanitizeGitURL(cfg.ReconcileConfig.RepoURL)
		resp.RepoBranch = cfg.ReconcileConfig.RepoBranch
	}

	return resp
}

// convertDriftItems converts reconcile drift items to API drift items.
func convertDriftItems(items []reconcile.DriftItem) []APIDriftItem {
	result := make([]APIDriftItem, 0, len(items))
	for _, item := range items {
		result = append(result, APIDriftItem{
			Service:  item.Service,
			Type:     string(item.Type),
			Declared: item.Declared,
			Actual:   item.Actual,
		})
	}
	return result
}
