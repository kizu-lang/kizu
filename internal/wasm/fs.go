package wasm

import "fmt"

const (
	fsReadFileIntoBuiltin = "std::internal::builtin::fs_read_file_into"
	fsWriteFileBuiltin    = "std::internal::builtin::fs_write_file"
	fsExistsBuiltin       = "std::internal::builtin::fs_exists"
	fsMetadataBuiltin     = "std::internal::builtin::fs_metadata"
	fsRenameBuiltin       = "std::internal::builtin::fs_rename"
	fsCreateDirBuiltin    = "std::internal::builtin::fs_create_dir"
	fsRemoveDirBuiltin    = "std::internal::builtin::fs_remove_dir"
	fsRemoveFileBuiltin   = "std::internal::builtin::fs_remove_file"
	fsErrorSet            = "std::fs::Error"

	fsPreopenFD          = 3
	fsOpenedFDOffset     = 24
	fsFilestatOffset     = 64
	fsFilestatTypeOffset = 16
	fsFilestatSizeOffset = 32

	wasiLookupSymlinkFollow = 1
	wasiRightFDRead         = 2
	wasiRightFDWrite        = 64
	wasiOpenCreateTruncate  = 9
	wasiFiletypeDirectory   = 3

	wasiErrnoAccess       = 2
	wasiErrnoExist        = 20
	wasiErrnoInvalid      = 28
	wasiErrnoIsDirectory  = 31
	wasiErrnoTooManyFiles = 33
	wasiErrnoNameTooLong  = 37
	wasiErrnoSystemFiles  = 41
	wasiErrnoNotFound     = 44
	wasiErrnoNoSpace      = 51
	wasiErrnoNotDirectory = 54
	wasiErrnoNotEmpty     = 55
	wasiErrnoPermission   = 63
	wasiErrnoNotCapable   = 76
)

type fsErrorCodes struct {
	invalidPath       int
	notFound          int
	permissionDenied  int
	isDirectory       int
	notDirectory      int
	alreadyExists     int
	directoryNotEmpty int
	noSpaceLeft       int
	tooManyOpenFiles  int
	readFailed        int
	writeFailed       int
	outOfMemory       int
	operationFailed   int
	ioFailing         int
	limitExceeded     int
}

// usesFSRuntime reports whether the pruned guest reaches a filesystem host
// primitive implemented by the current WASI boundary.
func (e *emitter) usesFSRuntime() bool {
	return e.usesBuiltinCall(fsReadFileIntoBuiltin) ||
		e.usesBuiltinCall(fsWriteFileBuiltin) ||
		e.usesBuiltinCall(fsExistsBuiltin) ||
		e.usesBuiltinCall(fsMetadataBuiltin) ||
		e.usesBuiltinCall(fsRenameBuiltin) ||
		e.usesBuiltinCall(fsCreateDirBuiltin) ||
		e.usesBuiltinCall(fsRemoveDirBuiltin) ||
		e.usesBuiltinCall(fsRemoveFileBuiltin)
}

// usesFSOpen reports whether a reached primitive opens a file descriptor.
func (e *emitter) usesFSOpen() bool {
	return e.usesBuiltinCall(fsReadFileIntoBuiltin) || e.usesBuiltinCall(fsWriteFileBuiltin)
}

// usesFSStat reports whether path metadata is needed by a reached primitive.
func (e *emitter) usesFSStat() bool {
	return e.usesBuiltinCall(fsExistsBuiltin) || e.usesBuiltinCall(fsMetadataBuiltin)
}

