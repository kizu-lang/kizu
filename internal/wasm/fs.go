package wasm

import "fmt"

const (
	fsReadFileIntoBuiltin = "std::internal::builtin::fs_read_file_into"
	fsReadDirBuiltin      = "std::internal::builtin::fs_read_dir"
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
	fsReaddirUsedOffset  = 32
	fsDirentHeaderSize   = 24
	fsDirentNameOffset   = 24
	fsDirentNameLen      = 16
	fsReaddirBufferSize  = 4096

	wasiLookupSymlinkFollow = 1
	wasiRightFDRead         = 2
	wasiRightFDWrite        = 64
	wasiRightFDReaddir      = 16384
	wasiOpenDirectory       = 2
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

// fsDirLayout is the declaration-derived wasm32 storage used by read_dir.
// String's private Array field is included so the runtime does not pin the
// source-level struct shape.
type fsDirLayout struct {
	entrySize    int
	nameOffset   int
	pathOffset   int
	isDirOffset  int
	resultOffset int
}

// usesFSRuntime reports whether the pruned guest reaches a filesystem host
// primitive implemented by the current WASI boundary.
func (e *emitter) usesFSRuntime() bool {
	return e.usesBuiltinCall(fsReadFileIntoBuiltin) ||
		e.usesBuiltinCall(fsReadDirBuiltin) ||
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
	return e.usesBuiltinCall(fsReadFileIntoBuiltin) ||
		e.usesBuiltinCall(fsReadDirBuiltin) ||
		e.usesBuiltinCall(fsWriteFileBuiltin)
}

// usesFSStat reports whether path metadata is needed by a reached primitive.
func (e *emitter) usesFSStat() bool {
	return e.usesBuiltinCall(fsExistsBuiltin) ||
		e.usesBuiltinCall(fsMetadataBuiltin) ||
		e.usesBuiltinCall(fsReadDirBuiltin)
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
	if e.usesBuiltinCall(fsReadDirBuiltin) {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"fd_readdir\"\n")
		e.out.WriteString("    (func $__wasi_fd_readdir (param i32 i32 i32 i64 i32) (result i32)))\n")
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
	if e.usesBuiltinCall(fsReadDirBuiltin) {
		layout, err := e.loadFSDirLayout()
		if err != nil {
			return err
		}
		e.writeFSReadDir(layout, codes)
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

// loadFSDirLayout resolves the public entry fields, String's storage field,
// and the success payload from declarations. Runtime code may depend on Array's
// ABI, but not on one source-level aggregate continuing to have fixed offsets.
func (e *emitter) loadFSDirLayout() (fsDirLayout, error) {
	const (
		entryType  = "std::fs::DirEntry"
		stringType = "std::string::String"
		arrayType  = "std::array::Array<std::fs::DirEntry>"
		resultType = "std::fs::Error!std::array::Array<std::fs::DirEntry>"
	)
	entry, err := e.typeLayout(entryType)
	if err != nil {
		return fsDirLayout{}, err
	}
	name, nameOffset, err := e.fieldLayout(entryType, "name")
	if err != nil {
		return fsDirLayout{}, err
	}
	path, pathOffset, err := e.fieldLayout(entryType, "path")
	if err != nil {
		return fsDirLayout{}, err
	}
	isDir, isDirOffset, err := e.fieldLayout(entryType, "is_dir")
	if err != nil {
		return fsDirLayout{}, err
	}
	bytes, bytesOffset, err := e.fieldLayout(stringType, "bytes")
	if err != nil {
		return fsDirLayout{}, err
	}
	_, success, resultOffset, err := e.errorPayloadOffset(resultType)
	if err != nil {
		return fsDirLayout{}, err
	}
	if name.Type != stringType || path.Type != stringType || isDir.Type != "bool" ||
		bytes.Type != "std::array::Array<u8>" || success != arrayType || entry.size <= 0 {
		return fsDirLayout{}, fmt.Errorf(
			"wasm error: incompatible `%s` storage for fs_read_dir", entryType)
	}
	return fsDirLayout{
		entrySize:    entry.size,
		nameOffset:   nameOffset + bytesOffset,
		pathOffset:   pathOffset + bytesOffset,
		isDirOffset:  isDirOffset,
		resultOffset: resultOffset,
	}, nil
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

// writeFSReadDir emits an owned, sorted directory snapshot. The host buffer
// is temporary; every returned name and path belongs to the caller allocator.
func (e *emitter) writeFSReadDir(layout fsDirLayout, codes fsErrorCodes) {
	e.writeFSDirEntriesFree(layout)
	e.writeFSDirEntryAfter(layout)
	e.writeFSSortDirEntries(layout)
	e.writeFSReadDirFailHelper(layout)
	e.writeFSReadDirBuiltin(layout, codes)
}

// writeFSDirEntriesFree releases nested String storage before Array storage.
func (e *emitter) writeFSDirEntriesFree(layout fsDirLayout) {
	e.out.WriteString("  (func $__fs_dir_entries_free (param $allocator i32) (param $array i32)\n")
	e.out.WriteString("    (local $index i64) (local $item i32) (local $capacity i64)\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $entries\n")
	fmt.Fprintf(&e.out,
		"        (br_if $done (i64.ge_u (local.get $index) "+
			"(i64.load (i32.add (local.get $array) (i32.const %d)))))\n",
		arrayLenOffset)
	e.out.WriteString("        (local.set $item\n")
	e.out.WriteString("          (i32.add (i32.load (local.get $array))\n")
	fmt.Fprintf(&e.out,
		"            (i32.wrap_i64 (i64.mul (local.get $index) (i64.const %d)))))\n",
		layout.entrySize)
	e.writeFSDirStringFree(layout.nameOffset)
	e.writeFSDirStringFree(layout.pathOffset)
	e.out.WriteString("        (local.set $index (i64.add (local.get $index) (i64.const 1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	fmt.Fprintf(&e.out,
		"    (local.set $capacity (i64.load (i32.add (local.get $array) (i32.const %d))))\n",
		arrayCapacityOffset)
	e.out.WriteString("    (call $__allocator_free (local.get $allocator) " +
		"(i32.load (local.get $array))\n")
	fmt.Fprintf(&e.out,
		"      (i32.wrap_i64 (i64.mul (local.get $capacity) (i64.const %d))))\n",
		layout.entrySize)
	e.out.WriteString("    (i32.store (local.get $array) (i32.const 0))\n")
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $array) (i32.const %d)) (i64.const 0))\n",
		arrayLenOffset)
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $array) (i32.const %d)) (i64.const 0))\n",
		arrayCapacityOffset)
	e.out.WriteString("  )\n\n")
}

