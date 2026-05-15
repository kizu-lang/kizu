package buildcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// Version is included in cache keys so incompatible formats do not collide.
	Version = "v0.3-module-cache-v1"

	// DefaultMaxBytes bounds the local cache to 256 MiB by default.
	DefaultMaxBytes int64 = 256 * 1024 * 1024
)

// Cache stores local Kizu build artifacts.
type Cache struct {
	Dir      string
	MaxBytes int64
}

// Entry describes one cached build artifact.
type Entry struct {
	Key                 string            `json:"key"`
	Target              string            `json:"target"`
	SourcePath          string            `json:"source_path"`
	SourceHash          string            `json:"source_hash"`
	Version             string            `json:"version"`
	Output              string            `json:"output"`
	SizeBytes           int64             `json:"size_bytes"`
	CreatedAt           time.Time         `json:"created_at"`
	InputKind           string            `json:"input_kind,omitempty"`
	ManifestHash        string            `json:"manifest_hash,omitempty"`
	ModuleGraphHash     string            `json:"module_graph_hash,omitempty"`
	PublicInterfaceHash string            `json:"public_interface_hash,omitempty"`
	StdlibHash          string            `json:"stdlib_hash,omitempty"`
	SourceHashes        map[string]string `json:"source_hashes,omitempty"`
}

// Status summarizes cache state.
type Status struct {
	Dir       string
	MaxBytes  int64
	SizeBytes int64
	Entries   int
}

// Result contains the artifact and whether it came from cache.
type Result struct {
	Output string
	Hit    bool
	Key    string
}

// New returns the default local cache.
func New() (*Cache, error) {
	dir := os.Getenv("KIZU_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "kizu")
	}
	return &Cache{Dir: dir, MaxBytes: DefaultMaxBytes}, nil
}

// GetOrBuild returns a cached artifact or stores builder output.
func (c *Cache) GetOrBuild(
	path string,
	target string,
	builder func() (string, error),
) (Result, error) {
	input, err := newInput(path, target)
	if err != nil {
		return Result{}, err
	}
	if entry, ok := c.readEntry(input.key); ok {
		out, err := os.ReadFile(c.outputPath(entry.Output))
		if err != nil {
			return Result{}, err
		}
		return Result{Output: string(out), Hit: true, Key: input.key}, nil
	}
	output, err := builder()
	if err != nil {
		return Result{}, err
	}
	if err := c.writeEntry(input, output); err != nil {
		return Result{}, err
	}
	return Result{Output: output, Hit: false, Key: input.key}, c.enforceLimit()
}

// Status returns current cache size and entry count.
func (c *Cache) Status() (Status, error) {
	entries, err := c.entries()
	if err != nil {
		return Status{}, err
	}
	var size int64
	for _, entry := range entries {
		size += entry.SizeBytes
	}
	return Status{Dir: c.Dir, MaxBytes: c.MaxBytes, SizeBytes: size, Entries: len(entries)}, nil
}

// Prune removes every cache entry.
func (c *Cache) Prune() (int, int64, error) {
	entries, err := c.entries()
	if err != nil {
		return 0, 0, err
	}
	var bytes int64
	for _, entry := range entries {
		bytes += entry.SizeBytes
		if err := os.Remove(c.metaPath(entry.Key)); err != nil && !os.IsNotExist(err) {
			return 0, 0, err
		}
		if err := os.Remove(c.outputPath(entry.Output)); err != nil && !os.IsNotExist(err) {
			return 0, 0, err
		}
	}
	return len(entries), bytes, nil
}

// WhyRebuild explains whether target would rebuild for path.
func (c *Cache) WhyRebuild(path string, target string) (string, error) {
	input, err := newInput(path, target)
	if err != nil {
		return "", err
	}
	if _, ok := c.readEntry(input.key); ok {
		return fmt.Sprintf("cache hit: %s", input.key), nil
	}
	previous, ok, err := c.latestFor(input.sourcePath, target)
	if err != nil {
		return "", err
	}
	if !ok {
		return "cache miss: no previous build for input", nil
	}
	if previous.Version != Version {
		return "cache miss: compiler cache version changed", nil
	}
	return explainChangedInput(previous, input), nil
}

type cacheInput struct {
	key                 string
	target              string
	sourcePath          string
	sourceHash          string
	inputKind           string
	manifestHash        string
	moduleGraphHash     string
	publicInterfaceHash string
	stdlibHash          string
	sourceHashes        map[string]string
}

// writeEntry writes metadata and output for one artifact.
func (c *Cache) writeEntry(input cacheInput, output string) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	outputName := input.key + ".out"
	if err := os.WriteFile(c.outputPath(outputName), []byte(output), 0o644); err != nil {
		return err
	}
	entry := Entry{
		Key: input.key, Target: input.target, SourcePath: input.sourcePath,
		SourceHash: input.sourceHash, Version: Version, Output: outputName,
		SizeBytes: int64(len(output)), CreatedAt: time.Now().UTC(),
		InputKind: input.inputKind, ManifestHash: input.manifestHash,
		ModuleGraphHash: input.moduleGraphHash, StdlibHash: input.stdlibHash,
		PublicInterfaceHash: input.publicInterfaceHash, SourceHashes: input.sourceHashes,
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.metaPath(input.key), data, 0o644)
}

// readEntry reads one metadata entry.
func (c *Cache) readEntry(key string) (Entry, bool) {
	data, err := os.ReadFile(c.metaPath(key))
	if err != nil {
		return Entry{}, false
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, false
	}
	return entry, true
}

// entries reads all valid metadata entries.
func (c *Cache) entries() ([]Entry, error) {
	items, err := filepath.Glob(filepath.Join(c.Dir, "*.json"))
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		data, err := os.ReadFile(item)
		if err != nil {
			return nil, err
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// latestFor returns the most recent entry for path and target.
func (c *Cache) latestFor(path string, target string) (Entry, bool, error) {
	entries, err := c.entries()
	if err != nil {
		return Entry{}, false, err
	}
	var latest Entry
	found := false
	for _, entry := range entries {
		if entry.SourcePath != path || entry.Target != target {
			continue
		}
		if !found || entry.CreatedAt.After(latest.CreatedAt) {
			latest = entry
			found = true
		}
	}
	return latest, found, nil
}

// enforceLimit removes oldest entries until the cache fits MaxBytes.
func (c *Cache) enforceLimit() error {
	entries, err := c.entries()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i int, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	var size int64
	for _, entry := range entries {
		size += entry.SizeBytes
	}
	for _, entry := range entries {
		if size <= c.MaxBytes {
			return nil
		}
		size -= entry.SizeBytes
		if err := os.Remove(c.metaPath(entry.Key)); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(c.outputPath(entry.Output)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// metaPath returns the metadata path for key.
func (c *Cache) metaPath(key string) string {
	return filepath.Join(c.Dir, key+".json")
}

// outputPath returns the artifact path for name.
func (c *Cache) outputPath(name string) string {
	return filepath.Join(c.Dir, name)
}
