// Package quote renders compiler-owned byte strings deterministically.
package quote

import "strings"

// Bytes returns an ASCII double-quoted spelling of text's bytes.
func Bytes(text string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	out.WriteByte('"')
	for i := 0; i < len(text); i++ {
		value := text[i]
		switch value {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteByte(value)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if value >= 0x20 && value <= 0x7e {
				out.WriteByte(value)
				continue
			}
			out.WriteString(`\x`)
			out.WriteByte(hex[value>>4])
			out.WriteByte(hex[value&0x0f])
		}
	}
	out.WriteByte('"')
	return out.String()
}
