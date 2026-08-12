package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// selfhostCommandResult records one selfhost executable invocation: what was
// run, what it printed, how it exited, and how long it took. Every gate that
// drives the stage0-native binary reports through this shape.
type selfhostCommandResult struct {
	name    string
	command string
	stdout  string
	stderr  string
	code    int
	elapsed time.Duration
}

// exitCode returns a process exit code, using -1 for runner errors.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return -1
	}
	return exitErr.ExitCode()
}

// runSelfhostCommand runs one command through a selfhost executable and
// captures its streams.
func runSelfhostCommand(
	t *testing.T,
	exePath string,
	args ...string,
) selfhostCommandResult {
	t.Helper()
	start := time.Now()
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Errorf("resolve %s: %v", exePath, err)
	}
	run := exec.Command(absExe, args...)
	run.Env = append(os.Environ(), "KIZU_CACHE_DIR=target/selfhost/gate-cache")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	return selfhostCommandResult{
		name:    strings.Join(args, " "),
		command: absExe + " " + strings.Join(args, " "),
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(err),
		elapsed: time.Since(start),
	}
}

// textFingerprint hashes gate output so two runs can be compared by digest.
func textFingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// hostedLinkStackArgs gives the linker the deep stack the selfhost compiler's
// recursive descent needs on hosts whose default is too small.
func hostedLinkStackArgs() []string {
	if goruntime.GOOS == "darwin" {
		return []string{"-Wl,-stack_size,0x20000000"}
	}
	return nil
}

// countWithIsolatedSelfhostTarget runs a destructive direct gate without
// discarding an existing native selfhost artifact.
func countWithIsolatedSelfhostTarget(t *testing.T, count func() int) (failures int) {
	t.Helper()
	restoreTarget, err := isolateSelfhostTargetFromPackageCWD(t)
	if err != nil {
		t.Errorf("isolate target/selfhost: %v", err)
		return 1
	}
	defer func() {
		if err := restoreTarget(); err != nil {
			t.Errorf("restore target/selfhost: %v", err)
			failures++
		}
	}()
	return count()
}

// isolateSelfhostTargetFromPackageCWD preserves target/selfhost when the caller
// is running from cmd/kizu, which is the default Go test working directory.
func isolateSelfhostTargetFromPackageCWD(t *testing.T) (func() error, error) {
	t.Helper()
	restoreCWD, err := chdirRepoRoot()
	if err != nil {
		return nil, err
	}
	restoreTarget, err := isolateSelfhostTarget(t)
	restoreCWD()
	if err != nil {
		return nil, err
	}
	return restoreTarget, nil
}

// isolateSelfhostTarget preserves an existing native selfhost artifact while an
// interpreted selfhost gate writes or removes fixed target/selfhost paths.
func isolateSelfhostTarget(t *testing.T) (func() error, error) {
	t.Helper()
	targetRoot, err := filepath.Abs("target")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return nil, err
	}
	targetPath := filepath.Join(targetRoot, "selfhost")
	backupDir, err := os.MkdirTemp(targetRoot, "selfhost-pipeline-backup-")
	if err != nil {
		return nil, err
	}
	backupPath := filepath.Join(backupDir, "selfhost")
	hadTarget := true
	if err := os.Rename(targetPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			_ = os.RemoveAll(backupDir)
			return nil, err
		}
		hadTarget = false
	}
	restore := func() error {
		restoreErr := os.RemoveAll(targetPath)
		if hadTarget {
			if err := os.Rename(backupPath, targetPath); err != nil && restoreErr == nil {
				restoreErr = err
			}
		}
		if err := os.RemoveAll(backupDir); err != nil && restoreErr == nil {
			restoreErr = err
		}
		return restoreErr
	}
	return restore, nil
}

// writeSelfhostGateReport writes one gate's durable report, creating the reports
// directory. Nothing else creates it now that the bootstrap step is gone, so a
// gate that assumed it existed fails on a fresh checkout after passing.
func writeSelfhostGateReport(path string, report string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(report), 0o644)
}
