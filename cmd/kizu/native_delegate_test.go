package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeDelegateEnvGate(t *testing.T) {
	t.Setenv(nativeRunEnvVar, "")
	if nativeRunEnabled() {
		t.Fatalf("expected run native delegate disabled when %s is empty", nativeRunEnvVar)
	}
	t.Setenv(nativeRunEnvVar, "0")
	if nativeRunEnabled() {
		t.Fatalf("expected run native delegate disabled when %s=0", nativeRunEnvVar)
	}
	t.Setenv(nativeRunEnvVar, "1")
	if !nativeRunEnabled() {
		t.Fatalf("expected run native delegate enabled when %s=1", nativeRunEnvVar)
	}

	t.Setenv(nativeTestEnvVar, "")
	if nativeTestEnabled() {
		t.Fatalf("expected test native delegate disabled when %s is empty", nativeTestEnvVar)
	}
	t.Setenv(nativeTestEnvVar, "0")
	if nativeTestEnabled() {
		t.Fatalf("expected test native delegate disabled when %s=0", nativeTestEnvVar)
	}
	t.Setenv(nativeTestEnvVar, "1")
	if !nativeTestEnabled() {
		t.Fatalf("expected test native delegate enabled when %s=1", nativeTestEnvVar)
	}
}

func TestNativeDelegateEnvOffKeepsGoPaths(t *testing.T) {
	root := t.TempDir()
	writeFakeNativeSelfhost(t, root)
	t.Setenv("KIZU_REPO_ROOT", root)
	t.Setenv(nativeRunEnvVar, "0")
	t.Setenv(nativeTestEnvVar, "0")
	t.Setenv(selfhostRunEnvVar, "0")
	t.Setenv(selfhostTestEnvVar, "0")

	runPath := filepath.Join(t.TempDir(), "run.kizu")
	if err := os.WriteFile(runPath, []byte(`fn main() { print("go run"); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runDispatchCaptureOutput(t, "run", []string{runPath})
	if err != nil {
		t.Fatalf("run default path failed: %v\nstdout=%q\nstderr=%q", err, stdout, stderr)
	}
	if stdout != "go run\n" || stderr != "" {
		t.Fatalf("run default path stdout=%q stderr=%q", stdout, stderr)
	}

	testPath := filepath.Join(t.TempDir(), "test.kizu")
	if err := os.WriteFile(testPath, []byte("test \"ok\" { std::testing::expect(true); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runDispatchCaptureOutput(t, "test", []string{testPath})
	if err != nil {
		t.Fatalf("test default path failed: %v\nstdout=%q\nstderr=%q", err, stdout, stderr)
	}
	if stdout != "test: ok\n" || stderr != "" {
		t.Fatalf("test default path stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestNativeDelegateExecsStage2WithTransparentArgs(t *testing.T) {
	root := t.TempDir()
	writeFakeNativeSelfhost(t, root)
	t.Setenv("KIZU_REPO_ROOT", root)
	t.Setenv("KIZU_FAKE_NATIVE_EXIT", "7")
	t.Setenv(selfhostRunEnvVar, "0")
	t.Setenv(selfhostTestEnvVar, "0")

	for _, tt := range []struct {
		name       string
		command    string
		envVar     string
		otherEnv   string
		args       []string
		wantStdout string
	}{
		{
			name:       "run",
			command:    "run",
			envVar:     nativeRunEnvVar,
			otherEnv:   nativeTestEnvVar,
			args:       []string{"input.kizu", "--", "alpha", "beta"},
			wantStdout: "native argv:<run><input.kizu><--><alpha><beta>\n",
		},
		{
			name:       "test",
			command:    "test",
			envVar:     nativeTestEnvVar,
			otherEnv:   nativeRunEnvVar,
			args:       []string{"suite.kizu", "--", "case"},
			wantStdout: "native argv:<test><suite.kizu><--><case>\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, "1")
			t.Setenv(tt.otherEnv, "0")
			stdout, stderr, err := runDispatchCaptureOutput(t, tt.command, tt.args)
			var status exitStatus
			if !errors.As(err, &status) || status.code != 7 {
				t.Fatalf("got error %v, want exit status 7", err)
			}
			if stdout != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if stderr != "native stderr\n" {
				t.Fatalf("stderr = %q, want native stderr", stderr)
			}
		})
	}
}

func TestNativeDelegateRejectsDirectoryArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, nativeSelfhostBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIZU_REPO_ROOT", root)
	t.Setenv(nativeRunEnvVar, "1")
	t.Setenv(nativeTestEnvVar, "0")

	stdout, stderr, err := runDispatchCaptureOutput(t, "run", []string{"input.kizu"})
	if err == nil {
		t.Fatalf("expected directory artifact error")
	}
	if !strings.Contains(err.Error(), "got directory") {
		t.Fatalf("got error %q, want directory artifact diagnostic", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q, want no child output", stdout, stderr)
	}
}

func writeFakeNativeSelfhost(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(root, nativeSelfhostBinary)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf 'native argv:'
for arg in "$@"; do
  printf '<%s>' "$arg"
done
printf '\n'
printf 'native stderr\n' >&2
exit "${KIZU_FAKE_NATIVE_EXIT:-0}"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}
