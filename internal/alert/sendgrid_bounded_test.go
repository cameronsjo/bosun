package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendGrid_Send_ErrorBodyBounded pins that an error response is decoded
// under a size cap. The object is valid JSON, but its closing brace sits past 1
// MiB. Unbounded, the decoder reads all of it and reports the provider's
// message; bounded, the body is cut off, fails to parse, and the status-only
// message is used.
func TestSendGrid_Send_ErrorBodyBounded(t *testing.T) {
	const limit = 1 << 20

	tests := []struct {
		name    string
		padding int
		wantErr string
	}{
		{name: "small body keeps the provider message", padding: 0, wantErr: "sendgrid api error (status 400): boom"},
		{name: "body past the limit falls back to the status", padding: limit + 4096, wantErr: "sendgrid api error: status 400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"errors":[{"message":"boom"}],"pad":"` + strings.Repeat("A", tt.padding) + `"}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			sg := &SendGrid{
				config: SendGridConfig{
					APIKey:    "[redacted: secret-named value]",
					FromEmail: "sender@example.com",
					ToEmails:  []string{"ops@example.com"},
				},
				client:   server.Client(),
				endpoint: server.URL,
			}

			err := sg.Send(context.Background(), &Alert{Title: "Test", Message: "Test", Severity: SeverityCritical})

			require.Error(t, err)
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}
