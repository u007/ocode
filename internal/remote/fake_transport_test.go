package remote

import "io"

// fakeTransport is a scriptable Transport for unit tests — no real ssh/scp
// process is ever spawned.
type fakeTransport struct {
	// execResults maps an exact command string to its canned result. A
	// command not present here uses execDefault.
	execResults map[string]ExecResult
	execErrs    map[string]error
	execDefault ExecResult

	execCalls       []string
	execStdinCalls  []string
	interactiveCall string
	copyDestPath    string
	copyContent     []byte

	copyErr error
}

var _ Transport = (*fakeTransport)(nil)

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		execResults: map[string]ExecResult{},
		execErrs:    map[string]error{},
	}
}

func (f *fakeTransport) Exec(command string) (ExecResult, error) {
	f.execCalls = append(f.execCalls, command)
	if err, ok := f.execErrs[command]; ok {
		return ExecResult{}, err
	}
	if res, ok := f.execResults[command]; ok {
		return res, nil
	}
	return f.execDefault, nil
}

func (f *fakeTransport) ExecStdin(command string, stdin io.Reader) (ExecResult, error) {
	f.execStdinCalls = append(f.execStdinCalls, command)
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		f.copyContent = b
	}
	if err, ok := f.execErrs[command]; ok {
		return ExecResult{}, err
	}
	if res, ok := f.execResults[command]; ok {
		return res, nil
	}
	return f.execDefault, nil
}

func (f *fakeTransport) ExecInteractive(command string) error {
	f.interactiveCall = command
	return nil
}

func (f *fakeTransport) Copy(src io.Reader, size int64, destPath string) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	b, _ := io.ReadAll(src)
	f.copyContent = b
	f.copyDestPath = destPath
	return nil
}

func (f *fakeTransport) Describe() string { return "fake" }
