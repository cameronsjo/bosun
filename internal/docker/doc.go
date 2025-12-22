// Package docker provides a wrapper around the Docker API for bosun operations.
//
// The Client type handles container lifecycle operations including listing,
// inspecting, starting, stopping, and collecting stats. The ComposeClient
// type manages docker-compose orchestration for stack deployments.
//
// # Interface Abstraction
//
// The DockerAPI interface abstracts the Docker SDK, enabling mock injection
// for testing. Use NewTestableClient for test scenarios.
//
// # Example
//
//	client, err := docker.NewClient()
//	if err != nil {
//	    return err
//	}
//	defer client.Close()
//
//	containers, err := client.ListContainers(ctx, true)
//	for _, c := range containers {
//	    fmt.Printf("%s: %s\n", c.Name, c.Status)
//	}
package docker
