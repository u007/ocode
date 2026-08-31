package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectExecutedCustomScripts(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	// Helper to create file
	mk := func(path, content string) {
		dir := filepath.Dir(path)
		if dir != "." {
			os.MkdirAll(filepath.Join(tmp, dir), 0o755)
		}
		if err := os.WriteFile(filepath.Join(tmp, path), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mk("script.sh", "#!/bin/bash\necho hi\n")
	mk("scripts/deploy.sh", "echo deploy\n")
	mk("env.sh", "export FOO=1\n")
	mk("a.sh", "echo a\n")
	mk("b.sh", "echo b\n")
	mk("binary", string([]byte{0, 1, 2})) // binary with NUL
	mk("tool.py", "print('hi')\n")
	mk("scripts/build.js", "console.log(1)\n")
	mk("scripts/run.ts", "console.log(2)\n")
	mk("gen.rb", "puts 1\n")
	mk("main.php", "<?php echo 1;\n")
	mk("plot.R", "print(1)\n")
	// outside root: use a real file that is outside allowed roots (not under workDir or temp-allowed? but /etc/hosts is outside because allowed roots are workDir + caches + /tmp etc, but /etc is not allowed)
	// We use /etc/hosts which exists on macOS/Linux and is not inside any allowed root (except maybe not, but check)
	outsideFile := "/etc/hosts"

	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)

	tests := []struct {
		name    string
		cmd     string
		want    []string // suffixes expected
		wantLen int
	}{
		{"direct slash", "./script.sh", []string{"script.sh"}, 1},
		{"direct subdir", "scripts/deploy.sh", []string{"deploy.sh"}, 1},
		{"bash wrapper", "bash script.sh", []string{"script.sh"}, 1},
		{"bash wrapper slash", "bash ./scripts/deploy.sh", []string{"deploy.sh"}, 1},
		{"sh wrapper", "sh ./script.sh", []string{"script.sh"}, 1},
		{"source builtin", "source ./env.sh", []string{"env.sh"}, 1},
		{"dot builtin", ". ./env.sh", []string{"env.sh"}, 1},
		{"chained", "./a.sh && ./b.sh", []string{"a.sh", "b.sh"}, 2},
		{"quoted", "bash 'script.sh'", []string{"script.sh"}, 1},
		{"quoted with slash", "bash './scripts/deploy.sh'", []string{"deploy.sh"}, 1},
		{"bash -c skip", "bash -c 'echo hi'", nil, 0},
		{"system binary", "ls -la", nil, 0},
		{"system ls with slash not exist", "/bin/ls -la", nil, 0},
		{"binary file skipped in context but detected? direct binary path should be detected then filtered by binary guard in context", "./binary", []string{"binary"}, 1},
		{"outside root", outsideFile, nil, 0},
		{"generic with flag", "bash -e script.sh", []string{"script.sh"}, 1},
		{"piped", "cat file | bash ./script.sh", []string{"script.sh"}, 1},
		{"cd and exec", "cd subdir && ./a.sh", []string{"a.sh"}, 1},
		{"cd and bash", "cd scripts && bash scripts/deploy.sh", []string{"deploy.sh"}, 1},
		{"semicolon chained", "./a.sh; ./b.sh", []string{"a.sh", "b.sh"}, 2},
		{"quoted with space", "bash \"./script.sh\"", []string{"script.sh"}, 1},
		{"dot with flag", ". -a ./env.sh", nil, 0},
		// Interpreter-executed scripts (python x.py, node x.js, ...).
		{"python file", "python tool.py", []string{"tool.py"}, 1},
		{"python3 abs bin", "/usr/bin/python3 ./tool.py --flag", []string{"tool.py"}, 1},
		{"python with flag", "python3 -u tool.py", []string{"tool.py"}, 1},
		{"node file", "node scripts/build.js", []string{"build.js"}, 1},
		{"tsx file", "tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"bun run file", "bun run ./scripts/run.ts", []string{"run.ts"}, 1},
		{"bun file", "bun scripts/run.ts", []string{"run.ts"}, 1},
		{"deno run file", "deno run --allow-net scripts/run.ts", []string{"run.ts"}, 1},
		{"ruby file", "ruby gen.rb", []string{"gen.rb"}, 1},
		{"php file", "php main.php", []string{"main.php"}, 1},
		{"Rscript file", "Rscript plot.R", []string{"plot.R"}, 1},
		{"cd then python", "cd . && python3 tool.py", []string{"tool.py"}, 1},
		{"make then node", "make build && node scripts/build.js", []string{"build.js"}, 1},
		{"python inline -c skip", "python3 -c 'print(1)'", nil, 0},
		{"node inline -e skip", "node -e 'console.log(1)'", nil, 0},
		{"python -m module skip", "python3 -m pytest tool.py", nil, 0},
		{"bun builtin skip", "bun test tool.py", nil, 0},
		{"bun run package script skip", "bun run typecheck", nil, 0},
		{"deno builtin skip", "deno fmt scripts/run.ts", nil, 0},
		{"node missing file", "node nope.js", nil, 0},
		// Runner-wrapped interpreters.
		{"uv run python", "uv run python tool.py", []string{"tool.py"}, 1},
		{"uv run file", "uv run tool.py", []string{"tool.py"}, 1},
		{"poetry run python", "poetry run python3 tool.py", []string{"tool.py"}, 1},
		{"pipenv run python", "pipenv run python tool.py", []string{"tool.py"}, 1},
		{"npx tsx", "npx tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"npx remote pkg skip", "npx create-react-app app", nil, 0},
		{"bunx tsx", "bunx tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"pnpm exec tsx", "pnpm exec tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"pnpm dlx tsx", "pnpm dlx tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"bun x tsx", "bun x tsx scripts/run.ts", []string{"run.ts"}, 1},
		{"npm exec tsx", "npm exec tsx scripts/run.ts", []string{"run.ts"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := a.detectExecutedCustomScripts(tc.cmd)
			if len(got) != tc.wantLen {
				t.Fatalf("cmd %q: got %v len %d want len %d", tc.cmd, got, len(got), tc.wantLen)
			}
			for _, wantSuffix := range tc.want {
				found := false
				for _, g := range got {
					if strings.HasSuffix(g, wantSuffix) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("cmd %q: expected suffix %q in %v", tc.cmd, wantSuffix, got)
				}
			}
		})
	}
}

func TestBuildPermissionContextIncludesCustomScriptEvenWhenBudgetExhausted(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	os.WriteFile(filepath.Join(tmp, "my.sh"), []byte("echo my\nrun something\n"), 0o644)

	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)

	args, _ := json.Marshal(map[string]string{"command": "./my.sh --flag"})
	// Use maxSources=3, same as default. With old logic, metadata would consume budget and script would be excluded.
	// New logic reserves metadata not counting, so script should still appear.
	ctx := a.buildPermissionContext("bash", args, 2048, 3, 40)
	if !strings.Contains(ctx, "Executed custom script") {
		t.Fatalf("expected Executed custom script in context, got: %q", ctx)
	}
	if !strings.Contains(ctx, "echo my") {
		t.Fatalf("expected script content in context, got: %q", ctx)
	}
}

func TestBuildPermissionContextTruncationLabel(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	// Create a file where first 40 lines already exceed maxInterpreterSourceBytes (16384).
	// Each line ~500 bytes, 40 lines ~20000 bytes.
	longLine := "echo " + strings.Repeat("x", 500) + "\n"
	bigContent := strings.Repeat(longLine, 50)
	os.WriteFile(filepath.Join(tmp, "big.sh"), []byte(bigContent), 0o644)

	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	args, _ := json.Marshal(map[string]string{"command": "./big.sh"})
	// Use small maxLines but large byte truncation should trigger
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if !strings.Contains(ctx, "TRUNCATED") {
		t.Fatalf("expected TRUNCATED label for big file, got: %q", ctx[:1000])
	}
	if !strings.Contains(ctx, "DO NOT auto-approve") {
		t.Fatalf("expected DO NOT auto-approve warning, got: %q", ctx[:1000])
	}
}

func TestBuildPermissionContextBinaryNotIncluded(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	binaryContent := []byte{0x7f, 'E', 'L', 'F', 0, 1}
	os.WriteFile(filepath.Join(tmp, "binfile"), binaryContent, 0o644)
	// also test binary via direct execution with slash
	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	args, _ := json.Marshal(map[string]string{"command": "./binfile"})
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if strings.Contains(ctx, "Executed custom script") {
		t.Fatalf("binary file should not be included as Executed custom script, got: %q", ctx)
	}
}

func TestDetectDoesNotReadPATHExecutables(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	got := a.detectExecutedCustomScripts("git status")
	if len(got) != 0 {
		t.Fatalf("expected no custom script for git status, got %v", got)
	}
	got = a.detectExecutedCustomScripts("npm install")
	if len(got) != 0 {
		t.Fatalf("expected no custom script for npm install, got %v", got)
	}
}

func TestVerifyAutoGrantDeniesTruncatedScript(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	// Big script: 50 lines, each 500 chars -> truncated by both lines and bytes
	longLine := "echo " + strings.Repeat("x", 500) + "\n"
	bigContent := strings.Repeat(longLine, 50)
	os.WriteFile(filepath.Join(tmp, "big.sh"), []byte(bigContent), 0o644)
	// Small script: not truncated
	os.WriteFile(filepath.Join(tmp, "small.sh"), []byte("echo hi\n"), 0o644)

	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)

	// Truncated should be denied
	argsBig, _ := json.Marshal(map[string]string{"command": "./big.sh"})
	if ok, reason := a.verifyAutoGrant("bash", argsBig, &PermissionRequest{ToolName: "bash", Command: "./big.sh"}); ok {
		t.Fatalf("expected truncated big.sh to be denied, got allow reason %q", reason)
	}
	// Non-truncated should be allowed (assuming hard-block and scope pass)
	argsSmall, _ := json.Marshal(map[string]string{"command": "./small.sh"})
	if ok, reason := a.verifyAutoGrant("bash", argsSmall, &PermissionRequest{ToolName: "bash", Command: "./small.sh"}); !ok {
		t.Fatalf("expected small.sh to be allowed, got deny %q", reason)
	}
}

func TestVerifyAutoGrantDeniesTruncatedInterpreterScript(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	bigContent := strings.Repeat("print('"+strings.Repeat("x", 500)+"')\n", 50)
	os.WriteFile(filepath.Join(tmp, "big.py"), []byte(bigContent), 0o644)
	os.WriteFile(filepath.Join(tmp, "small.js"), []byte("console.log(1)\n"), 0o644)

	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)

	// Compound form bypasses the structured interpreter path (not first command),
	// so the generic guard must catch the truncated script.
	for _, cmd := range []string{"cd . && python3 big.py", "ls && uv run big.py", "true && npx tsx big.py"} {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		if ok, reason := a.verifyAutoGrant("bash", args, &PermissionRequest{ToolName: "bash", Command: cmd}); ok {
			t.Fatalf("%q: expected truncated big.py to be denied, got allow reason %q", cmd, reason)
		}
	}
	cmd := "cd . && node small.js"
	args, _ := json.Marshal(map[string]string{"command": cmd})
	if ok, reason := a.verifyAutoGrant("bash", args, &PermissionRequest{ToolName: "bash", Command: cmd}); !ok {
		t.Fatalf("expected small.js to be allowed, got deny %q", reason)
	}
	// Context builder surfaces the interpreter script content.
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if !strings.Contains(ctx, "Executed custom script") || !strings.Contains(ctx, "console.log(1)") {
		t.Fatalf("expected small.js content in context, got: %q", ctx)
	}
}

func TestVerifyAutoGrantAllowsNonScriptCommand(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	args, _ := json.Marshal(map[string]string{"command": "ls -la"})
	if ok, _ := a.verifyAutoGrant("bash", args, &PermissionRequest{ToolName: "bash", Command: "ls -la"}); !ok {
		t.Fatal("expected ls -la to be allowed by verifyAutoGrant")
	}
}

func TestBuildPermissionContextLineTruncation(t *testing.T) {
	tmp := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmp)
	// 50 lines, each short, exceeds line limit 40 but not byte limit
	content := strings.Repeat("echo hi\n", 50)
	os.WriteFile(filepath.Join(tmp, "manylines.sh"), []byte(content), 0o644)
	a := NewAgent(nil, nil, nil, nil)
	a.Permissions().SetWorkDir(tmp)
	args, _ := json.Marshal(map[string]string{"command": "./manylines.sh"})
	ctx := a.buildPermissionContext("bash", args, 50000, 3, 40)
	if !strings.Contains(ctx, "TRUNCATED") {
		t.Fatalf("expected TRUNCATED for line-exceeded script, got: %q", ctx[:500])
	}
	// Also verify guard denies
	if ok, _ := a.verifyAutoGrant("bash", args, &PermissionRequest{ToolName: "bash", Command: "./manylines.sh"}); ok {
		t.Fatal("expected line-truncated script to be denied by verifyAutoGrant")
	}
}
