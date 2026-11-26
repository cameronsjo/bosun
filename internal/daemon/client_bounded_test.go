package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientDecode_BoundedBody pins the 1 MiB ceiling on every success-path
// decode in the daemon client. The padded object is valid JSON, so an unbounded
// decoder returns a result; a bounded one cuts it off and reports a decode
// error.
func TestClientDecode_BoundedBody(t *testing.T) {
	calls := []struct {
		name   string
		status int
		body   string
		call   func(c *Client) error
	}{
		{
			name: "Trigger", status: http.StatusAccepted, body: `{"status":"accepted","message":"ok","pad":"%s"}`,
			call: func(c *Client) error { _, err := c.Trigger(context.Background(), "test", false); return err },
		},
		{
			name: "Status", status: http.StatusOK, body: `{"state":"idle","uptime":"1s","pad":"%s"}`,
			call: func(c *Client) error { _, err := c.Status(context.Background()); return err },
		},
		{
			name: "Health", status: http.StatusOK, body: `{"status":"healthy","pad":"%s"}`,
			call: func(c *Client) error { _, err := c.Health(context.Background()); return err },
		},
		{
			name: "Config", status: http.StatusOK, body: `{"poll_interval":60,"pad":"%s"}`,
			call: func(c *Client) error { _, err := c.Config(context.Background()); return err },
		},
	}

	for _, tt := range calls {
		for _, size := range []struct {
			name    string
			padding int
			wantErr bool
		}{
			{name: "normal body decodes", padding: 0, wantErr: false},
			{name: "body past the ceiling fails to decode", padding: MaxWebhookBodySize + 4096, wantErr: true},
		} {
			t.Run(tt.name+"/"+size.name, func(t *testing.T) {
				payload := strings.Replace(tt.body, "%s", strings.Repeat("A", size.padding), 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(payload))
				}))
				defer server.Close()

				err := tt.call(&Client{baseURL: server.URL, httpClient: server.Client()})

				if size.wantErr {
					require.Error(t, err)
					assert.ErrorContains(t, err, "failed to decode response")
					return
				}
				require.NoError(t, err)
			})
		}
	}
}
