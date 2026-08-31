package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// userAllocatorAllocSignature returns allocator_from's allocation callback ABI.
func userAllocatorAllocSignature() funcSignature {
	return funcSignature{params: []string{"i32", "i64"}, result: "i32"}
}

// userAllocatorReleaseSignature returns allocator_from's release callback ABI.
func userAllocatorReleaseSignature() funcSignature {
	return funcSignature{params: []string{"i32", "i32", "i64"}}
}

// usesUserAllocatorRuntime reports whether allocator dispatch needs the two
// indirect callback shapes reserved by allocator_from.
func (e *emitter) usesUserAllocatorRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "call.std::internal::builtin::mem_allocator_from" {
					return true
				}
			}
		}
	}
	return false
}

// usesAllocatorRuntime reports whether function frames must share linear
// memory with heap allocations. A shared free-list keeps recursive frames and
// owner storage from invalidating one another.
func (e *emitter) usesAllocatorRuntime() bool {
	if e.usesArrayRuntime() || e.usesMapRuntime() || e.usesBoxRuntime() || e.usesArenaRuntime() ||
		e.usesUserAllocatorRuntime() || e.usesProcessExecutablePath() {
		return true
	}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "call.std::internal::builtin::mem_fixed_buffer" {
					return true
				}
			}
		}
	}
	return false
}

