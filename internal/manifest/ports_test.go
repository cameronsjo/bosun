package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ParsePortEntry
// =============================================================================

func TestParsePortEntry_IntegerPort(t *testing.T) {
	result := ParsePortEntry(80)
	require.Len(t, result, 1)
	assert.Equal(t, 80, result[0].HostPort)
	assert.Equal(t, "tcp", result[0].Protocol)
	assert.Empty(t, result[0].BindAddr)
}

func TestParsePortEntry_ZeroInteger(t *testing.T) {
	result := ParsePortEntry(0)
	assert.Empty(t, result)
}

func TestParsePortEntry_StringShortSyntax(t *testing.T) {
	result := ParsePortEntry("8080")
	require.Len(t, result, 1)
	assert.Equal(t, 8080, result[0].HostPort)
	assert.Equal(t, "tcp", result[0].Protocol)
}

func TestParsePortEntry_StringMapped(t *testing.T) {
	result := ParsePortEntry("8080:80")
	require.Len(t, result, 1)
	assert.Equal(t, 8080, result[0].HostPort)
}

func TestParsePortEntry_StringWithTCPProtocol(t *testing.T) {
	result := ParsePortEntry("8080:80/tcp")
	require.Len(t, result, 1)
	assert.Equal(t, "tcp", result[0].Protocol)
}

func TestParsePortEntry_StringWithUDPProtocol(t *testing.T) {
	result := ParsePortEntry("53:53/udp")
	require.Len(t, result, 1)
	assert.Equal(t, 53, result[0].HostPort)
	assert.Equal(t, "udp", result[0].Protocol)
}

func TestParsePortEntry_StringHostBound(t *testing.T) {
	result := ParsePortEntry("127.0.0.1:8080:80")
	require.Len(t, result, 1)
	assert.Equal(t, 8080, result[0].HostPort)
	assert.Equal(t, "127.0.0.1", result[0].BindAddr)
}

func TestParsePortEntry_StringPortRange(t *testing.T) {
	result := ParsePortEntry("8000-8002:8000-8002")
	require.Len(t, result, 3)
	assert.Equal(t, 8000, result[0].HostPort)
	assert.Equal(t, 8001, result[1].HostPort)
	assert.Equal(t, 8002, result[2].HostPort)
}

func TestParsePortEntry_LongSyntaxIntPublished(t *testing.T) {
	entry := map[string]any{"published": 9090, "target": 80}
	result := ParsePortEntry(entry)
	require.Len(t, result, 1)
	assert.Equal(t, 9090, result[0].HostPort)
	assert.Equal(t, "tcp", result[0].Protocol)
}

func TestParsePortEntry_LongSyntaxStringPublished(t *testing.T) {
	entry := map[string]any{"published": "9090", "target": 80}
	result := ParsePortEntry(entry)
	require.Len(t, result, 1)
	assert.Equal(t, 9090, result[0].HostPort)
}

func TestParsePortEntry_LongSyntaxUDPProtocol(t *testing.T) {
	entry := map[string]any{"published": 5353, "target": 5353, "protocol": "udp"}
	result := ParsePortEntry(entry)
	require.Len(t, result, 1)
	assert.Equal(t, "udp", result[0].Protocol)
}

func TestParsePortEntry_LongSyntaxMissingPublished(t *testing.T) {
	entry := map[string]any{"target": 80}
	result := ParsePortEntry(entry)
	assert.Empty(t, result)
}

func TestParsePortEntry_NilEntry(t *testing.T) {
	result := ParsePortEntry(nil)
	assert.Empty(t, result)
}

// =============================================================================
// expandPortRange
// =============================================================================

func TestExpandPortRange_Single(t *testing.T) {
	result := expandPortRange("8080")
	assert.Equal(t, []int{8080}, result)
}

func TestExpandPortRange_Range(t *testing.T) {
	result := expandPortRange("8000-8003")
	assert.Equal(t, []int{8000, 8001, 8002, 8003}, result)
}

func TestExpandPortRange_ReverseRange(t *testing.T) {
	result := expandPortRange("8010-8000")
	assert.Nil(t, result)
}

