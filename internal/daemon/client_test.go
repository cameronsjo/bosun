package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("default socket path", func(t *testing.T) {
		client := NewClient("")
		if client.socketPath != "/var/run/bosun.sock" {
			t.Errorf("socketPath = %q, want /var/run/bosun.sock", client.socketPath)
		}
	})

	t.Run("custom socket path", func(t *testing.T) {
		client := NewClient("/tmp/test.sock")
		if client.socketPath != "/tmp/test.sock" {
			t.Errorf("socketPath = %q, want /tmp/test.sock", client.socketPath)
		}
	})
}

func TestNewTCPClient(t *testing.T) {
	client := NewTCPClient("localhost:9090", "test-token")

	if client.tcpAddr != "localhost:9090" {
		t.Errorf("tcpAddr = %q, want localhost:9090", client.tcpAddr)
	}
	if client.bearerToken != "test-token" {
		t.Errorf("bearerToken = %q, want test-token", client.bearerToken)
	}
	if client.baseURL != "http://localhost:9090" {
		t.Errorf("baseURL = %q, want http://localhost:9090", client.baseURL)
	}
}

func TestClient_endpoint(t *testing.T) {
	t.Run("socket client", func(t *testing.T) {
		client := NewClient("/tmp/test.sock")
		if client.endpoint() != "/tmp/test.sock" {
			t.Errorf("endpoint() = %q, want /tmp/test.sock", client.endpoint())
		}
	})

	t.Run("TCP client", func(t *testing.T) {
		client := NewTCPClient("localhost:9090", "token")
		if client.endpoint() != "localhost:9090" {
			t.Errorf("endpoint() = %q, want localhost:9090", client.endpoint())
		}
	})
}

func TestClient_addAuth(t *testing.T) {
	t.Run("TCP client adds bearer token", func(t *testing.T) {
		client := NewTCPClient("localhost:9090", "my-token")
		req, _ := http.NewRequest("GET", "/test", nil)
		client.addAuth(req)

		auth := req.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want 'Bearer my-token'", auth)
		}
	})

	t.Run("socket client does not add auth", func(t *testing.T) {
		client := NewClient("/tmp/test.sock")
		req, _ := http.NewRequest("GET", "/test", nil)
		client.addAuth(req)

		auth := req.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("Authorization = %q, want empty", auth)
		}
	})
}

func TestClient_Trigger(t *testing.T) {
	t.Run("successful trigger", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("Method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/trigger" {
				t.Errorf("Path = %s, want /trigger", r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(TriggerResponse{
				Status:  "accepted",
				Message: "Reconciliation triggered",
			})
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		resp, err := client.Trigger(ctx, "test", false)
		if err != nil {
			t.Fatalf("Trigger() error = %v", err)
		}
		if resp.Status != "accepted" {
			t.Errorf("Status = %q, want accepted", resp.Status)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		_, err := client.Trigger(ctx, "test", false)
		if err == nil {
			t.Error("Trigger() should return error on 500")
		}
	})
}

func TestClient_Status(t *testing.T) {
	t.Run("successful status", func(t *testing.T) {
		now := time.Now()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("Method = %s, want GET", r.Method)
			}
			if r.URL.Path != "/status" {
				t.Errorf("Path = %s, want /status", r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(StatusResponse{
				State:         "idle",
				LastReconcile: &now,
				Uptime:        "1h0m0s",
			})
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		resp, err := client.Status(ctx)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if resp.State != "idle" {
			t.Errorf("State = %q, want idle", resp.State)
		}
	})
}

func TestClient_Health(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				t.Errorf("Path = %s, want /health", r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthResponse{
				Status: "healthy",
				Ready:  true,
			})
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		resp, err := client.Health(ctx)
		if err != nil {
			t.Fatalf("Health() error = %v", err)
		}
		if resp.Status != "healthy" {
			t.Errorf("Status = %q, want healthy", resp.Status)
		}
		if !resp.Ready {
			t.Error("Ready should be true")
		}
	})

	t.Run("degraded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthResponse{
				Status: "degraded",
				Ready:  true,
				Uptime: 2 * time.Minute,
			})
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		resp, err := client.Health(ctx)
		if err != nil {
			t.Fatalf("Health() error = %v", err)
		}
		if resp.Status != "degraded" {
			t.Errorf("Status = %q, want degraded", resp.Status)
		}
		assert.Equal(t, 2*time.Minute, resp.Uptime)
	})

	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "{")
		}))
		defer server.Close()

		client := &Client{baseURL: server.URL, httpClient: server.Client()}
		_, err := client.Health(context.Background())
		require.ErrorContains(t, err, "failed to decode response")
	})

	t.Run("transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		client := &Client{baseURL: server.URL, httpClient: server.Client()}
		server.Close()

		_, err := client.Health(context.Background())
		require.ErrorContains(t, err, "failed to connect to daemon")
	})

	t.Run("non-health status remains an error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"status":"unhealthy","ready":false,"uptime":0}`)
		}))
		defer server.Close()

		client := &Client{baseURL: server.URL, httpClient: server.Client()}
		_, err := client.Health(context.Background())
		require.EqualError(t, err, `daemon returned status 401: {"status":"unhealthy","ready":false,"uptime":0}`)
	})
}

