package cmd

import (
	"context"
	"fmt"

	"github.com/cameronsjo/bosun/internal/docker"
)

// withDockerClient executes a function with a Docker client, handling connection and cleanup.
// Use this for simple operations that don't need special context handling.
func withDockerClient(fn func(ctx context.Context, client *docker.Client) error) error {
	client, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer client.Close()

	return fn(context.Background(), client)
}

// withDockerClientContext executes a function with a Docker client and custom context.
// Use this when the caller needs to control the context (e.g., for cancellation or timeout).
func withDockerClientContext(ctx context.Context, fn func(*docker.Client) error) error {
	client, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer client.Close()

	return fn(client)
}
