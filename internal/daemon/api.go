// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

const (
	// apiWriteDeadlineMargin is the time left to write a response after the
	// handler's own context budget runs out.
	apiWriteDeadlineMargin = 5 * time.Second
	// apiWriteDeadlineCeiling bounds the extended deadline so a huge
	// BOSUN_API_TIMEOUT cannot produce an unbounded or overflowed one.
	apiWriteDeadlineCeiling = 10 * time.Minute
)

// apiWriteDeadlinePolicy holds the three numbers behind the extended API write
// deadline: the floor (the server's own deadline, which an extension never
// shortens), the margin added to the handler budget, and the ceiling. Production
// uses defaultAPIWriteDeadlinePolicy. A test sets Daemon.apiWriteDeadlines to a
// sub-second policy so a fast handler can tell which routes extend their
// deadline and by which factor.
type apiWriteDeadlinePolicy struct {
	floor   time.Duration
	margin  time.Duration
	ceiling time.Duration
}

func defaultAPIWriteDeadlinePolicy() apiWriteDeadlinePolicy {
	return apiWriteDeadlinePolicy{
		floor:   tcpWriteTimeout,
		margin:  apiWriteDeadlineMargin,
		ceiling: apiWriteDeadlineCeiling,
	}
}

// deadlineFor returns how long from now a response write may take for a handler
// whose context budget is factor*apiTimeout, plus the margin. It never shortens
// the floor and never exceeds the ceiling. Restart is the longest API budget
// (factor 2); a longer future route passes its own factor.
func (p apiWriteDeadlinePolicy) deadlineFor(apiTimeout time.Duration, factor int64) time.Duration {
	if apiTimeout <= 0 || factor <= 0 {
		return p.floor
	}
	// Compare before multiplying: anything past this is capped anyway, and the
	// product would overflow for a value near the Duration maximum.
	if apiTimeout > (p.ceiling-p.margin)/time.Duration(factor) {
		return p.ceiling
	}
	return max(p.floor, time.Duration(factor)*apiTimeout+p.margin)
}

// apiWriteDeadlineFor is deadlineFor under the production policy.
func apiWriteDeadlineFor(apiTimeout time.Duration, factor int64) time.Duration {
	return defaultAPIWriteDeadlinePolicy().deadlineFor(apiTimeout, factor)
}

// extendAPIWriteDeadline moves this response's write deadline out to cover a
// handler whose budget is factor*BOSUN_API_TIMEOUT (see deadlineFor). A writer
// that cannot change its deadline (httptest.ResponseRecorder, or a wrapper
// without Unwrap) reports ErrNotSupported; the server's own deadline then
// stands and the request goes on.
func (d *Daemon) extendAPIWriteDeadline(w http.ResponseWriter, factor int64) {
	policy := defaultAPIWriteDeadlinePolicy()
	if d.apiWriteDeadlines != nil {
		policy = *d.apiWriteDeadlines
	}
	deadline := time.Now().Add(policy.deadlineFor(d.config.APITimeout, factor))
	if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil {
		logger := log.Component(log.ComponentHTTP)
		logger.Debug().
			Err(err).
			Msg("Could not extend the response write deadline; the server default stands")
	}
}

// APIStatusResponse is the extended status response for the WebUI API.
type APIStatusResponse struct {
	Health        string               `json:"health"`
	State         string               `json:"state"`
	Uptime        string               `json:"uptime"`
	UptimeSeconds int64                `json:"uptime_seconds"`
	LastReconcile *time.Time           `json:"last_reconcile,omitempty"`
	LastError     string               `json:"last_error,omitempty"`
	NextPoll      *time.Time           `json:"next_poll,omitempty"`
	PollInterval  int                  `json:"poll_interval,omitempty"`
	Targets       []TargetHealthStatus `json:"targets,omitempty"`
}

// APIContainerResponse represents a container in the API response.
type APIContainerResponse struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Health  string   `json:"health,omitempty"`
	Created string   `json:"created"`
	Ports   []string `json:"ports,omitempty"`
}