func TestClient_Ping(t *testing.T) {
	t.Run("ping success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		err := client.Ping(ctx)
		if err != nil {
			t.Errorf("Ping() error = %v", err)
		}
	})

	t.Run("ping failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		err := client.Ping(ctx)
		if err == nil {
			t.Error("Ping() should return error when server is unavailable")
		}
	})
}

func TestClient_Config(t *testing.T) {
	t.Run("TCP client blocked", func(t *testing.T) {
		client := NewTCPClient("localhost:9090", "token")

		ctx := context.Background()
		_, err := client.Config(ctx)
		if err == nil {
			t.Error("Config() should fail for TCP client")
		}
		if !strings.Contains(err.Error(), "security restriction") {
			t.Errorf("Error = %q, should mention security restriction", err.Error())
		}
	})

	t.Run("socket client success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/config" {
				t.Errorf("Path = %s, want /config", r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ConfigResponse{
				WebhookSecret: "secret123",
				RepoURL:       "https://github.com/example/repo",
			})
		}))
		defer server.Close()

		// Simulate socket client by not setting tcpAddr
		client := &Client{
			socketPath: "/tmp/test.sock",
			baseURL:    server.URL,
			httpClient: server.Client(),
		}

		ctx := context.Background()
		resp, err := client.Config(ctx)
		if err != nil {
			t.Fatalf("Config() error = %v", err)
		}
		if resp.WebhookSecret != "secret123" {
			t.Errorf("WebhookSecret = %q, want secret123", resp.WebhookSecret)
		}
	})
}

type daemonClientErrorMethod struct {
	name   string
	path   string
	invoke func(context.Context, *Client) error
}

func daemonClientErrorMethods() []daemonClientErrorMethod {
	return []daemonClientErrorMethod{
		{
			name: "trigger",
			path: "/trigger",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Trigger(ctx, "test", false)
				return err
			},
		},
		{
			name: "status",
			path: "/status",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Status(ctx)
				return err
			},
		},
		{
			name: "health",
			path: "/health",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Health(ctx)
				return err
			},
		},
		{
			name: "config",
			path: "/config",
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Config(ctx)
				return err
			},
		},
	}
}

