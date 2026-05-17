package native

// RuntimeLLVM returns the minimal target stdlib runtime symbols used by LLVM output.
func RuntimeLLVM() string {
	return `; Kizu native runtime
@.kizu.fmt.str = private unnamed_addr constant [6 x i8] c"%.*s\0A\00"
@.kizu.fmt.i64 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.kizu.true = private unnamed_addr constant [5 x i8] c"true\00"
@.kizu.false = private unnamed_addr constant [6 x i8] c"false\00"

declare i32 @printf(ptr, ...)

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
