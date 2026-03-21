// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PortMapping represents a port extracted from a compose file.
type PortMapping struct {
	Port        int
	ServiceName string
	Source      string // "ports" or "traefik"
}

// ComposeFileWithPorts represents a Docker Compose file structure for port extraction.
type ComposeFileWithPorts struct {
	Services map[string]ServiceWithPorts `yaml:"services"`
}

// ServiceWithPorts represents a service with ports and labels.
type ServiceWithPorts struct {
	Ports  []any             `yaml:"ports"`
	Labels map[string]string `yaml:"labels"`
}

// extractPorts parses a compose file and extracts all port mappings.
// Returns a map of host port -> service name.
func extractPorts(filename string) map[int]string {
	portMap := make(map[int]string)

	data, err := os.ReadFile(filename)
	if err != nil {
		return portMap
	}

	var compose ComposeFileWithPorts
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return portMap
	}

	for serviceName, service := range compose.Services {
		// Extract ports from ports: section
		for _, portEntry := range service.Ports {
			ports := parsePortEntry(portEntry)
			for _, port := range ports {
				portMap[port] = serviceName
			}
		}

		// Extract ports from traefik labels
		for labelKey, labelValue := range service.Labels {
			if strings.Contains(labelKey, "loadbalancer.server.port") {
				port, err := strconv.Atoi(labelValue)
				if err == nil && port > 0 {
					portMap[port] = serviceName + " (traefik)"
				}
			}
		}
	}

	return portMap
}

// parsePortEntry extracts host ports from various port mapping formats.
// Supports:
// - Short syntax: "80", 80
// - Standard mapping: "8080:80", "8080:80/tcp"
// - Host-bound: "127.0.0.1:8080:80"
// - Port ranges: "8000-8010:8000-8010"
func parsePortEntry(entry any) []int {
	var ports []int

	switch v := entry.(type) {
	case int:
		// Short syntax: - 80
		if v > 0 {
			ports = append(ports, v)
		}
	case string:
		ports = append(ports, parsePortString(v)...)
	case map[string]any:
		// Long syntax: published: 8080, target: 80
		if published, ok := v["published"]; ok {
			switch p := published.(type) {
			case int:
				ports = append(ports, p)
			case string:
				if port, err := strconv.Atoi(p); err == nil {
					ports = append(ports, port)
				}
			}
		}
	}

	return ports
}

// parsePortString parses a port string and returns host ports.
func parsePortString(portStr string) []int {
	var ports []int

	// Remove protocol suffix (e.g., /tcp, /udp)
	if idx := strings.Index(portStr, "/"); idx != -1 {
		portStr = portStr[:idx]
	}

	// Split by colon to find components
	parts := strings.Split(portStr, ":")

	var hostPart string
	switch len(parts) {
	case 1:
		// Short syntax: "80"
		hostPart = parts[0]
	case 2:
		// Standard: "8080:80"
		hostPart = parts[0]
	case 3:
		// Host-bound: "127.0.0.1:8080:80"
		hostPart = parts[1]
	default:
		return ports
	}

	// Handle port ranges (e.g., "8000-8010")
	if strings.Contains(hostPart, "-") {
		rangeParts := strings.Split(hostPart, "-")
		if len(rangeParts) == 2 {
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 == nil && err2 == nil && start <= end {
				for port := start; port <= end; port++ {
					ports = append(ports, port)
				}
			}
		}
	} else {
		// Single port
		if port, err := strconv.Atoi(hostPart); err == nil && port > 0 {
			ports = append(ports, port)
		}
	}

	return ports
}