func TestClient_ErrorResponseBodyBounded(t *testing.T) {
	methods := daemonClientErrorMethods()

	bodies := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "under limit is preserved",
			body:     "small daemon error",
			expected: "small daemon error",
		},
		{
			name:     "exact limit is preserved",
			body:     strings.Repeat("e", maxDaemonClientErrorBodySize),
			expected: strings.Repeat("e", maxDaemonClientErrorBodySize),
		},
		{
			name:     "oversized body is truncated",
			body:     strings.Repeat("o", maxDaemonClientErrorBodySize+1),
			expected: strings.Repeat("o", maxDaemonClientErrorBodySize) + daemonErrorBodyTruncated,
		},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			for _, body := range bodies {
				t.Run(body.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusTeapot)
						_, _ = io.WriteString(w, body.body)
					}))
					defer server.Close()

					client := &Client{
						socketPath: "/tmp/test.sock",
						baseURL:    server.URL,
						httpClient: server.Client(),
					}

					err := method.invoke(context.Background(), client)
					require.Error(t, err)
					assert.Equal(t, fmt.Sprintf("daemon returned status %d: %s", http.StatusTeapot, body.expected), err.Error())
				})
			}
		})
	}
}

func TestClient_ErrorResponseReadFailure(t *testing.T) {
	for _, method := range daemonClientErrorMethods() {
		t.Run(method.name, func(t *testing.T) {
			readErr := errors.New("response stream failed")
			body := &trackingReadCloser{
				Reader: io.MultiReader(strings.NewReader("partial daemon error"), iotest.ErrReader(readErr)),
			}
			client := &Client{
				socketPath: "/tmp/test.sock",
				baseURL:    "http://daemon",
				httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					require.Equal(t, method.path, request.URL.Path)
					return &http.Response{
						StatusCode: http.StatusBadGateway,
						Body:       body,
						Header:     make(http.Header),
						Request:    request,
					}, nil
				})},
			}

			err := method.invoke(context.Background(), client)

			require.ErrorIs(t, err, readErr)
			assert.Equal(t, "daemon returned status 502: partial daemon error [response body read error: response stream failed]", err.Error())
			assert.Equal(t, 1, body.closeCalls)
		})
	}
}

func TestClient_SuccessResponsesUnchanged(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		statusCode int
		response   string
		invoke     func(context.Context, *Client) (string, error)
		expected   string
	}{
		{
			name:       "trigger",
			path:       "/trigger",
			statusCode: http.StatusAccepted,
			response:   `{"status":"accepted","message":"queued"}`,
			invoke: func(ctx context.Context, client *Client) (string, error) {
				response, err := client.Trigger(ctx, "test", false)
				if err != nil {
					return "", err
				}
				return response.Status, nil
			},
			expected: "accepted",
		},
		{
			name:       "status",
			path:       "/status",
			statusCode: http.StatusOK,
			response:   `{"state":"idle"}`,
			invoke: func(ctx context.Context, client *Client) (string, error) {
				response, err := client.Status(ctx)
				if err != nil {
					return "", err
				}
				return response.State, nil
			},
			expected: "idle",
		},
		{
			name:       "config",
			path:       "/config",
			statusCode: http.StatusOK,
			response:   `{"webhook_secret":"secret123"}`,
			invoke: func(ctx context.Context, client *Client) (string, error) {
				response, err := client.Config(ctx)
				if err != nil {
					return "", err
				}
				return response.WebhookSecret, nil
			},
			expected: "secret123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.response)
			}))
			defer server.Close()

			client := &Client{
				socketPath: "/tmp/test.sock",
				baseURL:    server.URL,
				httpClient: server.Client(),
			}

			got, err := tt.invoke(context.Background(), client)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDaemonStatusError_ReadFailure(t *testing.T) {
	readErr := errors.New("boom")
	tests := []struct {
		name     string
		body     io.Reader
		expected string
	}{
		{
			name:     "partial body",
			body:     io.MultiReader(strings.NewReader("partial body"), iotest.ErrReader(readErr)),
			expected: "daemon returned status 502: partial body [response body read error: boom]",
		},
		{
			name:     "empty body",
			body:     iotest.ErrReader(readErr),
			expected: "daemon returned status 502: [response body read error: boom]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := daemonStatusError(http.StatusBadGateway, tt.body)

			assert.Equal(t, tt.expected, err.Error())
			require.ErrorIs(t, err, readErr)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingReadCloser struct {
	io.Reader
	closeCalls int
}

func (r *trackingReadCloser) Close() error {
	r.closeCalls++
	return nil
}
