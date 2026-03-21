package manifest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PortKey uniquely identifies a host-port + protocol pair.
// TCP and UDP on the same port number do not conflict with each other.
type PortKey struct {
	Port     int
	Protocol string // "tcp" or "udp"
}

// PortEntry records a single port allocation from a compose file.
type PortEntry struct {
	Port        int
	Protocol    string // "tcp" or "udp"
	BindAddr    string // "" means all interfaces (0.0.0.0)
	ServiceName string
	StackName   string
}

// Qualifier returns a human-readable description of this allocation.
func (e PortEntry) Qualifier() string {
	if e.BindAddr != "" {
		return fmt.Sprintf("%s:%d/%s (%s@%s)", e.BindAddr, e.Port, e.Protocol, e.ServiceName, e.StackName)
	}
	return fmt.Sprintf("%d/%s (%s@%s)", e.Port, e.Protocol, e.ServiceName, e.StackName)
}

// PortConflict describes two services that claim the same host port+protocol.
type PortConflict struct {
	Key    PortKey
	First  PortEntry
	Second PortEntry
}

// PortRegistry collects port allocations across all compose outputs and
// detects host-port conflicts.
type PortRegistry struct {
	// entries maps PortKey to the first claimant. Populated by AddEntry.
	entries map[PortKey]PortEntry

	// conflicts accumulates conflicts found during AddEntry.
	conflicts []PortConflict

	// allEntries preserves insertion order for sorted display.
	allEntries []PortEntry
}

// NewPortRegistry creates an empty PortRegistry.
func NewPortRegistry() *PortRegistry {
	return &PortRegistry{
		entries: make(map[PortKey]PortEntry),
	}
}

// AddEntry registers a port allocation.
// If the same (port, protocol) pair is already claimed by a different
// service on the same bind address (or both on 0.0.0.0), a conflict is recorded.
func (r *PortRegistry) AddEntry(entry PortEntry) {
	r.allEntries = append(r.allEntries, entry)

	key := PortKey{Port: entry.Port, Protocol: entry.Protocol}
	existing, seen := r.entries[key]
	if !seen {
		r.entries[key] = entry
		return
	}

	// Same service in the same stack is not a conflict (idempotent).
	if existing.ServiceName == entry.ServiceName && existing.StackName == entry.StackName {
		return
	}

	// Two distinct non-empty bind addresses do not conflict because they listen
	// on separate interfaces. A wildcard (empty BindAddr, i.e. 0.0.0.0) does
	// conflict with any specific address because it captures all interfaces.
	if existing.BindAddr != "" && entry.BindAddr != "" && existing.BindAddr != entry.BindAddr {
		return
	}

	r.conflicts = append(r.conflicts, PortConflict{
		Key:    key,
		First:  existing,
		Second: entry,
	})
}

// Conflicts returns all detected port conflicts.
func (r *PortRegistry) Conflicts() []PortConflict {
	return r.conflicts
}

// Entries returns all recorded port allocations sorted by port then protocol.
func (r *PortRegistry) Entries() []PortEntry {
	sorted := make([]PortEntry, len(r.allEntries))
	copy(sorted, r.allEntries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Port != sorted[j].Port {
			return sorted[i].Port < sorted[j].Port
		}
		return sorted[i].Protocol < sorted[j].Protocol
	})
	return sorted
}

// EntriesForService returns all allocations for a named service (across all stacks).
func (r *PortRegistry) EntriesForService(name string) []PortEntry {
	var out []PortEntry
	for _, e := range r.allEntries {
		if e.ServiceName == name {
			out = append(out, e)
		}
	}
	return out
}

// FreePorts returns host ports in [start, end] with no allocation on any protocol.
func (r *PortRegistry) FreePorts(start, end int) []int {
	var free []int
	for p := start; p <= end; p++ {
		if !r.isPortUsed(p, "tcp") && !r.isPortUsed(p, "udp") {
			free = append(free, p)
		}
	}
	return free
}

