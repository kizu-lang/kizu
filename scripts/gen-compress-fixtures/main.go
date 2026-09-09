// Command gen-compress-fixtures writes the deflate, gzip, and zlib streams
// that the std::compress behavior tests read back, using Go's compress
// packages as the reference encoder.
//
// Run it from the repository root; the streams land in
// examples/fixtures/compress/ next to the bytes they encode:
//
//	go run ./scripts/gen-compress-fixtures
//
// The streams are checked in, so running the Kizu tests needs no Go. A
// fixture only has to be a valid encoding of its plain file, so a newer Go
// that compresses differently does not invalidate the checked-in set; rerun
// this when adding a case.
package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dir = "examples/fixtures/compress"

// plain is one input: its file stem, the extension its bytes are written
// under, and the bytes.
type plain struct {
	name string
	ext  string
	data []byte
}

// A neutral paragraph, repeated so that the compressor finds matches at
// growing distances and a dynamic Huffman code pays off.
const paragraph = "A deflate stream is a run of blocks. A block is stored as it is, " +
	"or coded with the fixed Huffman code, or with a code the block " +
	"itself describes. A coded block is literals and back-references: " +
	"a length and a distance that name bytes already written.\n"

// main writes every fixture and reports what stopped it.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

// run builds the inputs and writes each in every format it is tested in.
func run() error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	noise := rand.New(rand.NewSource(1))
	random := func(n int) []byte {
		out := make([]byte, n)
		for i := range out {
			out[i] = byte(noise.Intn(256))
		}
		return out
	}
	farHalf := random(20000)
	inputs := []plain{
		{"empty", "txt", nil},
		{"hello", "txt", []byte("hello, kizu\n")},
		{"runs", "txt", []byte(strings.Repeat("abc", 700))},
		{"text", "txt", []byte(strings.Repeat(paragraph, 24))},
		{"binary", "bin", random(4096)},
		// The second half repeats the first, so every byte of it is a
		// back-reference at a distance near the window's edge.
		{"far", "bin", append(append([]byte{}, farHalf...), farHalf...)},
	}
	for _, in := range inputs {
		if err := write(in.name+"."+in.ext, in.data); err != nil {
			return err
		}
		if err := write(in.name+".deflate", deflate(in.data, flate.DefaultCompression)); err != nil {
			return err
		}
		if in.name == "far" {
			continue
		}
		if err := write(in.name+".gz", gz(in.data, nil)); err != nil {
			return err
		}
		if err := write(in.name+".zlib", zlibBytes(in.data)); err != nil {
			return err
		}
	}

	text := inputs[3].data
	for suffix, level := range map[string]int{
		"stored":  flate.NoCompression,
		"speed":   flate.BestSpeed,
		"best":    flate.BestCompression,
		"huffman": flate.HuffmanOnly,
	} {
		if err := write("text."+suffix+".deflate", deflate(text, level)); err != nil {
			return err
		}
	}

	hello := inputs[1].data
	header := &gzip.Header{
		Name:    "hello.txt",
		Comment: "a comment",
		Extra:   []byte{1, 2, 3, 4},
		ModTime: time.Unix(1_700_000_000, 0),
	}
	if err := write("hello.header.gz", gz(hello, header)); err != nil {
		return err
	}
	if err := write("hello.hcrc.gz", gzWithHeaderCRC(hello)); err != nil {
		return err
	}
	multi := append(gz(hello, nil), gz(text, nil)...)
	return write("multi.gz", multi)
}

// write puts one fixture in place.
func write(name string, data []byte) error {
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// deflate encodes a raw RFC 1951 stream at the given level.
func deflate(data []byte, level int) []byte {
	var out bytes.Buffer
	w, err := flate.NewWriter(&out, level)
	if err != nil {
		panic(err)
	}
	finish(w, data)
	return out.Bytes()
}

// gz encodes one gzip member, with the header fields given.
func gz(data []byte, header *gzip.Header) []byte {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if header != nil {
		w.Header = *header
	}
	finish(w, data)
	return out.Bytes()
}

// zlibBytes encodes one RFC 1950 stream.
func zlibBytes(data []byte) []byte {
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	finish(w, data)
	return out.Bytes()
}

// finish writes data through an encoder into its in-memory buffer, where
// the only failure would be a bug in the encoder.
func finish(w io.WriteCloser, data []byte) {
	if _, err := w.Write(data); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
}

// gzWithHeaderCRC builds a member whose header carries FHCRC, which Go's
// writer never emits: the flag, a name, the low 16 bits of the header's
// CRC-32, then the deflate stream and the usual trailer.
func gzWithHeaderCRC(data []byte) []byte {
	head := []byte{0x1f, 0x8b, 8, 0x02 | 0x08, 0, 0, 0, 0, 0, 255}
	head = append(head, []byte("hello.txt\x00")...)
	sum := crc32.ChecksumIEEE(head)
	head = append(head, byte(sum), byte(sum>>8))
	out := append(head, deflate(data, flate.DefaultCompression)...)
	body := crc32.ChecksumIEEE(data)
	out = append(out, byte(body), byte(body>>8), byte(body>>16), byte(body>>24))
	n := uint32(len(data))
	return append(out, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
}
