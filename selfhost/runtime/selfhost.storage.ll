; kizu selfhost runtime storage ll v0
source_filename = "target/selfhost/selfhost.storage"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.handle = type { ptr, i64 }
%kizu.error.void = type { i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }
%kizu.rt.array = type { ptr, i64, i64 }
%kizu.rt.string = type { ptr, ptr, i64, i64 }
%kizu.rt.map = type { ptr, i1, i64 }
%kizu.rt.diagnostics = type { ptr, i64 }
%kizu.rt.arena = type { ptr, i64, i64, i1 }

@.kizu.rt.arena_invalid_handle = private unnamed_addr constant [20 x i8] c"invalid arena handle"
@.kizu.rt.allocation_failed = private unnamed_addr constant [17 x i8] c"allocation failed"
@.kizu.rt.invalid_slice = private unnamed_addr constant [13 x i8] c"invalid slice"
@.kizu.rt.string_smoke = private unnamed_addr constant [3 x i8] c"kiz"

declare ptr @kizu_rt_alloc(ptr, i64)
declare void @kizu_rt_free(ptr, ptr)
declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1 immarg)

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
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 32)
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  store ptr null, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 3
  store i64 0, ptr %cap_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_string_append_bytes(%kizu.owned %string, %kizu.slice.u8 %bytes) {
entry:
  %byte_ptr = extractvalue %kizu.slice.u8 %bytes, 0
  %byte_len = extractvalue %kizu.slice.u8 %bytes, 1
  %len_negative = icmp slt i64 %byte_len, 0
  br i1 %len_negative, label %invalid_slice, label %valid_length
valid_length:
  %has_bytes = icmp sgt i64 %byte_len, 0
  br i1 %has_bytes, label %validate_ptr, label %empty
empty:
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
validate_ptr:
  %ptr_ok = icmp ne ptr %byte_ptr, null
  br i1 %ptr_ok, label %append, label %invalid_slice
append:
  %raw = extractvalue %kizu.owned %string, 0
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %current_data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  %current = load i64, ptr %len_field
  %current_valid = icmp sge i64 %current, 0
  br i1 %current_valid, label %length_check, label %invalid_slice
length_check:
  %max_delta = sub i64 9223372036854775807, %current
  %fits = icmp sle i64 %byte_len, %max_delta
  br i1 %fits, label %allocate, label %invalid_slice
allocate:
  %next = add i64 %current, %byte_len
  %new_data = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %next)
  %allocated = icmp ne ptr %new_data, null
  br i1 %allocated, label %copy_old_check, label %allocation_failed
copy_old_check:
  %has_old = icmp sgt i64 %current, 0
  br i1 %has_old, label %copy_old, label %copy_new
copy_old:
  call void @llvm.memcpy.p0.p0.i64(ptr %new_data, ptr %current_data, i64 %current, i1 false)
  br label %copy_new
copy_new:
  %dest = getelementptr i8, ptr %new_data, i64 %current
  call void @llvm.memcpy.p0.p0.i64(ptr %dest, ptr %byte_ptr, i64 %byte_len, i1 false)
  %has_old_data = icmp ne ptr %current_data, null
  br i1 %has_old_data, label %free_old, label %store
free_old:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %current_data)
  br label %store
store:
  store ptr %new_data, ptr %data_field
  store i64 %next, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 3
  store i64 %next, ptr %cap_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
allocation_failed:
  %message_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.allocation_failed, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 17, 1
  %failed_base = insertvalue %kizu.error.void poison, i1 false, 0
  %failed = insertvalue %kizu.error.void %failed_base, %kizu.slice.u8 %message, 1
  ret %kizu.error.void %failed
invalid_slice:
  %invalid_message_ptr = getelementptr inbounds [13 x i8], ptr @.kizu.rt.invalid_slice, i64 0, i64 0
  %invalid_message_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_message_ptr, 0
  %invalid_message = insertvalue %kizu.slice.u8 %invalid_message_base, i64 13, 1
  %invalid_base = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_result = insertvalue %kizu.error.void %invalid_base, %kizu.slice.u8 %invalid_message, 1
  ret %kizu.error.void %invalid_result
}

define %kizu.error.void @kizu_rt_string_append_byte(%kizu.owned %string, i8 %byte) {
entry:
  %slot = alloca i8
  store i8 %byte, ptr %slot
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %slot, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 1, 1
  %result = call %kizu.error.void @kizu_rt_string_append_bytes(
    %kizu.owned %string,
    %kizu.slice.u8 %slice
  )
  ret %kizu.error.void %result
}

define i64 @kizu_rt_string_len(%kizu.owned %string) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  ret i64 %len
}

define %kizu.slice.u8 @kizu_rt_string_as_bytes(%kizu.owned %string) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %data_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %data, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %len, 1
  ret %kizu.slice.u8 %slice
}

define void @kizu_rt_string_deinit(%kizu.owned %string) {
entry:
  %raw = extractvalue %kizu.owned %string, 0
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %has_data = icmp ne ptr %data, null
  br i1 %has_data, label %free_data, label %free_string
free_data:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %data)
  br label %free_string
free_string:
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