// writeFSDirStringFree releases one String's declaration-derived Array field.
func (e *emitter) writeFSDirStringFree(offset int) {
	fmt.Fprintf(&e.out,
		"        (local.set $capacity (i64.load (i32.add (local.get $item) (i32.const %d))))\n",
		offset+arrayCapacityOffset)
	e.out.WriteString("        (call $__allocator_free (local.get $allocator)\n")
	fmt.Fprintf(&e.out,
		"          (i32.load (i32.add (local.get $item) (i32.const %d)))\n",
		offset+arrayDataOffset)
	e.out.WriteString("          (i32.wrap_i64 (local.get $capacity)))\n")
}

// writeFSDirEntryAfter compares names by unsigned bytes for insertion sort.
func (e *emitter) writeFSDirEntryAfter(layout fsDirLayout) {
	e.out.WriteString("  (func $__fs_dir_entry_after " +
		"(param $left i32) (param $right i32) (result i32)\n")
	e.out.WriteString("    (local $left_len i64) (local $right_len i64) (local $limit i64)\n")
	e.out.WriteString("    (local $index i64) (local $left_byte i32) (local $right_byte i32)\n")
	fmt.Fprintf(&e.out,
		"    (local.set $left_len (i64.load (i32.add (local.get $left) (i32.const %d))))\n",
		layout.nameOffset+arrayLenOffset)
	fmt.Fprintf(&e.out,
		"    (local.set $right_len (i64.load (i32.add (local.get $right) (i32.const %d))))\n",
		layout.nameOffset+arrayLenOffset)
	e.out.WriteString("    (local.set $limit (local.get $left_len))\n")
	e.out.WriteString("    (if (i64.lt_u (local.get $right_len) (local.get $limit))\n")
	e.out.WriteString("      (then (local.set $limit (local.get $right_len))))\n")
	e.out.WriteString("    (block $same_prefix\n")
	e.out.WriteString("      (loop $bytes\n")
	e.out.WriteString("        (br_if $same_prefix " +
		"(i64.ge_u (local.get $index) (local.get $limit)))\n")
	e.out.WriteString("        (local.set $left_byte\n")
	fmt.Fprintf(&e.out,
		"          (i32.load8_u (i32.add (i32.load (i32.add (local.get $left) (i32.const %d)))\n",
		layout.nameOffset+arrayDataOffset)
	e.out.WriteString("            (i32.wrap_i64 (local.get $index)))))\n")
	e.out.WriteString("        (local.set $right_byte\n")
	fmt.Fprintf(&e.out,
		"          (i32.load8_u (i32.add (i32.load (i32.add (local.get $right) (i32.const %d)))\n",
		layout.nameOffset+arrayDataOffset)
	e.out.WriteString("            (i32.wrap_i64 (local.get $index)))))\n")
	e.out.WriteString("        (if (i32.ne (local.get $left_byte) (local.get $right_byte))\n")
	e.out.WriteString("          (then (return (i32.gt_u " +
		"(local.get $left_byte) (local.get $right_byte)))))\n")
	e.out.WriteString("        (local.set $index (i64.add (local.get $index) (i64.const 1)))\n")
	e.out.WriteString("        (br $bytes)))\n")
	e.out.WriteString("    (i64.gt_u (local.get $left_len) (local.get $right_len))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSSortDirEntries emits a stable in-place insertion sort by name.
func (e *emitter) writeFSSortDirEntries(layout fsDirLayout) {
	e.out.WriteString("  (func $__fs_sort_dir_entries (param $array i32)\n")
	e.out.WriteString("    (local $index i64) (local $current i64) (local $left i64)\n")
	e.out.WriteString("    (local $left_ptr i32) (local $right_ptr i32)\n")
	e.out.WriteString("    (local.set $index (i64.const 1))\n")
	e.out.WriteString("    (block $sorted\n")
	e.out.WriteString("      (loop $outer\n")
	fmt.Fprintf(&e.out,
		"        (br_if $sorted (i64.ge_u (local.get $index) "+
			"(i64.load (i32.add (local.get $array) (i32.const %d)))))\n",
		arrayLenOffset)
	e.out.WriteString("        (local.set $current (local.get $index))\n")
	e.out.WriteString("        (block $inserted\n")
	e.out.WriteString("          (loop $inner\n")
	e.out.WriteString("            (br_if $inserted (i64.eqz (local.get $current)))\n")
	e.out.WriteString("            (local.set $left (i64.sub (local.get $current) (i64.const 1)))\n")
	e.out.WriteString("            (local.set $left_ptr (i32.add (i32.load (local.get $array))\n")
	fmt.Fprintf(&e.out,
		"              (i32.wrap_i64 (i64.mul (local.get $left) (i64.const %d)))))\n",
		layout.entrySize)
	e.out.WriteString("            (local.set $right_ptr (i32.add (i32.load (local.get $array))\n")
	fmt.Fprintf(&e.out,
		"              (i32.wrap_i64 (i64.mul (local.get $current) (i64.const %d)))))\n",
		layout.entrySize)
	e.out.WriteString("            (br_if $inserted (i32.eqz (call $__fs_dir_entry_after\n")
	e.out.WriteString("              (local.get $left_ptr) (local.get $right_ptr))))\n")
	e.out.WriteString("            (drop (call $__array_swap (local.get $array)\n")
	fmt.Fprintf(&e.out,
		"              (local.get $left) (local.get $current) (i32.const %d)))\n",
		layout.entrySize)
	e.out.WriteString("            (local.set $current (local.get $left))\n")
	e.out.WriteString("            (br $inner)))\n")
	e.out.WriteString("        (local.set $index (i64.add (local.get $index) (i64.const 1)))\n")
	e.out.WriteString("        (br $outer)))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSReadDirFailHelper centralizes cleanup for every post-open failure.
func (e *emitter) writeFSReadDirFailHelper(layout fsDirLayout) {
	e.out.WriteString("  (func $__fs_read_dir_fail\n")
	e.out.WriteString("      (param $out i32) (param $allocator i32) (param $array i32)\n")
	e.out.WriteString("      (param $buffer i32) (param $capacity i32) " +
		"(param $fd i32) (param $code i64)\n")
	e.out.WriteString("    (call $__fs_dir_entries_free (local.get $allocator) (local.get $array))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (local.get $buffer) (local.get $capacity))\n")
	e.out.WriteString("    (if (i32.ge_s (local.get $fd) (i32.const 0))\n")
	e.out.WriteString("      (then (drop (call $__wasi_fd_close (local.get $fd)))))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 0))\n")
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $out) (i32.const %d)) (local.get $code))\n",
		layout.resultOffset)
	e.out.WriteString("  )\n\n")
}

