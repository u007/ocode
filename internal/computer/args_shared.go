package computer

import (
	_ "embed"
	"encoding/base64"
	"strconv"
)

// windowsScript is the PowerShell helper the Windows driver shells out to.
// It is embedded in a build-tag-free file so tests that assert its shape run
// on every GOOS.
//
//go:embed input_windows.ps1
var windowsScript []byte

// windowsArgs returns the argv for PowerShell that runs the
// embedded script with the given op and parameters.
func windowsArgs(scriptPath, op string, params ...string) []string {
	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, op,
	}
	return append(args, params...)
}

// windowsTypeArgs returns the argv for the type op. The text is base64
// encoded so neither the process argv nor PowerShell parsing can alter it;
// the script decodes it as UTF-8.
func windowsTypeArgs(scriptPath, text string) []string {
	return windowsArgs(scriptPath, "type", base64.StdEncoding.EncodeToString([]byte(text)))
}

// windowsKeyArgs returns the argv for the key op, translating an
// xdotool-style combo into virtual-key codes with modifiers first and the
// single main key last.
func windowsKeyArgs(scriptPath, combo string) ([]string, error) {
	codes, err := windowsKeyCombo(combo)
	if err != nil {
		return nil, err
	}
	params := make([]string, len(codes))
	for i, c := range codes {
		params[i] = strconv.Itoa(c)
	}
	return windowsArgs(scriptPath, "key", params...), nil
}
