package cmd

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestComputeHMAC(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		secret string
		want   string
	}{
		{
			name:   "simple message",
			data:   []byte("hello world"),
			secret: "secret",
			want:   computeExpectedHMAC([]byte("hello world"), "secret"),
		},
		{
			name:   "empty data",
			data:   []byte{},
			secret: "secret",
			want:   computeExpectedHMAC([]byte{}, "secret"),
		},
		{
			name:   "json payload",
			data:   []byte(`{"action":"push","ref":"refs/heads/main"}`),
			secret: "webhook-secret-123",
			want:   computeExpectedHMAC([]byte(`{"action":"push","ref":"refs/heads/main"}`), "webhook-secret-123"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHMAC(tt.data, tt.secret)
			if got != tt.want {
				t.Errorf("computeHMAC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeHMACSHA1(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		secret string
	}{
		{
			name:   "simple message",
			data:   []byte("hello world"),
			secret: "secret",
		},
		{
			name:   "json payload",
			data:   []byte(`{"push":{"changes":[]}}`),
			secret: "bitbucket-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHMACSHA1(tt.data, tt.secret)

			// Verify it's valid hex
			_, err := hex.DecodeString(got)
			if err != nil {
				t.Errorf("computeHMACSHA1() returned invalid hex: %v", err)
			}

			// Verify length (SHA1 = 20 bytes = 40 hex chars)
			if len(got) != 40 {
				t.Errorf("computeHMACSHA1() length = %d, want 40", len(got))
			}

			// Verify it matches manual computation
			mac := hmac.New(sha1.New, []byte(tt.secret))
			mac.Write(tt.data)
			expected := hex.EncodeToString(mac.Sum(nil))
			if got != expected {
				t.Errorf("computeHMACSHA1() = %v, want %v", got, expected)
			}
		})
	}
}

func TestValidateSignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"push"}`)
	validSig := "sha256=" + computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid signature with prefix",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid signature without prefix",
			body:      payload,
			signature: computeHMAC(payload, secret),
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "sha256=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSig,
			secret:    "",
			want:      false,
		},
		{
			name:      "wrong secret",
			body:      payload,
			signature: validSig,
			secret:    "wrong-secret",
			want:      false,
		},
		{
			name:      "modified payload",
			body:      []byte(`{"action":"pull"}`),
			signature: validSig,
			secret:    secret,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateGitHubSignature(t *testing.T) {
	secret := "github-webhook-secret"
	payload := []byte(`{"ref":"refs/heads/main","pusher":{"name":"user"}}`)
	validSig := "sha256=" + computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid GitHub signature",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "missing sha256 prefix",
			body:      payload,
			signature: computeHMAC(payload, secret),
			secret:    secret,
			want:      false, // GitHub requires prefix
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "short signature",
			body:      payload,
			signature: "sha256",
			secret:    secret,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGitHubSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateGitHubSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateGiteaSignature(t *testing.T) {
	secret := "gitea-secret"
	payload := []byte(`{"ref":"refs/heads/main","pusher":{"login":"user"}}`)
	validSig := computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid Gitea signature",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSig,
			secret:    "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGiteaSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateGiteaSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateBitbucketSignature(t *testing.T) {
	secret := "bitbucket-secret"
	payload := []byte(`{"push":{"changes":[{"new":{"name":"main"}}]}}`)
	validSHA256 := "sha256=" + computeHMAC(payload, secret)
	validSHA1 := "sha1=" + computeHMACSHA1(payload, secret)
	validPlain := computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid sha256 signature (Bitbucket Cloud)",
			body:      payload,
			signature: validSHA256,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid sha1 signature (Bitbucket Server)",
			body:      payload,
			signature: validSHA1,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid plain signature (fallback)",
			body:      payload,
			signature: validPlain,
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid sha256 signature",
			body:      payload,
			signature: "sha256=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "invalid sha1 signature",
			body:      payload,
			signature: "sha1=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSHA256,
			secret:    "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateBitbucketSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateBitbucketSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// computeExpectedHMAC is a helper for test verification.
func computeExpectedHMAC(data []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
