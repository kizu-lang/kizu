; kizu selfhost runtime storage ll v0
source_filename = "target/selfhost/selfhost.storage"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.handle = type { ptr, i64 }
%kizu.error.void = type { i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }
%kizu.rt.array = type { ptr, i64, i64 }
%kizu.rt.string = type { ptr, i64, i64 }
%kizu.rt.map = type { ptr, i1, i64 }
%kizu.rt.diagnostics = type { ptr, i64 }
%kizu.rt.arena = type { ptr, i64, i64, i1 }

@.kizu.rt.arena_invalid_handle = private unnamed_addr constant [20 x i8] c"invalid arena handle"

declare ptr @kizu_rt_alloc(ptr, i64)
declare void @kizu_rt_free(ptr, ptr)

define %kizu.owned @kizu_rt_array_new(%kizu.owned %allocator, i64 %element_size) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 24)
  %allocator_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  store i64 0, ptr %cap_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_array_append(%kizu.owned %array, %kizu.slice.u8 %element) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %len_field
  %next = add i64 %current, 1
  store i64 %next, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
}

define i64 @kizu_rt_array_len(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  ret i64 %len
}

define %kizu.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index) {
entry:
  ret %kizu.slice.u8 zeroinitializer
}

define void @kizu_rt_array_deinit(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %allocator_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  ret void
}

define %kizu.owned @kizu_rt_string_new(%kizu.owned %allocator) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 24)
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  store i64 0, ptr %cap_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_string_append_bytes(%kizu.owned %string, %kizu.slice.u8 %bytes) {
entry:
  %byte_len = extractvalue %kizu.slice.u8 %bytes, 1
  %raw = extractvalue %kizu.owned %string, 0
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %len_field
  %next = add i64 %current, %byte_len
  store i64 %next, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
}

define %kizu.error.void @kizu_rt_string_append_byte(%kizu.owned %string, i8 %byte) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %len_field
  %next = add i64 %current, 1
  store i64 %next, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
}

define i64 @kizu_rt_string_len(%kizu.owned %string) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  ret i64 %len
}

define %kizu.slice.u8 @kizu_rt_string_as_bytes(%kizu.owned %string) {
entry:
  ret %kizu.slice.u8 zeroinitializer
}

define void @kizu_rt_string_deinit(%kizu.owned %string) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  ret void
}

define %kizu.owned @kizu_rt_map_new(%kizu.owned %allocator) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 24)
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %found_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  store i1 false, ptr %found_field
  %value_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  store i64 0, ptr %value_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_map_insert(%kizu.owned %map, %kizu.slice.u8 %key, i64 %value) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %found_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  store i1 true, ptr %found_field
  %value_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  store i64 %value, ptr %value_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
}

define i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %found_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %found = load i1, ptr %found_field
  ret i1 %found
}

define %kizu.error.i64 @kizu_rt_map_get_i64(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %value_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %value = load i64, ptr %value_field
  %ok = insertvalue %kizu.error.i64 poison, i1 true, 0
  %with_value = insertvalue %kizu.error.i64 %ok, i64 %value, 1
  %result = insertvalue %kizu.error.i64 %with_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.i64 %result
}

define void @kizu_rt_map_deinit(%kizu.owned %map) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  ret void
}

define %kizu.owned @kizu_rt_diagnostic_buffer_new(%kizu.owned %allocator) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 16)
  %allocator_field = getelementptr inbounds %kizu.rt.diagnostics, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %count_field = getelementptr inbounds %kizu.rt.diagnostics, ptr %raw, i32 0, i32 1
  store i64 0, ptr %count_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_diagnostic_push(%kizu.owned %buffer, %kizu.slice.u8 %message) {
entry:
  %raw = extractvalue %kizu.owned %buffer, 0
  %count_field = getelementptr inbounds %kizu.rt.diagnostics, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %count_field
  %next = add i64 %current, 1
  store i64 %next, ptr %count_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
}

define void @kizu_rt_diagnostic_buffer_deinit(%kizu.owned %buffer) {
entry:
  %raw = extractvalue %kizu.owned %buffer, 0
  %allocator_field = getelementptr inbounds %kizu.rt.diagnostics, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  ret void
}