define i1 @kizu_selfhost__runtime_invalid_slice_message_ok(%kizu.slice.u8 %message) {
entry:
  %ptr = extractvalue %kizu.slice.u8 %message, 0
  %len = extractvalue %kizu.slice.u8 %message, 1
  %len_ok = icmp eq i64 %len, 13
  %ptr_ok = icmp ne ptr %ptr, null
  %base_ok = and i1 %len_ok, %ptr_ok
  br i1 %base_ok, label %bytes, label %fail
bytes:
  %b0p = getelementptr i8, ptr %ptr, i64 0
  %b0 = load i8, ptr %b0p
  %b1p = getelementptr i8, ptr %ptr, i64 1
  %b1 = load i8, ptr %b1p
  %b2p = getelementptr i8, ptr %ptr, i64 2
  %b2 = load i8, ptr %b2p
  %b3p = getelementptr i8, ptr %ptr, i64 3
  %b3 = load i8, ptr %b3p
  %b4p = getelementptr i8, ptr %ptr, i64 4
  %b4 = load i8, ptr %b4p
  %b5p = getelementptr i8, ptr %ptr, i64 5
  %b5 = load i8, ptr %b5p
  %b6p = getelementptr i8, ptr %ptr, i64 6
  %b6 = load i8, ptr %b6p
  %b7p = getelementptr i8, ptr %ptr, i64 7
  %b7 = load i8, ptr %b7p
  %b8p = getelementptr i8, ptr %ptr, i64 8
  %b8 = load i8, ptr %b8p
  %b9p = getelementptr i8, ptr %ptr, i64 9
  %b9 = load i8, ptr %b9p
  %b10p = getelementptr i8, ptr %ptr, i64 10
  %b10 = load i8, ptr %b10p
  %b11p = getelementptr i8, ptr %ptr, i64 11
  %b11 = load i8, ptr %b11p
  %b12p = getelementptr i8, ptr %ptr, i64 12
  %b12 = load i8, ptr %b12p
  %b0_ok = icmp eq i8 %b0, 105
  %b1_ok = icmp eq i8 %b1, 110
  %b2_ok = icmp eq i8 %b2, 118
  %b3_ok = icmp eq i8 %b3, 97
  %b4_ok = icmp eq i8 %b4, 108
  %b5_ok = icmp eq i8 %b5, 105
  %b6_ok = icmp eq i8 %b6, 100
  %b7_ok = icmp eq i8 %b7, 32
  %b8_ok = icmp eq i8 %b8, 115
  %b9_ok = icmp eq i8 %b9, 108
  %b10_ok = icmp eq i8 %b10, 105
  %b11_ok = icmp eq i8 %b11, 99
  %b12_ok = icmp eq i8 %b12, 101
  %p0 = and i1 %b0_ok, %b1_ok
  %p1 = and i1 %b2_ok, %b3_ok
  %p2 = and i1 %b4_ok, %b5_ok
  %p3 = and i1 %b6_ok, %b7_ok
  %p4 = and i1 %b8_ok, %b9_ok
  %p5 = and i1 %b10_ok, %b11_ok
  %p6 = and i1 %p0, %p1
  %p7 = and i1 %p2, %p3
  %p8 = and i1 %p4, %p5
  %p9 = and i1 %p6, %p7
  %p10 = and i1 %p8, %b12_ok
  %ok = and i1 %p9, %p10
  ret i1 %ok
fail:
  ret i1 false
}

define i64 @kizu_selfhost__runtime_string_invalid_smoke() {
entry:
  %string = call %kizu.owned @kizu_rt_string_new(%kizu.owned zeroinitializer)
  %negative_base = insertvalue %kizu.slice.u8 poison, ptr null, 0
  %negative_slice = insertvalue %kizu.slice.u8 %negative_base, i64 -1, 1
  %negative_result = call %kizu.error.void @kizu_rt_string_append_bytes(
    %kizu.owned %string,
    %kizu.slice.u8 %negative_slice
  )
  %negative_ok = extractvalue %kizu.error.void %negative_result, 0
  %negative_rejected = icmp eq i1 %negative_ok, false
  %negative_message = extractvalue %kizu.error.void %negative_result, 1
  %negative_message_ok = call i1 @kizu_selfhost__runtime_invalid_slice_message_ok(
    %kizu.slice.u8 %negative_message
  )
  %null_base = insertvalue %kizu.slice.u8 poison, ptr null, 0
  %null_slice = insertvalue %kizu.slice.u8 %null_base, i64 1, 1
  %null_result = call %kizu.error.void @kizu_rt_string_append_bytes(
    %kizu.owned %string,
    %kizu.slice.u8 %null_slice
  )
  %null_ok = extractvalue %kizu.error.void %null_result, 0
  %null_rejected = icmp eq i1 %null_ok, false
  %null_message = extractvalue %kizu.error.void %null_result, 1
  %null_message_ok = call i1 @kizu_selfhost__runtime_invalid_slice_message_ok(
    %kizu.slice.u8 %null_message
  )
  %raw = extractvalue %kizu.owned %string, 0
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  store i64 9223372036854775807, ptr %len_field
  %overflow_result = call %kizu.error.void @kizu_rt_string_append_byte(
    %kizu.owned %string,
    i8 1
  )
  %overflow_ok = extractvalue %kizu.error.void %overflow_result, 0
  %overflow_rejected = icmp eq i1 %overflow_ok, false
  %overflow_message = extractvalue %kizu.error.void %overflow_result, 1
  %overflow_message_ok = call i1 @kizu_selfhost__runtime_invalid_slice_message_ok(
    %kizu.slice.u8 %overflow_message
  )
  %negative_all_ok = and i1 %negative_rejected, %negative_message_ok
  %null_all_ok = and i1 %null_rejected, %null_message_ok
  %overflow_all_ok = and i1 %overflow_rejected, %overflow_message_ok
  %first_ok = and i1 %negative_all_ok, %null_all_ok
  %all_ok = and i1 %first_ok, %overflow_all_ok
  call void @kizu_rt_string_deinit(%kizu.owned %string)
  br i1 %all_ok, label %pass, label %fail
pass:
  ret i64 0
fail:
  ret i64 1
}

