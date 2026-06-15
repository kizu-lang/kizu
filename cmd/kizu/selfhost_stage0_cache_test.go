package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Stage0 seed cache (#1072, phase 2).
//
// The slowest step of the selfhost bootstrap is running the Go-compiled stage0
// executable through `stage selfhost` (~336s): a slow, unoptimized selfhost
// compiler compiling itself to produce the deterministic seed artifacts
// (bootstrapArtifactFiles). That seed is a pure function of the sources and
// toolchain that build and drive the stage0 executable, so it is safe to
// content-address and reuse when none of them changed. See stage0SeedCacheKey for
// the exact inputs (and why the executable binary itself is not one of them).
//
// This cache is opt-in (KIZU_SELFHOST_STAGE0_CACHE=1) and default OFF: with the
// variable unset the gates run the full stage step exactly as before. It only
// short-circuits the stage0 *seed* production. It never touches the bootstrap
// determinism comparison (stage1 vs stage2 fingerprints), which always rebuilds
// and links real executables downstream — and because a cached seed is
// byte-identical to a freshly staged one, that comparison's result is unchanged
// whether the cache is on or off.

const (
	// stage0SeedCacheVersion is folded into every key so an incompatible change
	// to the key inputs or stored layout can never reuse a stale entry.
	stage0SeedCacheVersion = "stage0-seed-v1"

	// stage0SeedCacheEnvVar opts into the cache. Unset => full stage step (safe).
	stage0SeedCacheEnvVar = "KIZU_SELFHOST_STAGE0_CACHE"

	// stage0SeedCacheDirEnvVar overrides the persistent cache root. Tests use it
	// to isolate from the developer cache.
	stage0SeedCacheDirEnvVar = "KIZU_SELFHOST_STAGE0_CACHE_DIR"

	// stage0SeedCacheCompleteMarker is written last so an interrupted store
	// (a partial entry) is treated as a miss instead of a corrupt hit.
	stage0SeedCacheCompleteMarker = "complete"
)

// stage0SeedCacheEnabled reports whether the opt-in stage0 seed cache is on.
func stage0SeedCacheEnabled() bool {
	return os.Getenv(stage0SeedCacheEnvVar) == "1"
}

// stage0SeedCacheDir returns the persistent cache root. It is deliberately kept
// outside target/selfhost because the gate removes that whole tree on every run.
func stage0SeedCacheDir() (string, error) {
	if dir := os.Getenv(stage0SeedCacheDirEnvVar); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "kizu", "selfhost-stage0"), nil
}

// stage0SeedCacheInputRoots are the repository paths whose content fully
// determines the stage output: the Go module sources (plus manifests) that build
// the stage0 compiler, and the selfhost tree it compiles (src) and copies
// (runtime/*.ll templates).
var stage0SeedCacheInputRoots = []string{"go.mod", "go.sum", "cmd", "internal", "selfhost"}

// stage0SeedCacheKey content-addresses the stage step output from everything that
// determines it: the toolchain versions and the source trees above.
//
// The stage0 executable itself is deliberately NOT hashed. The native linker
// stamps a fresh build id into the Mach-O/ELF binary on every link, so two builds
// of identical sources differ byte-for-byte even though they behave identically;
// hashing the binary would force a permanent miss. The deterministic
// source+toolchain fingerprint captures the same information without that
// non-determinism.
//
// False-hit analysis (must be zero):
//   - selfhost source change => selfhost tree hash changes => new key.
//   - runtime *.ll template change (selfhost/runtime/*) => selfhost tree hash
//     changes => new key. (These are copied by the stage step, so they matter.)
//   - cmd/kizu or internal compiler change => cmd/internal tree hash changes => new key.
//   - dependency change => go.mod/go.sum hash changes => new key.
//   - Go or clang toolchain change => toolchain tag changes => new key.
//   - format/version change => version constant changes => new key.
//
// Trees are hashed whole (an over-approximation: editing an unrelated test or the
// README forces a harmless extra miss) so no stage input can be silently dropped.
func stage0SeedCacheKey() (string, error) {
	tag, err := stage0ToolchainTag()
	if err != nil {
		return "", err
	}
	return stage0SeedCacheKeyFrom(tag, stage0SeedCacheInputRoots)
}

