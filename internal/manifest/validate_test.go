package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAPIVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "valid v1 version",
			version: "bosun.io/v1",
			wantErr: false,
		},
		{
			name:    "empty version (backwards compatible)",
			version: "",
			wantErr: false,
		},
		{
			name:    "unsupported version",
			version: "bosun.io/v999",
			wantErr: true,
		},
		{
			name:    "invalid format",
			version: "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAPIVersion(tt.version)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrUnsupportedAPIVersion)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateKind(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		expected string
		wantErr  error
	}{
		{
			name:     "valid Provision kind",
			kind:     "Provision",
			expected: "Provision",
			wantErr:  nil,
		},
		{
			name:     "valid Stack kind",
			kind:     "Stack",
			expected: "Stack",
			wantErr:  nil,
		},
		{
			name:     "valid Service kind",
			kind:     "Service",
			expected: "Service",
			wantErr:  nil,
		},
		{
			name:     "empty kind (backwards compatible)",
			kind:     "",
			expected: "Provision",
			wantErr:  nil,
		},
		{
			name:     "invalid kind",
			kind:     "Unknown",
			expected: "Provision",
			wantErr:  ErrInvalidKind,
		},
		{
			name:     "kind mismatch",
			kind:     "Service",
			expected: "Provision",
			wantErr:  ErrKindMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKind(tt.kind, tt.expected)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{
			name: "versioned manifest",
			data: `apiVersion: bosun.io/v1
kind: Provision
compose:
  services: {}`,
			wantErr: false,
		},
		{
			name: "unversioned manifest (backwards compatible)",
			data: `compose:
  services: {}`,
			wantErr: false,
		},
		{
			name: "invalid apiVersion",
			data: `apiVersion: invalid/v999
kind: Provision`,
			wantErr: true,
		},
		{
			name:    "invalid YAML",
			data:    `invalid: yaml: [`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ValidateManifest([]byte(tt.data))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, meta)
			}
		})
	}
}

func TestValidateManifestStrict(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		expectedKind string
		wantErr      error
	}{
		{
			name: "valid versioned manifest",
			data: `apiVersion: bosun.io/v1
kind: Provision`,
			expectedKind: "Provision",
			wantErr:      nil,
		},
		{
			name: "missing apiVersion",
			data: `kind: Provision`,
			expectedKind: "Provision",
			wantErr:      ErrMissingAPIVersion,
		},
		{
			name: "missing kind",
			data: `apiVersion: bosun.io/v1`,
			expectedKind: "Provision",
			wantErr:      ErrMissingKind,
		},
		{
			name: "kind mismatch",
			data: `apiVersion: bosun.io/v1
kind: Stack`,
			expectedKind: "Provision",
			wantErr:      ErrKindMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ValidateManifestStrict([]byte(tt.data), tt.expectedKind)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, meta)
				assert.Equal(t, APIVersionV1, meta.APIVersion)
				assert.Equal(t, tt.expectedKind, meta.Kind)
			}
		})
	}
}

func TestIsVersioned(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantResult bool
		wantErr    bool
	}{
		{
			name: "fully versioned",
			data: `apiVersion: bosun.io/v1
kind: Provision`,
			wantResult: true,
			wantErr:    false,
		},
		{
			name:       "apiVersion only",
			data:       `apiVersion: bosun.io/v1`,
			wantResult: false,
			wantErr:    false,
		},
		{
			name:       "kind only",
			data:       `kind: Provision`,
			wantResult: false,
			wantErr:    false,
		},
		{
			name:       "unversioned",
			data:       `compose: {}`,
			wantResult: false,
			wantErr:    false,
		},
		{
			name:       "invalid YAML",
			data:       `invalid: yaml: [`,
			wantResult: false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := IsVersioned([]byte(tt.data))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}

func TestGetManifestKind(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantKind string
		wantErr  bool
	}{
		{
			name: "with kind",
			data: `apiVersion: bosun.io/v1
kind: Stack`,
			wantKind: "Stack",
			wantErr:  false,
		},
		{
			name:     "without kind",
			data:     `compose: {}`,
			wantKind: "",
			wantErr:  false,
		},
		{
			name:     "invalid YAML",
			data:     `invalid: yaml: [`,
			wantKind: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, err := GetManifestKind([]byte(tt.data))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKind, kind)
			}
		})
	}
}
