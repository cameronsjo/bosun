package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ParsePortEntry
// =============================================================================

func TestParsePortEntry(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		wantLen  int
		wantPort int
		wantProto string
		wantBind string
	}{
		{"integer port", 80, 1, 80, "tcp", ""},
		{"zero integer", 0, 0, 0, "", ""},
		{"nil entry", nil, 0, 0, "", ""},
		{"short syntax", "8080", 1, 8080, "tcp", ""},
		{"mapped ports", "8080:80", 1, 8080, "tcp", ""},
		{"explicit tcp", "8080:80/tcp", 1, 8080, "tcp", ""},
		{"udp protocol", "53:53/udp", 1, 53, "udp", ""},
		{"host bound", "127.0.0.1:8080:80", 1, 8080, "tcp", "127.0.0.1"},
		{"long syntax int published", map[string]any{"published": 9090, "target": 80}, 1, 9090, "tcp", ""},
		{"long syntax string published", map[string]any{"published": "9090", "target": 80}, 1, 9090, "tcp", ""},
		{"long syntax udp", map[string]any{"published": 5353, "target": 5353, "protocol": "udp"}, 1, 5353, "udp", ""},
		{"long syntax missing published", map[string]any{"target": 80}, 0, 0, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ParsePortEntry(tc.input)
			require.Len(t, result, tc.wantLen)
			if tc.wantLen > 0 {
				assert.Equal(t, tc.wantPort, result[0].HostPort)
				assert.Equal(t, tc.wantProto, result[0].Protocol)
				assert.Equal(t, tc.wantBind, result[0].BindAddr)
			}
		})
	}
}

func TestParsePortEntry_PortRange(t *testing.T) {
	result := ParsePortEntry("8000-8002:8000-8002")
	require.Len(t, result, 3)
	assert.Equal(t, 8000, result[0].HostPort)
	assert.Equal(t, 8001, result[1].HostPort)
	assert.Equal(t, 8002, result[2].HostPort)
}

// =============================================================================
// expandPortRange
// =============================================================================

func TestExpandPortRange(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{"single port", "8080", []int{8080}},
		{"range", "8000-8003", []int{8000, 8001, 8002, 8003}},
		{"reversed range", "8010-8000", nil},
		{"zero port", "0", nil},
		{"invalid string", "notaport", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := expandPortRange(tc.input)
			assert.Equal(t, tc.want, result)
		})
	}
}

// =============================================================================
// PortRegistry - AddEntry and conflict detection
// =============================================================================

func TestPortRegistry_Conflicts(t *testing.T) {
	tests := []struct {
		name          string
		entries       []PortEntry
		wantConflicts int
		wantEntries   int // 0 = len(entries), >0 = exact count
	}{
		{
			name: "no conflicts on different ports",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"},
				{Port: 3000, Protocol: "tcp", ServiceName: "api", StackName: "stack1"},
			},
			wantConflicts: 0,
		},
		{
			name: "conflict across stacks",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"},
				{Port: 8080, Protocol: "tcp", ServiceName: "api", StackName: "stack2"},
			},
			wantConflicts: 1,
		},
		{
			name: "tcp and udp do not conflict",
			entries: []PortEntry{
				{Port: 53, Protocol: "tcp", ServiceName: "dns", StackName: "stack1"},
				{Port: 53, Protocol: "udp", ServiceName: "dns", StackName: "stack1"},
			},
			wantConflicts: 0,
		},
		{
			name: "same service is idempotent",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"},
				{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"},
			},
			wantConflicts: 0,
			wantEntries:   1, // duplicate discarded, not double-counted
		},
		{
			name: "different bind addresses do not conflict",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "web", StackName: "s1"},
				{Port: 8080, Protocol: "tcp", BindAddr: "192.168.1.1", ServiceName: "api", StackName: "s2"},
			},
			wantConflicts: 0,
		},
		{
			name: "wildcard conflicts with specific bind",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", BindAddr: "", ServiceName: "web", StackName: "s1"},
				{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "api", StackName: "s2"},
			},
			wantConflicts: 1,
		},
		{
			name: "multiple port conflicts",
			entries: []PortEntry{
				{Port: 80, Protocol: "tcp", ServiceName: "web", StackName: "s1"},
				{Port: 80, Protocol: "tcp", ServiceName: "nginx", StackName: "s2"},
				{Port: 443, Protocol: "tcp", ServiceName: "web", StackName: "s1"},
				{Port: 443, Protocol: "tcp", ServiceName: "caddy", StackName: "s3"},
			},
			wantConflicts: 2,
		},
		{
			name: "three-way pairwise conflict with wildcard",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "web", StackName: "s1"},
				{Port: 8080, Protocol: "tcp", BindAddr: "192.168.1.10", ServiceName: "api", StackName: "s2"},
				{Port: 8080, Protocol: "tcp", BindAddr: "", ServiceName: "proxy", StackName: "s3"},
			},
			wantConflicts: 2, // wildcard conflicts with both specific addresses
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewPortRegistry()
			for _, e := range tc.entries {
				r.AddEntry(e)
			}
			assert.Len(t, r.Conflicts(), tc.wantConflicts)
			if tc.wantEntries > 0 {
				assert.Len(t, r.Entries(), tc.wantEntries)
			}
		})
	}
}

