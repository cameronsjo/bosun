package alert

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwilio_Name(t *testing.T) {
	tw := NewTwilio(TwilioConfig{})
	assert.Equal(t, "twilio", tw.Name())
}

func TestTwilio_IsConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config TwilioConfig
		want   bool
	}{
		{
			name:   "empty config",
			config: TwilioConfig{},
			want:   false,
		},
		{
			name: "missing auth token",
			config: TwilioConfig{
				AccountSID: "AC123",
				FromNumber: "+15551234567",
				ToNumbers:  []string{"+15559876543"},
			},
			want: false,
		},
		{
			name: "missing account SID",
			config: TwilioConfig{
				AuthToken:  "token123",
				FromNumber: "+15551234567",
				ToNumbers:  []string{"+15559876543"},
			},
			want: false,
		},
		{
			name: "missing from number",
			config: TwilioConfig{
				AccountSID: "AC123",
				AuthToken:  "token123",
				ToNumbers:  []string{"+15559876543"},
			},
			want: false,
		},
		{
			name: "missing to numbers",
			config: TwilioConfig{
				AccountSID: "AC123",
				AuthToken:  "token123",
				FromNumber: "+15551234567",
				ToNumbers:  []string{},
			},
			want: false,
		},
		{
			name: "fully configured",
			config: TwilioConfig{
				AccountSID: "AC123",
				AuthToken:  "token123",
				FromNumber: "+15551234567",
				ToNumbers:  []string{"+15559876543"},
			},
			want: true,
		},
		{
			name: "multiple recipients",
			config: TwilioConfig{
				AccountSID: "AC123",
				AuthToken:  "token123",
				FromNumber: "+15551234567",
				ToNumbers:  []string{"+15559876543", "+15551111111"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tw := NewTwilio(tt.config)
			assert.Equal(t, tt.want, tw.IsConfigured())
		})
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		name   string
		msg    string
		maxLen int
		want   string
	}{
		{
			name:   "short message",
			msg:    "Hello",
			maxLen: 10,
			want:   "Hello",
		},
		{
			name:   "exact length",
			msg:    "Hello",
			maxLen: 5,
			want:   "Hello",
		},
		{
			name:   "needs truncation",
			msg:    "Hello World",
			maxLen: 8,
			want:   "Hello...",
		},
		{
			name:   "long message",
			msg:    "This is a very long message that needs to be truncated",
			maxLen: 20,
			want:   "This is a very lo...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, truncateMessage(tt.msg, tt.maxLen))
		})
	}
}

func TestFormatPhoneNumber(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   string
	}{
		{
			name:   "already has plus",
			number: "+15551234567",
			want:   "+15551234567",
		},
		{
			name:   "needs plus prefix",
			number: "15551234567",
			want:   "+15551234567",
		},
		{
			name:   "with spaces",
			number: " 15551234567 ",
			want:   "+15551234567",
		},
		{
			name:   "empty string",
			number: "",
			want:   "",
		},
		{
			name:   "international format",
			number: "+447911123456",
			want:   "+447911123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatPhoneNumber(tt.number))
		})
	}
}

func TestMaskPhoneNumber(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   string
	}{
		{
			name:   "normal phone number",
			number: "+15551234567",
			want:   "****4567",
		},
		{
			name:   "short number",
			number: "1234",
			want:   "****",
		},
		{
			name:   "very short",
			number: "12",
			want:   "****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, maskPhoneNumber(tt.number))
		})
	}
}

func TestTwilio_buildAuthHeader(t *testing.T) {
	tw := NewTwilio(TwilioConfig{
		AccountSID: "AC123456",
		AuthToken:  "authtoken789",
	})

	got := tw.buildAuthHeader()

	// Verify it's a valid Basic auth header.
	require.Greater(t, len(got), 6)
	assert.Equal(t, "Basic ", got[:6])

	// Decode and verify credentials.
	decoded, err := base64.StdEncoding.DecodeString(got[6:])
	require.NoError(t, err)
	assert.Equal(t, "AC123456:authtoken789", string(decoded))
}

func TestTwilio_formatMessage(t *testing.T) {
	tw := NewTwilio(TwilioConfig{})

	tests := []struct {
		name  string
		alert *Alert
		want  string
	}{
		{
			name: "error alert",
			alert: &Alert{
				Title:    "Deploy Failed",
				Message:  "commit abc123 failed to deploy",
				Severity: SeverityError,
			},
			want: "[ERROR] Deploy Failed: commit abc123 failed to deploy",
		},
		{
			name: "critical alert",
			alert: &Alert{
				Title:    "Service Down",
				Message:  "API server not responding",
				Severity: SeverityCritical,
			},
			want: "[CRITICAL] Service Down: API server not responding",
		},
		{
			name: "warning alert",
			alert: &Alert{
				Title:    "High Memory",
				Message:  "Memory usage at 90%",
				Severity: SeverityWarning,
			},
			want: "[WARNING] High Memory: Memory usage at 90%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tw.formatMessage(tt.alert))
		})
	}
}

