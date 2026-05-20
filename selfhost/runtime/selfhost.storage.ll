; kizu selfhost runtime storage ll v0
source_filename = "target/selfhost/selfhost.storage"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.error.void = type { i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.rt.array = type { ptr, i64, i64 }
%kizu.rt.string = type { ptr, i64, i64 }
%kizu.rt.map = type { ptr, i1, i64 }
%kizu.rt.diagnostics = type { ptr, i64 }

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
  ret i64 0
}
