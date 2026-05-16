package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func RunPreHook(toolName string, args string, hooks map[string]config.HookConfig) error {
	cmds := resolveHooks(toolName, hooks, true)
	return executeHooks(cmds, toolName, args)
}

func RunPostHook(toolName string, args string, result string, hooks map[string]config.HookConfig) error {
	cmds := resolveHooks(toolName, hooks, false)
	return executePostHooks(cmds, toolName, args, result)
}

func resolveHooks(toolName string, hooks map[string]config.HookConfig, pre bool) []string {
	var cmds []string

	if wildcard, ok := hooks["*"]; ok {
		if pre {
			cmds = append(cmds, wildcard.Pre...)
		} else {
			cmds = append(cmds, wildcard.Post...)
		}
	}

	if hc, ok := hooks[toolName]; ok {
		if pre {
			cmds = append(cmds, hc.Pre...)
		} else {
			cmds = append(cmds, hc.Post...)
		}
	}

	return cmds
}

func executeHooks(cmds []string, toolName string, args string) error {
	for _, cmd := range cmds {
		if err := runHook(cmd, toolName, args, ""); err != nil {
			return fmt.Errorf("pre-hook %q failed: %w", cmd, err)
		}
	}
	return nil
}

func executePostHooks(cmds []string, toolName string, args string, result string) error {
	for _, cmd := range cmds {
		if err := runHook(cmd, toolName, args, result); err != nil {
			fmt.Fprintf(os.Stderr, "post-hook %q failed: %v\n", cmd, err)
		}
	}
	return nil
}

func runHook(command string, toolName string, args string, result string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}

	cmd.Env = append(os.Environ(),
		"HOOK_TOOL_NAME="+toolName,
		"HOOK_ARGS="+args,
		"HOOK_RESULT="+result,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
