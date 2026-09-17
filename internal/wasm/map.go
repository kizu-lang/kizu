package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// A Map value is its inline header. wasm32 pointers occupy four bytes, but
// the i64 fields retain their natural alignment, so the five fields keep the
// same offsets and 40-byte size as the native header, and the entries and the
// index below are laid out the way runtime.c lays them out.
//
// An entry is a three-word header -- the key's hash, its length, and the key
// itself when it is at most eight bytes or the address of a copy when it is
// longer -- followed by the value rounded up to whole words. The index is
// index_cap control bytes, each mapFree or the top seven bits of the hash of
// the entry its slot names, followed by index_cap 32-bit entry numbers: a
// lookup that misses reads control bytes and nothing else.
const (
	mapWasmPrefix          = "std::map::Map<"
	mapHeaderSize          = 40
	mapEntriesOffset       = 0
	mapLenOffset           = 8
	mapCapacityOffset      = 16
	mapIndexOffset         = 24
	mapIndexCapacityOffset = 32
	mapEntryHeaderSize     = 24
	mapEntryHashOffset     = 0
	mapEntryKeyLenOffset   = 8
	mapEntryKeyOffset      = 16
	mapEntryValueOffset    = 24
	mapTempSize            = 16
	mapTempValueOffset     = 8
)

// mapEntrySize is the width of one entry of a map whose values are
// valueSize bytes.
func mapEntrySize(valueSize int) int {
	return mapEntryHeaderSize + alignUp(valueSize, 8)
}

// isMapWasmType reports whether name is a direct Map storage type.
func isMapWasmType(name string) bool {
	return strings.HasPrefix(name, mapWasmPrefix) && strings.HasSuffix(name, ">")
}

// mapElementWasmTypes returns K and V through either direct or borrowed Map.
func mapElementWasmTypes(name string) (string, string, bool) {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "&var "), "&")
	if !isMapWasmType(name) {
		return "", "", false
	}
	parts, err := typ.SplitArgs(name[len(mapWasmPrefix) : len(name)-1])
	if err != nil || len(parts) != 2 || !typ.IsMapKey(parts[0]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// usesMapRuntime reports whether this module operates on a Map.
func (e *emitter) usesMapRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "map.") {
					return true
				}
			}
		}
	}
	return false
}

// mapInstrNeedsTemp reports whether an operation normalizes a key or value
// into addressed bytes before entering the generic runtime.
func mapInstrNeedsTemp(op string) bool {
	switch op {
	case "map.insert", "map.get", "map.at", "map.at_mut", "map.contains", "map.remove":
		return true
	default:
		return false
	}
}

// mapTempSlot returns the per-instruction scratch cell planned in the current
// invocation. It holds at most an eight-byte scalar key and scalar value.
func (e *emitter) mapTempSlot(result ir.Value) (string, error) {
	return e.frameSlot(mapTempSlotKey(result.Name))
}

// mapTempSlotKey names a Map instruction's scratch cell in the frame plan.
func mapTempSlotKey(name string) string {
	return "map-temp:" + name
}

// writeMapRuntime emits the insertion-ordered hash table shared by every
// Map<K, V>. Keys enter as bytes and values as measured addresses, so no
// helper depends on a source type or generated function shape.
func (e *emitter) writeMapRuntime() {
	e.writeMapReadTailHelper()
	e.writeMapHashHelper()
	e.writeMapShortKeyHelper()
	e.writeMapKeysEqualHelper()
	e.writeMapSlotHelper()
	e.writeMapFindHelper()
	e.writeMapPlaceHelper()
	e.writeMapReindexHelper()
	e.writeMapReserveHelper()
	e.writeMapInsertHelper()
	e.writeMapGetHelper()
	e.writeMapGetWordHelper()
	e.writeMapRemoveHelper()
	e.writeMapDeinitHelper()
}

