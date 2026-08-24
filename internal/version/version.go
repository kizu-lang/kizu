// Package version renders what a kizu binary is: the release it was stamped
// with, or the VCS state it was built from. One formatter serves both the
// shipping CLI, which reads its own build info, and the generator that writes
// the selfhost compiler's copy, which reads the same values from git.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Release is the release version stamped at build time with
// `-ldflags "-X github.com/kizu-lang/kizu/internal/version.Release=v0.1.0"`.
// A development build carries none and names itself by the VCS state instead.
// Either way the binary can say what it is, so an old one is identified by
// asking it rather than by guessing from the parse errors it produces.
var Release string

// String renders the version of the running binary.
func String() string {
	revision, at, modified := buildVCS()
	return Format(Release, revision, at, modified)
}

// Format renders one version line from the values that identify a build: the
// release label, the revision it was built from, when that revision was made,
// and whether the tree carried uncommitted changes. An empty label is a
// development build, and an empty revision is a build with no VCS information
// (`go build` outside a checkout, nix build).
func Format(label string, revision string, at string, modified bool) string {
	if label == "" {
		label = "devel"
	}
	detail := formatDetail(revision, at, modified)
	if detail == "" {
		return "kizu " + label
	}
	return fmt.Sprintf("kizu %s (%s)", label, detail)
}

// formatDetail renders the parenthesized VCS state, empty when there is none.
func formatDetail(revision string, at string, modified bool) string {
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	parts := []string{revision}
	if at != "" {
		parts = append(parts, at)
	}
	if modified {
		parts = append(parts, "modified")
	}
	return strings.Join(parts, ", ")
}

// buildVCS reads the revision Go stamped into the running binary, empty when
// the build had no VCS information.
func buildVCS() (string, string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", false
	}
	var revision, at string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			at = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, at, modified
}