// writeFSImports declares only the WASI filesystem calls the guest reaches.
func (e *emitter) writeFSImports() {
	if !e.usesFSRuntime() {
		return
	}
	e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"fd_prestat_get\"\n")
	e.out.WriteString("    (func $__wasi_fd_prestat_get (param i32 i32) (result i32)))\n")
	if e.usesFSOpen() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_open\"\n")
		e.out.WriteString("    (func $__wasi_path_open\n")
		e.out.WriteString("      (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32)))\n")
	}
	if e.usesBuiltinCall(fsReadFileIntoBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"fd_read\"\n")
		e.out.WriteString("    (func $__wasi_fd_read (param i32 i32 i32 i32) (result i32)))\n")
	}
	if e.usesFSOpen() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"fd_close\"\n")
		e.out.WriteString("    (func $__wasi_fd_close (param i32) (result i32)))\n")
	}
	if e.usesFSStat() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_filestat_get\"\n")
		e.out.WriteString("    (func $__wasi_path_filestat_get\n")
		e.out.WriteString("      (param i32 i32 i32 i32 i32) (result i32)))\n")
	}
	if e.usesBuiltinCall(fsCreateDirBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_create_directory\"\n")
		e.out.WriteString("    (func $__wasi_path_create_directory (param i32 i32 i32) (result i32)))\n")
	}
	if e.usesBuiltinCall(fsRemoveDirBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_remove_directory\"\n")
		e.out.WriteString("    (func $__wasi_path_remove_directory (param i32 i32 i32) (result i32)))\n")
	}
	if e.usesBuiltinCall(fsRemoveFileBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_unlink_file\"\n")
		e.out.WriteString("    (func $__wasi_path_unlink_file (param i32 i32 i32) (result i32)))\n")
	}
	if e.usesBuiltinCall(fsRenameBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"path_rename\"\n")
		e.out.WriteString("    (func $__wasi_path_rename\n")
		e.out.WriteString("      (param i32 i32 i32 i32 i32 i32) (result i32)))\n")
	}
}

// writeFSRuntime emits the basic preopen-relative filesystem boundary.
func (e *emitter) writeFSRuntime() error {
	if !e.usesFSRuntime() {
		return nil
	}
	codes, err := e.loadFSErrorCodes()
	if err != nil {
		return err
	}
	e.writeFSPreopenHelper()
	e.writeFSErrnoHelper(codes)
	if e.usesBuiltinCall(fsReadFileIntoBuiltin) {
		e.writeFSReadFileInto(codes)
	}
	if e.usesBuiltinCall(fsWriteFileBuiltin) {
		e.writeFSWriteFile(codes)
	}
	if e.usesBuiltinCall(fsExistsBuiltin) {
		e.writeFSExists(codes)
	}
	if e.usesBuiltinCall(fsMetadataBuiltin) {
		e.writeFSMetadata(codes)
	}
	if e.usesBuiltinCall(fsCreateDirBuiltin) {
		e.writeFSPathVoidBuiltin(
			fsCreateDirBuiltin, "$__wasi_path_create_directory", codes)
	}
	if e.usesBuiltinCall(fsRemoveDirBuiltin) {
		e.writeFSPathVoidBuiltin(
			fsRemoveDirBuiltin, "$__wasi_path_remove_directory", codes)
	}
	if e.usesBuiltinCall(fsRemoveFileBuiltin) {
		e.writeFSPathVoidBuiltin(
			fsRemoveFileBuiltin, "$__wasi_path_unlink_file", codes)
	}
	if e.usesBuiltinCall(fsRenameBuiltin) {
		e.writeFSRename(codes)
	}
	return nil
}

// loadFSErrorCodes resolves every host errno target from the declared set.
func (e *emitter) loadFSErrorCodes() (fsErrorCodes, error) {
	codes := fsErrorCodes{}
	members := []struct {
		name string
		dst  *int
	}{
		{"InvalidPath", &codes.invalidPath},
		{"NotFound", &codes.notFound},
		{"PermissionDenied", &codes.permissionDenied},
		{"IsDirectory", &codes.isDirectory},
		{"NotDirectory", &codes.notDirectory},
		{"AlreadyExists", &codes.alreadyExists},
		{"DirectoryNotEmpty", &codes.directoryNotEmpty},
		{"NoSpaceLeft", &codes.noSpaceLeft},
		{"TooManyOpenFiles", &codes.tooManyOpenFiles},
		{"ReadFailed", &codes.readFailed},
		{"WriteFailed", &codes.writeFailed},
		{"OutOfMemory", &codes.outOfMemory},
		{"OperationFailed", &codes.operationFailed},
		{"IoFailing", &codes.ioFailing},
		{"LimitExceeded", &codes.limitExceeded},
	}
	for _, member := range members {
		code, err := e.wasmErrorCode(fsErrorSet, member.name)
		if err != nil {
			return fsErrorCodes{}, err
		}
		*member.dst = code
	}
	return codes, nil
}

