package sandbox

import "os/exec"

// noopWrapper is the intentional-degrade backend for platforms without real
// confinement (windows, any other GOOS, and darwin/linux before their backends
// land). Wrap passes the cmd through unchanged; Available() is always false so
// the permission layer treats the mode as "normal prompting". It never claims
// confinement it cannot provide.
type noopWrapper struct{}

func newNoop() Wrapper { return noopWrapper{} }

func (noopWrapper) Wrap(cmd *exec.Cmd, _ RootSet) (*exec.Cmd, error) {
	return cmd, nil
}

func (noopWrapper) Available() bool { return false }
