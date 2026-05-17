package native

// RuntimeLLVM returns the minimal target stdlib runtime symbols used by LLVM output.
func RuntimeLLVM() string {
	return `; Kizu native runtime
@.kizu.fmt.str = private unnamed_addr constant [6 x i8] c"%.*s\0A\00"
@.kizu.fmt.bytes = private unnamed_addr constant [5 x i8] c"%.*s\00"
@.kizu.fmt.i64 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.kizu.true = private unnamed_addr constant [5 x i8] c"true\00"
@.kizu.false = private unnamed_addr constant [6 x i8] c"false\00"
@.kizu.empty = private unnamed_addr constant [1 x i8] c"\00"
@.kizu.mode.rb = private unnamed_addr constant [3 x i8] c"rb\00"
@.kizu.mode.wb = private unnamed_addr constant [3 x i8] c"wb\00"
@.kizu.dot = private unnamed_addr constant [2 x i8] c".\00"
@kizu_argc = global i64 0
@kizu_argv = global ptr null
%kizu.String = type { ptr, i64 }

declare i32 @printf(ptr, ...)
declare i32 @strcmp(ptr, ptr)
declare i32 @strncmp(ptr, ptr, i64)
declare i64 @strlen(ptr)
declare ptr @fopen(ptr, ptr)
declare i32 @fseek(ptr, i64, i32)
declare i64 @ftell(ptr)
declare void @rewind(ptr)
declare ptr @malloc(i64)
declare ptr @realloc(ptr, i64)
declare i64 @fread(ptr, i64, i64, ptr)
declare i64 @fwrite(ptr, i64, i64, ptr)
declare i32 @fclose(ptr)
declare ptr @memcpy(ptr, ptr, i64)
declare ptr @opendir(ptr)
declare i32 @closedir(ptr)
declare i32 @mkdir(ptr, i16)
declare i32 @rmdir(ptr)
declare i32 @remove(ptr)
` + runtimePrintLLVM() + runtimeProcessLLVM() + runtimeMemoryLLVM() + runtimePathLLVM() +
		runtimeFileLLVM() + runtimeArrayLLVM() + runtimeStringLLVM()
}

// runtimePrintLLVM returns print helpers for the native target.
func runtimePrintLLVM() string {
	return `
define void @kizu_print_string(ptr %text, i64 %len) {
entry:
  %n = trunc i64 %len to i32
  call i32 (ptr, ...) @printf(ptr @.kizu.fmt.str, i32 %n, ptr %text)
  ret void
}

define void @kizu_write_stdout(ptr %text, i64 %len) {
entry:
  %n = trunc i64 %len to i32
  call i32 (ptr, ...) @printf(ptr @.kizu.fmt.bytes, i32 %n, ptr %text)
  ret void
}

define void @kizu_print_int(i64 %value) {
entry:
  call i32 (ptr, ...) @printf(ptr @.kizu.fmt.i64, i64 %value)
  ret void
}

define void @kizu_print_bool(i1 %value) {
entry:
  br i1 %value, label %is_true, label %is_false
is_true:
  call void @kizu_print_string(ptr @.kizu.true, i64 4)
  ret void
is_false:
  call void @kizu_print_string(ptr @.kizu.false, i64 5)
  ret void
}
`
}

// runtimeStringLLVM returns owned String helpers for the native target.
func runtimeStringLLVM() string {
	return runtimeStringNewLLVM() + runtimeStringAppendLLVM() + runtimeStringViewLLVM()
}

// runtimeStringNewLLVM returns String allocation helpers.
func runtimeStringNewLLVM() string {
	return `
define ptr @kizu_string_new() {
entry:
  %str = call ptr @malloc(i64 16)
  %missing = icmp eq ptr %str, null
  br i1 %missing, label %fail, label %init
init:
  %bytes_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 0
  store ptr @.kizu.empty, ptr %bytes_slot
  %len_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 1
  store i64 0, ptr %len_slot
  ret ptr %str
fail:
  ret ptr null
}
`
}

