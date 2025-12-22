package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
)

// DefaultOperationTimeout is the default timeout for Docker operations.
const DefaultOperationTimeout = 30 * time.Second

// withDockerClient executes a function with a Docker client and default timeout context.
// Use withDockerClientContext for custom context handling.
func withDockerClient(fn func(ctx context.Context, client *docker.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultOperationTimeout)
	defer cancel()

	return withDockerClientContext(ctx, func(client *docker.Client) error {
		return fn(ctx, client)
	})
}

// withDockerClientContext executes a function with a Docker client and custom context.
// The context is used for cancellation and timeout control.
func withDockerClientContext(ctx context.Context, fn func(*docker.Client) error) error {
	client, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer client.Close()

	return fn(client)
}
