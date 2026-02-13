// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// APIStatusResponse is the extended status response for the WebUI API.
type APIStatusResponse struct {
	Health        string     `json:"health"`
	State         string     `json:"state"`
	Uptime        string     `json:"uptime"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	LastReconcile *time.Time `json:"last_reconcile,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	NextPoll      *time.Time `json:"next_poll,omitempty"`
	PollInterval  int        `json:"poll_interval,omitempty"`
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
	Status         string                 `json:"status"`
	CheckedAt      *time.Time             `json:"checked_at,omitempty"`
	DeclaredCount  int                    `json:"declared_count"`
	DriftItemCount int                    `json:"drift_item_count"`
	Items          []APIDriftItem         `json:"items"`
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

	state := "idle"
	if reconciling {
		state = "reconciling"
	}

	health := d.HealthStatus()
	uptime := time.Since(startTime)

	resp := APIStatusResponse{
		Health:        health.Status,
		State:         state,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime.Seconds()),
	}

	if !lastReconcile.IsZero() {
		resp.LastReconcile = &lastReconcile
	}

	if lastErr != nil {
		resp.LastError = lastErr.Error()
	}

	// Calculate next poll time
	if d.config.PollInterval > 0 {
		resp.PollInterval = int(d.config.PollInterval.Seconds())
		if !lastReconcile.IsZero() {
			nextPoll := lastReconcile.Add(d.config.PollInterval)
			resp.NextPoll = &nextPoll
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPIContainers handles GET /api/containers.
func (d *Daemon) handleAPIContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client, err := d.DockerClient()
	if err != nil {
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), d.config.APITimeout)
	defer cancel()

	containers, err := client.ListContainers(ctx, false)
	if err != nil {
		http.Error(w, "Failed to list containers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := APIContainersResponse{
		Containers: make([]APIContainerResponse, 0, len(containers)),
		Summary: ContainerSummary{
			Total: len(containers),
		},
	}

	for _, c := range containers {
		resp.Containers = append(resp.Containers, APIContainerResponse{
			ID:      c.ID,
			Name:    c.Name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Health:  c.Health,
			Created: c.Created.Format(time.RFC3339),
			Ports:   c.Ports,
		})

		// Update summary counts
		if c.State == "running" {
			resp.Summary.Running++
		} else {
			resp.Summary.Stopped++
		}
		if c.Health == "unhealthy" {
			resp.Summary.Unhealthy++
		}
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
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), d.config.APITimeout)
	defer cancel()

	logs, err := client.GetContainerLogs(ctx, containerID, lines)
	if err != nil {
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client, err := d.DockerClient()
	if err != nil {
		http.Error(w, "Docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*d.config.APITimeout)
	defer cancel()

	if err := client.RestartContainer(ctx, containerID); err != nil {
		http.Error(w, "Failed to restart container: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req TriggerRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	source := req.Source
	if source == "" {
		source = "webui"
	}

	// Trigger reconcile
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), d.config.ReconcileTimeout)
		defer cancel()
		_ = d.TriggerReconcile(ctx, source, req.Force)
	}()

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

	status := "clean"
	if len(state.DriftItems) > 0 {
		status = "drifted"
	}
	if state.LastDeployedCommit == "" {
		status = "unknown"
	}

	items := make([]APIDriftItem, 0, len(state.DriftItems))
	for _, item := range state.DriftItems {
		items = append(items, APIDriftItem{
			Service:  item.Service,
			Type:     string(item.Type),
			Declared: item.Declared,
			Actual:   item.Actual,
		})
	}

	resp := APIDriftResponse{
		Status:         status,
		DeclaredCount:  len(state.DeclaredServices),
		DriftItemCount: len(state.DriftItems),
		Items:          items,
	}

	if !state.DriftCheckedAt.IsZero() {
		resp.CheckedAt = &state.DriftCheckedAt
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
