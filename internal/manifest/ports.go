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
	// entries maps PortKey to every claimant seen for that port/protocol.
	entries map[PortKey][]PortEntry

	// conflicts accumulates conflicts found during AddEntry.
	conflicts []PortConflict

	// allEntries preserves insertion order for sorted display.
	allEntries []PortEntry
}

// NewPortRegistry creates an empty PortRegistry.
func NewPortRegistry() *PortRegistry {
	return &PortRegistry{
		entries: make(map[PortKey][]PortEntry),
	}
}

// AddEntry registers a port allocation.
// Each new entry is checked pairwise against every existing claimant for the
// same (port, protocol) key. A conflict is recorded when two entries overlap:
// same bind address, or either uses the wildcard (empty BindAddr = 0.0.0.0).
func (r *PortRegistry) AddEntry(entry PortEntry) {
	entry.BindAddr = normalizeBindAddr(entry.BindAddr)
	key := PortKey{Port: entry.Port, Protocol: entry.Protocol}
	for _, existing := range r.entries[key] {
		// Same service+stack+bind is idempotent (e.g. duplicate port line).
		if existing.ServiceName == entry.ServiceName &&
			existing.StackName == entry.StackName &&
			existing.BindAddr == entry.BindAddr {
			return
		}

		// Two distinct non-empty bind addresses do not conflict because they
		// listen on separate interfaces. A wildcard (empty BindAddr, i.e.
		// 0.0.0.0) conflicts with any address because it captures all interfaces.
		if existing.BindAddr != "" && entry.BindAddr != "" && existing.BindAddr != entry.BindAddr {
			continue
		}

		r.conflicts = append(r.conflicts, PortConflict{
			Key:    key,
			First:  existing,
			Second: entry,
		})
	}
	r.entries[key] = append(r.entries[key], entry)
	r.allEntries = append(r.allEntries, entry)
}

// Conflicts returns all detected port conflicts.
func (r *PortRegistry) Conflicts() []PortConflict {
	out := make([]PortConflict, len(r.conflicts))
	copy(out, r.conflicts)
	return out
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
	return len(r.entries[PortKey{Port: port, Protocol: proto}]) > 0
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

// normalizeBindAddr converts explicit wildcard addresses ("0.0.0.0", "::")
// to empty string so conflict detection treats them the same as an omitted bind.
func normalizeBindAddr(addr string) string {
	if addr == "0.0.0.0" || addr == "::" {
		return ""
	}
	return addr
}

// ParsedPort is the fully-resolved port information from a single port entry.
type ParsedPort struct {
	HostPort int
	Protocol string // "tcp" or "udp"
	BindAddr string // "" means 0.0.0.0
}

// ParsePortEntry extracts fully-resolved port information from a compose port
// entry. Supports short string, integer, and long-syntax map formats.
//
// Per the Docker Compose spec, a bare integer or single-value string (e.g. 3000
// or "8080") specifies only a container port — Docker assigns an ephemeral host
// port at runtime. These are excluded because no deterministic host port exists
// for conflict detection.
func ParsePortEntry(entry any) []ParsedPort {
	switch v := entry.(type) {
	case int:
		// Bare integer is container-port-only (ephemeral host port); skip.
		_ = v
	case float64:
		// Some YAML decoders produce float64 for bare numbers; same semantics.
		_ = v
	case string:
		return parsePortStringFull(v)
	case map[string]any:
		return parseLongSyntaxPort(v)
	}
	return nil
}

// parsePortStringFull parses a port string such as:
//   - "8080:80"              → host 8080 (explicit mapping)
//   - "8080:80/udp"          → host 8080, UDP
//   - "127.0.0.1:8080:80"    → host 8080 on 127.0.0.1
//   - "[::1]:8080:80"        → host 8080 on IPv6 ::1
//   - "8000-8003:8000-8003"  → host range
//   - "80"                   → container-only (ephemeral host port, skipped)
func parsePortStringFull(portStr string) []ParsedPort {
	protocol := "tcp"
	if idx := strings.Index(portStr, "/"); idx != -1 {
		proto := strings.ToLower(portStr[idx+1:])
		if proto == "udp" {
			protocol = "udp"
		}
		portStr = portStr[:idx]
	}

	// Parse from the right to handle IPv6 bind addresses (e.g. "[::1]:8080:80").
	// The rightmost segment is always the container port, the second-to-last is
	// the host port, and anything before that is the bind address.
	var hostPart, bindAddr string
	hostPart, bindAddr = splitPortRight(portStr)
	if hostPart == "" {
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
//	  host_ip: 127.0.0.1
func parseLongSyntaxPort(m map[string]any) []ParsedPort {
	protocol := "tcp"
	if p, ok := m["protocol"].(string); ok && strings.ToLower(p) == "udp" {
		protocol = "udp"
	}

	bindAddr := ""
	if ip, ok := m["host_ip"].(string); ok {
		bindAddr = ip
	}

	published, ok := m["published"]
	if !ok {
		return nil
	}

	switch p := published.(type) {
	case int:
		if p <= 0 {
			return nil
		}
		return []ParsedPort{{HostPort: p, Protocol: protocol, BindAddr: bindAddr}}
	case string:
		// Handle port ranges like "8000-8003".
		ports := expandPortRange(p)
		if len(ports) == 0 {
			return nil
		}
		out := make([]ParsedPort, 0, len(ports))
		for _, port := range ports {
			out = append(out, ParsedPort{HostPort: port, Protocol: protocol, BindAddr: bindAddr})
		}
		return out
	default:
		return nil
	}
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

// splitPortRight parses a Docker Compose short-syntax port string from the
// right, returning (hostPart, bindAddr). This handles IPv6 bind addresses
// like "[::1]:8080:80" and "::1:8080:6000" correctly.
//
// Returns ("", "") for container-only ports (single value, no colon mapping).
func splitPortRight(portStr string) (hostPart string, bindAddr string) {
	// Bracketed IPv6: [::1]:8080:80
	if strings.HasPrefix(portStr, "[") {
		closeBracket := strings.Index(portStr, "]")
		if closeBracket == -1 {
			return "", ""
		}
		bindAddr = portStr[1:closeBracket]
		rest := portStr[closeBracket+1:] // e.g. ":8080:80"
		if !strings.HasPrefix(rest, ":") {
			return "", ""
		}
		rest = rest[1:] // "8080:80"
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) < 1 {
			return "", ""
		}
		return parts[0], bindAddr
	}

	// Parse from the right: last segment = container port, second-to-last = host port.
	// Count colons to decide format.
	colonCount := strings.Count(portStr, ":")
	switch colonCount {
	case 0:
		// Container port only (e.g. "80").
		return "", ""
	case 1:
		// "hostPort:containerPort"
		parts := strings.SplitN(portStr, ":", 2)
		return parts[0], ""
	case 2:
		// "bindAddr:hostPort:containerPort" — standard IPv4 (e.g. "127.0.0.1:8080:80")
		parts := strings.SplitN(portStr, ":", 3)
		return parts[1], parts[0]
	default:
		// Unbracketed IPv6: "::1:8080:80" has 4 colons.
		// Split from the right: last two segments are containerPort and hostPort.
		lastColon := strings.LastIndex(portStr, ":")
		beforeLast := portStr[:lastColon] // everything before container port
		secondLastColon := strings.LastIndex(beforeLast, ":")
		if secondLastColon == -1 {
			return "", ""
		}
		hostPart = beforeLast[secondLastColon+1:]
		bindAddr = beforeLast[:secondLastColon]
		return hostPart, bindAddr
	}
}
