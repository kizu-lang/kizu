package wasm

import (
	"encoding/binary"
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

const enumPrintDataPrefix = "enum-print:"

// enumPrintTable is one linear-memory array of {pointer, length} rows in tag
// order. The spellings themselves are ordinary static byte data.
type enumPrintTable struct {
	offset int
	rows   []dataRef
}

// printedEnums returns enum types used by print in deterministic IR discovery
// order. A module that never prints an enum gets no name data or helper.
func (e *emitter) printedEnums() []string {
	seen := map[string]bool{}
	var names []string
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op != "call.print" || len(instr.Args) != 1 {
					continue
				}
				typ := instr.Args[0].Type
				if _, ok := e.module.Enums[typ]; !ok || seen[typ] {
					continue
				}
				seen[typ] = true
				names = append(names, typ)
			}
		}
	}
	return names
}

// collectEnumPrintData assigns static offsets to printed Enum::Tag spellings
// and their fixed-width tables, returning the first byte after them.
func (e *emitter) collectEnumPrintData(offset int) int {
	for _, typ := range e.printedEnums() {
		declared := e.module.Enums[typ]
		spellings := make([]string, len(declared.Tags))
		for tag, index := range declared.Tags {
			spellings[index] = typ + "::" + tag
		}
		table := enumPrintTable{rows: make([]dataRef, len(spellings))}
		for index, spelling := range spellings {
			key := enumPrintDataPrefix + spelling
			ref := dataRef{offset: offset, length: len(spelling)}
			e.strings[key] = ref
			e.dataOrder = append(e.dataOrder, key)
			table.rows[index] = ref
			offset += len(spelling)
		}
		e.enumTables[typ] = table
		e.enumTableOrder = append(e.enumTableOrder, typ)
	}
	offset = alignUp(offset, 4)
	for _, typ := range e.enumTableOrder {
		table := e.enumTables[typ]
		table.offset = offset
		e.enumTables[typ] = table
		offset += len(table.rows) * 8
	}
	return offset
}

// writeEnumPrintTables writes i32 pointer/length pairs in wasm little-endian
// memory order. Runtime indexing therefore does not branch on source tags.
func (e *emitter) writeEnumPrintTables() {
	for _, typ := range e.enumTableOrder {
		table := e.enumTables[typ]
		data := make([]byte, len(table.rows)*8)
		for index, row := range table.rows {
			binary.LittleEndian.PutUint32(data[index*8:], uint32(row.offset))
			binary.LittleEndian.PutUint32(data[index*8+4:], uint32(row.length))
		}
		fmt.Fprintf(&e.out, "  (data (i32.const %d) \"%s\")\n",
			table.offset, stringBytes(string(data)))
	}
}

// writeEnumPrintHelper indexes one {pointer, length} table after validating
// the tag, then writes the selected spelling as one line.
func (e *emitter) writeEnumPrintHelper() {
	e.out.WriteString("  (func $__print_enum (param $names i32) (param $count i64) (param $tag i64)\n")
	e.out.WriteString("    (local $row i32)\n")
	e.out.WriteString("    (if (i32.or (i64.lt_s (local.get $tag) (i64.const 0))\n")
	e.out.WriteString("        (i64.ge_s (local.get $tag) (local.get $count)))\n")
	e.out.WriteString("      (then (unreachable)))\n")
	e.out.WriteString("    (local.set $row (i32.add (local.get $names)\n")
	e.out.WriteString("      (i32.shl (i32.wrap_i64 (local.get $tag)) (i32.const 3))))\n")
	e.out.WriteString("    (call $__write_line (i32.load (local.get $row))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $row) (i32.const 4))))\n")
	e.out.WriteString("  )\n\n")
}

// writeEnumPrint emits a table lookup for typ when it is a printed enum.
func (e *emitter) writeEnumPrint(typ string, value ir.Value) bool {
	table, ok := e.enumTables[typ]
	if !ok {
		return false
	}
	fmt.Fprintf(&e.out,
		"            (call $__print_enum (i32.const %d) (i64.const %d) %s)\n",
		table.offset, len(table.rows), e.value(value).expr)
	return true
}