// writeFSPreopenHelper verifies the WASI CLI's first directory capability.
// Paths remain relative to that explicit preopen; the compiler grants none.
func (e *emitter) writeFSPreopenHelper() {
	e.out.WriteString("  (func $__fs_preopen (result i32)\n")
	fmt.Fprintf(&e.out, "    (if (call $__wasi_fd_prestat_get (i32.const %d) (i32.const %d))\n",
		fsPreopenFD, scratchOffset)
	e.out.WriteString("      (then (return (i32.const -1))))\n")
	fmt.Fprintf(&e.out, "    (if (i32.ne (i32.load8_u (i32.const %d)) (i32.const 0))\n",
		scratchOffset)
	e.out.WriteString("      (then (return (i32.const -1))))\n")
	fmt.Fprintf(&e.out, "    (i32.const %d)\n", fsPreopenFD)
	e.out.WriteString("  )\n\n")
}

// writeFSErrnoHelper maps preview1 errno values to declaration-owned Kizu
// errors. NOTCAPABLE is permission failure, never a hidden wider-path retry.
func (e *emitter) writeFSErrnoHelper(codes fsErrorCodes) {
	e.out.WriteString("  (func $__fs_error (param $errno i32) (result i64)\n")
	e.writeFSErrnoCase(wasiErrnoNotFound, codes.notFound)
	e.writeFSErrnoTripleCase(
		wasiErrnoAccess, wasiErrnoPermission, wasiErrnoNotCapable, codes.permissionDenied)
	e.writeFSErrnoCase(wasiErrnoIsDirectory, codes.isDirectory)
	e.writeFSErrnoCase(wasiErrnoNotDirectory, codes.notDirectory)
	e.writeFSErrnoCase(wasiErrnoExist, codes.alreadyExists)
	e.writeFSErrnoCase(wasiErrnoNotEmpty, codes.directoryNotEmpty)
	e.writeFSErrnoCase(wasiErrnoNoSpace, codes.noSpaceLeft)
	e.writeFSErrnoPairCase(
		wasiErrnoTooManyFiles, wasiErrnoSystemFiles, codes.tooManyOpenFiles)
	e.writeFSErrnoPairCase(wasiErrnoInvalid, wasiErrnoNameTooLong, codes.invalidPath)
	fmt.Fprintf(&e.out, "    (i64.const %d)\n", codes.operationFailed)
	e.out.WriteString("  )\n\n")
}

// writeFSErrnoCase emits one direct errno-to-error return.
func (e *emitter) writeFSErrnoCase(errno int, code int) {
	fmt.Fprintf(&e.out, "    (if (i32.eq (local.get $errno) (i32.const %d))\n", errno)
	fmt.Fprintf(&e.out, "      (then (return (i64.const %d))))\n", code)
}

// writeFSErrnoPairCase emits two errno values with one Kizu meaning.
func (e *emitter) writeFSErrnoPairCase(first int, second int, code int) {
	e.out.WriteString("    (if (i32.or\n")
	fmt.Fprintf(&e.out, "          (i32.eq (local.get $errno) (i32.const %d))\n", first)
	fmt.Fprintf(&e.out, "          (i32.eq (local.get $errno) (i32.const %d)))\n", second)
	fmt.Fprintf(&e.out, "      (then (return (i64.const %d))))\n", code)
}

// writeFSErrnoTripleCase emits three errno values with one Kizu meaning.
func (e *emitter) writeFSErrnoTripleCase(first int, second int, third int, code int) {
	e.out.WriteString("    (if (i32.or\n")
	fmt.Fprintf(&e.out, "          (i32.eq (local.get $errno) (i32.const %d))\n", first)
	fmt.Fprintf(&e.out, "          (i32.or (i32.eq (local.get $errno) (i32.const %d))\n", second)
	fmt.Fprintf(&e.out, "            (i32.eq (local.get $errno) (i32.const %d))))\n", third)
	fmt.Fprintf(&e.out, "      (then (return (i64.const %d))))\n", code)
}

// writeFSGuard rejects the failing token and a missing directory capability.
func (e *emitter) writeFSGuard(ioFailing int, permissionDenied int) {
	fmt.Fprintf(&e.out, "    (if (i32.eq (local.get $io) (i32.const %d))\n", ioFailingToken)
	e.out.WriteString("      (then\n")
	e.writeErrorResult(ioFailing, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $fd (call $__fs_preopen))\n")
	e.out.WriteString("    (if (i32.lt_s (local.get $fd) (i32.const 0))\n")
	e.out.WriteString("      (then\n")
	e.writeErrorResult(permissionDenied, "        ")
	e.out.WriteString("        (return)))\n")
}

