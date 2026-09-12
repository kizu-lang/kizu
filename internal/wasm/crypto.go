package wasm

import "fmt"

const (
	cryptoRandomIntoBuiltin = "std::internal::builtin::crypto_random_into"
	cryptoErrorSet          = "std::crypto::Error"
)

// cryptoErrorCodes are the std::crypto::Error members the random draw fails
// with, resolved from the declared set.
type cryptoErrorCodes struct {
	ioFailing   int
	outOfMemory int
	readFailed  int
}

// usesCryptoRandom reports whether the guest reaches the host's random source.
func (e *emitter) usesCryptoRandom() bool {
	return e.usesBuiltinCall(cryptoRandomIntoBuiltin)
}

// writeCryptoImports declares the WASI random source only when it is reached.
func (e *emitter) writeCryptoImports() {
	if !e.usesCryptoRandom() {
		return
	}
	e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"random_get\"\n")
	e.out.WriteString("    (func $__wasi_random_get (param i32 i32) (result i32)))\n")
}

// writeBrowserCryptoImports declares the browser host's random source only
// when it is reached. The browser fills guest memory the way WASI does, so
// the one primitive below serves both hosts with a different callee.
func (e *emitter) writeBrowserCryptoImports() {
	if !e.usesCryptoRandom() {
		return
	}
	e.out.WriteString("  (import \"kizu\" \"random\"\n")
	e.out.WriteString("    (func $__kizu_random (param i32 i32) (result i32)))\n")
}

// writeCryptoRuntime emits the random draw when the guest reaches it.
func (e *emitter) writeCryptoRuntime() error {
	if !e.usesCryptoRandom() {
		return nil
	}
	codes, err := e.loadCryptoErrorCodes()
	if err != nil {
		return err
	}
	e.writeCryptoRandomInto(codes)
	return nil
}

// loadCryptoErrorCodes resolves the members the draw reports from the set.
func (e *emitter) loadCryptoErrorCodes() (cryptoErrorCodes, error) {
	codes := cryptoErrorCodes{}
	members := []struct {
		name string
		dst  *int
	}{
		{"IoFailing", &codes.ioFailing},
		{"OutOfMemory", &codes.outOfMemory},
		{"ReadFailed", &codes.readFailed},
	}
	for _, member := range members {
		code, err := e.wasmErrorCode(cryptoErrorSet, member.name)
		if err != nil {
			return cryptoErrorCodes{}, err
		}
		*member.dst = code
	}
	return codes, nil
}

// writeCryptoRandomInto appends `count` host random bytes to the destination
// String. The storage is grown first, the host writes straight into it, and
// the length is committed only after the host said yes, so a refused draw
// leaves the String as it was.
func (e *emitter) writeCryptoRandomInto(codes cryptoErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s\n", cryptoRandomIntoBuiltin)
	e.out.WriteString("      (param $out i32) (param $io i32) (param $allocator i32)\n")
	e.out.WriteString("      (param $dst i32) (param $count i64)\n")
	e.out.WriteString("    (local $errno i32) (local $ptr i32) (local $needed i64)\n")
	fmt.Fprintf(&e.out, "    (if (i32.eq (local.get $io) (i32.const %d))\n", ioFailingToken)
	e.out.WriteString("      (then\n")
	e.writeErrorResult(codes.ioFailing, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (if (i64.le_s (local.get $count) (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $needed\n")
	fmt.Fprintf(&e.out,
		"      (i64.add (i64.load (i32.add (local.get $dst) (i32.const %d)))\n",
		arrayLenOffset)
	e.out.WriteString("        (local.get $count)))\n")
	e.out.WriteString("    (if (i32.eqz (call $__array_reserve\n")
	e.out.WriteString("          (local.get $allocator) (local.get $dst)\n")
	e.out.WriteString("          (local.get $needed) (i32.const 1)))\n")
	e.out.WriteString("      (then\n")
	e.writeErrorResult(codes.outOfMemory, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $ptr\n")
	e.out.WriteString("      (i32.add (i32.load (local.get $dst))\n")
	fmt.Fprintf(&e.out,
		"        (i32.wrap_i64 (i64.load (i32.add (local.get $dst) (i32.const %d))))))\n",
		arrayLenOffset)
	callee := "$__wasi_random_get"
	if e.target.isBrowser() {
		callee = "$__kizu_random"
	}
	fmt.Fprintf(&e.out, "    (local.set $errno (call %s\n", callee)
	e.out.WriteString("      (local.get $ptr) (i32.wrap_i64 (local.get $count))))\n")
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeErrorResult(codes.readFailed, "        ")
	e.out.WriteString("        (return)))\n")
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $dst) (i32.const %d)) (local.get $needed))\n",
		arrayLenOffset)
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}