// writeFSReadDirFailure calls the shared cleanup helper from the main loop.
func (e *emitter) writeFSReadDirFailure(code string, indent string) {
	fmt.Fprintf(&e.out, "%s(call $__fs_read_dir_fail\n", indent)
	fmt.Fprintf(&e.out, "%s  (local.get $out) (local.get $allocator) (local.get $array)\n", indent)
	fmt.Fprintf(&e.out, "%s  (local.get $buffer) (local.get $capacity) (local.get $fd) %s)\n",
		indent, code)
}

// writeFSReadDirBuiltin opens one directory, follows opaque readdir cookies,
// and commits only complete dirent records.
func (e *emitter) writeFSReadDirBuiltin(layout fsDirLayout, codes fsErrorCodes) {
	fmt.Fprintf(&e.out, "  (func $%s\n", fsReadDirBuiltin)
	e.out.WriteString("      (param $out i32) (param $io i32) " +
		"(param $allocator i32) (param $path i32)\n")
	e.out.WriteString("    (local $root i32) (local $fd i32) (local $errno i32) (local $array i32)\n")
	e.out.WriteString("    (local $buffer i32) (local $capacity i32) (local $grown i32)\n")
	e.out.WriteString("    (local $used i32) (local $pos i32) (local $remaining i32)\n")
	e.out.WriteString("    (local $header i32) (local $name i32) (local $name_len i32)\n")
	e.out.WriteString("    (local $record_size i32) (local $cookie i64) (local $next i64)\n")
	e.out.WriteString("    (local $name_copy i32) (local $path_copy i32) (local $path_len i32)\n")
	e.out.WriteString("    (local $separator i32) (local $path_size i64) (local $length i64)\n")
	e.out.WriteString("    (local $item i32) (local $is_dir i32)\n")
	fmt.Fprintf(&e.out,
		"    (local.set $array (i32.add (local.get $out) (i32.const %d)))\n",
		layout.resultOffset)
	e.out.WriteString("    (i32.store (local.get $array) (i32.const 0))\n")
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $array) (i32.const %d)) (i64.const 0))\n",
		arrayLenOffset)
	fmt.Fprintf(&e.out,
		"    (i64.store (i32.add (local.get $array) (i32.const %d)) (i64.const 0))\n",
		arrayCapacityOffset)
	e.writeFSGuard(codes.ioFailing, codes.permissionDenied)
	e.out.WriteString("    (local.set $root (local.get $fd))\n")
	e.out.WriteString("    (local.set $errno (call $__wasi_path_open\n")
	fmt.Fprintf(&e.out,
		"      (local.get $root) (i32.const %d) (i32.load (local.get $path))\n",
		wasiLookupSymlinkFollow)
	e.out.WriteString("      (i32.load (i32.add (local.get $path) (i32.const 4)))\n")
	fmt.Fprintf(&e.out,
		"      (i32.const %d) (i64.const %d) (i64.const 0) (i32.const 0) (i32.const %d)))\n",
		wasiOpenDirectory, wasiRightFDReaddir, fsOpenedFDOffset)
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSStoredErrno("        ")
	e.out.WriteString("        (return)))\n")
	fmt.Fprintf(&e.out,
		"    (local.set $fd (i32.load (i32.const %d)))\n",
		fsOpenedFDOffset)
	fmt.Fprintf(&e.out,
		"    (local.set $capacity (i32.const %d))\n",
		fsReaddirBufferSize)
	e.out.WriteString("    (local.set $buffer\n")
	e.out.WriteString("      (call $__allocator_alloc " +
		"(local.get $allocator) (local.get $capacity)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $buffer))\n")
	e.out.WriteString("      (then\n")
	e.writeFSReadDirFailure(fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "        ")
	e.out.WriteString("        (return)))\n")
	e.writeFSReadDirLoop(layout, codes)
	e.out.WriteString("    (local.set $errno (call $__wasi_fd_close (local.get $fd)))\n")
	e.out.WriteString("    (local.set $fd (i32.const -1))\n")
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeFSReadDirFailure("(call $__fs_error (local.get $errno))", "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("      (local.get $buffer) (local.get $capacity))\n")
	e.out.WriteString("    (call $__fs_sort_dir_entries (local.get $array))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeFSReadDirLoop parses packed preview1 dirents and retries truncated
// records from the last complete opaque cookie.
func (e *emitter) writeFSReadDirLoop(layout fsDirLayout, codes fsErrorCodes) {
	e.out.WriteString("    (block $dir_done\n")
	e.out.WriteString("      (loop $dir_read\n")
	e.writeFSReadDirHostRead(codes)
	e.writeFSReadDirParseLoop(layout, codes)
	e.out.WriteString("        (br $dir_read)))\n")
}

// writeFSReadDirHostRead fills the temporary buffer and validates bufused.
func (e *emitter) writeFSReadDirHostRead(codes fsErrorCodes) {
	e.out.WriteString("        (local.set $errno (call $__wasi_fd_readdir\n")
	fmt.Fprintf(&e.out,
		"          (local.get $fd) (local.get $buffer) "+
			"(local.get $capacity) (local.get $cookie) (i32.const %d)))\n",
		fsReaddirUsedOffset)
	e.out.WriteString("        (if (local.get $errno)\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadDirFailure(fmt.Sprintf("(i64.const %d)", codes.readFailed), "            ")
	e.out.WriteString("            (return)))\n")
	fmt.Fprintf(&e.out,
		"        (local.set $used (i32.load (i32.const %d)))\n",
		fsReaddirUsedOffset)
	e.out.WriteString("        (if (i32.gt_u (local.get $used) (local.get $capacity))\n")
	e.out.WriteString("          (then\n")
	e.writeFSReadDirFailure(fmt.Sprintf("(i64.const %d)", codes.readFailed), "            ")
	e.out.WriteString("            (return)))\n")
	e.out.WriteString("        (br_if $dir_done (i32.eqz (local.get $used)))\n")
}

// writeFSReadDirParseLoop walks complete packed entries from one host fill.
func (e *emitter) writeFSReadDirParseLoop(layout fsDirLayout, codes fsErrorCodes) {
	e.out.WriteString("        (local.set $pos (i32.const 0))\n")
	e.out.WriteString("        (block $dir_refill\n")
	e.out.WriteString("          (loop $dir_parse\n")
	e.out.WriteString("            (br_if $dir_refill (i32.ge_u " +
		"(local.get $pos) (local.get $used)))\n")
	e.writeFSReadDirRecordHeader(codes)
	e.writeFSReadDirCookie(codes)
	e.writeFSReadDirEntry(layout, codes)
	e.out.WriteString("            (local.set $pos (i32.add " +
		"(local.get $pos) (local.get $record_size)))\n")
	e.out.WriteString("            (br $dir_parse)))\n")
}

// writeFSReadDirRecordHeader validates one record and grows for a long name.
func (e *emitter) writeFSReadDirRecordHeader(codes fsErrorCodes) {
	e.out.WriteString("            (local.set $remaining (i32.sub " +
		"(local.get $used) (local.get $pos)))\n")
	fmt.Fprintf(&e.out,
		"            (if (i32.lt_u (local.get $remaining) (i32.const %d))\n",
		fsDirentHeaderSize)
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (if (i32.lt_u (local.get $used) (local.get $capacity))\n")
	e.out.WriteString("                  (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.readFailed), "                    ")
	e.out.WriteString("                    (return)))\n")
	e.out.WriteString("                (br $dir_refill)))\n")
	e.out.WriteString("            (local.set $header (i32.add " +
		"(local.get $buffer) (local.get $pos)))\n")
	fmt.Fprintf(&e.out,
		"            (local.set $name_len (i32.load "+
			"(i32.add (local.get $header) (i32.const %d))))\n",
		fsDirentNameLen)
	e.out.WriteString("            (if (i32.gt_u (local.get $name_len) (i32.const 2147483616))\n")
	e.out.WriteString("              (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                ")
	e.out.WriteString("                (return)))\n")
	fmt.Fprintf(&e.out,
		"            (local.set $record_size (i32.add "+
			"(i32.const %d) (local.get $name_len)))\n",
		fsDirentHeaderSize)
	e.out.WriteString("            (if (i32.gt_u (local.get $record_size) (local.get $capacity))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (local.set $grown (call $__allocator_realloc\n")
	e.out.WriteString("                  (local.get $allocator) (local.get $buffer)\n")
	e.out.WriteString("                  (local.get $capacity) (local.get $record_size)))\n")
	e.out.WriteString("                (if (i32.eqz (local.get $grown))\n")
	e.out.WriteString("                  (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                    ")
	e.out.WriteString("                    (return)))\n")
	e.out.WriteString("                (local.set $buffer (local.get $grown))\n")
	e.out.WriteString("                (local.set $capacity (local.get $record_size))\n")
	e.out.WriteString("                (br $dir_refill)))\n")
	e.out.WriteString("            (if (i32.lt_u (local.get $remaining) (local.get $record_size))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (if (i32.lt_u (local.get $used) (local.get $capacity))\n")
	e.out.WriteString("                  (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.readFailed), "                    ")
	e.out.WriteString("                    (return)))\n")
	e.out.WriteString("                (br $dir_refill)))\n")
}

// writeFSReadDirCookie commits progress only after a complete record.
func (e *emitter) writeFSReadDirCookie(codes fsErrorCodes) {
	e.out.WriteString("            (local.set $next (i64.load (local.get $header)))\n")
	e.out.WriteString("            (if (i64.eq (local.get $next) (local.get $cookie))\n")
	e.out.WriteString("              (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.readFailed), "                ")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (local.set $cookie (local.get $next))\n")
	fmt.Fprintf(&e.out,
		"            (local.set $name (i32.add (local.get $header) (i32.const %d)))\n",
		fsDirentNameOffset)
}

// writeFSReadDirEntry filters dot entries and constructs one owned DirEntry.
func (e *emitter) writeFSReadDirEntry(layout fsDirLayout, codes fsErrorCodes) {
	e.writeFSReadDirDotFilter()
	e.writeFSReadDirNameCopy(codes)
	e.writeFSReadDirPathCopy(codes)
	e.writeFSReadDirStat()
	e.writeFSReadDirCommit(layout, codes)
}

// writeFSReadDirDotFilter removes the two traversal entries when present.
func (e *emitter) writeFSReadDirDotFilter() {
	e.out.WriteString("            (if (i32.or\n")
	e.out.WriteString("                  (i32.and (i32.eq (local.get $name_len) (i32.const 1))\n")
	e.out.WriteString("                    (i32.eq (i32.load8_u (local.get $name)) (i32.const 46)))\n")
	e.out.WriteString("                  (i32.and (i32.eq (local.get $name_len) (i32.const 2))\n")
	e.out.WriteString("                    (i32.and (i32.eq " +
		"(i32.load8_u (local.get $name)) (i32.const 46))\n")
	e.out.WriteString("                      (i32.eq (i32.load8_u " +
		"(i32.add (local.get $name) (i32.const 1))) (i32.const 46)))))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (local.set $pos (i32.add " +
		"(local.get $pos) (local.get $record_size)))\n")
	e.out.WriteString("                (br $dir_parse)))\n")
}

// writeFSReadDirNameCopy owns the host-provided name before the next fill.
func (e *emitter) writeFSReadDirNameCopy(codes fsErrorCodes) {
	e.out.WriteString("            (local.set $name_copy\n")
	e.out.WriteString("              (call $__allocator_alloc " +
		"(local.get $allocator) (local.get $name_len)))\n")
	e.out.WriteString("            (if (i32.eqz (local.get $name_copy))\n")
	e.out.WriteString("              (then\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                ")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (memory.copy (local.get $name_copy) " +
		"(local.get $name) (local.get $name_len))\n")
}

// writeFSReadDirPathCopy joins the requested path and name into owned bytes.
func (e *emitter) writeFSReadDirPathCopy(codes fsErrorCodes) {
	e.out.WriteString("            (local.set $path_len\n")
	e.out.WriteString("              (i32.load (i32.add (local.get $path) (i32.const 4))))\n")
	e.out.WriteString("            (local.set $separator (i32.const 0))\n")
	e.out.WriteString("            (if (local.get $path_len)\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (if (i32.ne (i32.load8_u\n")
	e.out.WriteString("                      (i32.add (i32.load (local.get $path))\n")
	e.out.WriteString("                        (i32.sub (local.get $path_len) (i32.const 1))))\n")
	e.out.WriteString("                    (i32.const 47))\n")
	e.out.WriteString("                  (then (local.set $separator (i32.const 1))))))\n")
	e.out.WriteString("            (local.set $path_size\n")
	e.out.WriteString("              (i64.add (i64.extend_i32_u (local.get $path_len))\n")
	e.out.WriteString("                (i64.add (i64.extend_i32_u (local.get $separator))\n")
	e.out.WriteString("                  (i64.extend_i32_u (local.get $name_len)))))\n")
	e.out.WriteString("            (if (i64.gt_u (local.get $path_size) (i64.const 2147483640))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("                  (local.get $name_copy) (local.get $name_len))\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                ")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (local.set $path_copy (call $__allocator_alloc\n")
	e.out.WriteString("              (local.get $allocator) (i32.wrap_i64 (local.get $path_size))))\n")
	e.out.WriteString("            (if (i32.eqz (local.get $path_copy))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("                  (local.get $name_copy) (local.get $name_len))\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                ")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (memory.copy (local.get $path_copy)\n")
	e.out.WriteString("              (i32.load (local.get $path)) (local.get $path_len))\n")
	e.out.WriteString("            (if (local.get $separator)\n")
	e.out.WriteString("              (then (i32.store8 " +
		"(i32.add (local.get $path_copy) (local.get $path_len))\n")
	e.out.WriteString("                (i32.const 47))))\n")
	e.out.WriteString("            (memory.copy\n")
	e.out.WriteString("              (i32.add (local.get $path_copy)\n")
	e.out.WriteString("                (i32.add (local.get $path_len) (local.get $separator)))\n")
	e.out.WriteString("              (local.get $name_copy) (local.get $name_len))\n")
}

// writeFSReadDirStat follows symlinks within the granted root capability.
func (e *emitter) writeFSReadDirStat() {
	e.out.WriteString("            (local.set $errno (call $__wasi_path_filestat_get\n")
	fmt.Fprintf(&e.out,
		"              (local.get $root) (i32.const %d) (local.get $path_copy)\n",
		wasiLookupSymlinkFollow)
	e.out.WriteString("              (i32.wrap_i64 (local.get $path_size))\n")
	fmt.Fprintf(&e.out, "              (i32.const %d)))\n", fsFilestatOffset)
	fmt.Fprintf(&e.out,
		"            (local.set $is_dir (i32.and "+
			"(i32.eqz (local.get $errno)) (i32.eq "+
			"(i32.load8_u (i32.const %d)) (i32.const %d))))\n",
		fsFilestatOffset+fsFilestatTypeOffset, wasiFiletypeDirectory)
}

// writeFSReadDirCommit reserves the result slot and transfers both Strings.
func (e *emitter) writeFSReadDirCommit(layout fsDirLayout, codes fsErrorCodes) {
	fmt.Fprintf(&e.out,
		"            (local.set $length (i64.load "+
			"(i32.add (local.get $array) (i32.const %d))))\n",
		arrayLenOffset)
	e.out.WriteString("            (if (i32.eqz (call $__array_reserve\n")
	fmt.Fprintf(&e.out,
		"                  (local.get $allocator) (local.get $array) "+
			"(i64.add (local.get $length) (i64.const 1)) (i32.const %d)))\n",
		layout.entrySize)
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("                  (local.get $name_copy) (local.get $name_len))\n")
	e.out.WriteString("                (call $__allocator_free (local.get $allocator)\n")
	e.out.WriteString("                  (local.get $path_copy) " +
		"(i32.wrap_i64 (local.get $path_size)))\n")
	e.writeFSReadDirFailure(
		fmt.Sprintf("(i64.const %d)", codes.outOfMemory), "                ")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (local.set $item (i32.add (i32.load (local.get $array))\n")
	fmt.Fprintf(&e.out,
		"              (i32.wrap_i64 (i64.mul (local.get $length) (i64.const %d)))))\n",
		layout.entrySize)
	fmt.Fprintf(&e.out,
		"            (i32.store (i32.add (local.get $item) (i32.const %d)) (local.get $name_copy))\n",
		layout.nameOffset+arrayDataOffset)
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $item) (i32.const %d)) "+
			"(i64.extend_i32_u (local.get $name_len)))\n",
		layout.nameOffset+arrayLenOffset)
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $item) (i32.const %d)) "+
			"(i64.extend_i32_u (local.get $name_len)))\n",
		layout.nameOffset+arrayCapacityOffset)
	fmt.Fprintf(&e.out,
		"            (i32.store (i32.add (local.get $item) (i32.const %d)) (local.get $path_copy))\n",
		layout.pathOffset+arrayDataOffset)
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $item) (i32.const %d)) (local.get $path_size))\n",
		layout.pathOffset+arrayLenOffset)
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $item) (i32.const %d)) (local.get $path_size))\n",
		layout.pathOffset+arrayCapacityOffset)
	fmt.Fprintf(&e.out,
		"            (i32.store8 (i32.add (local.get $item) (i32.const %d)) (local.get $is_dir))\n",
		layout.isDirOffset)
	fmt.Fprintf(&e.out,
		"            (i64.store (i32.add (local.get $array) (i32.const %d)) "+
			"(i64.add (local.get $length) (i64.const 1)))\n",
		arrayLenOffset)
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