func TestTwilio_Send_SkipsNonErrorSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity Severity
		wantSkip bool
	}{
		{name: "info skipped", severity: SeverityInfo, wantSkip: true},
		{name: "warning skipped", severity: SeverityWarning, wantSkip: true},
		{name: "error sent", severity: SeverityError, wantSkip: false},
		{name: "critical sent", severity: SeverityCritical, wantSkip: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				callCount++
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"sid": "SM123"}`))
			}))
			defer server.Close()

			tw := &Twilio{
				config: TwilioConfig{
					AccountSID: "AC123",
					AuthToken:  "token",
					FromNumber: "+15551234567",
					ToNumbers:  []string{"+15559876543"},
				},
				client: server.Client(),
				apiURL: server.URL,
			}

			alert := &Alert{
				Title:    "Test",
				Message:  "Test message",
				Severity: tt.severity,
				Source:   "test",
			}

			err := tw.Send(context.Background(), alert)
			require.NoError(t, err)

			if tt.wantSkip {
				assert.Equal(t, 0, callCount, "skipped severities should not trigger HTTP calls")
			} else {
				assert.Equal(t, 1, callCount, "error/critical severities should trigger one HTTP call")
			}
		})
	}
}

func TestTwilio_Send_NotConfigured(t *testing.T) {
	tw := NewTwilio(TwilioConfig{})

	alert := &Alert{
		Title:    "Test",
		Message:  "Test message",
		Severity: SeverityError,
	}

	err := tw.Send(context.Background(), alert)
	assert.NoError(t, err, "Send() should silently skip when not configured")
}

func TestTwilio_Send_HTTPSuccess(t *testing.T) {
	var (
		receivedBody   string
		receivedMethod string
		receivedCT     string
		handlerErr     error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedCT = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			handlerErr = err
		}
		receivedBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid": "SM123"}`))
	}))
	defer server.Close()

	tw := &Twilio{
		config: TwilioConfig{
			AccountSID: "AC123456789",
			AuthToken:  "secrettoken",
			FromNumber: "+15551234567",
			ToNumbers:  []string{"+15559876543"},
		},
		client: server.Client(),
		apiURL: server.URL,
	}

	err := tw.Send(context.Background(), &Alert{
		Title:    "Deploy Failed",
		Message:  "commit abc failed",
		Severity: SeverityError,
		Source:   "reconcile",
	})
	require.NoError(t, err)

	// Assertions moved out of handler goroutine (t.FailNow is unsafe there).
	require.NoError(t, handlerErr)
	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, "application/x-www-form-urlencoded", receivedCT)

	// Verify the SMS body was sent in the form data.
	assert.Contains(t, receivedBody, "Body=")
	assert.Contains(t, receivedBody, "To=")
	assert.Contains(t, receivedBody, "From=")
}

func TestTwilio_Send_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code": 21211, "message": "Invalid 'To' Phone Number"}`))
	}))
	defer server.Close()

	tw := &Twilio{
		config: TwilioConfig{
			AccountSID: "AC123",
			AuthToken:  "token",
			FromNumber: "+15551234567",
			ToNumbers:  []string{"+15559876543"},
		},
		client: server.Client(),
		apiURL: server.URL,
	}

	err := tw.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityError,
	})
	require.Error(t, err, "Send() should return error for API error response")
	assert.Contains(t, err.Error(), "status 400")
}

func TestTwilio_Send_MultipleRecipients_PartialFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "Invalid number"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid": "SM123"}`))
	}))
	defer server.Close()

	tw := &Twilio{
		config: TwilioConfig{
			AccountSID: "AC123",
			AuthToken:  "token",
			FromNumber: "+15551234567",
			ToNumbers:  []string{"+15551111111", "+15552222222"},
		},
		client: server.Client(),
		apiURL: server.URL,
	}

	err := tw.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityCritical,
	})

	// Should return error from the first failed recipient.
	require.Error(t, err, "Send() should return error for partial failure")
	// Both recipients should have been attempted.
	assert.Equal(t, 2, callCount)
}

func TestTwilio_Send_AllRecipientsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "Server error"}`))
	}))
	defer server.Close()

	tw := &Twilio{
		config: TwilioConfig{
			AccountSID: "AC123",
			AuthToken:  "token",
			FromNumber: "+15551234567",
			ToNumbers:  []string{"+15551111111", "+15552222222"},
		},
		client: server.Client(),
		apiURL: server.URL,
	}

	err := tw.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityError,
	})

	require.Error(t, err, "Send() should return error when all recipients fail")
}

func TestTwilio_InterfaceCompliance(t *testing.T) {
	var _ Provider = (*Twilio)(nil)
}
