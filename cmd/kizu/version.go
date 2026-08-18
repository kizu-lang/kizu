package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// version is the release version stamped at build time with
// `-ldflags "-X main.version=v0.1.0"`. A development build carries none and
// names itself by the VCS state Go recorded instead. Either way the binary can
// say what it is, so an old one is identified by asking it rather than by
// guessing from the parse errors it produces.
var version string

// versionCommand prints what this binary is.
func versionCommand(args []string) error {
	if len(args) != 0 {
		usage()
		return fmt.Errorf("version takes no arguments")
	}
	_, _ = fmt.Println(versionString())
	return nil
}

// versionString renders the stamped release version or the recorded VCS state.
func versionString() string {
	label := version
	if label == "" {
		label = "devel"
	}
	if detail := vcsDetail(); detail != "" {
		return fmt.Sprintf("kizu %s (%s)", label, detail)
	}
	return "kizu " + label
}

// vcsDetail reads the revision Go stamped into the binary, empty when the
// build had no VCS information (`go build` outside a checkout, nix build).
func vcsDetail() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
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
