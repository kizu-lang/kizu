package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	machOMagic64         = 0xfeedfacf
	machOHeader64Size    = 32
	machOLoadHeaderSize  = 8
	machOLCUUID          = 0x1b
	machOLCCodeSignature = 0x1d
)

// TestSelfhostBootstrap uses the compiler built once by the shipping one to
// build the same source again, and requires the two executables to be
// identical.
//
// It is the one gate that asks the selfhost compiler to build its own source
// the whole way down. The shipping-built first stage is shared with the other
// selfhost gates, while only this test asks it for the second stage.
//
// Reproducing itself byte for byte is what says the self-built compiler
// compiles that source the way the shipping compiler does, so the
// comparisons against the shipping-built compiler hold for the self-built one
// without running those behavior gates twice. Mach-O's linker-made
// UUID and its derived ad-hoc signature are normalized first: they identify a
// link invocation rather than compiler output, and every other byte still has
// to agree.
//
// Both stages are written under the same file name in separate directories:
// the linker records the output file's name inside the executable, so two
// builds that differ only in where they were asked to write would differ by
// those bytes and by nothing else.
func TestSelfhostBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the selfhost compiler")
	}
	root := t.TempDir()
	stage1 := sharedSelfhost(t)
	stage2 := selfhostStagePath(t, root, "stage2")
	rebuild := exec.Command(stage1, "build", "--target", "native", "-o", stage2, "../../compiler")
	if out, err := rebuild.CombinedOutput(); err != nil {
		t.Fatalf("selfhost compiler building itself: %v\n%s", err, out)
	}
	first, err := os.ReadFile(stage1)
	if err != nil {
		t.Fatalf("read stage1: %v", err)
	}
	second, err := os.ReadFile(stage2)
	if err != nil {
		t.Fatalf("read stage2: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("selfhost compiler does not reproduce itself:"+
			" stage1 is %d bytes, stage2 is %d", len(first), len(second))
	}
	normalizeMachOLinkIdentity(t, "stage1", first)
	normalizeMachOLinkIdentity(t, "stage2", second)
	differing, firstAt := countDifferingBytes(first, second)
	if differing != 0 {
		t.Fatalf("selfhost compiler does not reproduce itself:"+
			" %d of %d bytes differ, first at offset %d",
			differing, len(first), firstAt)
	}
}

// normalizeMachOLinkIdentity removes the two pieces of link-invocation
// identity from a thin 64-bit Mach-O executable. LC_UUID changes between
// otherwise identical links on some Apple linker versions, and the ad-hoc
// code signature changes with it. Code, data, symbols and every load-command
// field remain in the byte comparison.
func normalizeMachOLinkIdentity(t *testing.T, stage string, executable []byte) {
	t.Helper()
	if len(executable) < 4 || binary.LittleEndian.Uint32(executable[:4]) != machOMagic64 {
		return
	}
	if len(executable) < machOHeader64Size {
		t.Fatalf("normalize %s Mach-O: truncated header", stage)
	}
	commandCount := binary.LittleEndian.Uint32(executable[16:20])
	commandBytes := uint64(binary.LittleEndian.Uint32(executable[20:24]))
	commandsEnd := uint64(machOHeader64Size) + commandBytes
	if commandsEnd > uint64(len(executable)) {
		t.Fatalf("normalize %s Mach-O: load commands exceed file", stage)
	}

	offset := uint64(machOHeader64Size)
	for commandIndex := uint32(0); commandIndex < commandCount; commandIndex++ {
		if offset+machOLoadHeaderSize > commandsEnd {
			t.Fatalf("normalize %s Mach-O: truncated load command %d", stage, commandIndex)
		}
		start := int(offset)
		command := binary.LittleEndian.Uint32(executable[start : start+4])
		commandSize := uint64(binary.LittleEndian.Uint32(executable[start+4 : start+8]))
		if commandSize < machOLoadHeaderSize || offset+commandSize > commandsEnd {
			t.Fatalf("normalize %s Mach-O: invalid load command %d size", stage, commandIndex)
		}

		normalizeMachOLoadIdentity(t, stage, executable, command, commandSize, start)
		offset += commandSize
	}
	if offset != commandsEnd {
		t.Fatalf("normalize %s Mach-O: load command size mismatch", stage)
	}
}

