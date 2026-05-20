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

@.kizu.host.manifest_path = private unnamed_addr constant [18 x i8] c"selfhost/kizu.toml"
@.kizu.host.source_dir = private unnamed_addr constant [12 x i8] c"selfhost/src"
@.kizu.host.target_dir = private unnamed_addr constant [15 x i8] c"target/selfhost"
@.kizu.host.status_path = private unnamed_addr constant [33 x i8] c"target/selfhost/host-smoke.status"
@.kizu.host.stdout = private unnamed_addr constant [17 x i8] c"selfhost-host:ok\0A"
@.kizu.host.stderr = private unnamed_addr constant [19 x i8] c"selfhost-host:diag\0A"
@.kizu.host.env_name = private unnamed_addr constant [15 x i8] c"KIZU_HOST_SMOKE"

declare ptr @kizu_host_page_allocator()
declare ptr @kizu_host_io_blocking()
declare ptr @kizu_host_alloc(ptr, i64)
declare void @kizu_host_free(ptr, ptr)
declare void @kizu_host_fs_exists(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_fs_metadata(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_fs_read_dir(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_fs_read_file(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_fs_write_file(ptr, ptr, %kizu.slice.u8, %kizu.slice.u8)
declare void @kizu_host_fs_create_dir(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_fs_rename(ptr, ptr, %kizu.slice.u8, %kizu.slice.u8)
declare void @kizu_host_io_write_stdout(ptr, ptr, %kizu.slice.u8)
declare void @kizu_host_io_write_stderr(ptr, ptr, %kizu.slice.u8)
declare i64 @kizu_host_process_arg_count()
declare void @kizu_host_process_arg(ptr, i64)
declare void @kizu_host_process_env(ptr, %kizu.slice.u8)
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
  %slot = alloca %kizu.error.bool
  call void @kizu_host_fs_exists(ptr %slot, ptr %raw_io, %kizu.slice.u8 %path)
  %result = load %kizu.error.bool, ptr %slot
  ret %kizu.error.bool %result
}

define %kizu.error.metadata @kizu_rt_fs_metadata(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.metadata
  call void @kizu_host_fs_metadata(ptr %slot, ptr %raw_io, %kizu.slice.u8 %path)
  %result = load %kizu.error.metadata, ptr %slot
  ret %kizu.error.metadata %result
}

define %kizu.error.owned @kizu_rt_fs_read_dir(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.owned
  call void @kizu_host_fs_read_dir(ptr %slot, ptr %raw_io, %kizu.slice.u8 %path)
  %result = load %kizu.error.owned, ptr %slot
  ret %kizu.error.owned %result
}

define %kizu.error.slice.u8 @kizu_rt_fs_read_file(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.slice.u8
  call void @kizu_host_fs_read_file(ptr %slot, ptr %raw_io, %kizu.slice.u8 %path)
  %result = load %kizu.error.slice.u8, ptr %slot
  ret %kizu.error.slice.u8 %result
}

define %kizu.error.void @kizu_rt_fs_write_file(
  %kizu.owned %io,
  %kizu.slice.u8 %path,
  %kizu.slice.u8 %bytes
) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.void
  call void @kizu_host_fs_write_file(
    ptr %slot,
    ptr %raw_io,
    %kizu.slice.u8 %path,
    %kizu.slice.u8 %bytes
  )
  %result = load %kizu.error.void, ptr %slot
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_fs_create_dir(%kizu.owned %io, %kizu.slice.u8 %path) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.void
  call void @kizu_host_fs_create_dir(ptr %slot, ptr %raw_io, %kizu.slice.u8 %path)
  %result = load %kizu.error.void, ptr %slot
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_fs_rename(
  %kizu.owned %io,
  %kizu.slice.u8 %from,
  %kizu.slice.u8 %to
) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.void
  call void @kizu_host_fs_rename(
    ptr %slot,
    ptr %raw_io,
    %kizu.slice.u8 %from,
    %kizu.slice.u8 %to
  )
  %result = load %kizu.error.void, ptr %slot
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_io_write_stdout(%kizu.owned %io, %kizu.slice.u8 %bytes) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.void
  call void @kizu_host_io_write_stdout(
    ptr %slot,
    ptr %raw_io,
    %kizu.slice.u8 %bytes
  )
  %result = load %kizu.error.void, ptr %slot
  ret %kizu.error.void %result
}

define %kizu.error.void @kizu_rt_io_write_stderr(%kizu.owned %io, %kizu.slice.u8 %bytes) {
entry:
  %raw_io = extractvalue %kizu.owned %io, 0
  %slot = alloca %kizu.error.void
  call void @kizu_host_io_write_stderr(
    ptr %slot,
    ptr %raw_io,
    %kizu.slice.u8 %bytes
  )
  %result = load %kizu.error.void, ptr %slot
  ret %kizu.error.void %result
}

define i64 @kizu_rt_process_arg_count() {
entry:
  %count = call i64 @kizu_host_process_arg_count()
  ret i64 %count
}

define %kizu.error.slice.u8 @kizu_rt_process_arg(i64 %index) {
entry:
  %slot = alloca %kizu.error.slice.u8
  call void @kizu_host_process_arg(ptr %slot, i64 %index)
  %result = load %kizu.error.slice.u8, ptr %slot
  ret %kizu.error.slice.u8 %result
}

define %kizu.error.slice.u8 @kizu_rt_process_env(%kizu.slice.u8 %name) {
entry:
  %slot = alloca %kizu.error.slice.u8
  call void @kizu_host_process_env(ptr %slot, %kizu.slice.u8 %name)
  %result = load %kizu.error.slice.u8, ptr %slot
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
  %manifest_ptr = getelementptr inbounds [18 x i8], ptr @.kizu.host.manifest_path, i64 0, i64 0
  %manifest_path = insertvalue %kizu.slice.u8 poison, ptr %manifest_ptr, 0
  %manifest_slice = insertvalue %kizu.slice.u8 %manifest_path, i64 18, 1
  %source_ptr = getelementptr inbounds [12 x i8], ptr @.kizu.host.source_dir, i64 0, i64 0
  %source_path = insertvalue %kizu.slice.u8 poison, ptr %source_ptr, 0
  %source_slice = insertvalue %kizu.slice.u8 %source_path, i64 12, 1
  %target_ptr = getelementptr inbounds [15 x i8], ptr @.kizu.host.target_dir, i64 0, i64 0
  %target_path = insertvalue %kizu.slice.u8 poison, ptr %target_ptr, 0
  %target_slice = insertvalue %kizu.slice.u8 %target_path, i64 15, 1
  %status_ptr = getelementptr inbounds [33 x i8], ptr @.kizu.host.status_path, i64 0, i64 0
  %status_path = insertvalue %kizu.slice.u8 poison, ptr %status_ptr, 0
  %status_slice = insertvalue %kizu.slice.u8 %status_path, i64 33, 1
  %stdout_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.host.stdout, i64 0, i64 0
  %stdout_text = insertvalue %kizu.slice.u8 poison, ptr %stdout_ptr, 0
  %stdout_slice = insertvalue %kizu.slice.u8 %stdout_text, i64 17, 1
  %stderr_ptr = getelementptr inbounds [19 x i8], ptr @.kizu.host.stderr, i64 0, i64 0
  %stderr_text = insertvalue %kizu.slice.u8 poison, ptr %stderr_ptr, 0
  %stderr_slice = insertvalue %kizu.slice.u8 %stderr_text, i64 19, 1
  %env_ptr = getelementptr inbounds [15 x i8], ptr @.kizu.host.env_name, i64 0, i64 0
  %env_name = insertvalue %kizu.slice.u8 poison, ptr %env_ptr, 0
  %env_slice = insertvalue %kizu.slice.u8 %env_name, i64 15, 1
  %exists = call %kizu.error.bool @kizu_rt_fs_exists(
    %kizu.owned %io,
    %kizu.slice.u8 %manifest_slice
  )
  %metadata = call %kizu.error.metadata @kizu_rt_fs_metadata(
    %kizu.owned %io,
    %kizu.slice.u8 %manifest_slice
  )
  %entries = call %kizu.error.owned @kizu_rt_fs_read_dir(
    %kizu.owned %io,
    %kizu.slice.u8 %source_slice
  )
  %file = call %kizu.error.slice.u8 @kizu_rt_fs_read_file(
    %kizu.owned %io,
    %kizu.slice.u8 %manifest_slice
  )
  %mkdir = call %kizu.error.void @kizu_rt_fs_create_dir(
    %kizu.owned %io,
    %kizu.slice.u8 %target_slice
  )
  %write = call %kizu.error.void @kizu_rt_fs_write_file(
    %kizu.owned %io,
    %kizu.slice.u8 %status_slice,
    %kizu.slice.u8 %stdout_slice
  )
  %rename = call %kizu.error.void @kizu_rt_fs_rename(
    %kizu.owned %io,
    %kizu.slice.u8 %status_slice,
    %kizu.slice.u8 %status_slice
  )
  %stdout = call %kizu.error.void @kizu_rt_io_write_stdout(
    %kizu.owned %io,
    %kizu.slice.u8 %stdout_slice
  )
  %stderr = call %kizu.error.void @kizu_rt_io_write_stderr(
    %kizu.owned %io,
    %kizu.slice.u8 %stderr_slice
  )
  %argc = call i64 @kizu_rt_process_arg_count()
  %argv = call %kizu.error.slice.u8 @kizu_rt_process_arg(i64 0)
  %env = call %kizu.error.slice.u8 @kizu_rt_process_env(%kizu.slice.u8 %env_slice)
  %exit_code = call i64 @kizu_rt_process_exit_code(i64 0)
  ret i64 %exit_code
}
