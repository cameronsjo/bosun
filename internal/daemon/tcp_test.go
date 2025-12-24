package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTCPServer_authMiddleware(t *testing.T) {
	// Create a minimal TCP server for testing middleware
	server := &TCPServer{
		bearerToken: "correct-token",
	}

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	handler := server.authMiddleware(testHandler)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "health endpoint is public",
			path:       "/health",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid bearer token",
			path:       "/trigger",
			authHeader: "Bearer correct-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing auth header",
			path:       "/trigger",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token",
			path:       "/trigger",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid auth format - no Bearer prefix",
			path:       "/trigger",
			authHeader: "correct-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid auth format - Basic instead of Bearer",
			path:       "/status",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestTCPServer_auditMiddleware(t *testing.T) {
	server := &TCPServer{}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("ok"))
	})

	handler := server.auditMiddleware(testHandler)

	req := httptest.NewRequest("POST", "/trigger", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify the handler was called
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want 'ok'", string(body))
	}
}

func TestResponseWriter(t *testing.T) {
	t.Run("captures status code", func(t *testing.T) {
		rr := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

		w.WriteHeader(http.StatusCreated)

		if w.statusCode != http.StatusCreated {
			t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusCreated)
		}
	})

	t.Run("default status code is 200", func(t *testing.T) {
		rr := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

		// Write body without calling WriteHeader
		w.Write([]byte("test"))

		if w.statusCode != http.StatusOK {
			t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusOK)
		}
	})
}