// writeAllocatorFromCall installs a user allocator header in the state the
// checker required to begin with AllocatorHeader. Function values are wasm
// table indexes, not linear-memory addresses.
func (e *emitter) writeAllocatorFromCall(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Result.Type != "Allocator" ||
		!strings.HasPrefix(instr.Args[0].Type, "&var ") ||
		!strings.HasPrefix(instr.Args[1].Type, "unsafe fn(") ||
		!strings.HasPrefix(instr.Args[2].Type, "unsafe fn(") {
		return fmt.Errorf("wasm error: mem_allocator_from expects state, alloc, free -> Allocator")
	}
	state := e.value(instr.Args[0]).expr
	alloc := e.value(instr.Args[1]).expr
	release := e.value(instr.Args[2]).expr
	e.out.WriteString("            (i64.store " + state + " (i64.const 2))\n")
	e.out.WriteString("            (i64.store " + addressAt(state, 8) +
		" (i64.extend_i32_u " + state + "))\n")
	e.out.WriteString("            (i64.store " + addressAt(state, 16) +
		" (i64.extend_i32_u " + alloc + "))\n")
	e.out.WriteString("            (i64.store " + addressAt(state, 24) +
		" (i64.extend_i32_u " + release + "))\n")
	symbol := symbolName(instr.Result.Name)
	e.out.WriteString("            (local.set " + symbol + " " + state + ")\n")
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeAllocatorRuntime emits a reusable page allocator, the Allocator
// dispatch boundary, and Array's generic reserve/swap helpers.
func (e *emitter) writeAllocatorRuntime() {
	e.writePageAllocHelper()
	e.writePageFreeHelper()
	e.writePageReallocHelper()
	e.writeFixedBufferHelper()
	e.writeFixedAllocHelper()
	e.writeAllocatorDispatchHelpers()
	e.writeArrayReserveHelper()
	e.writeArraySwapHelper()
}

// writeFixedBufferHelper installs the allocator header at the aligned front
// of the caller's byte storage; the remaining bytes become bump storage.
func (e *emitter) writeFixedBufferHelper() {
	e.out.WriteString("  (func $__fixed_buffer (param $view i32) (result i32)\n")
	e.out.WriteString("    (local $ptr i32) (local $length i32) (local $header i32)\n")
	e.out.WriteString("    (local $data i32) (local $end i32)\n")
	e.out.WriteString("    (local.set $ptr (i32.load (local.get $view)))\n")
	e.out.WriteString("    (local.set $length (i32.load (i32.add (local.get $view) (i32.const 4))))\n")
	e.out.WriteString("    (local.set $end (i32.add (local.get $ptr) (local.get $length)))\n")
	e.out.WriteString("    (local.set $header\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $ptr) (i32.const 15)) (i32.const -16)))\n")
	e.out.WriteString("    (local.set $data (i32.add (local.get $header) (i32.const 32)))\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $ptr))\n")
	e.out.WriteString("          (i32.or (i32.lt_u (local.get $end) (local.get $ptr))\n")
	e.out.WriteString("            (i32.gt_u (local.get $data) (local.get $end))))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (i64.store (i32.const 256) (i64.const 1))\n")
	e.out.WriteString("        (i32.store (i32.const 264) (i32.const 0))\n")
	e.out.WriteString("        (i64.store (i32.const 272) (i64.const 0))\n")
	e.out.WriteString("        (i64.store (i32.const 280) (i64.const 0))\n")
	e.out.WriteString("        (return (i32.const 256))))\n")
	e.out.WriteString("    (i64.store (local.get $header) (i64.const 1))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $header) (i32.const 8)) " +
		"(local.get $data))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $header) (i32.const 16))\n")
	e.out.WriteString("      (i64.extend_i32_u (i32.sub (local.get $end) (local.get $data))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $header) (i32.const 24)) (i64.const 0))\n")
	e.out.WriteString("    (local.get $header)\n")
	e.out.WriteString("  )\n\n")
}

// writeFixedAllocHelper emits aligned bump allocation within caller storage.
func (e *emitter) writeFixedAllocHelper() {
	e.out.WriteString("  (func $__fixed_alignment (param $size i32) (result i32)\n")
	e.out.WriteString("    (local $align i32)\n")
	e.out.WriteString("    (local.set $align (i32.const 1))\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $grow\n")
	e.out.WriteString("        (br_if $done (i32.ge_u (local.get $align) (local.get $size)))\n")
	e.out.WriteString("        (br_if $done (i32.ge_u (local.get $align) (i32.const 16)))\n")
	e.out.WriteString("        (local.set $align (i32.mul (local.get $align) (i32.const 2)))\n")
	e.out.WriteString("        (br $grow)))\n")
	e.out.WriteString("    (local.get $align)\n")
	e.out.WriteString("  )\n\n")
	e.out.WriteString("  (func $__fixed_alloc (param $allocator i32) (param $size i32) (result i32)\n")
	e.out.WriteString("    (local $align i32) (local $offset i64) (local $aligned i64)\n")
	e.out.WriteString("    (local $capacity i64) (local $next i64) (local $data i32)\n")
	e.out.WriteString("    (if (i32.lt_s (local.get $size) (i32.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $align (call $__fixed_alignment (local.get $size)))\n")
	e.out.WriteString("    (local.set $offset\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $allocator) (i32.const 24))))\n")
	e.out.WriteString("    (local.set $aligned\n")
	e.out.WriteString("      (i64.and\n")
	e.out.WriteString("        (i64.add (local.get $offset)\n")
	e.out.WriteString("          (i64.extend_i32_u (i32.sub (local.get $align) (i32.const 1))))\n")
	e.out.WriteString("        (i64.extend_i32_s (i32.sub (i32.const 0) (local.get $align)))))\n")
	e.out.WriteString("    (local.set $capacity\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $allocator) (i32.const 16))))\n")
	e.out.WriteString("    (local.set $next\n")
	e.out.WriteString("      (i64.add (local.get $aligned) (i64.extend_i32_u (local.get $size))))\n")
	e.out.WriteString("    (if (i64.gt_u (local.get $next) (local.get $capacity))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $allocator) (i32.const 24)) " +
		"(local.get $next))\n")
	e.out.WriteString("    (local.set $data " +
		"(i32.load (i32.add (local.get $allocator) (i32.const 8))))\n")
	e.out.WriteString("    (i32.add (local.get $data) (i32.wrap_i64 (local.get $aligned)))\n")
	e.out.WriteString("  )\n\n")
}

// writePageAllocHelper implements aligned first-fit allocation. Free blocks
// are address-ordered and may be split; a miss grows WebAssembly memory by the
// exact number of pages needed.
func (e *emitter) writePageAllocHelper() {
	e.out.WriteString("  (func $__page_alloc (param $size i32) (result i32)\n")
	e.out.WriteString("    (local $need i32) (local $previous i32) (local $current i32)\n")
	e.out.WriteString("    (local $capacity i32) (local $next i32) (local $remaining i32)\n")
	e.out.WriteString("    (local $split i32) (local $base i32) (local $end i32)\n")
	e.out.WriteString("    (local $required i32) (local $pages i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $size))\n")
	e.out.WriteString("      (then (local.set $size (i32.const 1))))\n")
	e.out.WriteString("    (if (i32.gt_u (local.get $size) (i32.const 2147483640))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $need\n")
	e.out.WriteString("      (i32.and (i32.add (local.get $size) (i32.const 7)) (i32.const -8)))\n")
	e.out.WriteString("    (local.set $current (global.get $__free_head))\n")
	e.writePageAllocFreeListPath()
	e.writePageAllocFreshPath()
	e.out.WriteString("  )\n\n")
}

// writePageAllocFreeListPath emits first-fit search and optional block split.
func (e *emitter) writePageAllocFreeListPath() {
	e.out.WriteString("    (block $fresh\n")
	e.out.WriteString("      (loop $scan\n")
	e.out.WriteString("        (br_if $fresh (i32.eqz (local.get $current)))\n")
	e.out.WriteString("        (local.set $capacity (i32.load (local.get $current)))\n")
	e.out.WriteString("        (local.set $next\n")
	e.out.WriteString("          (i32.load (i32.add (local.get $current) (i32.const 4))))\n")
	e.out.WriteString("        (if (i32.ge_u (local.get $capacity) (local.get $need))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (local.set $remaining\n")
	e.out.WriteString("              (i32.sub (local.get $capacity) (local.get $need)))\n")
	e.out.WriteString("            (if (i32.ge_u (local.get $remaining) (i32.const 16))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (local.set $split\n")
	e.out.WriteString("                  (i32.add (local.get $current)\n")
	e.out.WriteString("                    (i32.add (i32.const 8) (local.get $need))))\n")
	e.out.WriteString("                (i32.store (local.get $split)\n")
	e.out.WriteString("                  (i32.sub (local.get $remaining) (i32.const 8)))\n")
	e.out.WriteString("                (i32.store (i32.add (local.get $split) (i32.const 4))\n")
	e.out.WriteString("                  (local.get $next))\n")
	e.out.WriteString("                (if (local.get $previous)\n")
	e.out.WriteString("                  (then (i32.store\n")
	e.out.WriteString("                    (i32.add (local.get $previous) (i32.const 4))\n")
	e.out.WriteString("                    (local.get $split)))\n")
	e.out.WriteString("                  (else (global.set $__free_head (local.get $split))))\n")
	e.out.WriteString("                (i32.store (local.get $current) (local.get $need)))\n")
	e.out.WriteString("              (else\n")
	e.out.WriteString("                (if (local.get $previous)\n")
	e.out.WriteString("                  (then (i32.store\n")
	e.out.WriteString("                    (i32.add (local.get $previous) (i32.const 4))\n")
	e.out.WriteString("                    (local.get $next)))\n")
	e.out.WriteString("                  (else (global.set $__free_head (local.get $next))))))\n")
	e.out.WriteString("            (return (i32.add (local.get $current) (i32.const 8)))))\n")
	e.out.WriteString("        (local.set $previous (local.get $current))\n")
	e.out.WriteString("        (local.set $current (local.get $next))\n")
	e.out.WriteString("        (br $scan)))\n")
}

// writePageAllocFreshPath emits memory growth for a free-list miss.
func (e *emitter) writePageAllocFreshPath() {
	e.out.WriteString("    (local.set $base (global.get $__heap_end))\n")
	e.out.WriteString("    (local.set $end\n")
	e.out.WriteString("      (i32.add (local.get $base)\n")
	e.out.WriteString("        (i32.add (i32.const 8) (local.get $need))))\n")
	e.out.WriteString("    (if (i32.lt_u (local.get $end) (local.get $base))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i32.gt_u (local.get $end) (i32.const -65536))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $required\n")
	e.out.WriteString("      (i32.shr_u (i32.add (local.get $end) (i32.const 65535)) " +
		"(i32.const 16)))\n")
	e.out.WriteString("    (local.set $pages (memory.size))\n")
	e.out.WriteString("    (if (i32.gt_u (local.get $required) (local.get $pages))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (if (i32.eq (memory.grow\n")
	e.out.WriteString("              (i32.sub (local.get $required) (local.get $pages)))\n")
	e.out.WriteString("            (i32.const -1))\n")
	e.out.WriteString("          (then (return (i32.const 0))))))\n")
	e.out.WriteString("    (i32.store (local.get $base) (local.get $need))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $base) (i32.const 4)) (i32.const 0))\n")
	e.out.WriteString("    (global.set $__heap_end (local.get $end))\n")
	e.out.WriteString("    (i32.add (local.get $base) (i32.const 8))\n")
}

// writePageFreeHelper inserts one block in address order and coalesces both
// adjacent blocks, so explicit release makes the bytes reusable.
func (e *emitter) writePageFreeHelper() {
	e.out.WriteString("  (func $__page_free (param $ptr i32) (param $size i32)\n")
	e.out.WriteString("    (local $block i32) (local $previous i32) (local $current i32)\n")
	e.out.WriteString("    (local $capacity i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $ptr)) (then (return)))\n")
	e.out.WriteString("    (local.set $block (i32.sub (local.get $ptr) (i32.const 8)))\n")
	e.out.WriteString("    (local.set $current (global.get $__free_head))\n")
	e.writePageFreeInsertion()
	e.writePageFreeCoalesceNext()
	e.writePageFreeCoalescePrevious()
	e.out.WriteString("  )\n\n")
}

// writePageFreeInsertion emits address-ordered insertion into the free list.
func (e *emitter) writePageFreeInsertion() {
	e.out.WriteString("    (block $insert\n")
	e.out.WriteString("      (loop $scan\n")
	e.out.WriteString("        (br_if $insert (i32.eqz (local.get $current)))\n")
	e.out.WriteString("        (br_if $insert (i32.gt_u (local.get $current) (local.get $block)))\n")
	e.out.WriteString("        (local.set $previous (local.get $current))\n")
	e.out.WriteString("        (local.set $current\n")
	e.out.WriteString("          (i32.load (i32.add (local.get $current) (i32.const 4))))\n")
	e.out.WriteString("        (br $scan)))\n")
	e.out.WriteString("    (i32.store (i32.add (local.get $block) (i32.const 4)) " +
		"(local.get $current))\n")
	e.out.WriteString("    (if (local.get $previous)\n")
	e.out.WriteString("      (then (i32.store (i32.add (local.get $previous) (i32.const 4))\n")
	e.out.WriteString("        (local.get $block)))\n")
	e.out.WriteString("      (else (global.set $__free_head (local.get $block))))\n")
}

// writePageFreeCoalesceNext emits merging with the following free block.
func (e *emitter) writePageFreeCoalesceNext() {
	e.out.WriteString("    (if (local.get $current)\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $capacity (i32.load (local.get $block)))\n")
	e.out.WriteString("        (if (i32.eq\n")
	e.out.WriteString("              (i32.add (local.get $block)\n")
	e.out.WriteString("                (i32.add (i32.const 8) (local.get $capacity)))\n")
	e.out.WriteString("              (local.get $current))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (i32.store (local.get $block)\n")
	e.out.WriteString("              (i32.add (local.get $capacity)\n")
	e.out.WriteString("                (i32.add (i32.const 8) (i32.load (local.get $current)))))\n")
	e.out.WriteString("            (i32.store (i32.add (local.get $block) (i32.const 4))\n")
	e.out.WriteString("              (i32.load (i32.add (local.get $current) (i32.const 4))))))))\n")
}

// writePageFreeCoalescePrevious emits merging into the preceding free block.
func (e *emitter) writePageFreeCoalescePrevious() {
	e.out.WriteString("    (if (local.get $previous)\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $capacity (i32.load (local.get $previous)))\n")
	e.out.WriteString("        (if (i32.eq\n")
	e.out.WriteString("              (i32.add (local.get $previous)\n")
	e.out.WriteString("                (i32.add (i32.const 8) (local.get $capacity)))\n")
	e.out.WriteString("              (local.get $block))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (i32.store (local.get $previous)\n")
	e.out.WriteString("              (i32.add (local.get $capacity)\n")
	e.out.WriteString("                (i32.add (i32.const 8) (i32.load (local.get $block)))))\n")
	e.out.WriteString("            (i32.store (i32.add (local.get $previous) (i32.const 4))\n")
	e.out.WriteString("              (i32.load (i32.add (local.get $block) (i32.const 4))))))))\n")
}

// writePageReallocHelper grows a page allocation without losing old bytes.
func (e *emitter) writePageReallocHelper() {
	e.out.WriteString("  (func $__page_realloc\n")
	e.out.WriteString("      (param $ptr i32) (param $old_size i32) " +
		"(param $new_size i32) (result i32)\n")
	e.out.WriteString("    (local $grown i32) (local $copy i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $ptr))\n")
	e.out.WriteString("      (then (return (call $__page_alloc (local.get $new_size)))))\n")
	e.out.WriteString("    (if (i32.le_u (local.get $new_size)\n")
	e.out.WriteString("          (i32.load (i32.sub (local.get $ptr) (i32.const 8))))\n")
	e.out.WriteString("      (then (return (local.get $ptr))))\n")
	e.out.WriteString("    (local.set $grown (call $__page_alloc (local.get $new_size)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $grown)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $copy (local.get $old_size))\n")
	e.out.WriteString("    (if (i32.gt_u (local.get $copy) (local.get $new_size))\n")
	e.out.WriteString("      (then (local.set $copy (local.get $new_size))))\n")
	e.out.WriteString("    (memory.copy (local.get $grown) (local.get $ptr) (local.get $copy))\n")
	e.out.WriteString("    (call $__page_free (local.get $ptr) (local.get $old_size))\n")
	e.out.WriteString("    (local.get $grown)\n")
	e.out.WriteString("  )\n\n")
}

// The dispatch boundary keeps page, fixed-buffer, and user allocation explicit.
// Unknown non-zero kinds are refused instead of silently using page memory.
func (e *emitter) writeAllocatorDispatchHelpers() {
	user := e.usesUserAllocatorRuntime()
	allocSignature := 0
	releaseSignature := 0
	if user {
		allocSignature = e.internFuncSignature(userAllocatorAllocSignature())
		releaseSignature = e.internFuncSignature(userAllocatorReleaseSignature())
	}
	e.writeAllocatorAllocHelper(user, allocSignature)
	e.writeAllocatorReallocHelper(user, allocSignature, releaseSignature)
	e.writeAllocatorFreeHelper(user, releaseSignature)
}

// writeAllocatorAllocHelper emits allocation dispatch by allocator kind.
func (e *emitter) writeAllocatorAllocHelper(user bool, allocSignature int) {
	e.out.WriteString("  (func $__allocator_alloc (param $allocator i32) " +
		"(param $size i32) (result i32)\n")
	e.out.WriteString("    (if (local.get $allocator)\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (if (i64.eq (i64.load (local.get $allocator)) (i64.const 1))\n")
	e.out.WriteString("          (then (return (call $__fixed_alloc\n")
	e.out.WriteString("            (local.get $allocator) (local.get $size)))))\n")
	if user {
		e.out.WriteString("        (if (i64.eq (i64.load (local.get $allocator)) (i64.const 2))\n")
		fmt.Fprintf(&e.out, "          (then (return (call_indirect (type $sig%d)\n", allocSignature)
		e.out.WriteString("            (i32.wrap_i64 " +
			"(i64.load (i32.add (local.get $allocator) (i32.const 8))))\n")
		e.out.WriteString("            (i64.extend_i32_u (local.get $size))\n")
		e.out.WriteString("            (i32.wrap_i64 " +
			"(i64.load (i32.add (local.get $allocator) (i32.const 16))))))))\n")
	}
	e.out.WriteString("        (return (i32.const 0))))\n")
	e.out.WriteString("    (call $__page_alloc (local.get $size))\n")
	e.out.WriteString("  )\n\n")
}

// writeAllocatorReallocHelper emits growth dispatch by allocator kind.
func (e *emitter) writeAllocatorReallocHelper(
	user bool,
	allocSignature int,
	releaseSignature int,
) {
	e.out.WriteString("  (func $__allocator_realloc\n")
	e.out.WriteString("      (param $allocator i32) (param $ptr i32)\n")
	e.out.WriteString("      (param $old_size i32) (param $new_size i32) (result i32)\n")
	e.out.WriteString("    (local $grown i32) (local $copy i32)\n")
	e.out.WriteString("    (if (local.get $allocator)\n")
	e.out.WriteString("      (then\n")
	if user {
		e.writeUserAllocatorRealloc(allocSignature, releaseSignature)
	}
	e.writeFixedAllocatorRealloc()
	e.out.WriteString("    (call $__page_realloc\n")
	e.out.WriteString("      (local.get $ptr) (local.get $old_size) (local.get $new_size))\n")
	e.out.WriteString("  )\n\n")
}

// writeUserAllocatorRealloc emits allocate-copy-release callback dispatch.
func (e *emitter) writeUserAllocatorRealloc(allocSignature int, releaseSignature int) {
	e.out.WriteString("        (if (i64.eq (i64.load (local.get $allocator)) (i64.const 2))\n")
	e.out.WriteString("          (then\n")
	fmt.Fprintf(&e.out, "            (local.set $grown (call_indirect (type $sig%d)\n", allocSignature)
	e.out.WriteString("              (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $allocator) (i32.const 8))))\n")
	e.out.WriteString("              (i64.extend_i32_u (local.get $new_size))\n")
	e.out.WriteString("              (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $allocator) (i32.const 16))))))\n")
	e.out.WriteString("            (if (i32.eqz (local.get $grown))\n")
	e.out.WriteString("              (then (return (i32.const 0))))\n")
	e.out.WriteString("            (local.set $copy (local.get $old_size))\n")
	e.out.WriteString("            (if (i32.gt_u (local.get $copy) (local.get $new_size))\n")
	e.out.WriteString("              (then (local.set $copy (local.get $new_size))))\n")
	e.out.WriteString("            (if (local.get $ptr)\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (memory.copy (local.get $grown) " +
		"(local.get $ptr) (local.get $copy))\n")
	fmt.Fprintf(&e.out, "                (call_indirect (type $sig%d)\n", releaseSignature)
	e.out.WriteString("                  (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $allocator) (i32.const 8))))\n")
	e.out.WriteString("                  (local.get $ptr)\n")
	e.out.WriteString("                  (i64.extend_i32_u (local.get $old_size))\n")
	e.out.WriteString("                  (i32.wrap_i64 " +
		"(i64.load (i32.add (local.get $allocator) (i32.const 24)))))))\n")
	e.out.WriteString("            (return (local.get $grown))))\n")
}

// writeFixedAllocatorRealloc emits in-place growth and bump-copy fallback.
func (e *emitter) writeFixedAllocatorRealloc() {
	e.out.WriteString("        (if (i64.ne (i64.load (local.get $allocator)) (i64.const 1))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (if (i32.and (local.get $ptr)\n")
	e.out.WriteString("              (i32.eq\n")
	e.out.WriteString("                (i32.add (local.get $ptr) (local.get $old_size))\n")
	e.out.WriteString("                (i32.add\n")
	e.out.WriteString("                  (i32.load (i32.add (local.get $allocator) (i32.const 8)))\n")
	e.out.WriteString("                  (i32.wrap_i64\n")
	e.out.WriteString("                    (i64.load " +
		"(i32.add (local.get $allocator) (i32.const 24)))))))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (if (i64.le_u\n")
	e.out.WriteString("                  (i64.add\n")
	e.out.WriteString("                    (i64.sub\n")
	e.out.WriteString("                      (i64.load " +
		"(i32.add (local.get $allocator) (i32.const 24)))\n")
	e.out.WriteString("                      (i64.extend_i32_u (local.get $old_size)))\n")
	e.out.WriteString("                    (i64.extend_i32_u (local.get $new_size)))\n")
	e.out.WriteString("                  (i64.load " +
		"(i32.add (local.get $allocator) (i32.const 16))))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (i64.store (i32.add (local.get $allocator) (i32.const 24))\n")
	e.out.WriteString("                  (i64.add\n")
	e.out.WriteString("                    (i64.sub\n")
	e.out.WriteString("                      (i64.load " +
		"(i32.add (local.get $allocator) (i32.const 24)))\n")
	e.out.WriteString("                      (i64.extend_i32_u (local.get $old_size)))\n")
	e.out.WriteString("                    (i64.extend_i32_u (local.get $new_size))))\n")
	e.out.WriteString("                (return (local.get $ptr))))))\n")
	e.out.WriteString("        (local.set $grown (call $__fixed_alloc\n")
	e.out.WriteString("          (local.get $allocator) (local.get $new_size)))\n")
	e.out.WriteString("        (if (i32.eqz (local.get $grown))\n")
	e.out.WriteString("          (then (return (i32.const 0))))\n")
	e.out.WriteString("        (local.set $copy (local.get $old_size))\n")
	e.out.WriteString("        (if (i32.gt_u (local.get $copy) (local.get $new_size))\n")
	e.out.WriteString("          (then (local.set $copy (local.get $new_size))))\n")
	e.out.WriteString("        (if (local.get $ptr)\n")
	e.out.WriteString("          (then (memory.copy (local.get $grown) " +
		"(local.get $ptr) (local.get $copy))))\n")
	e.out.WriteString("        (return (local.get $grown))))\n")
}

// writeAllocatorFreeHelper emits page and callback release dispatch.
func (e *emitter) writeAllocatorFreeHelper(user bool, releaseSignature int) {
	e.out.WriteString("  (func $__allocator_free\n")
	e.out.WriteString("      (param $allocator i32) (param $ptr i32) (param $size i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $allocator))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (call $__page_free (local.get $ptr) (local.get $size))\n")
	e.out.WriteString("        (return)))\n")
	if user {
		e.out.WriteString("    (if (i64.eq (i64.load (local.get $allocator)) (i64.const 2))\n")
		e.out.WriteString("      (then\n")
		e.out.WriteString("        (if (local.get $ptr)\n")
		fmt.Fprintf(&e.out, "          (then (call_indirect (type $sig%d)\n", releaseSignature)
		e.out.WriteString("            (i32.wrap_i64 " +
			"(i64.load (i32.add (local.get $allocator) (i32.const 8))))\n")
		e.out.WriteString("            (local.get $ptr)\n")
		e.out.WriteString("            (i64.extend_i32_u (local.get $size))\n")
		e.out.WriteString("            (i32.wrap_i64 " +
			"(i64.load (i32.add (local.get $allocator) (i32.const 24)))))))\n")
		e.out.WriteString("        (return)))\n")
	}
	e.out.WriteString("  )\n\n")
}

// writeArrayReserveHelper emits generic checked Array capacity growth.
func (e *emitter) writeArrayReserveHelper() {
	e.out.WriteString("  (func $__array_reserve\n")
	e.out.WriteString("      (param $allocator i32) (param $array i32)\n")
	e.out.WriteString("      (param $needed i64) (param $elem_size i32) (result i32)\n")
	e.out.WriteString("    (local $capacity i64) (local $next i64)\n")
	e.out.WriteString("    (local $old_bytes i64) (local $new_bytes i64) (local $data i32)\n")
	e.out.WriteString("    (if (i32.or (i32.eqz (local.get $array)) " +
		"(i32.le_s (local.get $elem_size) (i32.const 0)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i64.lt_s (local.get $needed) (i64.const 0))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $capacity\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $array) (i32.const 16))))\n")
	e.out.WriteString("    (if (i64.le_u (local.get $needed) (local.get $capacity))\n")
	e.out.WriteString("      (then (return (i32.const 1))))\n")
	e.out.WriteString("    (local.set $next (local.get $needed))\n")
	e.out.WriteString("    (if (i64.lt_u (local.get $next) (i64.const 4))\n")
	e.out.WriteString("      (then (local.set $next (i64.const 4))))\n")
	e.out.WriteString("    (if (i64.gt_u (local.get $capacity) (i64.const 2))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (if (i64.gt_u\n")
	e.out.WriteString("              (i64.add (local.get $capacity)\n")
	e.out.WriteString("                (i64.div_u (local.get $capacity) (i64.const 2)))\n")
	e.out.WriteString("              (local.get $next))\n")
	e.out.WriteString("          (then (local.set $next\n")
	e.out.WriteString("            (i64.add (local.get $capacity)\n")
	e.out.WriteString("              (i64.div_u (local.get $capacity) (i64.const 2))))))))\n")
	e.out.WriteString("    (local.set $old_bytes\n")
	e.out.WriteString("      (i64.mul (local.get $capacity) " +
		"(i64.extend_i32_u (local.get $elem_size))))\n")
	e.out.WriteString("    (local.set $new_bytes\n")
	e.out.WriteString("      (i64.mul (local.get $next) (i64.extend_i32_u (local.get $elem_size))))\n")
	e.out.WriteString("    (if (i32.or\n")
	e.out.WriteString("          (i64.gt_u (local.get $new_bytes) (i64.const 2147483640))\n")
	e.out.WriteString("          (i64.lt_u (local.get $new_bytes) (local.get $next)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $data\n")
	e.out.WriteString("      (call $__allocator_realloc (local.get $allocator)\n")
	e.out.WriteString("        (i32.load (local.get $array))\n")
	e.out.WriteString("        (i32.wrap_i64 (local.get $old_bytes))\n")
	e.out.WriteString("        (i32.wrap_i64 (local.get $new_bytes))))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $data)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (i32.store (local.get $array) (local.get $data))\n")
	e.out.WriteString("    (i64.store (i32.add (local.get $array) (i32.const 16)) " +
		"(local.get $next))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}

// writeArraySwapHelper emits byte-wise exchange for arbitrary element layouts.
func (e *emitter) writeArraySwapHelper() {
	e.out.WriteString("  (func $__array_swap\n")
	e.out.WriteString("      (param $array i32) (param $left i64) (param $right i64)\n")
	e.out.WriteString("      (param $elem_size i32) (result i32)\n")
	e.out.WriteString("    (local $length i64) (local $left_ptr i32) (local $right_ptr i32)\n")
	e.out.WriteString("    (local $index i32) (local $byte i32)\n")
	e.out.WriteString("    (if (i32.eqz (local.get $array)) (then (return (i32.const 0))))\n")
	e.out.WriteString("    (local.set $length\n")
	e.out.WriteString("      (i64.load (i32.add (local.get $array) (i32.const 8))))\n")
	e.out.WriteString("    (if (i32.or (i64.ge_u (local.get $left) (local.get $length))\n")
	e.out.WriteString("          (i64.ge_u (local.get $right) (local.get $length)))\n")
	e.out.WriteString("      (then (return (i32.const 0))))\n")
	e.out.WriteString("    (if (i64.eq (local.get $left) (local.get $right))\n")
	e.out.WriteString("      (then (return (i32.const 1))))\n")
	e.out.WriteString("    (local.set $left_ptr\n")
	e.out.WriteString("      (i32.add (i32.load (local.get $array))\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.mul (local.get $left)\n")
	e.out.WriteString("          (i64.extend_i32_u (local.get $elem_size))))))\n")
	e.out.WriteString("    (local.set $right_ptr\n")
	e.out.WriteString("      (i32.add (i32.load (local.get $array))\n")
	e.out.WriteString("        (i32.wrap_i64 (i64.mul (local.get $right)\n")
	e.out.WriteString("          (i64.extend_i32_u (local.get $elem_size))))))\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $copy\n")
	e.out.WriteString("        (br_if $done (i32.ge_u (local.get $index) (local.get $elem_size)))\n")
	e.out.WriteString("        (local.set $byte\n")
	e.out.WriteString("          (i32.load8_u (i32.add (local.get $left_ptr) (local.get $index))))\n")
	e.out.WriteString("        (i32.store8 (i32.add (local.get $left_ptr) (local.get $index))\n")
	e.out.WriteString("          (i32.load8_u (i32.add (local.get $right_ptr) (local.get $index))))\n")
	e.out.WriteString("        (i32.store8 (i32.add (local.get $right_ptr) (local.get $index))\n")
	e.out.WriteString("          (local.get $byte))\n")
	e.out.WriteString("        (local.set $index (i32.add (local.get $index) (i32.const 1)))\n")
	e.out.WriteString("        (br $copy)))\n")
	e.out.WriteString("    (i32.const 1)\n")
	e.out.WriteString("  )\n\n")
}