func (r *PortRegistry) isPortUsed(port int, proto string) bool {
	_, ok := r.entries[PortKey{Port: port, Protocol: proto}]
	return ok
}

// =============================================================================
// Compose file parsing
// =============================================================================

// ComposePortFile represents the minimal structure of a compose YAML needed
// to extract port mappings.
type ComposePortFile struct {
	Services map[string]ComposePortService `yaml:"services"`
}

// ComposePortService holds the ports and labels for a single service.
type ComposePortService struct {
	Ports  []any             `yaml:"ports"`
	Labels map[string]string `yaml:"labels"`
}

// ParsedPort is the fully-resolved port information from a single port entry.
type ParsedPort struct {
	HostPort int
	Protocol string // "tcp" or "udp"
	BindAddr string // "" means 0.0.0.0
}

// ParsePortEntry extracts fully-resolved port information from a compose port
// entry. Supports short string, integer, and long-syntax map formats.
func ParsePortEntry(entry any) []ParsedPort {
	switch v := entry.(type) {
	case int:
		if v > 0 {
			return []ParsedPort{{HostPort: v, Protocol: "tcp"}}
		}
	case string:
		return parsePortStringFull(v)
	case map[string]any:
		return parseLongSyntaxPort(v)
	}
	return nil
}

// parsePortStringFull parses a port string such as:
//   - "80"
//   - "8080:80"
//   - "8080:80/udp"
//   - "127.0.0.1:8080:80"
//   - "8000-8003:8000-8003"
func parsePortStringFull(portStr string) []ParsedPort {
	protocol := "tcp"
	if idx := strings.Index(portStr, "/"); idx != -1 {
		proto := strings.ToLower(portStr[idx+1:])
		if proto == "udp" {
			protocol = "udp"
		}
		portStr = portStr[:idx]
	}

	parts := strings.Split(portStr, ":")
	var hostPart, bindAddr string
	switch len(parts) {
	case 1:
		hostPart = parts[0]
	case 2:
		hostPart = parts[0]
	case 3:
		bindAddr = parts[0]
		hostPart = parts[1]
	default:
		return nil
	}

	hostPorts := expandPortRange(hostPart)
	if len(hostPorts) == 0 {
		return nil
	}

	out := make([]ParsedPort, 0, len(hostPorts))
	for _, p := range hostPorts {
		out = append(out, ParsedPort{HostPort: p, Protocol: protocol, BindAddr: bindAddr})
	}
	return out
}

// parseLongSyntaxPort handles the YAML long-syntax port map:
//
//	- published: 8080
//	  target: 80
//	  protocol: tcp
func parseLongSyntaxPort(m map[string]any) []ParsedPort {
	protocol := "tcp"
	if p, ok := m["protocol"].(string); ok && strings.ToLower(p) == "udp" {
		protocol = "udp"
	}

	published, ok := m["published"]
	if !ok {
		return nil
	}

	var port int
	switch p := published.(type) {
	case int:
		port = p
	case string:
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			return nil
		}
	default:
		return nil
	}

	if port <= 0 {
		return nil
	}
	return []ParsedPort{{HostPort: port, Protocol: protocol}}
}

// expandPortRange converts a host-part string ("8080" or "8000-8003") into a
// slice of individual port numbers. Returns nil for invalid inputs.
func expandPortRange(hostPart string) []int {
	if !strings.Contains(hostPart, "-") {
		p, err := strconv.Atoi(hostPart)
		if err != nil || p <= 0 {
			return nil
		}
		return []int{p}
	}

	rangeParts := strings.SplitN(hostPart, "-", 2)
	start, err1 := strconv.Atoi(rangeParts[0])
	end, err2 := strconv.Atoi(rangeParts[1])
	if err1 != nil || err2 != nil || start > end || start <= 0 {
		return nil
	}

	out := make([]int, 0, end-start+1)
	for p := start; p <= end; p++ {
		out = append(out, p)
	}
	return out
}
