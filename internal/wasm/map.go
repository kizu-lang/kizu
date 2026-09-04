package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// A Map value is its inline header. wasm32 pointers occupy four bytes, but
// the i64 fields retain their natural alignment, so the five fields keep the
// same offsets and 40-byte size as the native header.
const (
	mapWasmPrefix          = "std::map::Map<"
	mapHeaderSize          = 40
	mapEntriesOffset       = 0
	mapLenOffset           = 8
	mapCapacityOffset      = 16
	mapIndexOffset         = 24
	mapIndexCapacityOffset = 32
	mapEntrySize           = 32
	mapEntryKeyOffset      = 0
	mapEntryKeyLenOffset   = 8
	mapEntryValueOffset    = 16
	mapEntryHashOffset     = 24
	mapTempSize            = 16
	mapTempValueOffset     = 8
)

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
	e.writeMapKeysEqualHelper()
	e.writeMapSlotHelper()
	e.writeMapFindHelper()
	e.writeMapReindexHelper()
	e.writeMapReserveHelper()
	e.writeMapInsertHelper()
	e.writeMapGetHelper()
	e.writeMapRehashHelper()
	e.writeMapRemoveHelper()
	e.writeMapDeinitHelper()
}

// writeMapReadTailHelper emits the short-tail reader used by Map hashing.
func (e *emitter) writeMapReadTailHelper() {
	e.out.WriteString("  (func $__map_read_tail (param $key i32) (param $length i64) (result i64)\n")
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
	e.out.WriteString("            (i32.wrap_i64 (i64.shr_u (local.get $length) (i64.const 1))))))\n")
	e.out.WriteString("          (i64.const 8))\n")
	e.out.WriteString("        (i64.extend_i32_u (i32.load8_u (i32.add (local.get $key)\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.sub (local.get $length) (i64.const 1))))))))\n")
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
	e.out.WriteString("          (i64.add (local.get $offset) (i64.const 8)) (local.get $length)))\n")
	e.out.WriteString("        (local.set $hash\n")
	e.out.WriteString("          (i64.mul\n")
	e.out.WriteString("            (i64.xor (i64.rotl (local.get $hash) (i64.const 5))\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $key) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("            (i64.const 0x517cc1b727220a95)))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const 8)))\n")
	e.out.WriteString("        (br $words)))\n")
	e.out.WriteString("    (if (i64.lt_u (local.get $offset) (local.get $length))\n")
	e.out.WriteString("      (then (local.set $hash\n")
	e.out.WriteString("        (i64.mul\n")
	e.out.WriteString("          (i64.xor (i64.rotl (local.get $hash) (i64.const 5))\n")
	e.out.WriteString("            (call $__map_read_tail\n")
	e.out.WriteString("              (i32.add (local.get $key) (i32.wrap_i64 (local.get $offset)))\n")
	e.out.WriteString("              (i64.sub (local.get $length) (local.get $offset))))\n")
	e.out.WriteString("          (i64.const 0x517cc1b727220a95)))))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.xor (local.get $hash) " +
		"(i64.shr_u (local.get $hash) (i64.const 32))))\n")
	e.out.WriteString("    (local.set $hash\n")
	e.out.WriteString("      (i64.mul (local.get $hash) (i64.const 0xd6e8feb86659fd93)))\n")
	e.out.WriteString("    (i64.xor (local.get $hash) (i64.shr_u (local.get $hash) (i64.const 32)))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapKeysEqualHelper emits byte equality for Map keys with equal lengths.
func (e *emitter) writeMapKeysEqualHelper() {
	e.out.WriteString("  (func $__map_keys_equal\n")
	e.out.WriteString("      (param $left i32) (param $right i32) (param $length i64) (result i32)\n")
	e.out.WriteString("    (local $offset i64)\n")
	e.out.WriteString("    (block $bytes\n")
	e.out.WriteString("      (loop $words\n")
	e.out.WriteString("        (br_if $bytes (i64.gt_u\n")
	e.out.WriteString("          (i64.add (local.get $offset) (i64.const 8)) (local.get $length)))\n")
	e.out.WriteString("        (if (i64.ne\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $left) " +
		"(i32.wrap_i64 (local.get $offset))))\n")
	e.out.WriteString("              (i64.load (i32.add (local.get $right) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const 8)))\n")
	e.out.WriteString("        (br $words)))\n")
	e.out.WriteString("    (block $equal\n")
	e.out.WriteString("      (loop $tail\n")
	e.out.WriteString("        (br_if $equal (i64.ge_u (local.get $offset) (local.get $length)))\n")
	e.out.WriteString("        (if (i32.ne\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $left) " +
		"(i32.wrap_i64 (local.get $offset))))\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $right) " +
		"(i32.wrap_i64 (local.get $offset)))))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $offset (i64.add (local.get $offset) (i64.const 1)))\n")
	e.out.WriteString("        (br $tail)))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapSlotHelper emits linear probing for a Map key or empty slot.
func (e *emitter) writeMapSlotHelper() {
	e.out.WriteString("  (func $__map_slot\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64)\n")
	e.out.WriteString("      (param $hash i64) (result i64)\n")
	e.out.WriteString("    (local $mask i64) (local $slot i64) (local $entry_index i64)\n")
	e.out.WriteString("    (local $entry i32)\n")
	e.out.WriteString("    (local.set $mask\n")
	e.out.WriteString("      (i64.sub (i64.load " +
		"(i32.add (local.get $map) (i32.const 32))) (i64.const 1)))\n")
	e.out.WriteString("    (local.set $slot (i64.and (local.get $hash) (local.get $mask)))\n")
	e.out.WriteString("    (loop $probe\n")
	e.out.WriteString("      (local.set $entry_index\n")
	e.out.WriteString("        (i64.load (i32.add\n")
	e.out.WriteString("          (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8))))))\n")
	e.out.WriteString("      (if (i64.lt_s (local.get $entry_index) (i64.const 0))\n")
	e.out.WriteString("        (then (return (local.get $slot))))\n")
	e.out.WriteString("      (local.set $entry\n")
	e.out.WriteString("        (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.mul (local.get $entry_index) (i64.const 32)))))\n")
	e.out.WriteString("      (if (i32.and\n")
	e.out.WriteString("            (i64.eq (i64.load (i32.add (local.get $entry) (i32.const 24)))\n")
	e.out.WriteString("              (local.get $hash))\n")
	e.out.WriteString("            (i32.and\n")
	e.out.WriteString("              (i64.eq (i64.load (i32.add (local.get $entry) (i32.const 8)))\n")
	e.out.WriteString("                (local.get $key_length))\n")
	e.out.WriteString("              (call $__map_keys_equal (i32.load (local.get $entry))\n")
	e.out.WriteString("                (local.get $key) (local.get $key_length))))\n")
	e.out.WriteString("        (then (return (local.get $slot))))\n")
	e.out.WriteString("      (local.set $slot\n")
	e.out.WriteString("        (i64.and (i64.add (local.get $slot) " +
		"(i64.const 1)) (local.get $mask)))\n")
	e.out.WriteString("      (br $probe))\n")
	e.out.WriteString("    (unreachable)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapFindHelper emits a Map lookup that returns the insertion index.
func (e *emitter) writeMapFindHelper() {
	e.out.WriteString("  (func $__map_find\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64) (result i64)\n")
	e.out.WriteString("    (local $slot i64)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i32.or (i64.lt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("            (i32.or\n")
	e.out.WriteString("              (i32.and (i32.eqz (local.get $key))\n")
	e.out.WriteString("                (i64.gt_s (local.get $key_length) (i64.const 0)))\n")
	e.out.WriteString("              (i64.eqz (i64.load " +
		"(i32.add (local.get $map) (i32.const 32)))))))\n")
	e.out.WriteString("      (then (return (i64.const -1))))\n")
	e.out.WriteString("    (local.set $slot (call $__map_slot (local.get $map) (local.get $key)\n")
	e.out.WriteString("      (local.get $key_length) (call $__map_hash " +
		"(local.get $key) (local.get $key_length))))\n")
	e.out.WriteString("    (i64.load (i32.add\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8)))))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapReindexHelper emits growth and rebuilding of a Map's hash index.
func (e *emitter) writeMapReindexHelper() {
	e.out.WriteString("  (func $__map_reindex\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) " +
		"(param $needed i64) (result i32)\n")
	e.out.WriteString("    (local $next i64) (local $old_index i32) (local $old_capacity i64)\n")
	e.out.WriteString("    (local $new_index i32) (local $bytes i32) (local $mask i64)\n")
	e.out.WriteString("    (local $entry_index i64) (local $entry i32) (local $slot i64)\n")
	e.writeMapReindexAllocation()
	e.writeMapReindexEntries()
	e.writeMapReindexFinish()
}

// writeMapReindexAllocation emits index capacity selection and allocation.
func (e *emitter) writeMapReindexAllocation() {
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i64.lt_s (local.get $needed) (i64.const 0)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $old_index " +
		"(i32.load (i32.add (local.get $map) (i32.const 24))))\n")
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
	e.out.WriteString("    (local.set $bytes " +
		"(i32.wrap_i64 (i64.mul (local.get $next) (i64.const 8))))\n")
	e.out.WriteString("    (local.set $new_index\n")
	e.out.WriteString("      (call $__allocator_alloc (local.get $allocator) (local.get $bytes)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $new_index)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (memory.fill (local.get $new_index) (i32.const 255) (local.get $bytes))\n")
	e.out.WriteString("    (local.set $mask (i64.sub (local.get $next) (i64.const 1)))\n")
}

// writeMapReindexEntries emits reinsertion of every ordered entry.
func (e *emitter) writeMapReindexEntries() {
	e.out.WriteString("    (block $rebuilt\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $rebuilt (i64.ge_u (local.get $entry_index)\n")
	e.out.WriteString("          (i64.load (i32.add (local.get $map) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $entry\n")
	e.out.WriteString("          (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("            (i32.wrap_i64 " +
		"(i64.mul (local.get $entry_index) (i64.const 32)))))\n")
	e.out.WriteString("        (local.set $slot\n")
	e.out.WriteString("          (i64.and (i64.load (i32.add (local.get $entry) (i32.const 24)))\n")
	e.out.WriteString("            (local.get $mask)))\n")
	e.out.WriteString("        (block $placed\n")
	e.out.WriteString("          (loop $probe\n")
	e.out.WriteString("            (if (i64.lt_s (i64.load (i32.add (local.get $new_index)\n")
	e.out.WriteString("                  (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8)))))\n")
	e.out.WriteString("                (i64.const 0))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (i64.store (i32.add (local.get $new_index)\n")
	e.out.WriteString("                  (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8))))\n")
	e.out.WriteString("                  (local.get $entry_index))\n")
	e.out.WriteString("                (br $placed)))\n")
	e.out.WriteString("            (local.set $slot (i64.and\n")
	e.out.WriteString("              (i64.add (local.get $slot) (i64.const 1)) (local.get $mask)))\n")
	e.out.WriteString("            (br $probe)))\n")
	e.out.WriteString("        (local.set $entry_index\n")
	e.out.WriteString("          (i64.add (local.get $entry_index) (i64.const 1)))\n")
	e.out.WriteString("        (br $entries)))\n")
}

// writeMapReindexFinish emits old-index release and header replacement.
func (e *emitter) writeMapReindexFinish() {
	e.out.WriteString("    (call $__allocator_free (local.get $allocator) (local.get $old_index)\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $old_capacity) (i64.const 8))))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $map) " +
		"(i32.const 24)) (local.get $new_index))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 32)) (local.get $next))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapReserveHelper emits growth of insertion-ordered Map entries.
func (e *emitter) writeMapReserveHelper() {
	e.out.WriteString("  (func $__map_reserve\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) " +
		"(param $needed i64) (result i32)\n")
	e.out.WriteString("    (local $capacity i64) (local $next i64) (local $old_bytes i32)\n")
	e.out.WriteString("    (local $new_bytes i32) (local $entries i32)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $map))\n")
	e.out.WriteString("          (i64.lt_s (local.get $needed) (i64.const 0)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $capacity\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 16))))\n")
	e.out.WriteString("    (if (i64.le_u (local.get $needed) (local.get $capacity))\n")
	e.out.WriteString("      (then (return (i32.const 1))))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $capacity))\n")
	e.out.WriteString("      (then (local.set $next (i64.const 4)))\n")
	e.out.WriteString("      (else\n")
	e.out.WriteString("        (if (i64.gt_u (local.get $capacity) (i64.const 33554431))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $next (i64.mul (local.get $capacity) (i64.const 2)))))\n")
	e.out.WriteString("    (block $grown\n")
	e.out.WriteString("      (loop $grow\n")
	e.out.WriteString("        (br_if $grown (i64.ge_u (local.get $next) (local.get $needed)))\n")
	e.out.WriteString("        (if (i64.gt_u (local.get $next) (i64.const 33554431))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $next (i64.mul (local.get $next) (i64.const 2)))\n")
	e.out.WriteString("        (br $grow)))\n")
	e.out.WriteString("    (local.set $old_bytes\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $capacity) (i64.const 32))))\n")
	e.out.WriteString("    (local.set $new_bytes\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $next) (i64.const 32))))\n")
	e.out.WriteString("    (local.set $entries\n")
	e.out.WriteString("      (call $__allocator_realloc (local.get $allocator) " +
		"(i32.load (local.get $map))\n")
	e.out.WriteString("        (local.get $old_bytes) (local.get $new_bytes)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $entries)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (memory.fill (i32.add (local.get $entries) (local.get $old_bytes))\n")
	e.out.WriteString("      (i32.const 0) (i32.sub (local.get $new_bytes) (local.get $old_bytes)))\n")
	e.out.WriteString("    (i32.store (local.get $map) (local.get $entries))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 16)) (local.get $next))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapInsertHelper emits insertion and replacement for generic Map values.
func (e *emitter) writeMapInsertHelper() {
	e.out.WriteString("  (func $__map_insert\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $key i32)\n")
	e.out.WriteString("      (param $key_length i64) (param $value i32) (param $value_size i32)\n")
	e.out.WriteString("      (result i32)\n")
	e.out.WriteString("    (local $needed i64) (local $hash i64) (local $slot i64)\n")
	e.out.WriteString("    (local $found i64) (local $key_copy i32) (local $value_copy i32)\n")
	e.out.WriteString("    (local $entry i32)\n")
	e.writeMapInsertLookup()
	e.writeMapInsertAllocation()
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
	e.out.WriteString("    (local.set $needed\n")
	e.out.WriteString("      (i64.add (i64.load " +
		"(i32.add (local.get $map) (i32.const 8))) (i64.const 1)))\n")
	e.out.WriteString("    (if (i64.eqz (local.get $needed)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i32.eqz (call $__map_reindex (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $hash " +
		"(call $__map_hash (local.get $key) (local.get $key_length)))\n")
	e.out.WriteString("    (local.set $slot (call $__map_slot (local.get $map) (local.get $key)\n")
	e.out.WriteString("      (local.get $key_length) (local.get $hash)))\n")
	e.out.WriteString("    (local.set $found\n")
	e.out.WriteString("      (i64.load (i32.add\n")
	e.out.WriteString("        (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8))))))\n")
	e.out.WriteString("    (if (i64.ge_s (local.get $found) (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.mul (local.get $found) (i64.const 32)))))\n")
	e.out.WriteString("        (memory.copy (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("          (local.get $value) (local.get $value_size))\n")
	e.out.WriteString("        (return (i32.const 1))))\n")
}

// writeMapInsertAllocation emits entry storage growth and owned copies.
func (e *emitter) writeMapInsertAllocation() {
	e.out.WriteString("    (if (i32.eqz (call $__map_reserve (local.get $allocator)\n")
	e.out.WriteString("          (local.get $map) (local.get $needed)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i64.gt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("      (then (local.set $key_copy " +
		"(call $__allocator_alloc (local.get $allocator)\n")
	e.out.WriteString("        (i32.wrap_i64 (local.get $key_length))))))\n")
	e.out.WriteString("    (local.set $value_copy\n")
	e.out.WriteString("      (call $__allocator_alloc (local.get $allocator) " +
		"(local.get $value_size)))\n")
	e.out.WriteString("    (if (i32.or\n")
	e.out.WriteString("          (i32.and (i64.gt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("            (i32.eqz (local.get $key_copy)))\n")
	e.out.WriteString("          (i32.eqz (local.get $value_copy)))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (call $__allocator_free (local.get $allocator) (local.get $key_copy)\n")
	e.out.WriteString("          (i32.wrap_i64 (local.get $key_length)))\n")
	e.out.WriteString("        (call $__allocator_free (local.get $allocator) " +
		"(local.get $value_copy)\n")
	e.out.WriteString("          (local.get $value_size))\n")
	e.out.WriteString("        (return (i32.const 0))))\n")
}

// writeMapInsertEntry emits copies and commits a new insertion-ordered entry.
func (e *emitter) writeMapInsertEntry() {
	e.out.WriteString("    (if (i64.gt_s (local.get $key_length) (i64.const 0))\n")
	e.out.WriteString("      (then (memory.copy (local.get $key_copy) (local.get $key)\n")
	e.out.WriteString("        (i32.wrap_i64 (local.get $key_length)))))\n")
	e.out.WriteString("    (memory.copy (local.get $value_copy) " +
		"(local.get $value) (local.get $value_size))\n")
	e.out.WriteString("    (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) " +
		"(i32.const 8))) (i64.const 32)))))\n")
	e.out.WriteString("    (i32.store (local.get $entry) (local.get $key_copy))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) " +
		"(i32.const 8)) (local.get $key_length))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $entry) " +
		"(i32.const 16)) (local.get $value_copy))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $entry) " +
		"(i32.const 24)) (local.get $hash))\n")
	e.out.WriteString("    (i64.store (i32.add\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8))))\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 8))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 8)) (local.get $needed))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapGetHelper emits a lookup that returns the stored value address.
func (e *emitter) writeMapGetHelper() {
	e.out.WriteString("  (func $__map_get\n")
	e.out.WriteString("      (param $map i32) (param $key i32) (param $key_length i64) (result i32)\n")
	e.out.WriteString("    (local $found i64)\n")
	e.out.WriteString("    (local.set $found\n")
	e.out.WriteString("      (call $__map_find (local.get $map) " +
		"(local.get $key) (local.get $key_length)))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $found) (i64.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (i32.load (i32.add\n")
	e.out.WriteString("      (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.mul (local.get $found) (i64.const 32))))\n")
	e.out.WriteString("      (i32.const 16)))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapRehashHelper emits an in-place rebuild of a Map's index from the
// hash every entry carries, for a table whose entries changed number.
func (e *emitter) writeMapRehashHelper() {
	e.out.WriteString("  (func $__map_rehash (param $map i32)\n")
	e.out.WriteString("    (local $index i32) (local $mask i64) (local $entry_index i64)\n")
	e.out.WriteString("    (local $entry i32) (local $slot i64)\n")
	e.out.WriteString("    (local.set $index " +
		"(i32.load (i32.add (local.get $map) (i32.const 24))))\n")
	e.out.WriteString("    (local.set $mask (i64.sub\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 32))) (i64.const 1)))\n")
	e.out.WriteString("    (memory.fill (local.get $index) (i32.const 255)\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) (i32.const 32))) " +
		"(i64.const 8))))\n")
	e.out.WriteString("    (block $rebuilt\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $rebuilt (i64.ge_u (local.get $entry_index)\n")
	e.out.WriteString("          (i64.load (i32.add (local.get $map) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $entry\n")
	e.out.WriteString("          (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("            (i32.wrap_i64 " +
		"(i64.mul (local.get $entry_index) (i64.const 32)))))\n")
	e.out.WriteString("        (local.set $slot\n")
	e.out.WriteString("          (i64.and (i64.load (i32.add (local.get $entry) (i32.const 24)))\n")
	e.out.WriteString("            (local.get $mask)))\n")
	e.out.WriteString("        (block $placed\n")
	e.out.WriteString("          (loop $probe\n")
	e.out.WriteString("            (if (i64.lt_s (i64.load (i32.add (local.get $index)\n")
	e.out.WriteString("                  (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8)))))\n")
	e.out.WriteString("                (i64.const 0))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (i64.store (i32.add (local.get $index)\n")
	e.out.WriteString("                  (i32.wrap_i64 (i64.mul (local.get $slot) (i64.const 8))))\n")
	e.out.WriteString("                  (local.get $entry_index))\n")
	e.out.WriteString("                (br $placed)))\n")
	e.out.WriteString("            (local.set $slot (i64.and\n")
	e.out.WriteString("              (i64.add (local.get $slot) (i64.const 1)) (local.get $mask)))\n")
	e.out.WriteString("            (br $probe)))\n")
	e.out.WriteString("        (local.set $entry_index\n")
	e.out.WriteString("          (i64.add (local.get $entry_index) (i64.const 1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	e.out.WriteString("  )\n\n")
}

// writeMapRemoveHelper emits removal of one entry: the value moves to out,
// the key copy and value storage go back to the allocator, the entries behind
// close the gap so insertion order holds, and the index is rebuilt.
func (e *emitter) writeMapRemoveHelper() {
	e.out.WriteString("  (func $__map_remove\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $key i32)\n")
	e.out.WriteString("      (param $key_length i64) (param $out i32) (param $value_size i32)\n")
	e.out.WriteString("      (result i32)\n")
	e.out.WriteString("    (local $found i64) (local $entry i32) (local $length i64)\n")
	e.out.WriteString("    (local.set $found\n")
	e.out.WriteString("      (call $__map_find (local.get $map) " +
		"(local.get $key) (local.get $key_length)))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $found) (i64.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul (local.get $found) (i64.const 32)))))\n")
	e.out.WriteString("    (memory.copy (local.get $out)\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("      (local.get $value_size))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator) " +
		"(i32.load (local.get $entry))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.load (i32.add (local.get $entry) (i32.const 8)))))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("      (local.get $value_size))\n")
	e.out.WriteString("    (local.set $length (i64.sub\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $map) (i32.const 8))) (i64.const 1)))\n")
	e.out.WriteString("    (memory.copy (local.get $entry) " +
		"(i32.add (local.get $entry) (i32.const 32))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.sub (local.get $length) (local.get $found)) (i64.const 32))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $map) (i32.const 8)) (local.get $length))\n")
	e.out.WriteString("    (call $__map_rehash (local.get $map))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeMapDeinitHelper emits release of Map-owned keys, values, and storage.
func (e *emitter) writeMapDeinitHelper() {
	e.out.WriteString("  (func $__map_deinit\n")
	e.out.WriteString("      (param $allocator i32) (param $map i32) (param $value_size i32)\n")
	e.out.WriteString("    (local $index i64) (local $entry i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $map)) (then (return)))\n")
	e.out.WriteString("    (block $released\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $released (i64.ge_u (local.get $index)\n")
	e.out.WriteString("          (i64.load (i32.add (local.get $map) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $entry (i32.add (i32.load (local.get $map))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.mul (local.get $index) (i64.const 32)))))\n")
	e.out.WriteString("        (call $__allocator_free (local.get $allocator) " +
		"(i32.load (local.get $entry))\n")
	e.out.WriteString("          (i32.wrap_i64 (i64.load " +
		"(i32.add (local.get $entry) (i32.const 8)))))\n")
	e.out.WriteString("        (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("          (i32.load (i32.add (local.get $entry) (i32.const 16)))\n")
	e.out.WriteString("          (local.get $value_size))\n")
	e.out.WriteString("        (local.set $index (i64.add (local.get $index) (i64.const 1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator) " +
		"(i32.load (local.get $map))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) " +
		"(i32.const 16))) (i64.const 32))))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $map) (i32.const 24)))\n")
	e.out.WriteString("      (i32.wrap_i64 (i64.mul\n")
	e.out.WriteString("        (i64.load (i32.add (local.get $map) " +
		"(i32.const 32))) (i64.const 8))))\n")
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
	fmt.Fprintf(&e.out, "            (memory.fill %s (i32.const 0) (i32.const %d))\n",
		slot, mapHeaderSize)
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
	keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[2], temp, key, keyLayout)
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
	ok := fmt.Sprintf("(call $__map_insert %s %s %s %s %s (i32.const %d))",
		e.value(instr.Args[1]).expr, e.value(instr.Args[0]).expr,
		keyPtr, keyLen, valuePtr, valueLayout.size)
	_, err = e.writeArrayErrorResult(
		instr.Result, ok, "std::mem::Error", "OutOfMemory")
	return err
}

// writeMapGet lowers a Map value lookup that copies into an optional.
func (e *emitter) writeMapGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: map.get expects Map, K -> ?V")
	}
	key, keyLayout, value, _, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != value {
		return fmt.Errorf("wasm error: map.get expects Map, K -> ?V")
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[1], temp, key, keyLayout)
	if err != nil {
		return err
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (call $__map_get %s %s %s))\n",
		symbol, e.value(instr.Args[0]).expr, keyPtr, keyLen)
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
	key, keyLayout, value, _, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	borrow, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || derefWasmType(borrow) != value || !isReferenceType(borrow) {
		return fmt.Errorf("wasm error: %s expects Map, K -> ?&V", instr.Op)
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[1], temp, key, keyLayout)
	if err != nil {
		return err
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (call $__map_get %s %s %s))\n",
		symbol, e.value(instr.Args[0]).expr, keyPtr, keyLen)
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

// mapEntryAddress returns the WAT address of an insertion-ordered Map entry.
func mapEntryAddress(mapExpr string, index string) string {
	return fmt.Sprintf("(i32.add (i32.load %s) "+
		"(i32.wrap_i64 (i64.mul %s (i64.const %d))))", mapExpr, index, mapEntrySize)
}

// writeMapTakeValueAt lowers the checked owner-value cleanup access.
func (e *emitter) writeMapTakeValueAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: map.take_value_at expects Map, i64 -> V")
	}
	_, _, value, _, err := e.mapElementLayouts(instr)
	if err != nil || instr.Result.Type != value {
		return fmt.Errorf("wasm error: map.take_value_at expects Map, i64 -> V")
	}
	mapExpr := e.value(instr.Args[0]).expr
	index := e.value(instr.Args[1]).expr
	length := fmt.Sprintf("(i64.load %s)", addressAt(mapExpr, mapLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s %s)\n", index, length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_bounds %s %s "+
		"(i64.const %d) (i64.const %d))))\n",
		index, length, instr.Span.Start.Line, instr.Span.Start.Column)
	entry := mapEntryAddress(mapExpr, index)
	return e.writeLoadValue(instr.Result,
		fmt.Sprintf("(i32.load %s)", addressAt(entry, mapEntryValueOffset)), 0)
}

// writeMapKeyAt lowers insertion-ordered key access into an optional.
func (e *emitter) writeMapKeyAt(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "i64" {
		return fmt.Errorf("wasm error: map.key_at expects Map, i64 -> ?K")
	}
	key, _, _, _, err := e.mapElementLayouts(instr)
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
	entry := mapEntryAddress(mapExpr, index)
	destination := addressAt(slot, payloadOffset)
	if key == "[]u8" {
		fmt.Fprintf(&e.out, "                (i32.store %s (i32.load %s))\n",
			destination, addressAt(entry, mapEntryKeyOffset))
		fmt.Fprintf(&e.out, "                (i32.store %s "+
			"(i32.wrap_i64 (i64.load %s)))\n",
			addressAt(destination, 4), addressAt(entry, mapEntryKeyLenOffset))
	} else if err := e.writeArrayCopyValue(destination,
		fmt.Sprintf("(i32.load %s)", addressAt(entry, mapEntryKeyOffset)), key); err != nil {
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
	key, keyLayout, _, _, err := e.mapElementLayouts(instr)
	if err != nil {
		return err
	}
	temp, err := e.mapTempSlot(instr.Result)
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := e.writeMapKeyParts(instr.Args[1], temp, key, keyLayout)
	if err != nil {
		return err
	}
	return e.writeScalarResult(instr.Result, fmt.Sprintf(
		"(i64.ge_s (call $__map_find %s %s %s) (i64.const 0))",
		e.value(instr.Args[0]).expr, keyPtr, keyLen))
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
