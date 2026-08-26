package tunnel

import (
	"context"
	"fmt"
	"sync"
)

type commandCall struct {
	name string
	args []string
}

type stubCommandRunner struct {
	mu sync.Mutex

	outputFn func(context.Context, string, ...string) ([]byte, string, error)
	runFn    func(context.Context, string, ...string) error
	calls    []commandCall
}

func (s *stubCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	s.record(name, args)
	if s.outputFn == nil {
		return nil, "", nil
	}
	return s.outputFn(ctx, name, args...)
}

func (s *stubCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	s.record(name, args)
	if s.runFn == nil {
		return nil
	}
	return s.runFn(ctx, name, args...)
}

func (s *stubCommandRunner) record(name string, args []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, commandCall{name: name, args: append([]string(nil), args...)})
}

func (s *stubCommandRunner) recordedCalls() []commandCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]commandCall(nil), s.calls...)
}

type stubExitError int

func (e stubExitError) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e stubExitError) ExitCode() int { return int(e) }