// writeFSStoredErrno stores a dynamically mapped host failure.
func (e *emitter) writeFSStoredErrno(indent string) {
	fmt.Fprintf(&e.out, "%s(i64.store (local.get $out) (i64.const 0))\n", indent)
	fmt.Fprintf(&e.out,
		"%s(i64.store (i32.add (local.get $out) (i32.const %d))\n",
		indent, voidErrorPayloadOffset)
	fmt.Fprintf(&e.out, "%s  (call $__fs_error (local.get $errno)))\n", indent)
}

// writeFSReadFileInto appends a whole file while enforcing the caller's cap.
func (e *emitter) writeFSReadFileInto(codes fsErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s\n", fsReadFileIntoBuiltin)
	e.out.WriteString("      (param $out i32) (param $io i32) (param $allocator i32)\n")
	e.out.WriteString("      (param $path i32) (param $dst i32) (param $max i64)\n")
	e.out.WriteString("    (local $fd i32) (local $errno i32) (local $ptr i32)\n")
	e.out.WriteString("    (local $want i32) (local $read i32)\n")
	e.out.WriteString("    (local $start i64) (local $total i64)\n")
	e.out.WriteString("    (local $remaining i64) (local $needed i64)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.writeFSReadOpen()
	e.writeFSReadLoop(codes)
	e.out.WriteString("    (drop (call $__wasi_fd_close (local.get $fd)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSReadOpen opens the path with read-only rights and snapshots length.
func (e *emitter) writeFSReadOpen() {
	e.out.WriteString("    (local.set $errno (call $__wasi_path_open\n")
	fmt.Fprintf(&e.out,
		"      (local.get $fd) (i32.const %d) (i32.load (local.get $path))\n",
		wasiLookupSymlinkFollow)
	e.out.WriteString("      (i32.load (i32.add (local.get $path) (i32.const 4)))\n")
	fmt.Fprintf(&e.out,
		"      (i32.const 0) (i64.const %d) (i64.const 0) (i32.const 0) (i32.const %d)))\n",
		wasiRightFDRead, fsOpenedFDOffset)
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	fmt.Fprintf(&e.out, "    (local.set $fd (i32.load (i32.const %d)))\n", fsOpenedFDOffset)
	fmt.Fprintf(&e.out,
		"    (local.set $start (i64.load (i32.add (local.get $dst) (i32.const %d))))\n",
		arrayLenOffset)
}

// writeFSReadLoop emits one EOF-terminated sequence of bounded chunks.
func (e *emitter) writeFSReadLoop(codes fsErrorCodes) {
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $read\n")
	e.writeFSReadReserve(codes.outOfMemory)
	e.writeFSReadChunk(codes)
	e.out.WriteString("        (br $read)))\n")
}

// writeFSReadReserve chooses the next bounded read and grows destination.
func (e *emitter) writeFSReadReserve(outOfMemory int) {
	e.out.WriteString("        (local.set $want (i32.const 65536))\n")
	e.out.WriteString("        (if (i64.ge_s (local.get $max) (i64.const 0))\n")
	e.out.WriteString("          (then\n")
	e.out.WriteString("            (local.set $remaining\n")
	e.out.WriteString("              (i64.sub (local.get $max) (local.get $total)))\n")
	e.out.WriteString("            (if (i64.lt_u (local.get $remaining) (i64.const 65536))\n")
	e.out.WriteString("              (then (local.set $want (i32.wrap_i64\n")
	e.out.WriteString("                (i64.add (local.get $remaining) (i64.const 1))))))))\n")
	e.out.WriteString("        (local.set $needed\n")
	fmt.Fprintf(&e.out,
		"          (i64.add (i64.load (i32.add (local.get $dst) (i32.const %d)))\n",
		arrayLenOffset)
	e.out.WriteString("            (i64.extend_i32_u (local.get $want))))\n")
	e.out.WriteString("        (if (i32.eqz (call $__array_reserve\n")
	e.out.WriteString("              (local.get $allocator) (local.get $dst)\n")
	e.out.WriteString("              (local.get $needed) (i32.const 1)))\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadFailure(outOfMemory)
	e.out.WriteString("            (return)))\n")
}

// writeFSReadChunk performs one host read and commits only checked progress.
func (e *emitter) writeFSReadChunk(codes fsErrorCodes) {
	e.out.WriteString("        (local.set $ptr\n")
	e.out.WriteString("          (i32.add (i32.load (local.get $dst))\n")
	fmt.Fprintf(&e.out,
		"            (i32.wrap_i64 (i64.load (i32.add (local.get $dst) (i32.const %d))))))\n",
		arrayLenOffset)
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $ptr))\n", scratchOffset)
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $want))\n", scratchOffset+4)
	e.out.WriteString("        (local.set $errno (call $__wasi_fd_read\n")
	fmt.Fprintf(&e.out,
		"          (local.get $fd) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	e.out.WriteString("        (if (local.get $errno)\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadFailure(codes.readFailed)
	e.out.WriteString("            (return)))\n")
	fmt.Fprintf(&e.out, "        (local.set $read (i32.load (i32.const %d)))\n", scratchOffset+16)
	e.out.WriteString("        (if (i32.gt_u (local.get $read) (local.get $want))\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadFailure(codes.readFailed)
	e.out.WriteString("            (return)))\n")
	e.out.WriteString("        (br_if $done (i32.eqz (local.get $read)))\n")
	fmt.Fprintf(&e.out,
		"        (i64.store (i32.add (local.get $dst) (i32.const %d))\n",
		arrayLenOffset)
	fmt.Fprintf(&e.out,
		"          (i64.add (i64.load (i32.add (local.get $dst) (i32.const %d)))\n",
		arrayLenOffset)
	e.out.WriteString("            (i64.extend_i32_u (local.get $read))))\n")
	e.out.WriteString("        (local.set $total\n")
	e.out.WriteString("          (i64.add (local.get $total) (i64.extend_i32_u (local.get $read))))\n")
	e.out.WriteString("        (if (i32.and (i64.ge_s (local.get $max) (i64.const 0))\n")
	e.out.WriteString("            (i64.gt_u (local.get $total) (local.get $max)))\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadFailure(codes.limitExceeded)
	e.out.WriteString("            (return)))\n")
}

// writeFSReadFailure rolls the destination back and closes the open file.
func (e *emitter) writeFSReadFailure(code int) {
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $dst) (i32.const %d)) (local.get $start))\n",
		arrayLenOffset)
	e.out.WriteString("            (drop (call $__wasi_fd_close (local.get $fd)))\n")
	e.writeErrorResult(code, "            ")
}

