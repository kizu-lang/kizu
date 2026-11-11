package main

import (
	"encoding/binary"
	"testing"
)

const (
	machOMagic64         = 0xfeedfacf
	machOHeader64Size    = 32
	machOLoadHeaderSize  = 8
	machOLCUUID          = 0x1b
	machOLCCodeSignature = 0x1d
)

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

// countDifferingBytes reports how many bytes two equal-length builds disagree
// on and the first offset they disagree at, so a mismatch says how far apart
// the builds are rather than only that they differ.
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
