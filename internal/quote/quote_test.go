package quote

import "testing"

// TestBytesUsesDeterministicASCII covers every byte class in the quote format.
func TestBytesUsesDeterministicASCII(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: `""`},
		{input: "plain ~", want: `"plain ~"`},
		{input: "café\n\r\t\\\"", want: `"caf\xC3\xA9\n\r\t\\\""`},
		{
			input: string([]byte{0x00, 0x1f, 0x20, 0x7e, 0x7f, 0xff}),
			want:  `"\x00\x1F ~\x7F\xFF"`,
		},
	}
	for _, test := range tests {
		if got := Bytes(test.input); got != test.want {
			t.Fatalf("Bytes(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
