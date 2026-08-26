// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Client communicates with the bosun daemon over Unix socket or TCP.
type Client struct {
	socketPath  string
	tcpAddr     string
	bearerToken string
	httpClient  *http.Client
	baseURL     string
}

const (
	// Keep daemon client error responses within the same 1 MiB budget used for
	// webhook request bodies. One additional byte is read only to detect
	// truncation deterministically.
	maxDaemonClientErrorBodySize = maxWebhookBodySize
	daemonErrorBodyTruncated     = "... [truncated]"
)

// NewClient creates a new daemon client using Unix socket.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	return &Client{
		socketPath: socketPath,
		baseURL:    "http://localhost",
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
			Timeout: 30 * time.Second,
		},
	}
}

// NewTCPClient creates a new daemon client using TCP with bearer token auth.
func NewTCPClient(addr, bearerToken string) *Client {
	return &Client{
		tcpAddr:     addr,
		bearerToken: bearerToken,
		baseURL:     "http://" + addr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Trigger sends a trigger request to the daemon.
func (c *Client) Trigger(ctx context.Context, source string, force bool) (*TriggerResponse, error) {
	req := TriggerRequest{Source: source, Force: force}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/trigger", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuth(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", c.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		return nil, daemonStatusError(resp.StatusCode, resp.Body)
	}

	var result TriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// addAuth adds bearer token authentication if using TCP.
func (c *Client) addAuth(req *http.Request) {
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
}

// endpoint returns the connection endpoint for error messages.
func (c *Client) endpoint() string {
	if c.tcpAddr != "" {
		return c.tcpAddr
	}
	return c.socketPath
}

// Status gets the current status from the daemon.
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuth(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", c.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, daemonStatusError(resp.StatusCode, resp.Body)
	}

	var result StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Health checks the daemon's bounded public liveness response.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuth(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", c.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusServiceUnavailable:
		// Both statuses carry the bounded health response. A 503 represents
		// degraded or unhealthy liveness rather than a transport failure.
	default:
		return nil, daemonStatusError(resp.StatusCode, resp.Body)
	}

	var result HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Ping checks if the daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Health(ctx)
	return err
}

// Config fetches configuration from the daemon.
// This is used for daemon-injected secrets - the webhook container
// fetches secrets from the daemon rather than storing them on disk.
// Note: This endpoint is only available over Unix socket, not TCP (for security).
func (c *Client) Config(ctx context.Context) (*ConfigResponse, error) {
	// Config endpoint is only available over socket for security
	if c.tcpAddr != "" {
		return nil, fmt.Errorf("config endpoint not available over TCP (security restriction)")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/config", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", c.endpoint(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, daemonStatusError(resp.StatusCode, resp.Body)
	}

	var result ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func daemonStatusError(statusCode int, body io.Reader) error {
	contents, readErr := io.ReadAll(io.LimitReader(body, maxDaemonClientErrorBodySize+1))
	truncated := len(contents) > maxDaemonClientErrorBodySize
	if truncated {
		contents = contents[:maxDaemonClientErrorBodySize]
	}

	detail := string(contents)
	if truncated {
		detail += daemonErrorBodyTruncated
	}
	if readErr != nil {
		separator := ""
		if detail != "" {
			separator = " "
		}
		return fmt.Errorf("daemon returned status %d: %s%s[response body read error: %w]", statusCode, detail, separator, readErr)
	}

	return fmt.Errorf("daemon returned status %d: %s", statusCode, detail)
}
