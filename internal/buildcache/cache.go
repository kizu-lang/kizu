package buildcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// Version is included in cache keys so incompatible formats do not collide.
	Version = "native-v1"

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
	Key        string    `json:"key"`
	Target     string    `json:"target"`
	SourceHash string    `json:"source_hash"`
	Version    string    `json:"version"`
	Output     string    `json:"output"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

// Status summarizes cache state.
type Status struct {
	Dir       string
	MaxBytes  int64
	SizeBytes int64
	Entries   int
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

// GetOrBuildArtifact returns the path of a cached file, building it with
// builder when it is missing. The cache holds only artifacts built from
// content the compiler carries in full -- the native runtime object from its
// source, the executable from its IR -- so a key always names exactly what
// the artifact is made of (ADR-0126). `name` and `content` stand in for the
// path and the file bytes a source artifact would be keyed by.
func (c *Cache) GetOrBuildArtifact(
	name string,
	target string,
	content []byte,
	builder func(output string) error,
) (string, error) {
	input := newInput(name, target, content)
	stored := outputName(input.key)
	output := c.outputPath(stored)
	if _, ok := c.hasArtifact(input.key); ok {
		return output, nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	size, err := c.buildArtifact(output, builder)
	if err != nil {
		return "", err
	}
	if err := c.writeMeta(input, stored, size); err != nil {
		return "", err
	}
	return output, c.enforceLimit()
}

// hasArtifact reports whether a key has both the entry that accounts for it and
// the file that entry names. An entry whose artifact is gone is a miss: the
// cache is a record of work already done, and work that is gone has to be done
// again rather than read from where it is not.
func (c *Cache) hasArtifact(key string) (Entry, bool) {
	entry, ok := c.readEntry(key)
	if !ok {
		return Entry{}, false
	}
	if _, err := os.Stat(c.outputPath(entry.Output)); err != nil {
		return Entry{}, false
	}
	return entry, true
}

// buildArtifact builds into a scratch file and moves it into place, so a reader
// never sees a half-written artifact under a key that claims to be complete.
func (c *Cache) buildArtifact(output string, builder func(output string) error) (int64, error) {
	scratch, err := os.CreateTemp(c.Dir, "artifact-*")
	if err != nil {
		return 0, err
	}
	path := scratch.Name()
	if err := scratch.Close(); err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(path) }()
	if err := builder(path); err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), os.Rename(path, output)
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

type cacheInput struct {
	key        string
	target     string
	sourceHash string
}

// newInput keys an artifact by everything it is made of: the cache format, the
// target it is built for, the name it is filed under, and the bytes it is built
// from.
func newInput(name string, target string, content []byte) cacheInput {
	sourceHashBytes := sha256.Sum256(content)
	sourceHash := hex.EncodeToString(sourceHashBytes[:])
	keyHash := sha256.Sum256([]byte(Version + "\n" + target + "\n" + name + "\n" + sourceHash))
	return cacheInput{key: hex.EncodeToString(keyHash[:]), target: target, sourceHash: sourceHash}
}

// writeMeta records what one stored artifact is and how much room it takes. It
// is written after the artifact it describes, so an entry never promises a file
// that is not there yet.
func (c *Cache) writeMeta(input cacheInput, outputName string, size int64) error {
	entry := Entry{
		Key: input.key, Target: input.target, SourceHash: input.sourceHash,
		Version: Version, Output: outputName,
		SizeBytes: size, CreatedAt: time.Now().UTC(),
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
			// A concurrent process may evict an entry between the glob and
			// the read. Work that is gone is a miss, not a failure.
			if os.IsNotExist(err) {
				continue
			}
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

// outputName returns the file name the artifact for key is stored under.
func outputName(key string) string {
	return key + ".out"
}

// outputPath returns the artifact path for name.
func (c *Cache) outputPath(name string) string {
	return filepath.Join(c.Dir, name)
}