func TestExpandPortRange_ZeroPort(t *testing.T) {
	result := expandPortRange("0")
	assert.Nil(t, result)
}

func TestExpandPortRange_InvalidString(t *testing.T) {
	result := expandPortRange("notaport")
	assert.Nil(t, result)
}

// =============================================================================
// PortRegistry - AddEntry and conflict detection
// =============================================================================

func TestPortRegistry_NoConflicts(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"})
	r.AddEntry(PortEntry{Port: 3000, Protocol: "tcp", ServiceName: "api", StackName: "stack1"})

	assert.Empty(t, r.Conflicts())
}

func TestPortRegistry_ConflictAcrossStacks(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"})
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "api", StackName: "stack2"})

	conflicts := r.Conflicts()
	require.Len(t, conflicts, 1)
	assert.Equal(t, 8080, conflicts[0].Key.Port)
	assert.Equal(t, "tcp", conflicts[0].Key.Protocol)
}

func TestPortRegistry_TCPAndUDPDoNotConflict(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 53, Protocol: "tcp", ServiceName: "dns", StackName: "stack1"})
	r.AddEntry(PortEntry{Port: 53, Protocol: "udp", ServiceName: "dns", StackName: "stack1"})

	assert.Empty(t, r.Conflicts())
}

func TestPortRegistry_SameServiceNoConflict(t *testing.T) {
	r := NewPortRegistry()
	e := PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "stack1"}
	r.AddEntry(e)
	r.AddEntry(e)

	assert.Empty(t, r.Conflicts())
}

func TestPortRegistry_DifferentBindAddressNoConflict(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "192.168.1.1", ServiceName: "api", StackName: "s2"})

	assert.Empty(t, r.Conflicts())
}

func TestPortRegistry_WildcardConflictsWithSpecificBind(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "api", StackName: "s2"})

	assert.Len(t, r.Conflicts(), 1)
}

func TestPortRegistry_MultipleConflicts(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 80, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 80, Protocol: "tcp", ServiceName: "nginx", StackName: "s2"})
	r.AddEntry(PortEntry{Port: 443, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 443, Protocol: "tcp", ServiceName: "caddy", StackName: "s3"})

	assert.Len(t, r.Conflicts(), 2)
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

	webEntries := r.EntriesForService("web")
	require.Len(t, webEntries, 2)
	for _, e := range webEntries {
		assert.Equal(t, "web", e.ServiceName)
	}
}

func TestPortRegistry_EntriesForServiceEmpty(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"})

	result := r.EntriesForService("nonexistent")
	assert.Empty(t, result)
}

func TestPortRegistry_FreePortsInRange(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8081, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 8083, Protocol: "udp", ServiceName: "dns", StackName: "s1"})

	// 8080 free, 8081 used (tcp), 8082 free, 8083 used (udp), 8084 free
	free := r.FreePorts(8080, 8084)
	assert.Equal(t, []int{8080, 8082, 8084}, free)
}

func TestPortRegistry_FreePortsEmptyRegistry(t *testing.T) {
	r := NewPortRegistry()
	free := r.FreePorts(9000, 9002)
	assert.Equal(t, []int{9000, 9001, 9002}, free)
}

func TestPortRegistry_FreePortsAllUsed(t *testing.T) {
	r := NewPortRegistry()
	r.AddEntry(PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
	r.AddEntry(PortEntry{Port: 8081, Protocol: "tcp", ServiceName: "api", StackName: "s1"})

	free := r.FreePorts(8080, 8081)
	assert.Empty(t, free)
}

// =============================================================================
// PortEntry.Qualifier
// =============================================================================

func TestPortEntry_QualifierWithBindAddr(t *testing.T) {
	e := PortEntry{Port: 8080, Protocol: "tcp", BindAddr: "127.0.0.1", ServiceName: "web", StackName: "s1"}
	assert.Equal(t, "127.0.0.1:8080/tcp (web@s1)", e.Qualifier())
}

func TestPortEntry_QualifierWithoutBindAddr(t *testing.T) {
	e := PortEntry{Port: 8080, Protocol: "tcp", ServiceName: "web", StackName: "s1"}
	assert.Equal(t, "8080/tcp (web@s1)", e.Qualifier())
}