// runtimeStringAppendLLVM returns String mutation helpers.
func runtimeStringAppendLLVM() string {
	return `
define ptr @kizu_string_append_bytes(ptr %str, ptr %bytes) {
entry:
  %missing = icmp eq ptr %str, null
  br i1 %missing, label %fail, label %append
append:
  %bytes_len = call i64 @strlen(ptr %bytes)
  %len_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 1
  %old_len = load i64, ptr %len_slot
  %new_len = add i64 %old_len, %bytes_len
  %alloc_size = add i64 %new_len, 1
  %data = call ptr @malloc(i64 %alloc_size)
  %data_missing = icmp eq ptr %data, null
  br i1 %data_missing, label %fail, label %copy_old
copy_old:
  %bytes_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 0
  %old_bytes = load ptr, ptr %bytes_slot
  call ptr @memcpy(ptr %data, ptr %old_bytes, i64 %old_len)
  %tail = getelementptr i8, ptr %data, i64 %old_len
  call ptr @memcpy(ptr %tail, ptr %bytes, i64 %bytes_len)
  %zero = getelementptr i8, ptr %data, i64 %new_len
  store i8 0, ptr %zero
  store ptr %data, ptr %bytes_slot
  store i64 %new_len, ptr %len_slot
  ret ptr null
fail:
  ret ptr inttoptr (i64 1 to ptr)
}

define ptr @kizu_string_append_byte(ptr %str, i8 %byte) {
entry:
  %missing = icmp eq ptr %str, null
  br i1 %missing, label %fail, label %append
append:
  %len_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 1
  %old_len = load i64, ptr %len_slot
  %new_len = add i64 %old_len, 1
  %alloc_size = add i64 %new_len, 1
  %data = call ptr @malloc(i64 %alloc_size)
  %data_missing = icmp eq ptr %data, null
  br i1 %data_missing, label %fail, label %copy_old
copy_old:
  %bytes_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 0
  %old_bytes = load ptr, ptr %bytes_slot
  call ptr @memcpy(ptr %data, ptr %old_bytes, i64 %old_len)
  %slot = getelementptr i8, ptr %data, i64 %old_len
  store i8 %byte, ptr %slot
  %zero = getelementptr i8, ptr %data, i64 %new_len
  store i8 0, ptr %zero
  store ptr %data, ptr %bytes_slot
  store i64 %new_len, ptr %len_slot
  ret ptr null
fail:
  ret ptr inttoptr (i64 1 to ptr)
}
`
}

// runtimeStringViewLLVM returns String view and cleanup helpers.
func runtimeStringViewLLVM() string {
	return `
define ptr @kizu_string_as_bytes(ptr %str) {
entry:
  %missing = icmp eq ptr %str, null
  br i1 %missing, label %fail, label %load
load:
  %bytes_slot = getelementptr inbounds %kizu.String, ptr %str, i64 0, i32 0
  %bytes = load ptr, ptr %bytes_slot
  ret ptr %bytes
fail:
  ret ptr @.kizu.empty
}

define void @kizu_string_deinit(ptr %str) {
entry:
  ret void
}
`
}

// runtimeProcessLLVM returns argv helpers for the native target.
func runtimeProcessLLVM() string {
	return `
define i64 @kizu_process_arg_count() {
entry:
  %argc = load i64, ptr @kizu_argc
  %has_args = icmp sgt i64 %argc, 1
  br i1 %has_args, label %some, label %none
some:
  %count = sub i64 %argc, 1
  ret i64 %count
none:
  ret i64 0
}

define ptr @kizu_process_arg(i64 %index) {
entry:
  %argv = load ptr, ptr @kizu_argv
  %actual = add i64 %index, 1
  %slot = getelementptr ptr, ptr %argv, i64 %actual
  %value = load ptr, ptr %slot
  ret ptr %value
}
`
}

