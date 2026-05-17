package native

// RuntimeLLVM returns the minimal target stdlib runtime symbols used by LLVM output.
func RuntimeLLVM() string {
	return `; Kizu native runtime
@.kizu.fmt.str = private unnamed_addr constant [6 x i8] c"%.*s\0A\00"
@.kizu.fmt.i64 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.kizu.true = private unnamed_addr constant [5 x i8] c"true\00"
@.kizu.false = private unnamed_addr constant [6 x i8] c"false\00"
@.kizu.empty = private unnamed_addr constant [1 x i8] c"\00"
@.kizu.mode.rb = private unnamed_addr constant [3 x i8] c"rb\00"
@kizu_argc = global i64 0
@kizu_argv = global ptr null

declare i32 @printf(ptr, ...)
declare i32 @strcmp(ptr, ptr)
declare i64 @strlen(ptr)
declare ptr @fopen(ptr, ptr)
declare i32 @fseek(ptr, i64, i32)
declare i64 @ftell(ptr)
declare void @rewind(ptr)
declare ptr @malloc(i64)
declare ptr @realloc(ptr, i64)
declare i64 @fread(ptr, i64, i64, ptr)
declare i32 @fclose(ptr)
declare ptr @memcpy(ptr, ptr, i64)
` + runtimePrintLLVM() + runtimeProcessLLVM() + runtimeMemoryLLVM() + runtimeFileLLVM() +
		runtimeArrayLLVM()
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

// runtimeFileLLVM returns filesystem helpers for the native target.
func runtimeFileLLVM() string {
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