// stage0SeedCacheKeyFrom folds a toolchain tag and the content hashes of the
// input roots (files or directories) into one key, in declared order.
func stage0SeedCacheKeyFrom(toolchainTag string, roots []string) (string, error) {
	digest := sha256.New()
	fmt.Fprintf(digest, "%s\n%s\n", stage0SeedCacheVersion, toolchainTag)
	for _, root := range roots {
		rootHash, err := hashPathTree(root)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%s\n%s\n", filepath.ToSlash(root), rootHash)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// stage0ToolchainTag returns the Go and clang version lines, which feed the cache
// key so a toolchain upgrade invalidates stale seeds.
func stage0ToolchainTag() (string, error) {
	goVersion, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", err
	}
	clangVersion, err := exec.Command("clang", "--version").Output()
	if err != nil {
		return "", err
	}
	clangFirst := clangVersion
	if i := bytes.IndexByte(clangVersion, '\n'); i >= 0 {
		clangFirst = clangVersion[:i]
	}
	return string(bytes.TrimSpace(goVersion)) + "\n" + string(bytes.TrimSpace(clangFirst)), nil
}

// hashPathTree returns a deterministic content hash of root: every regular file
// under it (or root itself if it is a file), folding each file's slash-normalized
// path and content hash in sorted order for stability across runs and platforms.
// A missing root hashes to a stable "absent" sentinel (e.g. go.sum is absent in a
// dependency-free module); if it later appears the key changes, so a dependency
// being added still forces a miss.
func hashPathTree(root string) (string, error) {
	if _, err := os.Lstat(root); err != nil {
		if os.IsNotExist(err) {
			return "absent", nil
		}
		return "", err
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	digest := sha256.New()
	for _, path := range paths {
		fileHash, err := fingerprintFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%s\n%s\n", filepath.ToSlash(path), fileHash)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// restoreStage0Seed copies a complete cached seed set into target/selfhost when
// one exists for key, reporting whether a usable hit was restored.
func restoreStage0Seed(key string) (bool, error) {
	dir, err := stage0SeedCacheDir()
	if err != nil {
		return false, err
	}
	entry := filepath.Join(dir, key)
	if _, err := os.Stat(filepath.Join(entry, stage0SeedCacheCompleteMarker)); err != nil {
		return false, nil // absent or partial entry => miss
	}
	for _, name := range bootstrapArtifactFiles {
		src := filepath.Join(entry, name)
		dst := filepath.Join("target/selfhost", name)
		if err := copyBootstrapFile(src, dst); err != nil {
			return false, err
		}
	}
	return true, nil
}

// storeStage0Seed copies the freshly staged seed set into the cache under key,
// writing the completion marker last.
func storeStage0Seed(key string) error {
	dir, err := stage0SeedCacheDir()
	if err != nil {
		return err
	}
	entry := filepath.Join(dir, key)
	for _, name := range bootstrapArtifactFiles {
		src := filepath.Join("target/selfhost", name)
		dst := filepath.Join(entry, name)
		if err := copyBootstrapFile(src, dst); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(entry, stage0SeedCacheCompleteMarker), []byte(key+"\n"), 0o644)
}

// runStage0SeedStageStep runs the expensive `stage selfhost` step, transparently
// substituting a content-addressed cache hit when the opt-in cache is enabled.
// With the cache disabled it is exactly the previous behavior.
func runStage0SeedStageStep(t *testing.T, report *strings.Builder) int {
	t.Helper()
	if !stage0SeedCacheEnabled() {
		return runStage0StageCommand(t, report)
	}
	key, err := stage0SeedCacheKey()
	if err != nil {
		t.Errorf("stage0 seed cache key: %v", err)
		return 1
	}
	hit, err := restoreStage0Seed(key)
	if err != nil {
		t.Errorf("restore stage0 seed cache: %v", err)
		return 1
	}
	if hit {
		fmt.Fprintf(report, "stage0.stage.cache hit %s\n", key)
		return 0
	}
	fmt.Fprintf(report, "stage0.stage.cache miss %s\n", key)
	failures := runStage0StageCommand(t, report)
	if failures == 0 {
		if err := storeStage0Seed(key); err != nil {
			t.Errorf("store stage0 seed cache: %v", err)
			return 1
		}
	}
	return failures
}

// runStage0StageCommand runs the literal `stage selfhost` step and asserts its
// user-visible output, identical to the pre-cache gate behavior.
func runStage0StageCommand(t *testing.T, report *strings.Builder) int {
	t.Helper()
	return runStage0BackendArtifactCommand(
		t,
		report,
		"stage0.stage",
		[]string{"stage", "selfhost"},
		bootstrapStageStdout(),
	)
}

// TestStage0SeedCacheDisabledByDefault locks the safe default: with the env var
// unset, the cache path is never entered.
func TestStage0SeedCacheDisabledByDefault(t *testing.T) {
	if _, ok := os.LookupEnv(stage0SeedCacheEnvVar); ok {
		t.Skipf("%s set in environment", stage0SeedCacheEnvVar)
	}
	if stage0SeedCacheEnabled() {
		t.Fatalf("stage0 seed cache must be disabled when %s is unset", stage0SeedCacheEnvVar)
	}
}

// stage0KeyCase is one false-hit probe: mutate an input, recompute the key, and
// expect it to differ from the baseline. Each closure reverts its mutation.
type stage0KeyCase struct {
	name string
	key  func() string
}

// stage0KeyCases enumerates one mutation per cache-key input class.
func stage0KeyCases(
	t *testing.T,
	manifest string,
	treeRoot string,
	tag string,
	roots []string,
) []stage0KeyCase {
	srcFile := filepath.Join(treeRoot, "src", "a.kizu")
	runtimeFile := filepath.Join(treeRoot, "runtime", "r.ll")
	return []stage0KeyCase{
		{"toolchain tag", func() string { return mustKey(t, "go-other-tag\nclang-test-tag", roots) }},
		{"manifest content", func() string {
			mustWrite(t, manifest, "module x\n\nrequire y v1\n")
			defer mustWrite(t, manifest, "module x\n")
			return mustKey(t, tag, roots)
		}},
		{"source file content", func() string {
			mustWrite(t, srcFile, "fn a() { let x = 1; }")
			defer mustWrite(t, srcFile, "fn a() {}")
			return mustKey(t, tag, roots)
		}},
		{"runtime template content", func() string {
			mustWrite(t, runtimeFile, "; template edited")
			defer mustWrite(t, runtimeFile, "; template")
			return mustKey(t, tag, roots)
		}},
		{"added tree file", func() string {
			added := filepath.Join(treeRoot, "src", "b.kizu")
			mustWrite(t, added, "fn b() {}")
			defer mustRemove(t, added)
			return mustKey(t, tag, roots)
		}},
		{"removed tree file", func() string {
			mustRemove(t, runtimeFile)
			defer mustWrite(t, runtimeFile, "; template")
			return mustKey(t, tag, roots)
		}},
	}
}

// TestStage0SeedCacheKeySensitivity proves false-hit-zero at the key layer: any
// change to the toolchain tag, a manifest file, or any tree file (content, add,
// or remove) yields a different key, so a stale artifact can never be served.
func TestStage0SeedCacheKeySensitivity(t *testing.T) {
	base := t.TempDir()
	manifest := filepath.Join(base, "go.mod")
	treeRoot := filepath.Join(base, "selfhost")
	mustWrite(t, manifest, "module x\n")
	mustWrite(t, filepath.Join(treeRoot, "src", "a.kizu"), "fn a() {}")
	mustWrite(t, filepath.Join(treeRoot, "runtime", "r.ll"), "; template")

	tag := "go-test-tag\nclang-test-tag"
	roots := []string{manifest, treeRoot}

	baseKey := mustKey(t, tag, roots)
	if again := mustKey(t, tag, roots); again != baseKey {
		t.Fatalf("key is not stable: %s != %s", baseKey, again)
	}
	for _, tc := range stage0KeyCases(t, manifest, treeRoot, tag, roots) {
		if got := tc.key(); got == baseKey {
			t.Errorf("%s did not change the cache key (false-hit risk)", tc.name)
		}
	}
	if got := mustKey(t, tag, roots); got != baseKey {
		t.Errorf("undo did not restore the cache key: %s != %s", got, baseKey)
	}
}

// TestStage0SeedCacheRoundTrip proves store-then-restore reproduces the seed set
// byte-for-byte, and that a missing completion marker is a miss, not a hit.
func TestStage0SeedCacheRoundTrip(t *testing.T) {
	base := t.TempDir()
	t.Setenv(stage0SeedCacheDirEnvVar, filepath.Join(base, "cache"))

	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatalf("chdir work: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	want := map[string]string{}
	for i, name := range bootstrapArtifactFiles {
		content := fmt.Sprintf("artifact-%d-%s", i, name)
		want[name] = content
		mustWrite(t, filepath.Join("target/selfhost", name), content)
	}

	const key = "round-trip-key"
	if hit, err := restoreStage0Seed(key); err != nil || hit {
		t.Fatalf("expected miss before store: hit=%v err=%v", hit, err)
	}
	if err := storeStage0Seed(key); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := os.RemoveAll("target/selfhost"); err != nil {
		t.Fatalf("wipe target: %v", err)
	}
	hit, err := restoreStage0Seed(key)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !hit {
		t.Fatalf("expected hit after store")
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join("target/selfhost", name))
		if err != nil {
			t.Errorf("read restored %s: %v", name, err)
			continue
		}
		if string(got) != content {
			t.Errorf("restored %s = %q, want %q", name, got, content)
		}
	}
}

// mustKey computes a stage0 seed key for fixtures or fails the test.
func mustKey(t *testing.T, toolchainTag string, roots []string) string {
	t.Helper()
	key, err := stage0SeedCacheKeyFrom(toolchainTag, roots)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}
	return key
}

// mustWrite writes content to path, creating parent directories.
func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustRemove deletes path or fails the test.
func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}
