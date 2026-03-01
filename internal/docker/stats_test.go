package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateCPUPercent(t *testing.T) {
	tests := []struct {
		name        string
		cpuDelta    float64
		systemDelta float64
		numCPUs     int
		want        float64
	}{
		{
			name:        "single core 10% usage",
			cpuDelta:    100_000_000,
			systemDelta: 1_000_000_000,
			numCPUs:     1,
			want:        10.0,
		},
		{
			name:        "four cores 40% total",
			cpuDelta:    100_000_000,
			systemDelta: 1_000_000_000,
			numCPUs:     4,
			want:        40.0,
		},
		{
			name:        "100% on single core",
			cpuDelta:    1_000_000_000,
			systemDelta: 1_000_000_000,
			numCPUs:     1,
			want:        100.0,
		},
		{
			name:        "zero system delta",
			cpuDelta:    100,
			systemDelta: 0,
			numCPUs:     4,
			want:        0.0,
		},
		{
			name:        "negative system delta",
			cpuDelta:    100,
			systemDelta: -1,
			numCPUs:     4,
			want:        0.0,
		},
		{
			name:        "zero cpu delta",
			cpuDelta:    0,
			systemDelta: 1_000_000_000,
			numCPUs:     4,
			want:        0.0,
		},
		{
			name:        "negative cpu delta",
			cpuDelta:    -100,
			systemDelta: 1_000_000_000,
			numCPUs:     4,
			want:        0.0,
		},
		{
			name:        "zero CPUs",
			cpuDelta:    100,
			systemDelta: 1000,
			numCPUs:     0,
			want:        0.0,
		},
		{
			name:        "both deltas zero",
			cpuDelta:    0,
			systemDelta: 0,
			numCPUs:     4,
			want:        0.0,
		},
		{
			name:        "very small deltas",
			cpuDelta:    1,
			systemDelta: 1_000_000,
			numCPUs:     2,
			want:        0.0002,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUPercent(tt.cpuDelta, tt.systemDelta, tt.numCPUs)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestCalculateMemPercent(t *testing.T) {
	tests := []struct {
		name  string
		usage uint64
		limit uint64
		want  float64
	}{
		{
			name:  "50% usage",
			usage: 536_870_912,  // 512 MB
			limit: 1_073_741_824, // 1 GB
			want:  50.0,
		},
		{
			name:  "100% usage",
			usage: 1_073_741_824,
			limit: 1_073_741_824,
			want:  100.0,
		},
		{
			name:  "zero limit (no memory cap)",
			usage: 1_000_000,
			limit: 0,
			want:  0.0,
		},
		{
			name:  "zero usage",
			usage: 0,
			limit: 1_073_741_824,
			want:  0.0,
		},
		{
			name:  "both zero",
			usage: 0,
			limit: 0,
			want:  0.0,
		},
		{
			name:  "tiny usage",
			usage: 1,
			limit: 1_073_741_824,
			want:  0.0,
		},
		{
			name:  "25% usage",
			usage: 256_000_000,
			limit: 1_024_000_000,
			want:  25.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateMemPercent(tt.usage, tt.limit)
			assert.InDelta(t, tt.want, got, 0.01)
		})
	}
}

func TestFormatPortString(t *testing.T) {
	tests := []struct {
		name        string
		publicPort  uint16
		privatePort uint16
		protocol    string
		want        string
	}{
		{
			name:        "published TCP port",
			publicPort:  8080,
			privatePort: 80,
			protocol:    "tcp",
			want:        "8080:80/tcp",
		},
		{
			name:        "unpublished TCP port",
			publicPort:  0,
			privatePort: 80,
			protocol:    "tcp",
			want:        "80/tcp",
		},
		{
			name:        "published UDP port",
			publicPort:  5353,
			privatePort: 53,
			protocol:    "udp",
			want:        "5353:53/udp",
		},
		{
			name:        "unpublished UDP port",
			publicPort:  0,
			privatePort: 53,
			protocol:    "udp",
			want:        "53/udp",
		},
		{
			name:        "same public and private port",
			publicPort:  443,
			privatePort: 443,
			protocol:    "tcp",
			want:        "443:443/tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPortString(tt.publicPort, tt.privatePort, tt.protocol)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHealthFromStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{
			name:   "healthy container",
			status: "Up 2 hours (healthy)",
			want:   "healthy",
		},
		{
			name:   "unhealthy container",
			status: "Up 5 minutes (unhealthy)",
			want:   "unhealthy",
		},
		{
			name:   "health starting",
			status: "Up 10 seconds (health: starting)",
			want:   "starting",
		},
		{
			name:   "no health info",
			status: "Up 2 hours",
			want:   "",
		},
		{
			name:   "exited container",
			status: "Exited (0) 5 minutes ago",
			want:   "",
		},
		{
			name:   "empty status",
			status: "",
			want:   "",
		},
		{
			name:   "case insensitive healthy",
			status: "Up 2 hours (Healthy)",
			want:   "healthy",
		},
		{
			name:   "case insensitive unhealthy",
			status: "Up 5 minutes (UNHEALTHY)",
			want:   "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHealthFromStatus(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}