define i64 @kizu_selfhost__runtime_storage_smoke() {
entry:
  %array = call %kizu.owned @kizu_rt_array_new(%kizu.owned zeroinitializer, i64 16)
  %array_append = call %kizu.error.void @kizu_rt_array_append(%kizu.owned %array, %kizu.slice.u8 zeroinitializer)
  %array_len = call i64 @kizu_rt_array_len(%kizu.owned %array)
  call void @kizu_rt_array_deinit(%kizu.owned %array)
  %string = call %kizu.owned @kizu_rt_string_new(%kizu.owned zeroinitializer)
  %string_input_ptr = getelementptr inbounds [3 x i8], ptr @.kizu.rt.string_smoke, i64 0, i64 0
  %string_input_base = insertvalue %kizu.slice.u8 poison, ptr %string_input_ptr, 0
  %string_input = insertvalue %kizu.slice.u8 %string_input_base, i64 3, 1
  %string_append = call %kizu.error.void @kizu_rt_string_append_bytes(%kizu.owned %string, %kizu.slice.u8 %string_input)
  %string_append_ok = extractvalue %kizu.error.void %string_append, 0
  %string_append_byte = call %kizu.error.void @kizu_rt_string_append_byte(%kizu.owned %string, i8 117)
  %string_append_byte_ok = extractvalue %kizu.error.void %string_append_byte, 0
  %string_len = call i64 @kizu_rt_string_len(%kizu.owned %string)
  %string_view = call %kizu.slice.u8 @kizu_rt_string_as_bytes(%kizu.owned %string)
  %string_view_ptr = extractvalue %kizu.slice.u8 %string_view, 0
  %string_view_len = extractvalue %kizu.slice.u8 %string_view, 1
  %string_len_ok = icmp eq i64 %string_len, 4
  %string_view_len_ok = icmp eq i64 %string_view_len, 4
  %string_view_has_ptr = icmp ne ptr %string_view_ptr, null
  %string_ok_a = and i1 %string_append_ok, %string_append_byte_ok
  %string_ok_b = and i1 %string_len_ok, %string_view_len_ok
  %string_ok_c = and i1 %string_ok_a, %string_ok_b
  %string_base_ok = and i1 %string_ok_c, %string_view_has_ptr
  br i1 %string_base_ok, label %string_check_bytes, label %string_fail
string_check_bytes:
  %string_b0_ptr = getelementptr i8, ptr %string_view_ptr, i64 0
  %string_b0 = load i8, ptr %string_b0_ptr
  %string_b1_ptr = getelementptr i8, ptr %string_view_ptr, i64 1
  %string_b1 = load i8, ptr %string_b1_ptr
  %string_b2_ptr = getelementptr i8, ptr %string_view_ptr, i64 2
  %string_b2 = load i8, ptr %string_b2_ptr
  %string_b3_ptr = getelementptr i8, ptr %string_view_ptr, i64 3
  %string_b3 = load i8, ptr %string_b3_ptr
  %string_b0_ok = icmp eq i8 %string_b0, 107
  %string_b1_ok = icmp eq i8 %string_b1, 105
  %string_b2_ok = icmp eq i8 %string_b2, 122
  %string_b3_ok = icmp eq i8 %string_b3, 117
  %string_bytes_ok_a = and i1 %string_b0_ok, %string_b1_ok
  %string_bytes_ok_b = and i1 %string_b2_ok, %string_b3_ok
  %string_bytes_ok = and i1 %string_bytes_ok_a, %string_bytes_ok_b
  br i1 %string_bytes_ok, label %string_pass, label %string_fail
string_fail:
  call void @kizu_rt_string_deinit(%kizu.owned %string)
  ret i64 1
string_pass:
  call void @kizu_rt_string_deinit(%kizu.owned %string)
  %string_invalid = call i64 @kizu_selfhost__runtime_string_invalid_smoke()
  %string_invalid_ok = icmp eq i64 %string_invalid, 0
  br i1 %string_invalid_ok, label %storage_continue, label %storage_fail
storage_fail:
  ret i64 1
storage_continue:
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