// writeFSWriteFile replaces one preopen-relative file and writes every byte.
func (e *emitter) writeFSWriteFile(codes fsErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s\n", fsWriteFileBuiltin)
	e.out.WriteString("      (param $out i32) (param $io i32) (param $path i32) (param $bytes i32)\n")
	e.out.WriteString("    (local $fd i32) (local $errno i32) (local $ptr i32)\n")
	e.out.WriteString("    (local $remaining i32) (local $written i32)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.out.WriteString("    (local.set $errno (call $__wasi_path_open\n")
	fmt.Fprintf(&e.out,
		"      (local.get $fd) (i32.const %d) (i32.load (local.get $path))\n",
		wasiLookupSymlinkFollow)
	e.out.WriteString("      (i32.load (i32.add (local.get $path) (i32.const 4)))\n")
	fmt.Fprintf(&e.out,
		"      (i32.const %d) (i64.const %d) (i64.const 0) (i32.const 0) (i32.const %d)))\n",
		wasiOpenCreateTruncate, wasiRightFDWrite, fsOpenedFDOffset)
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	fmt.Fprintf(&e.out, "    (local.set $fd (i32.load (i32.const %d)))\n", fsOpenedFDOffset)
	e.out.WriteString("    (local.set $ptr (i32.load (local.get $bytes)))\n")
	e.out.WriteString("    (local.set $remaining\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $bytes) (i32.const 4))))\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $write\n")
	e.out.WriteString("        (br_if $done (i32.eqz (local.get $remaining)))\n")
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $ptr))\n", scratchOffset)
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $remaining))\n",
		scratchOffset+4)
	e.out.WriteString("        (local.set $errno (call $__wasi_fd_write\n")
	fmt.Fprintf(&e.out,
		"          (local.get $fd) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	e.out.WriteString("        (if (local.get $errno)\n")
	e.out.WriteString("          (then\n")
	e.writeFSWriteFailure(codes.writeFailed)
	e.out.WriteString("            (return)))\n")
	fmt.Fprintf(&e.out, "        (local.set $written (i32.load (i32.const %d)))\n",
		scratchOffset+16)
	e.out.WriteString("        (if (i32.or (i32.eqz (local.get $written))\n")
	e.out.WriteString("            (i32.gt_u (local.get $written) (local.get $remaining)))\n")
	e.out.WriteString("          (then\n")
	e.writeFSWriteFailure(codes.writeFailed)
	e.out.WriteString("            (return)))\n")
	e.out.WriteString("        (local.set $ptr\n")
	e.out.WriteString("          (i32.add (local.get $ptr) (local.get $written)))\n")
	e.out.WriteString("        (local.set $remaining\n")
	e.out.WriteString("          (i32.sub (local.get $remaining) (local.get $written)))\n")
	e.out.WriteString("        (br $write)))\n")
	e.out.WriteString("    (drop (call $__wasi_fd_close (local.get $fd)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSWriteFailure closes a partially written file before returning error.
func (e *emitter) writeFSWriteFailure(code int) {
	e.out.WriteString("            (drop (call $__wasi_fd_close (local.get $fd)))\n")
	e.writeErrorResult(code, "            ")
}

// writeFSExists mirrors access-style existence: path errors become false.
func (e *emitter) writeFSExists(codes fsErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $io i32) (param $path i32)\n",
		fsExistsBuiltin)
	e.out.WriteString("    (local $fd i32) (local $errno i32)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.writeFSFilestatCall()
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("    (i32.store8 (i32.add (local.get $out) (i32.const 8))\n")
	e.out.WriteString("      (i32.eqz (local.get $errno)))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSMetadata copies size and directory kind out of WASI filestat.
func (e *emitter) writeFSMetadata(codes fsErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $io i32) (param $path i32)\n",
		fsMetadataBuiltin)
	e.out.WriteString("    (local $fd i32) (local $errno i32)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.writeFSFilestatCall()
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $out) (i32.const %d))\n",
		voidErrorPayloadOffset)
	fmt.Fprintf(&e.out, "      (i64.load (i32.const %d)))\n",
		fsFilestatOffset+fsFilestatSizeOffset)
	fmt.Fprintf(&e.out,
		"    (i32.store8 (i32.add (local.get $out) (i32.const %d))\n",
		voidErrorPayloadOffset+8)
	fmt.Fprintf(&e.out, "      (i32.eq (i32.load8_u (i32.const %d)) (i32.const %d)))\n",
		fsFilestatOffset+fsFilestatTypeOffset, wasiFiletypeDirectory)
	e.out.WriteString("  )\n\n")
}

