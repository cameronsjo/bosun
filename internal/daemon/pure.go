package daemon

import (
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
		resp.LastError = lastErr.Error()
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

// buildConfigResponse constructs a ConfigResponse from daemon configuration.
func buildConfigResponse(cfg *Config) ConfigResponse {
	resp := ConfigResponse{
		WebhookSecret: cfg.WebhookSecret,
	}

	if cfg.PollInterval > 0 {
		resp.PollInterval = int(cfg.PollInterval.Seconds())
	}

	if cfg.ReconcileConfig != nil {
		resp.RepoURL = cfg.ReconcileConfig.RepoURL
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
