package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
			json.NewEncoder(w).Encode(TriggerResponse{
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
		resp, err := client.Trigger(ctx, "test")
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
		_, err := client.Trigger(ctx, "test")
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
			json.NewEncoder(w).Encode(StatusResponse{
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
			json.NewEncoder(w).Encode(HealthStatus{
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
			json.NewEncoder(w).Encode(HealthStatus{
				Status:    "degraded",
				Ready:     true,
				LastError: "previous reconcile failed",
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
	})
}

func TestClient_Ping(t *testing.T) {
	t.Run("ping success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
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
			json.NewEncoder(w).Encode(ConfigResponse{
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

func TestJsonReader(t *testing.T) {
	data := []byte("hello world")
	reader := jsonReader(data)

	// Read in chunks
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Read() n = %d, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("Read() = %q, want 'hello'", string(buf))
	}

	// Read rest
	buf = make([]byte, 10)
	n, err = reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 6 {
		t.Errorf("Read() n = %d, want 6", n)
	}
	if string(buf[:n]) != " world" {
		t.Errorf("Read() = %q, want ' world'", string(buf[:n]))
	}

	// Read at EOF
	n, err = reader.Read(buf)
	if n != 0 {
		t.Errorf("Read() at EOF n = %d, want 0", n)
	}
	if err.Error() != "EOF" {
		t.Errorf("Read() at EOF error = %v, want EOF", err)
	}
}