// APIContainersResponse is the response for the containers list endpoint.
type APIContainersResponse struct {
	Containers []APIContainerResponse `json:"containers"`
	Summary    ContainerSummary       `json:"summary"`
}

// ContainerSummary provides aggregate container stats.
type ContainerSummary struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Stopped   int `json:"stopped"`
	Unhealthy int `json:"unhealthy"`
}

// APILogsResponse is the response for the container logs endpoint.
type APILogsResponse struct {
	Container string `json:"container"`
	Lines     int    `json:"lines"`
	Logs      string `json:"logs"`
}

// APIRestartResponse is the response for the container restart endpoint.
type APIRestartResponse struct {
	Status    string `json:"status"`
	Container string `json:"container"`
	Message   string `json:"message"`
}

// APIDriftResponse is the response for the drift status endpoint.
type APIDriftResponse struct {
	Status         string         `json:"status"`
	CheckedAt      *time.Time     `json:"checked_at,omitempty"`
	DeclaredCount  int            `json:"declared_count"`
	DriftItemCount int            `json:"drift_item_count"`
	Items          []APIDriftItem `json:"items"`
}

// APIDriftItem represents a single drift item in the API response.
type APIDriftItem struct {
	Service  string `json:"service"`
	Type     string `json:"type"`
	Declared string `json:"declared,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// RegisterAPIRoutes registers the WebUI API routes on an http.ServeMux.
func (d *Daemon) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", d.handleAPIStatus)
	mux.HandleFunc("/api/containers", d.handleAPIContainers)
	mux.HandleFunc("/api/containers/", d.handleAPIContainerAction)
	mux.HandleFunc("/api/trigger", d.handleAPITrigger)
	mux.HandleFunc("/api/drift", d.handleAPIDrift)
}

// handleAPIStatus handles GET /api/status.
func (d *Daemon) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lastReconcile, lastErr := d.LastReconcile()

	d.reconcileMu.Lock()
	reconciling := d.reconciling
	d.reconcileMu.Unlock()

	health := d.HealthStatus()
	uptime := time.Since(startTime)

	resp := buildAPIStatusResponse(lastReconcile, lastErr, reconciling, health.Status, uptime, d.config.PollInterval)
	resp.Targets = health.Targets

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPIContainers handles GET /api/containers.
func (d *Daemon) handleAPIContainers(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client, err := d.DockerClient()
	if err != nil {
		logger.Warn().Err(err).Msg("Docker unavailable for containers list")
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), d.config.APITimeout)
	defer cancel()
	containers, err := client.ListContainers(ctx, false)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to list containers")
		http.Error(w, "Failed to list containers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	apiContainers := make([]APIContainerResponse, 0, len(containers))
	for _, c := range containers {
		apiContainers = append(apiContainers, APIContainerResponse{
			ID:      c.ID,
			Name:    c.Name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Health:  c.Health,
			Created: c.Created.Format(time.RFC3339),
			Ports:   c.Ports,
		})
	}

	resp := APIContainersResponse{
		Containers: apiContainers,
		Summary:    computeContainerSummary(containers),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPIContainerAction handles /api/containers/:id/* routes.
func (d *Daemon) handleAPIContainerAction(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/containers/{id}/logs or /api/containers/{id}/restart
	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	containerID := parts[0]
	action := parts[1]

	switch action {
	case "logs":
		d.handleAPIContainerLogs(w, r, containerID)
	case "restart":
		d.handleAPIContainerRestart(w, r, containerID)
	default:
		http.Error(w, "Unknown action: "+action, http.StatusNotFound)
	}
}

// handleAPIContainerLogs handles GET /api/containers/:id/logs.
func (d *Daemon) handleAPIContainerLogs(w http.ResponseWriter, r *http.Request, containerID string) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse lines query param (default 100)
	lines := 100
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		if parsed, err := strconv.Atoi(linesParam); err == nil && parsed > 0 {
			lines = parsed
			// Cap at 10000 to prevent excessive memory usage
			if lines > 10000 {
				lines = 10000
			}
		}
	}

	client, err := d.DockerClient()
	if err != nil {
		logger.Warn().Err(err).Str(log.FieldContainer, containerID).Msg("Docker unavailable for container logs")
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), d.config.APITimeout)
	defer cancel()
	logs, err := client.GetContainerLogs(ctx, containerID, lines)
	if err != nil {
		logger.Error().Err(err).Str(log.FieldContainer, containerID).Msg("Failed to get container logs")
		http.Error(w, "Failed to get logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := APILogsResponse{
		Container: containerID,
		Lines:     lines,
		Logs:      logs,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPIContainerRestart handles POST /api/containers/:id/restart.
func (d *Daemon) handleAPIContainerRestart(w http.ResponseWriter, r *http.Request, containerID string) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info().Str(log.FieldContainer, containerID).Msg("Container restart requested via API")

	client, err := d.DockerClient()
	if err != nil {
		logger.Warn().Err(err).Str(log.FieldContainer, containerID).Msg("Docker unavailable for container restart")
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*d.config.APITimeout)
	defer cancel()
	d.extendAPIWriteDeadline(w, 1)

	if err := client.RestartContainer(ctx, containerID); err != nil {
		logger.Error().Err(err).Str(log.FieldContainer, containerID).Msg("Failed to restart container")
		http.Error(w, "Failed to restart container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info().Str(log.FieldContainer, containerID).Msg("Container restart succeeded")

	resp := APIRestartResponse{
		Status:    "success",
		Container: containerID,
		Message:   "Container restart initiated",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPITrigger handles POST /api/trigger.
func (d *Daemon) handleAPITrigger(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request under a size cap. Do not gate on ContentLength — it may be
	// -1 (unknown) when the client does not set it explicitly.
	req, ok := decodeTriggerRequest(w, r)
	if !ok {
		logger.Warn().Msg("Rejected trigger request: oversized or invalid JSON body")
		return
	}

	source := SanitizeWebhookPusherName(req.Source)
	if source == "" {
		source = "webui"
	}

	// Trigger reconcile under the daemon lifecycle while retaining request log
	// fields. The request context itself ends with this response.
	if !d.startReconcileGoroutine(r.Context(), func(ctx context.Context) {
		_ = d.runTriggerReconcile(ctx, source, req.Force)
	}) {
		http.Error(w, "Daemon is shutting down", http.StatusServiceUnavailable)
		return
	}

	logger.Info().
		Str(log.FieldSource, source).
		Bool("force", req.Force).
		Msg("Reconciliation trigger accepted via API")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(TriggerResponse{
		Status:  "accepted",
		Message: "Reconciliation triggered",
	})
}

// handleAPIDrift handles GET /api/drift.
func (d *Daemon) handleAPIDrift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stateFile := ""
	if d.config.ReconcileConfig != nil {
		stateFile = d.config.ReconcileConfig.StateFile
	}
	if stateFile == "" {
		http.Error(w, "State file not configured", http.StatusServiceUnavailable)
		return
	}

	state := reconcile.LoadState(stateFile)

	// A dry-run daemon saves no state, so the file's drift fields are frozen at
	// the last real run. Serve the daemon's own observation of the default target
	// instead (nothing until its first tick) rather than stale items.
	driftItems, driftCheckedAt := state.DriftItems, state.DriftCheckedAt
	status := computeDriftStatus(len(driftItems), state.LastDeployedCommit)
	if d.config.ReconcileConfig.DryRun {
		driftItems, driftCheckedAt = nil, time.Time{}
		status = "unknown" // no observation yet: do not claim clean without data
		if observed, ok := d.observedDriftFor(reconcile.DefaultTargetName); ok {
			driftItems, driftCheckedAt = observed.items, observed.checkedAt
			status = computeDriftStatus(len(driftItems), state.LastDeployedCommit)
		}
	}

	resp := APIDriftResponse{
		Status:         status,
		DeclaredCount:  len(state.DeclaredServices),
		DriftItemCount: len(driftItems),
		Items:          convertDriftItems(driftItems),
	}

	if !driftCheckedAt.IsZero() {
		resp.CheckedAt = &driftCheckedAt
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