// normalizeMachOLoadIdentity clears one identity-bearing load command.
func normalizeMachOLoadIdentity(
	t *testing.T,
	stage string,
	executable []byte,
	command uint32,
	commandSize uint64,
	start int,
) {
	t.Helper()
	switch command {
	case machOLCUUID:
		if commandSize < 24 {
			t.Fatalf("normalize %s Mach-O: truncated LC_UUID", stage)
		}
		clear(executable[start+8 : start+24])
	case machOLCCodeSignature:
		if commandSize < 16 {
			t.Fatalf("normalize %s Mach-O: truncated LC_CODE_SIGNATURE", stage)
		}
		dataOffset := uint64(binary.LittleEndian.Uint32(executable[start+8 : start+12]))
		dataSize := uint64(binary.LittleEndian.Uint32(executable[start+12 : start+16]))
		dataEnd := dataOffset + dataSize
		if dataEnd < dataOffset || dataEnd > uint64(len(executable)) {
			t.Fatalf("normalize %s Mach-O: code signature exceeds file", stage)
		}
		clear(executable[int(dataOffset):int(dataEnd)])
	}
}

// TestNormalizeMachOLinkIdentity checks that only Mach-O identity bytes vary.
func TestNormalizeMachOLinkIdentity(t *testing.T) {
	first := make([]byte, 76)
	binary.LittleEndian.PutUint32(first[0:4], machOMagic64)
	binary.LittleEndian.PutUint32(first[16:20], 2)
	binary.LittleEndian.PutUint32(first[20:24], 40)
	first[31] = 0x7f
	binary.LittleEndian.PutUint32(first[32:36], machOLCUUID)
	binary.LittleEndian.PutUint32(first[36:40], 24)
	binary.LittleEndian.PutUint32(first[56:60], machOLCCodeSignature)
	binary.LittleEndian.PutUint32(first[60:64], 16)
	binary.LittleEndian.PutUint32(first[64:68], 72)
	binary.LittleEndian.PutUint32(first[68:72], 4)
	second := append([]byte(nil), first...)
	for index := 0; index < 16; index++ {
		first[40+index] = byte(index + 1)
		second[40+index] = byte(index + 17)
	}
	for index := 0; index < 4; index++ {
		first[72+index] = byte(index + 33)
		second[72+index] = byte(index + 37)
	}

	normalizeMachOLinkIdentity(t, "first fixture", first)
	normalizeMachOLinkIdentity(t, "second fixture", second)
	if differing, firstAt := countDifferingBytes(first, second); differing != 0 {
		t.Fatalf("normalized fixtures differ in %d bytes, first at %d", differing, firstAt)
	}
	if first[31] != 0x7f || second[31] != 0x7f {
		t.Fatal("normalization changed non-identity Mach-O bytes")
	}
}

// selfhostStagePath names one bootstrap stage's executable. Every stage uses
// the same file name in a directory of its own, because the name is what the
// executable records about where it was written.
func selfhostStagePath(t *testing.T, root string, stage string) string {
	t.Helper()
	dir := filepath.Join(root, stage)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s directory: %v", stage, err)
	}
	return filepath.Join(dir, "kizu")
}

// countDifferingBytes reports how many bytes two equal-length builds disagree
// on and the first offset they disagree at, so a broken fixed point says how
// far from one it is rather than only that it is not one.
func countDifferingBytes(first []byte, second []byte) (int, int) {
	differing := 0
	firstAt := -1
	for index := range first {
		if first[index] == second[index] {
			continue
		}
		if firstAt < 0 {
			firstAt = index
		}
		differing++
	}
	return differing, firstAt
}
