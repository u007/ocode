package computer

// windowsArgs returns the argv for PowerShell that runs the
// embedded script with the given op and parameters.
func windowsArgs(scriptPath, op string, params ...string) []string {
	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, op,
	}
	for _, p := range params {
		args = append(args, p)
	}
	return args
}