// =============================================================================
// PortRegistry - Entries / EntriesForService / FreePorts
// =============================================================================

func TestPortRegistry_EntriesSortedByPort(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 9000, Protocol: "tcp", ServiceName: "api", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 80, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 443, Protocol: "tcp", ServiceName: "web", StackName: "s1"})

	entries := r.Entries()
	require.Len(t, entries, 3)
	assert.Equal(t, 80, entries[0].Port)
	assert.Equal(t, 443, entries[1].Port)
	assert.Equal(t, 9000, entries[2].Port)
}

func TestPortRegistry_EntriesForService(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 3000, Protocol: "tcp", ServiceName: "api", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 8443, Protocol: "tcp", ServiceName: "web", StackName: "s1"})

	t.Run("existing service", func(t *testing.T) {
		webEntries := r.EntriesForService("web")
		require.Len(t, webEntries, 2)
		for _, e := range webEntries {
			assert.Equal(t, "web", e.ServiceName)
		}
	})

	t.Run("nonexistent service", func(t *testing.T) {
		result := r.EntriesForService("nonexistent")
		assert.Empty(t, result)
	})
}

func TestPortRegistry_FreePorts(t *testing.T) {
	tests := []struct {
		name    string
		entries []PortEntry
		start   int
		end     int
		want    []int
	}{
		{
			name: "mixed used and free",
			entries: []PortEntry{
				{Port: 8081, Protocol: "tcp", ServiceName: "web", StackName: "s1"},
				{Port: 8083, Protocol: "udp", ServiceName: "dns", StackName: "s1"},
			},
			start: 8080, end: 8084,
			want: []int{8080, 8082, 8084},
		},
		{
			name:    "empty registry",
			entries: nil,
			start:   9000, end: 9002,
			want: []int{9000, 9001, 9002},
		},
		{
			name: "all used",
			entries: []PortEntry{
				{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"},
				{Port: 8081, Protocol: "tcp", ServiceName: "api", StackName: "s1"},
			},
			start: 8080, end: 8081,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewPortRegistry()
			for _, e := range tc.entries {
				r.AddEntry(e)
			}
			free := r.FreePorts(tc.start, tc.end)
			if tc.want == nil {
				assert.Empty(t, free)
			} else {
				assert.Equal(t, tc.want, free)
			}
		})
	}
}

// =============================================================================
// PortEntry.Qualifier
// =============================================================================

func TestPortEntry_Qualifier(t *testing.T) {
	tests := []struct {
		name  string
		entry PortEntry
		want  string
	}{
		{
			name:  "with bind address",
			entry: PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "web", StackName: "s1"},
			want:  "127.0.0.1:8080/tcp (web@s1)",
		},
		{
			name:  "without bind address",
			entry: PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"},
			want:  "8080/tcp (web@s1)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.entry.Qualifier())
		})
	}
}
