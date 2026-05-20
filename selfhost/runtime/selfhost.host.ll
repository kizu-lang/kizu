; kizu selfhost host capabilities ll v0
source_filename = "target/selfhost/selfhost.host"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.fs.metadata = type { i64, i1 }
%kizu.error.bool = type { i1, i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }
%kizu.error.metadata = type { i1, %kizu.fs.metadata, %kizu.slice.u8 }
%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }
%kizu.error.void = type { i1, %kizu.slice.u8 }

declare ptr @kizu_host_page_allocator()
declare ptr @kizu_host_io_blocking()
declare ptr @kizu_host_alloc(ptr, i64)
declare void @kizu_host_free(ptr, ptr)
declare %kizu.error.bool @kizu_host_fs_exists(ptr, %kizu.slice.u8)
declare %kizu.error.metadata @kizu_host_fs_metadata(ptr, %kizu.slice.u8)
declare %kizu.error.owned @kizu_host_fs_read_dir(ptr, %kizu.slice.u8)
declare %kizu.error.slice.u8 @kizu_host_fs_read_file(ptr, %kizu.slice.u8)
declare %kizu.error.void @kizu_host_fs_write_file(ptr, %kizu.slice.u8, %kizu.slice.u8)
declare %kizu.error.void @kizu_host_fs_create_dir(ptr, %kizu.slice.u8)
declare %kizu.error.void @kizu_host_fs_rename(ptr, %kizu.slice.u8, %kizu.slice.u8)
declare %kizu.error.void @kizu_host_io_write_stdout(ptr, %kizu.slice.u8)
declare %kizu.error.void @kizu_host_io_write_stderr(ptr, %kizu.slice.u8)
declare i64 @kizu_host_process_arg_count()
declare %kizu.error.slice.u8 @kizu_host_process_arg(i64)
declare %kizu.error.slice.u8 @kizu_host_process_env(%kizu.slice.u8)
declare i64 @kizu_host_process_exit_code(i64)
declare void @kizu_host_process_exit(i64) noreturn
declare void @kizu_host_trap(%kizu.slice.u8) noreturn

define %kizu.owned @kizu_rt_mem_page_allocator() {
entry:
  %raw = call ptr @kizu_host_page_allocator()
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.owned @kizu_rt_io_blocking() {
entry:
  %raw = call ptr @kizu_host_io_blocking()
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define ptr @kizu_rt_alloc(ptr %allocator, i64 %bytes) {
entry:
  %raw = call ptr @kizu_host_alloc(ptr %allocator, i64 %bytes)
  ret ptr %raw
}

define void @kizu_rt_free(ptr %allocator, ptr %value) {
entry:
  call void @kizu_host_free(ptr %allocator, ptr %value)
  ret void
}

define %kizu.error.bool @kizu_rt_fs_exists(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.bool @kizu_host_fs_exists(ptr %raw_io, %kizu.slice.u8 %path)
  ret %kizu.error.bool %result
}

define %kizu.error.metadata @kizu_rt_fs_metadata(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.metadata @kizu_host_fs_metadata(ptr %raw_io, %kizu.slice.u8 %path)
  ret %kizu.error.metadata %result
}

define %kizu.error.owned @kizu_rt_fs_read_dir(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.owned @kizu_host_fs_read_dir(ptr %raw_io, %kizu.slice.u8 %path)
  ret %kizu.error.owned %result
}

define %kizu.error.slice.u8 @kizu_rt_fs_read_file(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.slice.u8 @kizu_host_fs_read_file(ptr %raw_io, %kizu.slice.u8 %path)
  ret %kizu.error.slice.u8 %result
}

define %kizu.error.void @kizu_rt_fs_write_file(
  %kizu.owned %io,
  %kizu.slice.u8 %path,
  %kizu.slice.u8 %bytes
) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.void @kizu_host_fs_write_file(
    ptr %raw_io,
    %kizu.slice.u8 %path,
    %kizu.slice.u8 %bytes
  )
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_fs_create_dir(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.void @kizu_host_fs_create_dir(ptr %raw_io, %kizu.slice.u8 %path)
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_fs_rename(
  %kizu.owned %io,
  %kizu.slice.u8 %from,
  %kizu.slice.u8 %to
) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.void @kizu_host_fs_rename(
    ptr %raw_io,
    %kizu.slice.u8 %from,
    %kizu.slice.u8 %to
  )
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_io_write_stdout(%kizu.owned %io, %kizu.slice.u8 %bytes) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.void @kizu_host_io_write_stdout(
    ptr %raw_io,
    %kizu.slice.u8 %bytes
  )
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_io_write_stderr(%kizu.owned %io, %kizu.slice.u8 %bytes) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %result = call %kizu.error.void @kizu_host_io_write_stderr(
    ptr %raw_io,
    %kizu.slice.u8 %bytes
  )
  ret %kizu.error.void %result
}

define i64 @kizu_rt_process_arg_count() {
entry:
  %count = call i64 @kizu_host_process_arg_count()
  ret i64 %count
}

define %kizu.error.slice.u8 @kizu_rt_process_arg(i64 %index) {
entry:
  %result = call %kizu.error.slice.u8 @kizu_host_process_arg(i64 %index)
  ret %kizu.error.slice.u8 %result
}

define %kizu.error.slice.u8 @kizu_rt_process_env(%kizu.slice.u8 %name) {
entry:
  %result = call %kizu.error.slice.u8 @kizu_host_process_env(%kizu.slice.u8 %name)
  ret %kizu.error.slice.u8 %result
}

define i64 @kizu_rt_process_exit_code(i64 %code) {
entry:
  %value = call i64 @kizu_host_process_exit_code(i64 %code)
  ret i64 %value
}

define void @kizu_rt_process_exit(i64 %code) noreturn {
entry:
  call void @kizu_host_process_exit(i64 %code)
  unreachable
}

define void @kizu_rt_owned_deinit(%kizu.owned %value) {
entry:
  ret void
}

define void @kizu_rt_trap(%kizu.slice.u8 %message) noreturn {
entry:
  call void @kizu_host_trap(%kizu.slice.u8 %message)
  unreachable
}

define i64 @kizu_selfhost__host_capability_smoke() {
entry:
  %allocator = call %kizu.owned @kizu_rt_mem_page_allocator()
  %io = call %kizu.owned @kizu_rt_io_blocking()
  %exists = call %kizu.error.bool @kizu_rt_fs_exists(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %metadata = call %kizu.error.metadata @kizu_rt_fs_metadata(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %entries = call %kizu.error.owned @kizu_rt_fs_read_dir(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %file = call %kizu.error.slice.u8 @kizu_rt_fs_read_file(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %write = call %kizu.error.void @kizu_rt_fs_write_file(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer,
    %kizu.slice.u8 zeroinitializer
  )
  %mkdir = call %kizu.error.void @kizu_rt_fs_create_dir(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %rename = call %kizu.error.void @kizu_rt_fs_rename(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer,
    %kizu.slice.u8 zeroinitializer
  )
  %stdout = call %kizu.error.void @kizu_rt_io_write_stdout(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %stderr = call %kizu.error.void @kizu_rt_io_write_stderr(
    %kizu.owned %io,
    %kizu.slice.u8 zeroinitializer
  )
  %argc = call i64 @kizu_rt_process_arg_count()
  %argv = call %kizu.error.slice.u8 @kizu_rt_process_arg(i64 0)
  %env = call %kizu.error.slice.u8 @kizu_rt_process_env(%kizu.slice.u8 zeroinitializer)
  %exit_code = call i64 @kizu_rt_process_exit_code(i64 0)
  ret i64 %exit_code
}