define %kizu.owned @kizu_rt_arena_new(%kizu.owned %allocator, i64 %element_size) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 32)
  %allocator_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %len_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 1
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 2
  store i64 0, ptr %cap_field
  %deinit_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 3
  store i1 false, ptr %deinit_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %value) {
entry:
  %raw = extractvalue %kizu.owned %arena, 0
  %len_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %len_field
  %next = add i64 %current, 1
  store i64 %next, ptr %len_field
  %handle_base = insertvalue %kizu.handle poison, ptr %raw, 0
  %handle = insertvalue %kizu.handle %handle_base, i64 %current, 1
  ret %kizu.handle %handle
}

define %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %handle) {
entry:
  %raw = extractvalue %kizu.owned %arena, 0
  %handle_arena = extractvalue %kizu.handle %handle, 0
  %same_arena = icmp eq ptr %raw, %handle_arena
  %index = extractvalue %kizu.handle %handle, 1
  %non_negative = icmp sge i64 %index, 0
  %len_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  %in_bounds = icmp slt i64 %index, %len
  %provenance_ok = and i1 %same_arena, %non_negative
  %ok = and i1 %provenance_ok, %in_bounds
  br i1 %ok, label %valid, label %invalid
valid:
  %valid_ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %valid_value = insertvalue %kizu.error.slice.u8 %valid_ok, %kizu.slice.u8 zeroinitializer, 1
  %valid_result = insertvalue %kizu.error.slice.u8 %valid_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.slice.u8 %valid_result
invalid:
  %message_ptr = getelementptr inbounds [20 x i8], ptr @.kizu.rt.arena_invalid_handle, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 20, 1
  %invalid_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.slice.u8 %invalid_ok, %kizu.slice.u8 zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.slice.u8 %invalid_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.slice.u8 %invalid_result
}

define void @kizu_rt_arena_deinit(%kizu.owned %arena) {
entry:
  %raw = extractvalue %kizu.owned %arena, 0
  %deinit_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 3
  store i1 true, ptr %deinit_field
  %allocator_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  ret void
}

define i64 @kizu_selfhost__runtime_storage_smoke() {
entry:
  %array = call %kizu.owned @kizu_rt_array_new(%kizu.owned zeroinitializer, i64 16)
  %array_append = call %kizu.error.void @kizu_rt_array_append(%kizu.owned %array, %kizu.slice.u8 zeroinitializer)
  %array_len = call i64 @kizu_rt_array_len(%kizu.owned %array)
  call void @kizu_rt_array_deinit(%kizu.owned %array)
  %string = call %kizu.owned @kizu_rt_string_new(%kizu.owned zeroinitializer)
  %string_append = call %kizu.error.void @kizu_rt_string_append_bytes(%kizu.owned %string, %kizu.slice.u8 zeroinitializer)
  %string_view = call %kizu.slice.u8 @kizu_rt_string_as_bytes(%kizu.owned %string)
  call void @kizu_rt_string_deinit(%kizu.owned %string)
  %map = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer)
  %map_insert = call %kizu.error.void @kizu_rt_map_insert(%kizu.owned %map, %kizu.slice.u8 zeroinitializer, i64 0)
  %map_contains = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 zeroinitializer)
  call void @kizu_rt_map_deinit(%kizu.owned %map)
  %diagnostics = call %kizu.owned @kizu_rt_diagnostic_buffer_new(%kizu.owned zeroinitializer)
  %diagnostic_push = call %kizu.error.void @kizu_rt_diagnostic_push(%kizu.owned %diagnostics, %kizu.slice.u8 zeroinitializer)
  call void @kizu_rt_diagnostic_buffer_deinit(%kizu.owned %diagnostics)
  %arena = call %kizu.owned @kizu_rt_arena_new(%kizu.owned zeroinitializer, i64 24)
  %node_id = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 zeroinitializer)
  %node = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %node_id)
  %node_ok = extractvalue %kizu.error.slice.u8 %node, 0
  call void @kizu_rt_arena_deinit(%kizu.owned %arena)
  ret i64 0
}
