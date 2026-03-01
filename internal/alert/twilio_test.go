package alert

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTwilio_Name(t *testing.T) {
	tw := NewTwilio(TwilioConfig{})
	if tw.Name() != "twilio" {
		t.Errorf("Name() = %q, want %q", tw.Name(), "twilio")
	}
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
			if got := tw.IsConfigured(); got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
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
			if got := truncateMessage(tt.msg, tt.maxLen); got != tt.want {
				t.Errorf("truncateMessage() = %q, want %q", got, tt.want)
			}
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
			if got := formatPhoneNumber(tt.number); got != tt.want {
				t.Errorf("formatPhoneNumber() = %q, want %q", got, tt.want)
			}
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
			if got := maskPhoneNumber(tt.number); got != tt.want {
				t.Errorf("maskPhoneNumber() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTwilio_buildAuthHeader(t *testing.T) {
	tw := NewTwilio(TwilioConfig{
		AccountSID: "AC123456",
		AuthToken:  "authtoken789",
	})

	got := tw.buildAuthHeader()

	// Verify it's a valid Basic auth header
	if len(got) < 7 || got[:6] != "Basic " {
		t.Fatalf("buildAuthHeader() = %q, want Basic auth header", got)
	}

	// Decode and verify credentials
	encoded := got[6:]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to decode auth header: %v", err)
	}

	want := "AC123456:authtoken789"
	if string(decoded) != want {
		t.Errorf("decoded auth = %q, want %q", string(decoded), want)
	}
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
			if got := tw.formatMessage(tt.alert); got != tt.want {
				t.Errorf("formatMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTwilio_Send_SkipsNonErrorSeverity(t *testing.T) {
	tw := NewTwilio(TwilioConfig{
		AccountSID: "AC123",
		AuthToken:  "token",
		FromNumber: "+15551234567",
		ToNumbers:  []string{"+15559876543"},
	})

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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"sid": "SM123"}`)
			}))
			defer server.Close()

			// For this test, we can't easily override the URL, so we just check
			// that the Send method returns nil for non-error severities
			// (it won't make any HTTP calls for those)
			alert := &Alert{
				Title:    "Test",
				Message:  "Test message",
				Severity: tt.severity,
				Source:   "test",
			}

			err := tw.Send(context.Background(), alert)

			if tt.wantSkip {
				if err != nil {
					t.Errorf("Send() returned error for skipped severity: %v", err)
				}
				// Note: We can't easily check if HTTP was called without mocking
				// the URL, but the function should return early for non-error severities
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
	if err == nil {
		t.Error("Send() should return error when not configured")
	}

	want := "twilio is not configured"
	if err.Error() != want {
		t.Errorf("Send() error = %q, want %q", err.Error(), want)
	}
}

func TestTwilio_Send_HTTPSuccess(t *testing.T) {
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		}
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"sid": "SM123"}`)
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
	if err != nil {
		t.Fatalf("Send() returned unexpected error: %v", err)
	}

	// Verify the SMS body was sent in the form data.
	if !containsString(receivedBody, "Body=") {
		t.Error("request body missing Body field")
	}
	if !containsString(receivedBody, "To=") {
		t.Error("request body missing To field")
	}
	if !containsString(receivedBody, "From=") {
		t.Error("request body missing From field")
	}
}

func TestTwilio_Send_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code": 21211, "message": "Invalid 'To' Phone Number"}`)
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
	if err == nil {
		t.Fatal("Send() should return error for API error response")
	}
	if !containsString(err.Error(), "status 400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestTwilio_Send_MultipleRecipients_PartialFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"message": "Invalid number"}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"sid": "SM123"}`)
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
	if err == nil {
		t.Fatal("Send() should return error for partial failure")
	}
	// Both recipients should have been attempted.
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestTwilio_Send_AllRecipientsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message": "Server error"}`)
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

	if err == nil {
		t.Fatal("Send() should return error when all recipients fail")
	}
}

func TestTwilio_InterfaceCompliance(t *testing.T) {
	var _ Provider = (*Twilio)(nil)
}