// writeMapReadTailHelper emits the short-tail reader used by Map hashing.
func (e *emitter) writeMapReadTailHelper() {
	e.out.WriteString("  (func $__map_read_tail (param $key i32) (param $length i64) (result " +
		"i64)\n")
	e.out.WriteString("    (if (i64.ge_u (local.get $length) (i64.const 4))\n")
	e.out.WriteString("      (then (return\n")
	e.out.WriteString("        (i64.or\n")
	e.out.WriteString("          (i64.shl (i64.extend_i32_u " +
		"(i32.load (local.get $key))) (i64.const 32))\n")
	e.out.WriteString("          (i64.extend_i32_u\n")
	e.out.WriteString("            (i32.load (i32.add (local.get $key)\n")
	e.out.WriteString("              (i32.wrap_i64 " +
		"(i64.sub (local.get $length) (i64.const 4))))))))))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $length))\n")
	e.out.WriteString("      (then (return (i64.const 0))))\n")
	e.out.WriteString("    (i64.or\n")
	e.out.WriteString("      (i64.shl (i64.extend_i32_u " +
		"(i32.load8_u (local.get $key))) (i64.const 16))\n")
	e.out.WriteString("      (i64.or\n")
	e.out.WriteString("        (i64.shl\n")
	e.out.WriteString("          (i64.extend_i32_u (i32.load8_u (i32.add (local.get $key)\n")
	e.out.WriteString("            (i32.wrap_i64 (i64.shr_u (local.get $length) (i64.const " +
		"1))))))\n")
	e.out.WriteString("          (i64.const 8))\n")
	e.out.WriteString("        (i64.extend_i32_u (i32.load8_u (i32.add (local.get $key)\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.sub (local.get $length) (i64.const " +
		"1))))))))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapHashHelper emits the byte-oriented Map key hash.
func (e *emitter) writeMapHashHelper() {
	e.out.WriteString("  (func $__map_hash (param $key i32) (param $length i64) (result i64)\n")
	e.out.WriteString("    (local $hash i64) (local $offset i64)\n")
	e.out.WriteString("    (local.set $hash (local.get $length))\n")
	e.out.WriteString("    (block $tail\n")
	e.out.WriteString("      (loop $words\n")
	e.out.WriteString("        (br_if $tail (i64.gt_u\n")
	e.out.WriteString("          (i64.add (local.get $offset) (i64.const 8)) (local.get " +
		"$length)))\n")
	e.out.WriteString("        (local.set $hash\n")
	e.out.WriteString("          (i64.mul\n")
	e.out.WriteString("            (i64.xor (i64.rotl (local.get $hash) (i64.const 5))\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $key) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("            (i64.const 0x517cc1b727220a95)))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const " +
		"8)))\n")
	e.out.WriteString("        (br $words)))\n")
	e.out.WriteString("    (if (i64.lt_u (local.get $offset) (local.get $length))\n")
	e.out.WriteString("      (then (local.set $hash\n")
	e.out.WriteString("        (i64.mul\n")
	e.out.WriteString("          (i64.xor (i64.rotl (local.get $hash) (i64.const 5))\n")
	e.out.WriteString("            (call $__map_read_tail\n")
	e.out.WriteString("              (i32.add (local.get $key) (i32.wrap_i64 (local.get " +
		"$offset)))\n")
	e.out.WriteString("              (i64.sub (local.get $length) (local.get $offset))))\n")
	e.out.WriteString("          (i64.const 0x517cc1b727220a95)))))\n")
	e.out.WriteString("    (call $__map_mix (local.get $hash))\n")
	e.out.WriteString("  )\n\n")
	e.out.WriteString("  (func $__map_mix (param $hash i64) (result i64)\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) " +
		"(i64.shr_u (local.get $hash) (i64.const 32))))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.mul (local.get $hash) (i64.const 0xd6e8feb86659fd93)))\n")
	e.out.WriteString("    (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) " +
		"(i64.const 32)))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapShortKeyHelper emits the reader of a key of at most eight bytes as
// the zero-padded word an entry stores it as.
func (e *emitter) writeMapShortKeyHelper() {
	e.out.WriteString("  (func $__map_short_key (param $key i32) (param $length i64) (result " +
		"i64)\n")
	e.out.WriteString("    (local $word i64) (local $offset i64)\n")
	e.out.WriteString("    (if (i64.eq (local.get $length) (i64.const 8))\n")
	e.out.WriteString("      (then (return (i64.load (local.get $key)))))\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $bytes\n")
	e.out.WriteString("        (br_if $done (i64.ge_u (local.get $offset) (local.get " +
		"$length)))\n")
	e.out.WriteString("        (local.set $word (i64.or (local.get $word)\n")
	e.out.WriteString("          (i64.shl (i64.load8_u (i32.add (local.get $key) " +
		"(i32.wrap_i64 (local.get $offset))))\n")
	e.out.WriteString("            (i64.mul (local.get $offset) (i64.const 8)))))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const " +
		"1)))\n")
	e.out.WriteString("        (br $bytes)))\n")
	e.out.WriteString("    (local.get $word)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapKeysEqualHelper emits byte equality for Map keys with equal lengths.
func (e *emitter) writeMapKeysEqualHelper() {
	e.out.WriteString("  (func $__map_keys_equal\n")
	e.out.WriteString("      (param $left i32) (param $right i32) (param $length i64) " +
		"(result i32)\n")
	e.out.WriteString("    (local $offset i64)\n")
	e.out.WriteString("    (block $bytes\n")
	e.out.WriteString("      (loop $words\n")
	e.out.WriteString("        (br_if $bytes (i64.gt_u\n")
	e.out.WriteString("          (i64.add (local.get $offset) (i64.const 8)) (local.get " +
		"$length)))\n")
	e.out.WriteString("        (if (i64.ne\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $left) " +
		"(i32.wrap_i64 (local.get $offset))))\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $right) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const " +
		"8)))\n")
	e.out.WriteString("        (br $words)))\n")
	e.out.WriteString("    (block $equal\n")
	e.out.WriteString("      (loop $tail\n")
	e.out.WriteString("        (br_if $equal (i64.ge_u (local.get $offset) (local.get " +
		"$length)))\n")
	e.out.WriteString("        (if (i32.ne\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $left) " +
		"(i32.wrap_i64 (local.get $offset))))\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $right) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const " +
		"1)))\n")
	e.out.WriteString("        (br $tail)))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapSlotHelper emits linear probing for a Map key or the free slot it
// belongs in. A slot whose control byte differs from the key's is passed
// without reading its entry.
func (e *emitter) writeMapSlotHelper() {
	e.out.WriteString("  (func $__map_slot\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64)\n")
	e.out.WriteString("      (param $hash i64) (param $entry_size i32) (result i64)\n")
	e.out.WriteString("    (local $mask i64) (local $slot i64) (local $index i32) (local " +
		"$capacity i32)\n")
	e.out.WriteString("    (local $tag i32) (local $control i32) (local $entry i32) (local " +
		"$short i64)\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (local.set $mask (i64.sub " +
		"(i64.extend_i32_u (local.get $capacity)) (i64.const 1)))\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $slot (i64.and (local.get $hash) (local.get $mask)))\n")
	e.out.WriteString("    (local.set $tag (i32.wrap_i64 (i64.shr_u (local.get $hash) " +
		"(i64.const 57))))\n")
	e.out.WriteString("    (if (i64.le_u (local.get $key_length) (i64.const 8))\n")
	e.out.WriteString("      (then (local.set $short (call $__map_short_key " +
		"(local.get $key) (local.get $key_length)))))\n")
	e.out.WriteString("    (loop $probe\n")
	e.out.WriteString("      (local.set $control (i32.load8_u (i32.add (local.get $index) " +
		"(i32.wrap_i64 (local.get $slot)))))\n")
	e.out.WriteString("      (if (i32.eq (local.get $control) (i32.const 128))\n")
	e.out.WriteString("        (then (return (local.get $slot))))\n")
	e.out.WriteString("      (if (i32.eq (local.get $control) (local.get $tag))\n")
	e.out.WriteString("        (then\n")
	e.out.WriteString("          (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("            (i32.mul (local.get $entry_size)\n")
	e.out.WriteString("              (i32.load (i32.add (i32.add (local.get $index) " +
		"(local.get $capacity))\n")
	e.out.WriteString("                (i32.shl (i32.wrap_i64 (local.get $slot)) (i32.const " +
		"2)))))))\n")
	e.out.WriteString("          (if (i32.and\n")
	e.out.WriteString("                (i64.eq (i64.load (local.get $entry)) (local.get " +
		"$hash))\n")
	e.out.WriteString("                (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 8)))\n")
	e.out.WriteString("                  (local.get $key_length)))\n")
	e.out.WriteString("            (then\n")
	e.out.WriteString("              (if (i64.le_u (local.get $key_length) (i64.const 8))\n")
	e.out.WriteString("                (then\n")
	e.out.WriteString("                  (if (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 16)))\n")
	e.out.WriteString("                        (local.get $short))\n")
	e.out.WriteString("                    (then (return (local.get $slot)))))\n")
	e.out.WriteString("                (else\n")
	e.out.WriteString("                  (if (call $__map_keys_equal\n")
	e.out.WriteString("                        (i32.load (i32.add (local.get $entry) " +
		"(i32.const 16)))\n")
	e.out.WriteString("                        (local.get $key) (local.get $key_length))\n")
	e.out.WriteString("                    (then (return (local.get $slot))))))))))\n")
	e.out.WriteString("      (local.set $slot\n")
	e.out.WriteString("        (i64.and (i64.add (local.get $slot) (i64.const 1)) (local.get " +
		"$mask)))\n")
	e.out.WriteString("      (br $probe))\n")
	e.out.WriteString("    (unreachable)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapFindHelper emits a Map lookup that returns the insertion index.
func (e *emitter) writeMapFindHelper() {
	e.out.WriteString("  (func $__map_find\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64) " +
		"(param $entry_size i32)\n")
	e.out.WriteString("      (result i64)\n")
	e.out.WriteString("    (local $slot i64) (local $capacity i32) (local $index i32)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i32.or (i64.lt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("            (i32.or\n")
	e.out.WriteString("              (i32.and (i32.eqz (local.get $key))\n")
	e.out.WriteString("                (i64.gt_s (local.get $key_length) (i64.const 0)))\n")
	e.out.WriteString("              (i64.eqz (i64.load " +
		"(i32.add (local.get $map) (i32.const 32)))))))\n")
	e.out.WriteString("      (then (return (i64.const -1))))\n")
	e.out.WriteString("    (local.set $slot (call $__map_slot (local.get $map) (local.get " +
		"$key)\n")
	e.out.WriteString("      (local.get $key_length) (call $__map_hash " +
		"(local.get $key) (local.get $key_length))\n")
	e.out.WriteString("      (local.get $entry_size)))\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (if (i32.eq (i32.load8_u (i32.add (local.get $index) " +
		"(i32.wrap_i64 (local.get $slot))))\n")
	e.out.WriteString("          (i32.const 128))\n")
	e.out.WriteString("      (then (return (i64.const -1))))\n")
	e.out.WriteString("    (i64.extend_i32_u (i32.load (i32.add (i32.add (local.get $index) " +
		"(local.get $capacity))\n")
	e.out.WriteString("      (i32.shl (i32.wrap_i64 (local.get $slot)) (i32.const 2)))))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapPlaceHelper emits the filing of every entry into an index whose
// control bytes are all free, by the hash each entry carries.
func (e *emitter) writeMapPlaceHelper() {
	e.out.WriteString("  (func $__map_place (param $map i32) (param $entry_size i32)\n")
	e.out.WriteString("    (local $index i32) (local $capacity i32) (local $mask i64)\n")
	e.out.WriteString("    (local $number i64) (local $hash i64) (local $slot i64)\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (local.set $mask (i64.sub " +
		"(i64.extend_i32_u (local.get $capacity)) (i64.const 1)))\n")
	e.out.WriteString("    (block $placed_all\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $placed_all (i64.ge_u (local.get $number)\n")
	e.out.WriteString("          (i64.load (i32.add (local.get $map) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $hash (i64.load (i32.add (i32.load (local.get " +
		"$map))\n")
	e.out.WriteString("          (i32.mul (local.get $entry_size) (i32.wrap_i64 (local.get " +
		"$number))))))\n")
	e.out.WriteString("        (local.set $slot (i64.and (local.get $hash) (local.get " +
		"$mask)))\n")
	e.out.WriteString("        (block $placed\n")
	e.out.WriteString("          (loop $probe\n")
	e.out.WriteString("            (br_if $placed (i32.eq (i32.load8_u (i32.add (local.get " +
		"$index)\n")
	e.out.WriteString("              (i32.wrap_i64 (local.get $slot)))) (i32.const 128)))\n")
	e.out.WriteString("            (local.set $slot (i64.and\n")
	e.out.WriteString("              (i64.add (local.get $slot) (i64.const 1)) (local.get " +
		"$mask)))\n")
	e.out.WriteString("            (br $probe)))\n")
	e.out.WriteString("        (i32.store8 (i32.add (local.get $index) (i32.wrap_i64 " +
		"(local.get $slot)))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.shr_u (local.get $hash) (i64.const " +
		"57))))\n")
	e.out.WriteString("        (i32.store (i32.add (i32.add (local.get $index) (local.get " +
		"$capacity))\n")
	e.out.WriteString("          (i32.shl (i32.wrap_i64 (local.get $slot)) (i32.const 2)))\n")
	e.out.WriteString("          (i32.wrap_i64 (local.get $number)))\n")
	e.out.WriteString("        (local.set $number (i64.add (local.get $number) (i64.const " +
		"1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapReindexHelper emits growth and rebuilding of a Map's hash index.
func (e *emitter) writeMapReindexHelper() {
	e.out.WriteString("  (func $__map_reindex\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) " +
		"(param $needed i64) (param $entry_size i32) (result i32)\n")
	e.out.WriteString("    (local $next i64) (local $old_capacity i64) (local $new_index " +
		"i32)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i64.lt_s (local.get $needed) (i64.const 0)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $old_capacity\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 32))))\n")
	e.out.WriteString("    (local.set $next (local.get $old_capacity))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $next))\n")
	e.out.WriteString("      (then (local.set $next (i64.const 8))))\n")
	e.out.WriteString("    (block $grown\n")
	e.out.WriteString("      (loop $grow\n")
	e.out.WriteString("        (br_if $grown (i64.le_u (local.get $needed)\n")
	e.out.WriteString("          (i64.div_u (i64.mul (local.get $next) " +
		"(i64.const 3)) (i64.const 4))))\n")
	e.out.WriteString("        (if (i64.gt_u (local.get $next) (i64.const 134217727))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $next (i64.mul (local.get $next) (i64.const 2)))\n")
	e.out.WriteString("        (br $grow)))\n")
	e.out.WriteString("    (if (i64.eq (local.get $next) (local.get $old_capacity))\n")
	e.out.WriteString("      (then (return (i32.const 1))))\n")
	e.out.WriteString("    (local.set $new_index\n")
	e.out.WriteString("      (call $__allocator_alloc (local.get $allocator)\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.mul (local.get $next) (i64.const 5)))))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $new_index)) (then (return (i32.const " +
		"0))))\n")
	e.out.WriteString("    (memory.fill (local.get $new_index) (i32.const 128) " +
		"(i32.wrap_i64 (local.get $next)))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $old_capacity) (i64.const " +
		"5))))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $map) " +
		"(i32.const 24)) (local.get $new_index))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 32)) (local.get " +
		"$next))\n")
	e.out.WriteString("    (call $__map_place (local.get $map) (local.get $entry_size))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapReserveHelper emits growth of insertion-ordered Map entries.
func (e *emitter) writeMapReserveHelper() {
	e.out.WriteString("  (func $__map_reserve\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) " +
		"(param $needed i64) (param $entry_size i32) (result i32)\n")
	e.out.WriteString("    (local $capacity i64) (local $next i64) (local $old_bytes i32)\n")
	e.out.WriteString("    (local $new_bytes i32) (local $entries i32) (local $limit i64)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i64.lt_s (local.get $needed) (i64.const 0)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $capacity\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 16))))\n")
	e.out.WriteString("    (if (i64.le_u (local.get $needed) (local.get $capacity))\n")
	e.out.WriteString("      (then (return (i32.const 1))))\n")
	e.out.WriteString("    (local.set $limit (i64.div_u (i64.const 1073741823)\n")
	e.out.WriteString("      (i64.extend_i32_u (local.get $entry_size))))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $capacity))\n")
	e.out.WriteString("      (then (local.set $next (i64.const 4)))\n")
	e.out.WriteString("      (else (local.set $next (i64.mul (local.get $capacity) " +
		"(i64.const 2)))))\n")
	e.out.WriteString("    (block $grown\n")
	e.out.WriteString("      (loop $grow\n")
	e.out.WriteString("        (br_if $grown (i64.ge_u (local.get $next) (local.get " +
		"$needed)))\n")
	e.out.WriteString("        (local.set $next (i64.mul (local.get $next) (i64.const 2)))\n")
	e.out.WriteString("        (br $grow)))\n")
	e.out.WriteString("    (if (i64.gt_u (local.get $next) (local.get $limit))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $old_bytes\n")
	e.out.WriteString("      (i32.mul (i32.wrap_i64 (local.get $capacity)) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("    (local.set $new_bytes\n")
	e.out.WriteString("      (i32.mul (i32.wrap_i64 (local.get $next)) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("    (local.set $entries\n")
	e.out.WriteString("      (call $__allocator_realloc (local.get $allocator) " +
		"(i32.load (local.get $map))\n")
	e.out.WriteString("        (local.get $old_bytes) (local.get $new_bytes)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $entries)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (memory.fill (i32.add (local.get $entries) (local.get " +
		"$old_bytes))\n")
	e.out.WriteString("      (i32.const 0) (i32.sub (local.get $new_bytes) (local.get " +
		"$old_bytes)))\n")
	e.out.WriteString("    (i32.store (local.get $map) (local.get $entries))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 16)) (local.get " +
		"$next))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapInsertHelper emits insertion and replacement for generic Map values.
func (e *emitter) writeMapInsertHelper() {
	e.out.WriteString("  (func $__map_insert\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $key i32)\n")
	e.out.WriteString("      (param $key_length i64) (param $value i32) (param $value_size " +
		"i32)\n")
	e.out.WriteString("      (result i32)\n")
	e.out.WriteString("    (local $needed i64) (local $hash i64) (local $slot i64)\n")
	e.out.WriteString("    (local $entry_size i32) (local $index i32) (local $capacity i32)\n")
	e.out.WriteString("    (local $entry i32) (local $stored_key i64)\n")
	e.writeMapInsertLookup()
	e.writeMapInsertEntry()
}

// writeMapInsertLookup emits validation, index growth, and replacement.
func (e *emitter) writeMapInsertLookup() {
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i32.or (i64.lt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("            (i32.or\n")
	e.out.WriteString("              (i32.and (i32.eqz (local.get $key))\n")
	e.out.WriteString("                (i64.gt_s (local.get $key_length) (i64.const 0)))\n")
	e.out.WriteString("              (i32.or (i32.eqz (local.get $value))\n")
	e.out.WriteString("                (i32.le_s (local.get $value_size) (i32.const 0))))))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i64.gt_u (local.get $key_length) (i64.const 2147483640))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (local.set $needed\n")
	e.out.WriteString("      (i64.add (i64.load " +
		"(i32.add (local.get $map) (i32.const 8))) (i64.const 1)))\n")
	e.out.WriteString("    (if (i32.eqz (call $__map_reindex (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $hash " +
		"(call $__map_hash (local.get $key) (local.get $key_length)))\n")
	e.out.WriteString("    (local.set $slot (call $__map_slot (local.get $map) (local.get " +
		"$key)\n")
	e.out.WriteString("      (local.get $key_length) (local.get $hash) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (if (i32.ne (i32.load8_u (i32.add (local.get $index) " +
		"(i32.wrap_i64 (local.get $slot))))\n")
	e.out.WriteString("          (i32.const 128))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (memory.copy (i32.add (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("            (i32.mul (local.get $entry_size)\n")
	e.out.WriteString("              (i32.load (i32.add (i32.add (local.get $index) " +
		"(local.get $capacity))\n")
	e.out.WriteString("                (i32.shl (i32.wrap_i64 (local.get $slot)) (i32.const " +
		"2))))))\n")
	e.out.WriteString("            (i32.const 24))\n")
	e.out.WriteString("          (local.get $value) (local.get $value_size))\n")
	e.out.WriteString("        (return (i32.const 1))))\n")
}

// writeMapInsertEntry emits the new entry: the key in the entry's own form,
// taken before the entries can move since a key may be read out of this map,
// then the growth of the entries, and the entry and its slot.
func (e *emitter) writeMapInsertEntry() {
	e.out.WriteString("    (if (i64.le_u (local.get $key_length) (i64.const 8))\n")
	e.out.WriteString("      (then (local.set $stored_key " +
		"(call $__map_short_key (local.get $key) (local.get $key_length))))\n")
	e.out.WriteString("      (else\n")
	e.out.WriteString("        (local.set $entry (call $__allocator_alloc (local.get " +
		"$allocator)\n")
	e.out.WriteString("          (i32.wrap_i64 (local.get $key_length))))\n")
	e.out.WriteString("        (if (i32.eqz (local.get $entry)) (then (return (i32.const " +
		"0))))\n")
	e.out.WriteString("        (memory.copy (local.get $entry) (local.get $key) " +
		"(i32.wrap_i64 (local.get $key_length)))\n")
	e.out.WriteString("        (local.set $stored_key (i64.extend_i32_u (local.get " +
		"$entry)))))\n")
	e.out.WriteString("    (if (i32.eqz (call $__map_reserve (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (if (i64.gt_u (local.get $key_length) (i64.const 8))\n")
	e.out.WriteString("          (then (call $__allocator_free (local.get $allocator) " +
		"(local.get $entry)\n")
	e.out.WriteString("            (i32.wrap_i64 (local.get $key_length)))))\n")
	e.out.WriteString("        (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) (i32.const 8)))))))\n")
	e.out.WriteString("    (i64.store (local.get $entry) (local.get $hash))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) " +
		"(i32.const 8)) (local.get $key_length))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) " +
		"(i32.const 16)) (local.get $stored_key))\n")
	e.out.WriteString("    (memory.copy (i32.add (local.get $entry) (i32.const 24))\n")
	e.out.WriteString("      (local.get $value) (local.get $value_size))\n")
	e.out.WriteString("    (i32.store8 (i32.add (local.get $index) (i32.wrap_i64 (local.get " +
		"$slot)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.shr_u (local.get $hash) (i64.const 57))))\n")
	e.out.WriteString("    (i32.store (i32.add (i32.add (local.get $index) (local.get " +
		"$capacity))\n")
	e.out.WriteString("      (i32.shl (i32.wrap_i64 (local.get $slot)) (i32.const 2)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.load (i32.add (local.get $map) (i32.const " +
		"8)))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 8)) (local.get " +
		"$needed))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapGetHelper emits a lookup that returns the stored value address.
func (e *emitter) writeMapGetHelper() {
	e.out.WriteString("  (func $__map_get\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64) " +
		"(param $value_size i32)\n")
	e.out.WriteString("      (result i32)\n")
	e.out.WriteString("    (local $found i64) (local $entry_size i32)\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (local.set $found\n")
	e.out.WriteString("      (call $__map_find (local.get $map) " +
		"(local.get $key) (local.get $key_length) (local.get $entry_size)))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $found) (i64.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (i32.add (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64 (local.get " +
		"$found))))\n")
	e.out.WriteString("      (i32.const 24))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapGetWordHelper emits the lookup of an eight-byte key handed over as
// the word it is: the hash is the generic hash of those eight bytes, worked
// out without reading them from memory, and the probe compares words. A
// lookup written in place hands it the probes that reach the last control
// bytes.
func (e *emitter) writeMapGetWordHelper() {
	e.out.WriteString("  (func $__map_get_word\n")
	e.out.WriteString("      (param $map i32) (param $key i64) (param $value_size i32) " +
		"(result i32)\n")
	e.out.WriteString("    (local $hash i64) (local $mask i32) (local $slot i32) (local " +
		"$index i32)\n")
	e.out.WriteString("    (local $numbers i32) (local $entries i32) (local $tag i32) (local " +
		"$control i32)\n")
	e.out.WriteString("    (local $entry_size i32) (local $entry i32) (local $capacity i32)\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $capacity)) (then (return (i32.const " +
		"0))))\n")
	e.out.WriteString("    (local.set $hash (i64.mul\n")
	e.out.WriteString("      (i64.xor (i64.const 256) (local.get $key)) (i64.const " +
		"0x517cc1b727220a95)))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) " +
		"(i64.const 32))))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.mul (local.get $hash) (i64.const 0xd6e8feb86659fd93)))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) " +
		"(i64.const 32))))\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $numbers (i32.add (local.get $index) (local.get " +
		"$capacity)))\n")
	e.out.WriteString("    (local.set $entries (i32.load (local.get $map)))\n")
	e.out.WriteString("    (local.set $mask (i32.sub (local.get $capacity) (i32.const 1)))\n")
	e.out.WriteString("    (local.set $slot (i32.and (i32.wrap_i64 (local.get $hash)) " +
		"(local.get $mask)))\n")
	e.out.WriteString("    (local.set $tag (i32.wrap_i64 (i64.shr_u (local.get $hash) " +
		"(i64.const 57))))\n")
	e.out.WriteString("    (loop $probe\n")
	e.out.WriteString("      (local.set $control (i32.load8_u (i32.add (local.get $index) " +
		"(local.get $slot))))\n")
	e.out.WriteString("      (if (i32.eq (local.get $control) (i32.const 128))\n")
	e.out.WriteString("        (then (return (i32.const 0))))\n")
	e.out.WriteString("      (if (i32.eq (local.get $control) (local.get $tag))\n")
	e.out.WriteString("        (then\n")
	e.out.WriteString("          (local.set $entry (i32.add (local.get $entries)\n")
	e.out.WriteString("            (i32.mul (local.get $entry_size)\n")
	e.out.WriteString("              (i32.load (i32.add (local.get $numbers) " +
		"(i32.shl (local.get $slot) (i32.const 2)))))))\n")
	e.out.WriteString("          (if (i32.and\n")
	e.out.WriteString("                (i64.eq (i64.load (local.get $entry)) (local.get " +
		"$hash))\n")
	e.out.WriteString("                (i32.and\n")
	e.out.WriteString("                  (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 8)))\n")
	e.out.WriteString("                    (i64.const 8))\n")
	e.out.WriteString("                  (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 16)))\n")
	e.out.WriteString("                    (local.get $key))))\n")
	e.out.WriteString("            (then (return (i32.add (local.get $entry) (i32.const " +
		"24)))))))\n")
	e.out.WriteString("      (local.set $slot (i32.and (i32.add (local.get $slot) (i32.const 1)) " +
		"(local.get $mask)))\n")
	e.out.WriteString("      (br $probe))\n")
	e.out.WriteString("    (unreachable)\n")
	e.out.WriteString("  )\n\n")
	e.writeMapInsertWordHelper()
}

// writeMapInsertWordHelper emits insertion for an eight-byte key handed over
// as the word it is, which an entry stores in place. The index and the
// entries grow through the generic helpers only when they must.
func (e *emitter) writeMapInsertWordHelper() {
	e.out.WriteString("  (func $__map_insert_word\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $key i64)\n")
	e.out.WriteString("      (param $value i32) (param $value_size i32) (result i32)\n")
	e.out.WriteString("    (local $needed i64) (local $hash i64) (local $mask i32) (local " +
		"$slot i32)\n")
	e.out.WriteString("    (local $index i32) (local $numbers i32) (local $tag i32) (local " +
		"$control i32)\n")
	e.out.WriteString("    (local $entry_size i32) (local $entry i32) (local $capacity i32)\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (local.set $needed\n")
	e.out.WriteString("      (i64.add (i64.load (i32.add (local.get $map) (i32.const 8))) " +
		"(i64.const 1)))\n")
	e.out.WriteString("    (if (i64.gt_u (i64.mul (local.get $needed) (i64.const 4))\n")
	e.out.WriteString("          (i64.mul (i64.load (i32.add (local.get $map) (i32.const 32))) " +
		"(i64.const 3)))\n")
	e.out.WriteString("      (then (if (i32.eqz (call $__map_reindex (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("        (then (return (i32.const 0))))))\n")
	e.writeMapInsertWordProbe()
	e.writeMapInsertWordEntry()
}

// writeMapInsertWordProbe emits the word lookup of an insertion: a key
// already present has its value replaced, and a free slot leaves the probe.
func (e *emitter) writeMapInsertWordProbe() {
	e.out.WriteString("    (local.set $hash (i64.mul\n")
	e.out.WriteString("      (i64.xor (i64.const 256) (local.get $key)) (i64.const " +
		"0x517cc1b727220a95)))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) " +
		"(i64.const 32))))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.mul (local.get $hash) (i64.const 0xd6e8feb86659fd93)))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) " +
		"(i64.const 32))))\n")
	e.out.WriteString("    (local.set $capacity (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $map) (i32.const 32)))))\n")
	e.out.WriteString("    (local.set $index (i32.load (i32.add (local.get $map) (i32.const " +
		"24))))\n")
	e.out.WriteString("    (local.set $numbers (i32.add (local.get $index) (local.get " +
		"$capacity)))\n")
	e.out.WriteString("    (local.set $mask (i32.sub (local.get $capacity) (i32.const 1)))\n")
	e.out.WriteString("    (local.set $slot (i32.and (i32.wrap_i64 (local.get $hash)) " +
		"(local.get $mask)))\n")
	e.out.WriteString("    (local.set $tag (i32.wrap_i64 (i64.shr_u (local.get $hash) " +
		"(i64.const 57))))\n")
	e.out.WriteString("    (block $free\n")
	e.out.WriteString("      (loop $probe\n")
	e.out.WriteString("        (local.set $control (i32.load8_u " +
		"(i32.add (local.get $index) (local.get $slot))))\n")
	e.out.WriteString("        (br_if $free (i32.eq (local.get $control) (i32.const 128)))\n")
	e.out.WriteString("        (if (i32.eq (local.get $control) (local.get $tag))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("              (i32.mul (local.get $entry_size)\n")
	e.out.WriteString("                (i32.load (i32.add (local.get $numbers) " +
		"(i32.shl (local.get $slot) (i32.const 2)))))))\n")
	e.out.WriteString("            (if (i32.and\n")
	e.out.WriteString("                  (i64.eq (i64.load (local.get $entry)) (local.get " +
		"$hash))\n")
	e.out.WriteString("                  (i32.and\n")
	e.out.WriteString("                    (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 8)))\n")
	e.out.WriteString("                      (i64.const 8))\n")
	e.out.WriteString("                    (i64.eq (i64.load (i32.add (local.get $entry) " +
		"(i32.const 16)))\n")
	e.out.WriteString("                      (local.get $key))))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (memory.copy (i32.add (local.get $entry) (i32.const " +
		"24))\n")
	e.out.WriteString("                  (local.get $value) (local.get $value_size))\n")
	e.out.WriteString("                (return (i32.const 1))))))\n")
	e.out.WriteString("        (local.set $slot (i32.and (i32.add (local.get $slot) (i32.const 1)) " +
		"(local.get $mask)))\n")
	e.out.WriteString("        (br $probe)))\n")
}

// writeMapInsertWordEntry emits the new entry of an insertion, growing the
// entries first when they are full.
func (e *emitter) writeMapInsertWordEntry() {
	e.out.WriteString("    (if (i64.gt_u (local.get $needed) " +
		"(i64.load (i32.add (local.get $map) (i32.const 16))))\n")
	e.out.WriteString("      (then (if (i32.eqz (call $__map_reserve (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed) (local.get " +
		"$entry_size)))\n")
	e.out.WriteString("        (then (return (i32.const 0))))))\n")
	e.out.WriteString("    (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) (i32.const 8)))))))\n")
	e.out.WriteString("    (i64.store (local.get $entry) (local.get $hash))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) (i32.const 8)) (i64.const " +
		"8))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) (i32.const 16)) (local.get " +
		"$key))\n")
	e.out.WriteString("    (memory.copy (i32.add (local.get $entry) (i32.const 24))\n")
	e.out.WriteString("      (local.get $value) (local.get $value_size))\n")
	e.out.WriteString("    (i32.store8 (i32.add (local.get $index) (local.get $slot)) " +
		"(local.get $tag))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $numbers) (i32.shl (local.get " +
		"$slot) (i32.const 2)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.load (i32.add (local.get $map) (i32.const " +
		"8)))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 8)) (local.get " +
		"$needed))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapRemoveHelper emits removal of one entry: the value moves to out, a
// key copy goes back to the allocator, the entries behind close the gap so
// insertion order holds, and the index is rebuilt.
func (e *emitter) writeMapRemoveHelper() {
	e.out.WriteString("  (func $__map_remove\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $key i32)\n")
	e.out.WriteString("      (param $key_length i64) (param $out i32) (param $value_size " +
		"i32)\n")
	e.out.WriteString("      (result i32)\n")
	e.out.WriteString("    (local $found i64) (local $entry i32) (local $length i64) " +
		"(local $entry_size i32)\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (local.set $found\n")
	e.out.WriteString("      (call $__map_find (local.get $map) " +
		"(local.get $key) (local.get $key_length) (local.get $entry_size)))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $found) (i64.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64 (local.get " +
		"$found)))))\n")
	e.out.WriteString("    (memory.copy (local.get $out) (i32.add (local.get $entry) " +
		"(i32.const 24))\n")
	e.out.WriteString("      (local.get $value_size))\n")
	e.out.WriteString("    (if (i64.gt_u (i64.load (i32.add (local.get $entry) (i32.const 8))) " +
		"(i64.const 8))\n")
	e.out.WriteString("      (then (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("        (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.load (i32.add (local.get $entry) " +
		"(i32.const 8)))))))\n")
	e.out.WriteString("    (local.set $length (i64.sub\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 8))) (i64.const " +
		"1)))\n")
	e.out.WriteString("    (memory.copy (local.get $entry) " +
		"(i32.add (local.get $entry) (local.get $entry_size))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64\n")
	e.out.WriteString("        (i64.sub (local.get $length) (local.get $found)))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 8)) (local.get " +
		"$length))\n")
	e.out.WriteString("    (memory.fill (i32.load (i32.add (local.get $map) (i32.const 24))) " +
		"(i32.const 128)\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.load (i32.add (local.get $map) (i32.const " +
		"32)))))\n")
	e.out.WriteString("    (call $__map_place (local.get $map) (local.get $entry_size))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapDeinitHelper emits release of Map-owned key copies and storage.
func (e *emitter) writeMapDeinitHelper() {
	e.out.WriteString("  (func $__map_deinit\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $value_size i32)\n")
	e.out.WriteString("    (local $number i64) (local $entry i32) (local $entry_size i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $map)) (then (return)))\n")
	e.out.WriteString("    (local.set $entry_size (i32.add (i32.const 24)\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $value_size) (i32.const 7)) " +
		"(i32.const -8))))\n")
	e.out.WriteString("    (block $released\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $released (i64.ge_u (local.get $number)\n")
	e.out.WriteString("          (i64.load (i32.add (local.get $map) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("          (i32.mul (local.get $entry_size) (i32.wrap_i64 (local.get " +
		"$number)))))\n")
	e.out.WriteString("        (if (i64.gt_u (i64.load (i32.add (local.get $entry) (i32.const 8))) " +
		"(i64.const 8))\n")
	e.out.WriteString("          (then (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("            (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("            (i32.wrap_i64 (i64.load " +
		"(i32.add (local.get $entry) (i32.const 8)))))))\n")
	e.out.WriteString("        (local.set $number (i64.add (local.get $number) (i64.const " +
		"1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator) " +
		"(i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.mul (local.get $entry_size) (i32.wrap_i64\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) (i32.const 16))))))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) " +
		"(i32.const 32))) (i64.const 5))))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapInstr dispatches Map operations to their wasm32 lowerings.
func (e *emitter) writeMapInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "map.new":
		return e.writeMapNew(instr)
	case "map.insert":
		return e.writeMapInsert(instr)
	case "map.get":
		return e.writeMapGet(instr)
	case "map.remove":
		return e.writeMapRemove(instr)
	case "map.at", "map.at_mut":
		return e.writeMapAt(instr)
	case "map.take_value_at":
		return e.writeMapTakeValueAt(instr)
	case "map.key_at":
		return e.writeMapKeyAt(instr)
	case "map.contains":
		return e.writeMapContains(instr)
	case "map.len":
		return e.writeMapLen(instr)
	case "map.deinit":
		return e.writeMapDeinit(instr)
	default:
		return fmt.Errorf("wasm error: unsupported map instruction `%s`", instr.Op)
	}
}

// mapElementLayouts validates a Map instruction and measures its key and value.
func (e *emitter) mapElementLayouts(
	instr *ir.Instr,
) (string, wasmLayout, string, wasmLayout, error) {
	container := instr.Result.Type
	if instr.Op != "map.new" && len(instr.Args) > 0 {
		container = instr.Args[0].Type
	}
	key, value, ok := mapElementWasmTypes(container)
	if !ok {
		return "", wasmLayout{}, "", wasmLayout{}, fmt.Errorf(
			"wasm error: `%s` was handed no Map<K, V>", instr.Op)
	}
	keyLayout, err := e.typeLayout(key)
	if err != nil {
		return "", wasmLayout{}, "", wasmLayout{}, err
	}
	valueLayout, err := e.typeLayout(value)
	if err != nil {
		return "", wasmLayout{}, "", wasmLayout{}, err
	}
	if keyLayout.size <= 0 || valueLayout.size <= 0 {
		return "", wasmLayout{}, "", wasmLayout{}, fmt.Errorf(
			"wasm error: Map key and value need nonzero storage")
	}
	return key, keyLayout, value, valueLayout, nil
}

// writeMapNew lowers construction of an empty inline Map header.
func (e *emitter) writeMapNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "Allocator" ||
		!isMapWasmType(instr.Result.Type) {
		return fmt.Errorf("wasm error: map.new expects allocator -> Map<K, V>")
	}
	if _, _, _, _, err := e.mapElementLayouts(instr); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	e.writeMemoryZero(slot, mapHeaderSize)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeMapKeyParts normalizes a byte slice or integer key to the one
// pointer/length ABI consumed by the hash runtime.
func (e *emitter) writeMapKeyParts(
	key ir.Value,
	temp string,
	keyType string,
	keyLayout wasmLayout,
) (string, string, error) {
	if key.Type != keyType {
		return "", "", fmt.Errorf("wasm error: Map expects %s key, got %s", keyType, key.Type)
	}
	if keyType == "[]u8" {
		view := e.value(key).expr
		return fmt.Sprintf("(i32.load %s)", view),
			fmt.Sprintf("(i64.extend_i32_u (i32.load %s))", addressAt(view, 4)), nil
	}
	if err := e.writeStoreValue(temp, 0, keyType, e.value(key)); err != nil {
		return "", "", err
	}
	return temp, fmt.Sprintf("(i64.const %d)", keyLayout.size), nil
}

// writeMapLookup sets local to the address of the value stored for key, or 0.
// A key of eight bytes is looked up where it is asked for.
func (e *emitter) writeMapLookup(
	instr *ir.Instr,
	local string,
	key ir.Value,
	valueLayout wasmLayout,
) error {
	keyType, _, _ := mapElementWasmTypes(instr.Args[0].Type)
	keyLayout, err := e.typeLayout(keyType)
	if err != nil {
		return err
	}
	mapExpr := e.value(instr.Args[0]).expr
	if e.isMapWordLookup(instr) {
		e.writeMapWordLookup(local, mapExpr, e.value(key).expr, valueLayout.size)
		return nil
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := e.writeMapKeyParts(key, temp, keyType, keyLayout)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (local.set %s (call $__map_get %s %s %s (i32.const %d)))\n",
		local, mapExpr, keyPtr, keyLen, valueLayout.size)
	return nil
}

// A lookup of an eight-byte key is written where it is made, since an engine
// that compiles wasm calls a function as a function and a map read in a loop
// would pay for the call on every iteration. It reads the index eight control
// bytes at a time: an empty slot has its high bit set and a tag never does, so
// one load answers for eight slots whether any holds the tag and where the
// first empty one is. A group that would run past the last control byte is
// left to `$__map_get_word`, which steps one slot at a time.

// isMapWordLookup reports whether instr looks a Map value up by a key of eight
// bytes that is not a byte view, which the lookup reads as the word it is.
func (e *emitter) isMapWordLookup(instr *ir.Instr) bool {
	switch instr.Op {
	case "map.get", "map.at", "map.at_mut", "map.contains":
	default:
		return false
	}
	if len(instr.Args) != 2 {
		return false
	}
	keyType, _, ok := mapElementWasmTypes(instr.Args[0].Type)
	if !ok || keyType == "[]u8" || instr.Args[1].Type != keyType {
		return false
	}
	layout, err := e.typeLayout(keyType)
	return err == nil && layout.size == 8
}

// writeMapWordLocals declares the locals the word-key lookups of fn work in,
// when it makes any.
func (e *emitter) writeMapWordLocals(fn *ir.Function) {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if e.isMapWordLookup(instr) {
				e.out.WriteString(mapWordLocals)
				return
			}
		}
	}
}

const mapWordLocals = "" +
	`    (local $__kizu_map i32) (local $__kizu_map_key i64) (local $__kizu_map_hash i64)
    (local $__kizu_map_capacity i32) (local $__kizu_map_index i32) (local $__kizu_map_slot i32)
    (local $__kizu_map_tags i64) (local $__kizu_map_empty i64) (local $__kizu_map_match i64)
    (local $__kizu_map_entry i32) (local $__kizu_map_found i32)
`

// writeMapWordLookup sets local to the address of the value stored for the
// eight-byte key, or 0, hashing it the way `$__map_get_word` does.
func (e *emitter) writeMapWordLookup(local string, mapExpr string, key string, valueSize int) {
	fmt.Fprintf(&e.out, "            (local.set $__kizu_map %s)\n", mapExpr)
	fmt.Fprintf(&e.out, "            (local.set $__kizu_map_key %s)\n", key)
	e.out.WriteString(mapWordHash)
	fmt.Fprintf(&e.out, "                    (local.set $__kizu_map_found (call $__map_get_word\n"+
		"                      (local.get $__kizu_map) (local.get $__kizu_map_key) "+
		"(i32.const %d)))\n", valueSize)
	e.out.WriteString(mapWordGroup)
	fmt.Fprintf(&e.out, "                        (i32.mul (i32.const %d) (i32.load (i32.add\n",
		mapEntrySize(valueSize))
	e.out.WriteString(mapWordCandidate)
	fmt.Fprintf(&e.out, "            (local.set %s (local.get $__kizu_map_found))\n", local)
}

// mapWordHash hashes the key and starts the probe at the group its hash names.
const mapWordHash = "" +
	`            (local.set $__kizu_map_found (i32.const 0))
            (block $__kizu_map_done
              (local.set $__kizu_map_capacity (i32.wrap_i64
                (i64.load (i32.add (local.get $__kizu_map) (i32.const 32)))))
              (br_if $__kizu_map_done (i32.eqz (local.get $__kizu_map_capacity)))
              (local.set $__kizu_map_hash (i64.mul
                (i64.xor (i64.const 256) (local.get $__kizu_map_key))
                (i64.const 0x517cc1b727220a95)))
              (local.set $__kizu_map_hash (i64.xor (local.get $__kizu_map_hash)
                (i64.shr_u (local.get $__kizu_map_hash) (i64.const 32))))
              (local.set $__kizu_map_hash
                (i64.mul (local.get $__kizu_map_hash) (i64.const 0xd6e8feb86659fd93)))
              (local.set $__kizu_map_hash (i64.xor (local.get $__kizu_map_hash)
                (i64.shr_u (local.get $__kizu_map_hash) (i64.const 32))))
              (local.set $__kizu_map_index
                (i32.load (i32.add (local.get $__kizu_map) (i32.const 24))))
              (local.set $__kizu_map_slot
                (i32.and (i32.wrap_i64 (local.get $__kizu_map_hash))
                  (i32.sub (local.get $__kizu_map_capacity) (i32.const 1))))
              (local.set $__kizu_map_tags
                (i64.mul (i64.shr_u (local.get $__kizu_map_hash) (i64.const 57))
                  (i64.const 0x0101010101010101)))
              (loop $__kizu_map_probe
                (if (i32.gt_u (i32.add (local.get $__kizu_map_slot) (i32.const 8))
                      (local.get $__kizu_map_capacity))
                  (then
`

// mapWordGroup loads one group of control bytes and marks the bytes before
// its first empty slot that may hold the tag. A byte equal to the tag is zero
// after the xor; the borrow test marks every zero byte and may also mark a
// byte above one, which the key comparison then turns away.
const mapWordGroup = "" +
	`                    (br $__kizu_map_done)))
                (local.set $__kizu_map_match (i64.load
                  (i32.add (local.get $__kizu_map_index) (local.get $__kizu_map_slot))))
                (local.set $__kizu_map_empty
                  (i64.and (local.get $__kizu_map_match) (i64.const 0x8080808080808080)))
                (local.set $__kizu_map_match
                  (i64.xor (local.get $__kizu_map_match) (local.get $__kizu_map_tags)))
                (local.set $__kizu_map_match (i64.and
                  (i64.and
                    (i64.sub (local.get $__kizu_map_match) (i64.const 0x0101010101010101))
                    (i64.xor (local.get $__kizu_map_match) (i64.const -1)))
                  (i64.and (i64.const 0x8080808080808080)
                    (i64.sub
                      (i64.and (local.get $__kizu_map_empty)
                        (i64.sub (i64.const 0) (local.get $__kizu_map_empty)))
                      (i64.const 1)))))
                (loop $__kizu_map_candidates
                  (if (i64.ne (local.get $__kizu_map_match) (i64.const 0))
                    (then
                      (local.set $__kizu_map_entry (i32.add (i32.load (local.get $__kizu_map))
`

// mapWordCandidate compares the entry one marked byte names, and moves to the
// next group once no marked byte holds the key and no slot in this one is
// empty.
const mapWordCandidate = "" +
	`                        (i32.add (local.get $__kizu_map_index) (local.get $__kizu_map_capacity))
                        (i32.shl
                          (i32.add (local.get $__kizu_map_slot) (i32.wrap_i64 (i64.shr_u
                            (i64.ctz (local.get $__kizu_map_match)) (i64.const 3))))
                          (i32.const 2)))))))
                      (if (i32.and
                            (i64.eq (i64.load (local.get $__kizu_map_entry))
                              (local.get $__kizu_map_hash))
                            (i32.and
                              (i64.eq (i64.load (i32.add (local.get $__kizu_map_entry)
                                (i32.const 8))) (i64.const 8))
                              (i64.eq (i64.load (i32.add (local.get $__kizu_map_entry)
                                (i32.const 16))) (local.get $__kizu_map_key))))
                        (then
                          (local.set $__kizu_map_found
                            (i32.add (local.get $__kizu_map_entry) (i32.const 24)))
                          (br $__kizu_map_done)))
                      (local.set $__kizu_map_match (i64.and (local.get $__kizu_map_match)
                        (i64.sub (local.get $__kizu_map_match) (i64.const 1))))
                      (br $__kizu_map_candidates))))
                (br_if $__kizu_map_done (i64.ne (local.get $__kizu_map_empty) (i64.const 0)))
                (local.set $__kizu_map_slot
                  (i32.and (i32.add (local.get $__kizu_map_slot) (i32.const 8))
                    (i32.sub (local.get $__kizu_map_capacity) (i32.const 1))))
                (br $__kizu_map_probe)))
`

// writeMapInsert lowers a checked Map insertion through the generic runtime.
func (e *emitter) writeMapInsert(instr *ir.Instr) error {
	if len(instr.Args) != 4 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "std::mem::Error!void" {
		return fmt.Errorf(
			"wasm error: map.insert expects Map, Allocator, K, V -> std::mem::Error!void")
	}
	key, keyLayout, value, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil || instr.Args[3].Type != value {
		return fmt.Errorf(
			"wasm error: map.insert expects Map, Allocator, K, V -> std::mem::Error!void")
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	valuePtr := e.value(instr.Args[3]).expr
	if !e.isMemoryType(value) {
		if err := e.writeStoreValue(temp, mapTempValueOffset, value,
			e.value(instr.Args[3])); err != nil {
			return err
		}
		valuePtr = addressAt(temp, mapTempValueOffset)
	}
	var ok string
	if key != "[]u8" && keyLayout.size == 8 {
		ok = fmt.Sprintf("(call $__map_insert_word %s %s %s %s (i32.const %d))",
			e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr,
			e.value(instr.Args[2]).expr, valuePtr, valueLayout.size)
	} else {
		keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[2], temp, key, keyLayout)
		if err != nil {
			return err
		}
		ok = fmt.Sprintf("(call $__map_insert %s %s %s %s %s (i32.const %d))",
			e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr,
			keyPtr, keyLen, valuePtr, valueLayout.size)
	}
	_, err = e.writeArrayErrorResult(
		instr.Result, ok, "std::mem::Error", "OutOfMemory")
	return err
}

// writeMapGet lowers a Map value lookup that copies into an optional.
func (e *emitter) writeMapGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: map.get expects Map, K -> ?V")
	}
	_, _, value, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != value {
		return fmt.Errorf("wasm error: map.get expects Map, K -> ?V")
	}
	symbol := symbolName(instr.Result.Name)
	if e.optionLocals[instr.Result.Name] {
		payload := optionPayloadLocal(instr.Result.Name)
		if err := e.writeMapLookup(instr, symbol, instr.Args[1], valueLayout); err != nil {
			return err
		}
		load, err := e.loadExpr("(local.get "+symbol+")", 0, value)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.out, "            (if (local.get %s)\n", symbol)
		fmt.Fprintf(&e.out, "              (then (local.set %s %s)))\n", payload, load)
		e.writeOptionLocals(instr.Result, "(i32.ne (local.get "+symbol+") (i32.const 0))", "")
		return nil
	}
	if err := e.writeMapLookup(instr, symbol, instr.Args[1], valueLayout); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (local.get %s)\n", symbol)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	if err := e.writeArrayCopyValue(addressAt(slot, payloadOffset),
		"(local.get "+symbol+")", value); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeMapRemove lowers Map.remove(allocator, key): the runtime moves the
// value into the optional's payload before it releases the entry, and the
// tag says whether the key was there.
func (e *emitter) writeMapRemove(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "Allocator" {
		return fmt.Errorf("wasm error: map.remove expects Map, Allocator, K -> ?V")
	}
	key, keyLayout, value, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != value {
		return fmt.Errorf("wasm error: map.remove expects Map, Allocator, K -> ?V")
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[2], temp, key, keyLayout)
	if err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out,
		"            (local.set %s (call $__map_remove %s %s %s %s %s (i32.const %d)))\n",
		symbol, e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr,
		keyPtr, keyLen, addressAt(slot, payloadOffset), valueLayout.size)
	fmt.Fprintf(&e.out, "            (if (local.get %s)\n", symbol)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeMapAt lowers immutable and mutable borrowed Map value lookups.
func (e *emitter) writeMapAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: %s expects Map, K -> ?&V", instr.Op)
	}
	_, _, value, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	borrow, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || derefWasmType(borrow) != value || !isReferenceType(borrow) {
		return fmt.Errorf("wasm error: %s expects Map, K -> ?&V", instr.Op)
	}
	symbol := symbolName(instr.Result.Name)
	if e.optionLocals[instr.Result.Name] {
		payload := optionPayloadLocal(instr.Result.Name)
		if err := e.writeMapLookup(instr, payload, instr.Args[1], valueLayout); err != nil {
			return err
		}
		e.writeOptionLocals(instr.Result, "(i32.ne (local.get "+payload+") (i32.const 0))", "")
		return nil
	}
	if err := e.writeMapLookup(instr, symbol, instr.Args[1], valueLayout); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (if (local.get %s)\n", symbol)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i32.store %s (local.get %s))\n",
		addressAt(slot, payloadOffset), symbol)
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// mapEntryAddress returns the WAT address of an insertion-ordered Map entry
// of a map whose values are valueSize bytes.
func mapEntryAddress(mapExpr string, index string, valueSize int) string {
	return fmt.Sprintf("(i32.add (i32.load %s) "+
		"(i32.wrap_i64 (i64.mul %s (i64.const %d))))", mapExpr, index, mapEntrySize(valueSize))
}

// writeMapTakeValueAt lowers the checked owner-value cleanup access.
func (e *emitter) writeMapTakeValueAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: map.take_value_at expects Map, i64 -> V")
	}
	_, _, value, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil || instr.Result.Type != value {
		return fmt.Errorf("wasm error: map.take_value_at expects Map, i64 -> V")
	}
	mapExpr := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", addressAt(mapExpr, mapLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s %s)\n", index, length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_bounds %s %s "+
		"(i64.const %d) (i64.const %d)) (unreachable)))\n",
		index, length, instr.Span.Start.Line, instr.Span.Start.Column)
	entry := mapEntryAddress(mapExpr, index, valueLayout.size)
	return e.writeLoadValue(instr.Result, addressAt(entry, mapEntryValueOffset), 0)
}

// writeMapKeyAt lowers insertion-ordered key access into an optional.
func (e *emitter) writeMapKeyAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: map.key_at expects Map, i64 -> ?K")
	}
	key, _, _, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != key {
		return fmt.Errorf("wasm error: map.key_at expects Map, i64 -> ?K")
	}
	mapExpr := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", addressAt(mapExpr, mapLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, length)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	entry := mapEntryAddress(mapExpr, index, valueLayout.size)
	destination := addressAt(slot, payloadOffset)
	if key == "[]u8" {
		// A key of at most eight bytes lies in the entry; a longer one is at
		// the address the entry holds.
		length := fmt.Sprintf("(i64.load %s)", addressAt(entry, mapEntryKeyLenOffset))
		fmt.Fprintf(&e.out, "                (i32.store %s (select %s (i32.load %s) "+
			"(i64.le_u %s (i64.const 8))))\n",
			destination, addressAt(entry, mapEntryKeyOffset),
			addressAt(entry, mapEntryKeyOffset), length)
		fmt.Fprintf(&e.out, "                (i32.store %s (i32.wrap_i64 %s))\n",
			addressAt(destination, 4), length)
	} else if err := e.writeArrayCopyValue(destination,
		addressAt(entry, mapEntryKeyOffset), key); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeMapContains lowers Map key membership testing.
func (e *emitter) writeMapContains(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Result.Type != "bool" {
		return fmt.Errorf("wasm error: map.contains expects Map, K -> bool")
	}
	_, _, _, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	symbol := symbolName(instr.Result.Name)
	if err := e.writeMapLookup(instr, symbol, instr.Args[1], valueLayout); err != nil {
		return err
	}
	return e.writeScalarResult(instr.Result, "(i32.ne (local.get "+symbol+") (i32.const 0))")
}

// writeMapLen lowers access to the inline Map length.
func (e *emitter) writeMapLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: map.len expects Map -> i64")
	}
	if _, _, ok := mapElementWasmTypes(instr.Args[0].Type); !ok {
		return fmt.Errorf("wasm error: map.len expects Map -> i64")
	}
	return e.writeScalarResult(instr.Result, fmt.Sprintf("(i64.load %s)",
		addressAt(e.value(instr.Args[0]).expr, mapLenOffset)))
}

// writeMapDeinit lowers release of a Map through its allocator.
func (e *emitter) writeMapDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: map.deinit expects Map, Allocator -> void")
	}
	_, _, _, valueLayout, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (call $__map_deinit %s %s (i32.const %d))\n",
		e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr, valueLayout.size)
	return nil
}
