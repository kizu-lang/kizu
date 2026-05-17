package native

// RuntimeLLVM returns the minimal target stdlib runtime symbols used by LLVM output.
func RuntimeLLVM() string {
	return `; Kizu native runtime
@.kizu.fmt.str = private unnamed_addr constant [6 x i8] c"%.*s\0A\00"
@.kizu.fmt.i64 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.kizu.true = private unnamed_addr constant [5 x i8] c"true\00"
@.kizu.false = private unnamed_addr constant [6 x i8] c"false\00"
@.kizu.empty = private unnamed_addr constant [1 x i8] c"\00"
@kizu_argc = global i64 0
@kizu_argv = global ptr null

declare i32 @printf(ptr, ...)
declare i32 @strcmp(ptr, ptr)

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

define i1 @kizu_bytes_equal(ptr %left, ptr %right) {
entry:
  %cmp = call i32 @strcmp(ptr %left, ptr %right)
  %ok = icmp eq i32 %cmp, 0
  ret i1 %ok
}

define ptr @kizu_read_file(ptr %path) {
entry:
  ret ptr @.kizu.empty
}
`
}
