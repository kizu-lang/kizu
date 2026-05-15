package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const moduleConformanceManifest = "../../tests/conformance/modules/v0_3.json"

type moduleConformanceManifestData struct {
	Version string                  `json:"version"`
	Cases   []moduleConformanceCase `json:"cases"`
}

type moduleConformanceCase struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	Path           string   `json:"path"`
	RootSource     string   `json:"root_source"`
	StderrContains string   `json:"stderr_contains"`
	Features       []string `json:"features"`
}

// TestModuleConformanceManifest runs reusable module package fixtures.
func TestModuleConformanceManifest(t *testing.T) {
	manifest := loadModuleConformanceManifest(t)
	for _, tt := range manifest.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			validateModuleConformanceCase(t, tt)
			runModuleConformanceCase(t, tt)
		})
	}
}

// loadModuleConformanceManifest reads the module conformance manifest.
func loadModuleConformanceManifest(t *testing.T) moduleConformanceManifestData {
	t.Helper()
	data, err := os.ReadFile(moduleConformanceManifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest moduleConformanceManifestData
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v0.3-modules" {
		t.Fatalf("unexpected module conformance version %q", manifest.Version)
	}
	return manifest
}

// validateModuleConformanceCase checks fields shared with self-host runners.
func validateModuleConformanceCase(t *testing.T, tt moduleConformanceCase) {
	t.Helper()
	if tt.Name == "" || tt.Path == "" || tt.RootSource == "" {
		t.Fatalf("module conformance case must name path and root_source: %#v", tt)
	}
	if filepath.IsAbs(tt.Path) || filepath.IsAbs(tt.RootSource) {
		t.Fatalf("%s: paths must be relative to repo root", tt.Name)
	}
	if len(tt.Features) == 0 {
		t.Fatalf("%s: features must not be empty", tt.Name)
	}
	if tt.Mode == "error" && tt.StderrContains == "" {
		t.Fatalf("%s: error case must declare stderr_contains", tt.Name)
	}
}

// runModuleConformanceCase dispatches one module package fixture.
func runModuleConformanceCase(t *testing.T, tt moduleConformanceCase) {
	t.Helper()
	switch tt.Mode {
	case "check":
		runKizuOK(t, "check", tt.Path)
	case "error":
		out, err := runKizu("check", tt.Path)
		if err == nil {
			t.Fatalf("expected module check to fail\n%s", out)
		}
		if !strings.Contains(out, tt.StderrContains) {
			t.Fatalf("got %q, want substring %q", out, tt.StderrContains)
		}
	default:
		t.Fatalf("%s: unknown module mode %q", tt.Name, tt.Mode)
	}
}
