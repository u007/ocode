## Plan: Desktop Login Shell Env

1. Find bash execution in agent/tool code (`shellExecCommand` or desktop equivalent).
2. Modify to read `shell` from global config (`ocodeconfig.json`), default `zsh`.
3. Change `exec.Command("bash", "-c", ...)` to `exec.Command(configuredShell, "-l", "-c", ...)`.
4. Verify desktop binary builds.