// runtimeMemoryLLVM returns byte helpers for the native target.
func runtimeMemoryLLVM() string {
	return `
define i1 @kizu_bytes_equal(ptr %left, ptr %right) {
entry:
  %cmp = call i32 @strcmp(ptr %left, ptr %right)
  %ok = icmp eq i32 %cmp, 0
  ret i1 %ok
}

define i1 @kizu_bytes_starts_with(ptr %text, ptr %prefix) {
entry:
  %prefix_len = call i64 @strlen(ptr %prefix)
  %cmp = call i32 @strncmp(ptr %text, ptr %prefix, i64 %prefix_len)
  %ok = icmp eq i32 %cmp, 0
  ret i1 %ok
}

define i64 @kizu_bytes_len(ptr %text) {
entry:
  %len = call i64 @strlen(ptr %text)
  ret i64 %len
}

define i8 @kizu_byte_at(ptr %text, i64 %index) {
entry:
  %slot = getelementptr i8, ptr %text, i64 %index
  %value = load i8, ptr %slot
  ret i8 %value
}

define ptr @kizu_bytes_slice(ptr %text, i64 %start, i64 %end) {
entry:
  %len = sub i64 %end, %start
  %alloc_size = add i64 %len, 1
  %buffer = call ptr @malloc(i64 %alloc_size)
  %missing = icmp eq ptr %buffer, null
  br i1 %missing, label %fail, label %copy
copy:
  %slot = getelementptr i8, ptr %text, i64 %start
  call ptr @memcpy(ptr %buffer, ptr %slot, i64 %len)
  %tail = getelementptr i8, ptr %buffer, i64 %len
  store i8 0, ptr %tail
  ret ptr %buffer
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimePathLLVM returns path helpers for the native target.
func runtimePathLLVM() string {
	return runtimePathJoinLLVM() + runtimePathCleanLLVM() + runtimePathBasenameLLVM() +
		runtimePathDirnameLLVM() + runtimePathExtensionLLVM()
}

// runtimePathJoinLLVM returns path join support for the native target.
func runtimePathJoinLLVM() string {
	return `
define ptr @kizu_path_join(ptr %left, ptr %right) {
entry:
  %left_len = call i64 @strlen(ptr %left)
  %right_len = call i64 @strlen(ptr %right)
  %with_sep = add i64 %left_len, 1
  %text_len = add i64 %with_sep, %right_len
  %alloc_size = add i64 %text_len, 1
  %buffer = call ptr @malloc(i64 %alloc_size)
  %missing = icmp eq ptr %buffer, null
  br i1 %missing, label %fail, label %copy_left
copy_left:
  call ptr @memcpy(ptr %buffer, ptr %left, i64 %left_len)
  %sep = getelementptr i8, ptr %buffer, i64 %left_len
  store i8 47, ptr %sep
  %right_start = getelementptr i8, ptr %buffer, i64 %with_sep
  call ptr @memcpy(ptr %right_start, ptr %right, i64 %right_len)
  %tail = getelementptr i8, ptr %buffer, i64 %text_len
  store i8 0, ptr %tail
  ret ptr %buffer
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimePathCleanLLVM returns path clean support for the native target.
func runtimePathCleanLLVM() string {
	return runtimePathCleanScanLLVM() + runtimePathCleanCopyLLVM()
}

// runtimePathCleanScanLLVM returns the scan half of path clean support.
func runtimePathCleanScanLLVM() string {
	return `
define ptr @kizu_path_clean(ptr %path) {
entry:
  %len = call i64 @strlen(ptr %path)
  br label %scan
scan:
  %i = phi i64 [ 0, %entry ], [ %next_i, %advance ]
  %room = icmp ult i64 %i, %len
  br i1 %room, label %check_slash, label %done
check_slash:
  %slot = getelementptr i8, ptr %path, i64 %i
  %ch = load i8, ptr %slot
  %is_slash = icmp eq i8 %ch, 47
  br i1 %is_slash, label %check_dot1, label %advance
check_dot1:
  %dot1_i = add i64 %i, 1
  %dot1_in = icmp ult i64 %dot1_i, %len
  br i1 %dot1_in, label %load_dot1, label %advance
load_dot1:
  %dot1_slot = getelementptr i8, ptr %path, i64 %dot1_i
  %dot1 = load i8, ptr %dot1_slot
  %is_dot1 = icmp eq i8 %dot1, 46
  br i1 %is_dot1, label %check_dot2, label %advance
check_dot2:
  %dot2_i = add i64 %i, 2
  %dot2_in = icmp ult i64 %dot2_i, %len
  br i1 %dot2_in, label %load_dot2, label %advance
load_dot2:
  %dot2_slot = getelementptr i8, ptr %path, i64 %dot2_i
  %dot2 = load i8, ptr %dot2_slot
  %is_dot2 = icmp eq i8 %dot2, 46
  br i1 %is_dot2, label %check_tail, label %advance
check_tail:
  %tail_slash_i = add i64 %i, 3
  %tail_in = icmp ult i64 %tail_slash_i, %len
  br i1 %tail_in, label %load_tail, label %advance
load_tail:
  %tail_slot = getelementptr i8, ptr %path, i64 %tail_slash_i
  %tail = load i8, ptr %tail_slot
  %is_tail_slash = icmp eq i8 %tail, 47
  br i1 %is_tail_slash, label %find_prev, label %advance
find_prev:
  %prev_start = sub i64 %i, 1
  br label %prev_scan
prev_scan:
  %j = phi i64 [ %prev_start, %find_prev ], [ %prev_next, %prev_step ]
  %before_start = icmp slt i64 %j, 0
  br i1 %before_start, label %copy_root, label %prev_check
prev_check:
  %prev_slot = getelementptr i8, ptr %path, i64 %j
  %prev_ch = load i8, ptr %prev_slot
  %prev_is_slash = icmp eq i8 %prev_ch, 47
  br i1 %prev_is_slash, label %copy_join, label %prev_step
prev_step:
  %prev_next = sub i64 %j, 1
  br label %prev_scan
copy_join:
  %prefix_len = add i64 %j, 1
  br label %copy_clean
copy_root:
  br label %copy_clean
`
}

// runtimePathCleanCopyLLVM returns the copy half of path clean support.
func runtimePathCleanCopyLLVM() string {
	return `
copy_clean:
  %prefix = phi i64 [ %prefix_len, %copy_join ], [ 0, %copy_root ]
  %suffix_start = add i64 %i, 4
  %suffix_len = sub i64 %len, %suffix_start
  %result_len = add i64 %prefix, %suffix_len
  %alloc_size = add i64 %result_len, 1
  %buffer = call ptr @malloc(i64 %alloc_size)
  %missing = icmp eq ptr %buffer, null
  br i1 %missing, label %fail, label %copy_prefix
copy_prefix:
  call ptr @memcpy(ptr %buffer, ptr %path, i64 %prefix)
  %suffix_ptr = getelementptr i8, ptr %path, i64 %suffix_start
  %dst_suffix = getelementptr i8, ptr %buffer, i64 %prefix
  call ptr @memcpy(ptr %dst_suffix, ptr %suffix_ptr, i64 %suffix_len)
  %end = getelementptr i8, ptr %buffer, i64 %result_len
  store i8 0, ptr %end
  ret ptr %buffer
advance:
  %next_i = add i64 %i, 1
  br label %scan
done:
  ret ptr %path
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimePathBasenameLLVM returns path basename support for the native target.
func runtimePathBasenameLLVM() string {
	return `
define ptr @kizu_path_basename(ptr %path) {
entry:
  %len = call i64 @strlen(ptr %path)
  br label %scan
scan:
  %i = phi i64 [ %len, %entry ], [ %next, %step ]
  %at_start = icmp eq i64 %i, 0
  br i1 %at_start, label %whole, label %check
check:
  %next = sub i64 %i, 1
  %slot = getelementptr i8, ptr %path, i64 %next
  %ch = load i8, ptr %slot
  %is_slash = icmp eq i8 %ch, 47
  br i1 %is_slash, label %copy_tail, label %step
step:
  br label %scan
copy_tail:
  %start = add i64 %next, 1
  %tail_len = sub i64 %len, %start
  %tail = getelementptr i8, ptr %path, i64 %start
  %result = call ptr @kizu_bytes_slice(ptr %tail, i64 0, i64 %tail_len)
  ret ptr %result
whole:
  ret ptr %path
}
`
}

// runtimePathDirnameLLVM returns path dirname support for the native target.
func runtimePathDirnameLLVM() string {
	return `
define ptr @kizu_path_dirname(ptr %path) {
entry:
  %len = call i64 @strlen(ptr %path)
  br label %scan
scan:
  %i = phi i64 [ %len, %entry ], [ %next, %step ]
  %at_start = icmp eq i64 %i, 0
  br i1 %at_start, label %dot, label %check
check:
  %next = sub i64 %i, 1
  %slot = getelementptr i8, ptr %path, i64 %next
  %ch = load i8, ptr %slot
  %is_slash = icmp eq i8 %ch, 47
  br i1 %is_slash, label %copy_head, label %step
step:
  br label %scan
copy_head:
  %empty = icmp eq i64 %next, 0
  br i1 %empty, label %root, label %copy
copy:
  %result = call ptr @kizu_bytes_slice(ptr %path, i64 0, i64 %next)
  ret ptr %result
root:
  ret ptr @.kizu.empty
dot:
  ret ptr @.kizu.dot
}
`
}

// runtimePathExtensionLLVM returns path extension support for the native target.
func runtimePathExtensionLLVM() string {
	return `
define ptr @kizu_path_extension(ptr %path) {
entry:
  %len = call i64 @strlen(ptr %path)
  br label %scan
scan:
  %i = phi i64 [ %len, %entry ], [ %next, %step ]
  %at_start = icmp eq i64 %i, 0
  br i1 %at_start, label %none, label %check
check:
  %next = sub i64 %i, 1
  %slot = getelementptr i8, ptr %path, i64 %next
  %ch = load i8, ptr %slot
  %is_dot = icmp eq i8 %ch, 46
  br i1 %is_dot, label %copy_tail, label %slash_check
slash_check:
  %is_slash = icmp eq i8 %ch, 47
  br i1 %is_slash, label %none, label %step
step:
  br label %scan
copy_tail:
  %tail_len = sub i64 %len, %next
  %tail = getelementptr i8, ptr %path, i64 %next
  %result = call ptr @kizu_bytes_slice(ptr %tail, i64 0, i64 %tail_len)
  ret ptr %result
none:
  ret ptr @.kizu.empty
}
`
}

// runtimeFileLLVM returns filesystem helpers for the native target.
func runtimeFileLLVM() string {
	return runtimeReadFileLLVM() + runtimeFileExistsLLVM() + runtimeWriteFileLLVM() +
		runtimeFileMetadataLLVM() + runtimeFileMutationLLVM()
}

// runtimeReadFileLLVM returns file read support for the native target.
func runtimeReadFileLLVM() string {
	return `
define ptr @kizu_read_file(ptr %path) {
entry:
  %file = call ptr @fopen(ptr %path, ptr @.kizu.mode.rb)
  %missing = icmp eq ptr %file, null
  br i1 %missing, label %fail, label %opened
opened:
  call i32 @fseek(ptr %file, i64 0, i32 2)
  %size = call i64 @ftell(ptr %file)
  call void @rewind(ptr %file)
  %alloc_size = add i64 %size, 1
  %buffer = call ptr @malloc(i64 %alloc_size)
  %alloc_missing = icmp eq ptr %buffer, null
  br i1 %alloc_missing, label %close_fail, label %read
read:
  call i64 @fread(ptr %buffer, i64 1, i64 %size, ptr %file)
  %end = getelementptr i8, ptr %buffer, i64 %size
  store i8 0, ptr %end
  call i32 @fclose(ptr %file)
  ret ptr %buffer
close_fail:
  call i32 @fclose(ptr %file)
  ret ptr @.kizu.empty
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimeFileExistsLLVM returns file existence support for the native target.
func runtimeFileExistsLLVM() string {
	return `
define i1 @kizu_file_exists(ptr %path) {
entry:
  %file = call ptr @fopen(ptr %path, ptr @.kizu.mode.rb)
  %missing = icmp eq ptr %file, null
  br i1 %missing, label %no, label %yes
yes:
  call i32 @fclose(ptr %file)
  ret i1 true
no:
  ret i1 false
}
`
}

// runtimeWriteFileLLVM returns file write support for the native target.
func runtimeWriteFileLLVM() string {
	return `
define ptr @kizu_write_file(ptr %path, ptr %bytes) {
entry:
  %file = call ptr @fopen(ptr %path, ptr @.kizu.mode.wb)
  %missing = icmp eq ptr %file, null
  br i1 %missing, label %fail, label %opened
opened:
  %size = call i64 @strlen(ptr %bytes)
  call i64 @fwrite(ptr %bytes, i64 1, i64 %size, ptr %file)
  call i32 @fclose(ptr %file)
  ret ptr @.kizu.empty
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimeFileMetadataLLVM returns file metadata support for the native target.
func runtimeFileMetadataLLVM() string {
	return `
define ptr @kizu_file_metadata(ptr %path) {
entry:
  %metadata = call ptr @malloc(i64 16)
  %missing_metadata = icmp eq ptr %metadata, null
  br i1 %missing_metadata, label %fail, label %stat_file
stat_file:
  %file = call ptr @fopen(ptr %path, ptr @.kizu.mode.rb)
  %missing_file = icmp eq ptr %file, null
  br i1 %missing_file, label %stat_dir, label %opened
opened:
  call i32 @fseek(ptr %file, i64 0, i32 2)
  %size = call i64 @ftell(ptr %file)
  call i32 @fclose(ptr %file)
  %size_slot = getelementptr i64, ptr %metadata, i64 0
  store i64 %size, ptr %size_slot
  %is_dir_slot = getelementptr i8, ptr %metadata, i64 8
  store i1 false, ptr %is_dir_slot
  ret ptr %metadata
stat_dir:
  %dir = call ptr @opendir(ptr %path)
  %missing_dir = icmp eq ptr %dir, null
  br i1 %missing_dir, label %zero, label %opened_dir
opened_dir:
  call i32 @closedir(ptr %dir)
  %dir_size_slot = getelementptr i64, ptr %metadata, i64 0
  store i64 0, ptr %dir_size_slot
  %dir_flag_slot = getelementptr i8, ptr %metadata, i64 8
  store i1 true, ptr %dir_flag_slot
  ret ptr %metadata
zero:
  %zero_size_slot = getelementptr i64, ptr %metadata, i64 0
  store i64 0, ptr %zero_size_slot
  %zero_flag_slot = getelementptr i8, ptr %metadata, i64 8
  store i1 false, ptr %zero_flag_slot
  ret ptr %metadata
fail:
  ret ptr @.kizu.empty
}
`
}

// runtimeFileMutationLLVM returns file and directory mutation support.
func runtimeFileMutationLLVM() string {
	return `
define ptr @kizu_create_dir(ptr %path) {
entry:
  call i32 @mkdir(ptr %path, i16 493)
  ret ptr @.kizu.empty
}

define ptr @kizu_remove_dir(ptr %path) {
entry:
  call i32 @rmdir(ptr %path)
  ret ptr @.kizu.empty
}

define ptr @kizu_remove_file(ptr %path) {
entry:
  call i32 @remove(ptr %path)
  ret ptr @.kizu.empty
}
`
}

// runtimeArrayLLVM returns growable pointer-array helpers for the native target.
func runtimeArrayLLVM() string {
	return `
define ptr @kizu_array_new() {
entry:
  %array = call ptr @malloc(i64 24)
  %len_slot = getelementptr i64, ptr %array, i64 0
  store i64 0, ptr %len_slot
  %cap_slot = getelementptr i64, ptr %array, i64 1
  store i64 1024, ptr %cap_slot
  %data = call ptr @malloc(i64 8192)
  %data_slot = getelementptr ptr, ptr %array, i64 2
  store ptr %data, ptr %data_slot
  ret ptr %array
}

define void @kizu_array_append(ptr %array, ptr %value) {
entry:
  %len_slot = getelementptr i64, ptr %array, i64 0
  %len = load i64, ptr %len_slot
  %cap_slot = getelementptr i64, ptr %array, i64 1
  %cap = load i64, ptr %cap_slot
  %full = icmp sge i64 %len, %cap
  br i1 %full, label %grow, label %store
grow:
  %next_cap = mul i64 %cap, 2
  %next_bytes = mul i64 %next_cap, 8
  %old_data_slot = getelementptr ptr, ptr %array, i64 2
  %old_data = load ptr, ptr %old_data_slot
  %next_data = call ptr @realloc(ptr %old_data, i64 %next_bytes)
  store ptr %next_data, ptr %old_data_slot
  store i64 %next_cap, ptr %cap_slot
  br label %store
store:
  %data_slot = getelementptr ptr, ptr %array, i64 2
  %data = load ptr, ptr %data_slot
  %slot = getelementptr ptr, ptr %data, i64 %len
  store ptr %value, ptr %slot
  %next = add i64 %len, 1
  store i64 %next, ptr %len_slot
  ret void
}

define ptr @kizu_array_at(ptr %array, i64 %index) {
entry:
  %data_slot = getelementptr ptr, ptr %array, i64 2
  %data = load ptr, ptr %data_slot
  %slot = getelementptr ptr, ptr %data, i64 %index
  %value = load ptr, ptr %slot
  ret ptr %value
}

define i64 @kizu_array_len(ptr %array) {
entry:
  %len_slot = getelementptr i64, ptr %array, i64 0
  %len = load i64, ptr %len_slot
  ret i64 %len
}
`
}
