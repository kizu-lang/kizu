package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// exitStatusCase is one program and the exit status running it promises.
type exitStatusCase struct {
	name   string
	source string
	code   int
	stdout string
}

// TestRunForwardsExitStatus pins the process exit status `run` ends with:
// what main returns as std::process::ExitStatus reaches the host, an error
// from main is 1, and a child killed by a signal reports 128 plus the
// signal, the same convention std::process::spawn_wait8 uses.
func TestRunForwardsExitStatus(t *testing.T) {
	cases := []exitStatusCase{
		{
			name: "success",
			source: `import std::process;

fn main() -> !process::ExitStatus {
    print("ok");
    return process::ExitStatus::Success;
}
`,
			code:   0,
			stdout: "ok\n",
		},
		{
			name: "failure",
			source: `import std::process;

fn main() -> !process::ExitStatus {
    return process::ExitStatus::Failure;
}
`,
			code: 1,
		},
		{
			name: "specific",
			source: `import std::process;

fn main() -> !process::ExitStatus {
    return process::ExitStatus::Specific(7);
}
`,
			code: 7,
		},
		{
			name: "error",
			source: `error Cli {
    Failed,
}

fn main() -> !void {
    return Cli::Failed;
}
`,
			code: 1,
		},
		{
			name: "signal",
			source: `fn main() {
    let values = "abc";
    print(values[5]);
}
`,
			code: 128 + 6,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runExitStatusCase(t, tt)
		})
	}
}

// runExitStatusCase runs one program and checks the exit status it promises.
func runExitStatusCase(t *testing.T, tt exitStatusCase) {
	t.Helper()
	path := filepath.Join(t.TempDir(), tt.name+".kizu")
	if err := os.WriteFile(path, []byte(tt.source), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := kizuCommand("run", path)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run: %v\n%s", err, stderr.String())
		}
		code = exit.ExitCode()
	}
	if code != tt.code {
		t.Fatalf("exit code %d, want %d\nstderr:\n%s", code, tt.code, stderr.String())
	}
	if tt.stdout != "" && stdout.String() != tt.stdout {
		t.Fatalf("stdout %q, want %q", stdout.String(), tt.stdout)
	}
}