// writeFSFilestatCall emits the common symlink-following metadata request.
func (e *emitter) writeFSFilestatCall() {
	e.out.WriteString("    (local.set $errno (call $__wasi_path_filestat_get\n")
	fmt.Fprintf(&e.out,
		"      (local.get $fd) (i32.const %d) (i32.load (local.get $path))\n",
		wasiLookupSymlinkFollow)
	e.out.WriteString("      (i32.load (i32.add (local.get $path) (i32.const 4)))\n")
	fmt.Fprintf(&e.out, "      (i32.const %d)))\n", fsFilestatOffset)
}

// writeFSPathVoidBuiltin emits one fallible single-path mutation wrapper.
func (e *emitter) writeFSPathVoidBuiltin(
	name string,
	host string,
	codes fsErrorCodes,
) {
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $io i32) (param $path i32)\n",
		name)
	e.out.WriteString("    (local $fd i32) (local $errno i32)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	fmt.Fprintf(&e.out, "    (local.set $errno (call %s\n", host)
	e.out.WriteString("      (local.get $fd) (i32.load (local.get $path))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $path) (i32.const 4)))))\n")
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSRename moves one path within the explicitly granted preopen.
func (e *emitter) writeFSRename(codes fsErrorCodes) {
	fmt.Fprintf(&e.out,
		"  (func $%s (param $out i32) (param $io i32) (param $from i32) (param $to i32)\n",
		fsRenameBuiltin)
	e.out.WriteString("    (local $fd i32) (local $errno i32)\n")
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.out.WriteString("    (local.set $errno (call $__wasi_path_rename\n")
	e.out.WriteString("      (local.get $fd) (i32.load (local.get $from))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $from) (i32.const 4)))\n")
	e.out.WriteString("      (local.get $fd) (i32.load (local.get $to))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $to) (i32.const 4)))))\n")
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}
