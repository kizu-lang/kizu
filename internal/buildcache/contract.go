package buildcache

import "strconv"

// ContractSnapshotLines returns the Go-owned cache switch contract oracle.
func ContractSnapshotLines() []string {
	return []string{
		"owner", "go",
		"switch", "blocked",
		"blocked until", "filesystem APIs",
		"blocked until", "hashing APIs",
		"blocked until", "module graph APIs",
		"blocked until", "artifact layout APIs",
		"required input", "compiler version",
		"compiler version", Version,
		"required input", "target",
		"required input", "backend",
		"required input", "optimization mode",
		"required input", "input kind",
		"input kind", "file",
		"input kind", "package",
		"required input", "manifest hash",
		"required input", "module graph hash",
		"required input", "source hash",
		"required input", "public interface hash",
		"required input", "stdlib hash",
		"required input", "artifact layout",
		"limit", "default max bytes",
		"default max bytes", strconv.FormatInt(DefaultMaxBytes, 10),
		"positive", "no-op rebuild",
		"expected", "cache hit",
		"negative", "source edit",
		"expected", "cache miss: source changed",
		"negative", "private package edit",
		"expected", "cache miss: source changed without public interface change",
		"negative", "public interface edit",
		"expected", "cache miss: public interface changed",
		"negative", "manifest edit",
		"expected", "cache miss: manifest changed",
		"negative", "module graph edit",
		"expected", "cache miss: module graph changed",
		"negative", "stdlib edit",
		"expected", "cache miss: stdlib changed",
		"status", "reports size and entries",
		"prune", "removes entries predictably",
	}
}
