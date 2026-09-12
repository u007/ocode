package computer

import (
	"context"
	"fmt"
	"strings"
)

// stubRunner is a test-only commandRunner that records every
// call as a slice of {name, args...} and returns a configurable
// (stdout, err). It is shared by driver tests in later parts.
type stubRunner struct {
	Calls []string
	Stdout string
	Err    error
}

func (s *stubRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	s.Calls = append(s.Calls, strings.Join(append([]string{name}, args...), " "))
	return s.Stdout, s.Err
}

func (s *stubRunner) runStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	s.Calls = append(s.Calls, fmt.Sprintf("%s %s < stdin", name, strings.Join(args, " ")))
	return s.Stdout, s.Err
}
