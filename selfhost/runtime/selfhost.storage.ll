; kizu selfhost runtime storage ll v0
source_filename = "target/selfhost/selfhost.storage"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.handle = type { ptr, i64 }
%kizu.error.void = type { i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }
%kizu.error.owned = type { i1, %kizu.owned, %kizu.slice.u8 }
%kizu.record.abi.summary = type { i64, %kizu.slice.u8 }
%kizu.error.record.abi.summary = type { i1, %kizu.record.abi.summary, %kizu.slice.u8 }
%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }
%kizu.rt.string = type { ptr, ptr, i64, i64 }
%kizu.rt.map = type { ptr, ptr, i64, i64, i64 }
%kizu.rt.map.entry = type { ptr, i64, ptr }
%kizu.rt.box = type { ptr, ptr, i64 }
%kizu.rt.diagnostics = type { ptr, i64 }
%kizu.rt.arena = type { ptr, i64, i64, [64 x i8] }

@.kizu.rt.arena_invalid_handle = private unnamed_addr constant [20 x i8] c"invalid arena handle"
@.kizu.rt.allocation_failed = private unnamed_addr constant [17 x i8] c"allocation failed"
@.kizu.rt.invalid_slice = private unnamed_addr constant [13 x i8] c"invalid slice"
@.kizu.rt.invalid_box = private unnamed_addr constant [11 x i8] c"invalid box"
@.kizu.rt.invalid_array_element = private unnamed_addr constant [21 x i8] c"invalid array element"
@.kizu.rt.array_index_out_of_bounds = private unnamed_addr constant [25 x i8] c"array index out of bounds"
@.kizu.rt.array_pop_empty = private unnamed_addr constant [20 x i8] c"pop from empty array"
@.kizu.rt.array_smoke = private unnamed_addr constant [8 x i8] c"array-ok"
@.kizu.rt.array_smoke_second = private unnamed_addr constant [8 x i8] c"payload2"
@.kizu.rt.string_smoke = private unnamed_addr constant [3 x i8] c"kiz"
@.kizu.rt.string_reserve_negative = private unnamed_addr constant [39 x i8] c"String.reserve expects non-negative i64"
; kizu hosted map globals begin
@.kizu.rt.invalid_map_key = private unnamed_addr constant [15 x i8] c"invalid map key"
@.kizu.rt.invalid_map_value = private unnamed_addr constant [17 x i8] c"invalid map value"
@.kizu.rt.map_key_not_found = private unnamed_addr constant [21 x i8] c"Map.get key not found"
; kizu hosted map globals end
@.kizu.rt.map_key_alpha = private unnamed_addr constant [5 x i8] c"alpha"
@.kizu.rt.map_key_beta = private unnamed_addr constant [4 x i8] c"beta"
@.kizu.rt.map_key_gamma = private unnamed_addr constant [5 x i8] c"gamma"
@.kizu.rt.map_key_delta = private unnamed_addr constant [5 x i8] c"delta"
@.kizu.rt.map_key_missing = private unnamed_addr constant [7 x i8] c"missing"
@.kizu.rt.map_value_alpha = private unnamed_addr constant [16 x i8] c"alpha-value-0001"
@.kizu.rt.map_value_beta = private unnamed_addr constant [16 x i8] c"beta-value-00002"
@.kizu.rt.map_value_gamma = private unnamed_addr constant [16 x i8] c"gamma-value-0003"
@.kizu.rt.map_value_delta = private unnamed_addr constant [16 x i8] c"delta-value-0004"
@.kizu.rt.map_value_update = private unnamed_addr constant [16 x i8] c"alpha-value-new!"
@.kizu.rt.arena_smoke = private unnamed_addr constant [24 x i8] c"ast-node-payload-storage"
@.kizu.rt.arena_smoke_second = private unnamed_addr constant [24 x i8] c"ast-node-payload-second!"
@.kizu.rt.abi_summary_name = private unnamed_addr constant [5 x i8] c"token"
@.kizu.rt.abi_failure = private unnamed_addr constant [16 x i8] c"abi round failed"

declare ptr @kizu_rt_alloc(ptr, i64)
declare void @kizu_rt_free(ptr, ptr)
declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1 immarg)
declare void @llvm.memmove.p0.p0.i64(ptr, ptr, i64, i1 immarg)
declare void @llvm.trap()

define %kizu.owned @kizu_rt_array_new(%kizu.owned %allocator, i64 %element_size) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 40)
  %allocator_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  store ptr null, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 3
  store i64 0, ptr %cap_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  store i64 %element_size, ptr %element_size_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_array_append(%kizu.owned %array, %kizu.slice.u8 %element) {
entry:
  %element_ptr = extractvalue %kizu.slice.u8 %element, 0
  %element_len = extractvalue %kizu.slice.u8 %element, 1
  %raw = extractvalue %kizu.owned %array, 0
  %allocator_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %current_data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %current = load i64, ptr %len_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  %element_size = load i64, ptr %element_size_field
  %size_positive = icmp sgt i64 %element_size, 0
  br i1 %size_positive, label %check_element_len, label %invalid_element
check_element_len:
  %len_matches = icmp eq i64 %element_len, %element_size
  br i1 %len_matches, label %check_element_ptr, label %invalid_element
check_element_ptr:
  %element_ptr_ok = icmp ne ptr %element_ptr, null
  br i1 %element_ptr_ok, label %check_current, label %invalid_element
check_current:
  %current_valid = icmp sge i64 %current, 0
  br i1 %current_valid, label %check_count_capacity, label %invalid_element
check_count_capacity:
  %max_count = sdiv i64 9223372036854775807, %element_size
  %fits_count = icmp slt i64 %current, %max_count
  br i1 %fits_count, label %check_old_data, label %invalid_element
check_old_data:
  %has_old = icmp sgt i64 %current, 0
  %old_data_ok = icmp ne ptr %current_data, null
  %no_old = icmp eq i64 %current, 0
  %old_ok = or i1 %old_data_ok, %no_old
  br i1 %old_ok, label %allocate, label %invalid_element
allocate:
  %next = add i64 %current, 1
  %old_bytes = mul i64 %current, %element_size
  %new_bytes = mul i64 %next, %element_size
  %new_data = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %new_bytes)
  %allocated = icmp ne ptr %new_data, null
  br i1 %allocated, label %copy_old_check, label %allocation_failed
copy_old_check:
  br i1 %has_old, label %copy_old, label %copy_element
copy_old:
  call void @llvm.memcpy.p0.p0.i64(ptr %new_data, ptr %current_data, i64 %old_bytes, i1 false)
  br label %copy_element
copy_element:
  %dest = getelementptr i8, ptr %new_data, i64 %old_bytes
  call void @llvm.memcpy.p0.p0.i64(ptr %dest, ptr %element_ptr, i64 %element_size, i1 false)
  %has_old_data = icmp ne ptr %current_data, null
  br i1 %has_old_data, label %free_old, label %store
free_old:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %current_data)
  br label %store
store:
  store ptr %new_data, ptr %data_field
  store i64 %next, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 3
  store i64 %next, ptr %cap_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
allocation_failed:
  %message_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.allocation_failed, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 17, 1
  %failed_base = insertvalue %kizu.error.void poison, i1 false, 0
  %failed = insertvalue %kizu.error.void %failed_base, %kizu.slice.u8 %message, 1
  ret %kizu.error.void %failed
invalid_element:
  %invalid_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.invalid_array_element, i64 0, i64 0
  %invalid_message_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_ptr, 0
  %invalid_message = insertvalue %kizu.slice.u8 %invalid_message_base, i64 21, 1
  %invalid_base = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_result = insertvalue %kizu.error.void %invalid_base, %kizu.slice.u8 %invalid_message, 1
  ret %kizu.error.void %invalid_result
}

define i64 @kizu_rt_array_len(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  ret i64 %len
}

define %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 %index) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  %element_size = load i64, ptr %element_size_field
  %index_nonnegative = icmp sge i64 %index, 0
  %index_in_bounds = icmp slt i64 %index, %len
  %data_ok = icmp ne ptr %data, null
  %size_ok = icmp sgt i64 %element_size, 0
  %bounds_ok = and i1 %index_nonnegative, %index_in_bounds
  %storage_ok = and i1 %data_ok, %size_ok
  %ok = and i1 %bounds_ok, %storage_ok
  br i1 %ok, label %valid, label %invalid
valid:
  %offset = mul i64 %index, %element_size
  %element_ptr = getelementptr i8, ptr %data, i64 %offset
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %element_ptr, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %element_size, 1
  %valid_ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %valid_value = insertvalue %kizu.error.slice.u8 %valid_ok, %kizu.slice.u8 %slice, 1
  %valid_result = insertvalue %kizu.error.slice.u8 %valid_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.slice.u8 %valid_result
invalid:
  %message_ptr = getelementptr inbounds [25 x i8], ptr @.kizu.rt.array_index_out_of_bounds, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 25, 1
  %invalid_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.slice.u8 %invalid_ok, %kizu.slice.u8 zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.slice.u8 %invalid_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.slice.u8 %invalid_result
}

define %kizu.error.slice.u8 @kizu_rt_array_pop(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %inspect, label %invalid
inspect:
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  %element_size = load i64, ptr %element_size_field
  %data_ok = icmp ne ptr %data, null
  %len_ok = icmp sgt i64 %len, 0
  %size_ok = icmp sgt i64 %element_size, 0
  %storage_ok = and i1 %data_ok, %size_ok
  %ok = and i1 %len_ok, %storage_ok
  br i1 %ok, label %valid, label %invalid
valid:
  %next_len = sub i64 %len, 1
  %offset = mul i64 %next_len, %element_size
  %element_ptr = getelementptr i8, ptr %data, i64 %offset
  store i64 %next_len, ptr %len_field
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %element_ptr, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %element_size, 1
  %valid_ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %valid_value = insertvalue %kizu.error.slice.u8 %valid_ok, %kizu.slice.u8 %slice, 1
  %valid_result = insertvalue %kizu.error.slice.u8 %valid_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.slice.u8 %valid_result
invalid:
  %message_ptr = getelementptr inbounds [20 x i8], ptr @.kizu.rt.array_pop_empty, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 20, 1
  %invalid_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.slice.u8 %invalid_ok, %kizu.slice.u8 zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.slice.u8 %invalid_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.slice.u8 %invalid_result
}

define %kizu.slice.u8 @kizu_rt_array_pop_or_panic(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %inspect, label %invalid
inspect:
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  %element_size = load i64, ptr %element_size_field
  %data_ok = icmp ne ptr %data, null
  %len_ok = icmp sgt i64 %len, 0
  %size_ok = icmp sgt i64 %element_size, 0
  %storage_ok = and i1 %data_ok, %size_ok
  %ok = and i1 %len_ok, %storage_ok
  br i1 %ok, label %valid, label %invalid
valid:
  %next_len = sub i64 %len, 1
  %offset = mul i64 %next_len, %element_size
  %element_ptr = getelementptr i8, ptr %data, i64 %offset
  store i64 %next_len, ptr %len_field
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %element_ptr, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %element_size, 1
  ret %kizu.slice.u8 %slice
invalid:
  call void @llvm.trap()
  unreachable
}

define %kizu.error.void @kizu_rt_array_set(%kizu.owned %array, i64 %index, %kizu.slice.u8 %element) {
entry:
  %element_ptr = extractvalue %kizu.slice.u8 %element, 0
  %raw = extractvalue %kizu.owned %array, 0
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %element_size_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 4
  %element_size = load i64, ptr %element_size_field
  %index_nonnegative = icmp sge i64 %index, 0
  %index_in_bounds = icmp slt i64 %index, %len
  %data_ok = icmp ne ptr %data, null
  %size_ok = icmp sgt i64 %element_size, 0
  %element_ptr_ok = icmp ne ptr %element_ptr, null
  %bounds_ok = and i1 %index_nonnegative, %index_in_bounds
  %storage_ok = and i1 %data_ok, %size_ok
  %input_ok = and i1 %storage_ok, %element_ptr_ok
  %ok = and i1 %bounds_ok, %input_ok
  br i1 %ok, label %valid, label %invalid
valid:
  %offset = mul i64 %index, %element_size
  %dest = getelementptr i8, ptr %data, i64 %offset
  call void @llvm.memcpy.p0.p0.i64(ptr %dest, ptr %element_ptr, i64 %element_size, i1 false)
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
invalid:
  %message_ptr = getelementptr inbounds [25 x i8], ptr @.kizu.rt.array_index_out_of_bounds, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 25, 1
  %invalid_ok = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_result = insertvalue %kizu.error.void %invalid_ok, %kizu.slice.u8 %message, 1
  ret %kizu.error.void %invalid_result
}

define void @kizu_rt_array_deinit(%kizu.owned %array) {
entry:
  %raw = extractvalue %kizu.owned %array, 0
  %allocator_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.array, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %has_data = icmp ne ptr %data, null
  br i1 %has_data, label %free_data, label %free_array
free_data:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %data)
  br label %free_array
free_array:
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
  %cap_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 3
  %current_cap = load i64, ptr %cap_field
  %cap_valid = icmp sge i64 %current_cap, 0
  %storage_valid = and i1 %current_valid, %cap_valid
  br i1 %storage_valid, label %length_check, label %invalid_slice
length_check:
  %max_delta = sub i64 9223372036854775807, %current
  %fits = icmp sle i64 %byte_len, %max_delta
  br i1 %fits, label %capacity_check, label %invalid_slice
capacity_check:
  %next = add i64 %current, %byte_len
  %within_capacity = icmp sle i64 %next, %current_cap
  %data_ok = icmp ne ptr %current_data, null
  %copy_in_place = and i1 %within_capacity, %data_ok
  br i1 %copy_in_place, label %copy_new_in_place, label %allocate
copy_new_in_place:
  %in_place_dest = getelementptr i8, ptr %current_data, i64 %current
  call void @llvm.memcpy.p0.p0.i64(ptr %in_place_dest, ptr %byte_ptr, i64 %byte_len, i1 false)
  store i64 %next, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
allocate:
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

define %kizu.error.void @kizu_rt_string_reserve(%kizu.owned %string, i64 %additional) {
entry:
  %additional_nonnegative = icmp sge i64 %additional, 0
  br i1 %additional_nonnegative, label %load, label %negative
load:
  %raw = extractvalue %kizu.owned %string, 0
  %allocator_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 1
  %current_data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 2
  %current = load i64, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.string, ptr %raw, i32 0, i32 3
  %current_cap = load i64, ptr %cap_field
  %current_valid = icmp sge i64 %current, 0
  %cap_valid = icmp sge i64 %current_cap, 0
  %storage_valid = and i1 %current_valid, %cap_valid
  br i1 %storage_valid, label %overflow_check, label %invalid_slice
overflow_check:
  %max_delta = sub i64 9223372036854775807, %current
  %fits = icmp sle i64 %additional, %max_delta
  br i1 %fits, label %want_check, label %invalid_slice
want_check:
  %want = add i64 %current, %additional
  %already_reserved = icmp sle i64 %want, %current_cap
  br i1 %already_reserved, label %ok, label %select_start
select_start:
  %cap_zero = icmp eq i64 %current_cap, 0
  br i1 %cap_zero, label %grow_from_four, label %grow_loop
grow_from_four:
  br label %grow_loop
grow_loop:
  %candidate = phi i64 [ %current_cap, %select_start ], [ 4, %grow_from_four ], [ %doubled, %double ]
  %enough = icmp sge i64 %candidate, %want
  br i1 %enough, label %allocate, label %grow_check
grow_check:
  %can_double = icmp sle i64 %candidate, 4611686018427387903
  br i1 %can_double, label %double, label %invalid_slice
double:
  %doubled = mul i64 %candidate, 2
  br label %grow_loop
allocate:
  %new_data = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %candidate)
  %allocated = icmp ne ptr %new_data, null
  br i1 %allocated, label %copy_old_check, label %allocation_failed
copy_old_check:
  %has_old = icmp sgt i64 %current, 0
  br i1 %has_old, label %copy_old, label %free_old_check
copy_old:
  call void @llvm.memcpy.p0.p0.i64(ptr %new_data, ptr %current_data, i64 %current, i1 false)
  br label %free_old_check
free_old_check:
  %has_old_data = icmp ne ptr %current_data, null
  br i1 %has_old_data, label %free_old, label %store
free_old:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %current_data)
  br label %store
store:
  store ptr %new_data, ptr %data_field
  store i64 %candidate, ptr %cap_field
  br label %ok
ok:
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
negative:
  %negative_message_ptr = getelementptr inbounds [39 x i8], ptr @.kizu.rt.string_reserve_negative, i64 0, i64 0
  %negative_message_base = insertvalue %kizu.slice.u8 poison, ptr %negative_message_ptr, 0
  %negative_message = insertvalue %kizu.slice.u8 %negative_message_base, i64 39, 1
  %negative_base = insertvalue %kizu.error.void poison, i1 false, 0
  %negative_result = insertvalue %kizu.error.void %negative_base, %kizu.slice.u8 %negative_message, 1
  ret %kizu.error.void %negative_result
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

; kizu hosted map runtime begin
define %kizu.owned @kizu_rt_map_new(%kizu.owned %allocator, i64 %value_size) {
entry:
  %size_ok = icmp sgt i64 %value_size, 0
  br i1 %size_ok, label %allocate, label %failed
allocate:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 40)
  %allocated = icmp ne ptr %raw, null
  br i1 %allocated, label %init, label %failed
init:
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  store ptr null, ptr %entries_field
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  store i64 0, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  store i64 0, ptr %cap_field
  %value_size_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  store i64 %value_size, ptr %value_size_field
  %owned = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %owned
failed:
  ret %kizu.owned zeroinitializer
}

define i64 @kizu_rt_map_find(ptr %raw, %kizu.slice.u8 %key) {
entry:
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %load, label %missing
load:
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %entries = load ptr, ptr %entries_field
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %len_positive = icmp sgt i64 %len, 0
  %entries_ok = icmp ne ptr %entries, null
  %searchable = and i1 %len_positive, %entries_ok
  br i1 %searchable, label %loop, label %missing
loop:
  %index = phi i64 [0, %load], [%next, %continue]
  %done = icmp eq i64 %index, %len
  br i1 %done, label %missing, label %compare
compare:
  %entry_ptr = getelementptr inbounds %kizu.rt.map.entry, ptr %entries, i64 %index
  %key_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 0
  %stored_key = load ptr, ptr %key_field
  %key_len_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 1
  %stored_len = load i64, ptr %key_len_field
  %matches = call i1 @kizu_rt_map_key_equal(ptr %stored_key, i64 %stored_len, %kizu.slice.u8 %key)
  br i1 %matches, label %found, label %continue
continue:
  %next = add i64 %index, 1
  br label %loop
found:
  ret i64 %index
missing:
  ret i64 -1
}

define i1 @kizu_rt_map_reserve(ptr %raw, i64 %needed) {
entry:
  %raw_ok = icmp ne ptr %raw, null
  %needed_positive = icmp sgt i64 %needed, 0
  %needed_bounded = icmp sle i64 %needed, 384307168202282325
  %request_ok_a = and i1 %raw_ok, %needed_positive
  %request_ok = and i1 %request_ok_a, %needed_bounded
  br i1 %request_ok, label %load, label %failed
load:
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %entries = load ptr, ptr %entries_field
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %cap_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  %cap = load i64, ptr %cap_field
  %len_non_negative = icmp sge i64 %len, 0
  %cap_non_negative = icmp sge i64 %cap, 0
  %len_in_cap = icmp sle i64 %len, %cap
  %cap_bounded = icmp sle i64 %cap, 384307168202282325
  %has_capacity_storage = icmp sgt i64 %cap, 0
  %entries_non_null = icmp ne ptr %entries, null
  %storage_consistent = icmp eq i1 %has_capacity_storage, %entries_non_null
  %state_ok_a = and i1 %len_non_negative, %cap_non_negative
  %state_ok_b = and i1 %len_in_cap, %cap_bounded
  %state_ok_c = and i1 %state_ok_a, %state_ok_b
  %state_ok = and i1 %state_ok_c, %storage_consistent
  br i1 %state_ok, label %capacity_check, label %failed
capacity_check:
  %already_reserved = icmp sle i64 %needed, %cap
  br i1 %already_reserved, label %success, label %grow_start
grow_start:
  %cap_empty = icmp eq i64 %cap, 0
  %initial = select i1 %cap_empty, i64 4, i64 %cap
  br label %grow_loop
grow_loop:
  %next_cap = phi i64 [%initial, %grow_start], [%candidate, %grow_more]
  %enough = icmp sge i64 %next_cap, %needed
  br i1 %enough, label %allocate, label %grow_more
grow_more:
  %can_double = icmp sle i64 %next_cap, 192153584101141162
  %doubled = mul i64 %next_cap, 2
  %candidate = select i1 %can_double, i64 %doubled, i64 %needed
  br label %grow_loop
allocate:
  %bytes = mul i64 %next_cap, 24
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %new_entries = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %bytes)
  %allocated = icmp ne ptr %new_entries, null
  br i1 %allocated, label %copy_check, label %failed
copy_check:
  %has_old_entries = icmp ne ptr %entries, null
  br i1 %has_old_entries, label %copy, label %install
copy:
  %copy_bytes = mul i64 %len, 24
  call void @llvm.memcpy.p0.p0.i64(ptr %new_entries, ptr %entries, i64 %copy_bytes, i1 false)
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %entries)
  br label %install
install:
  store ptr %new_entries, ptr %entries_field
  store i64 %next_cap, ptr %cap_field
  br label %success
success:
  ret i1 true
failed:
  ret i1 false
}

define %kizu.error.void @kizu_rt_map_insert(
  %kizu.owned %map,
  %kizu.slice.u8 %key,
  %kizu.slice.u8 %value
) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %validate_key, label %invalid_value
validate_key:
  %key_ptr = extractvalue %kizu.slice.u8 %key, 0
  %key_len = extractvalue %kizu.slice.u8 %key, 1
  %key_len_non_negative = icmp sge i64 %key_len, 0
  %key_empty = icmp eq i64 %key_len, 0
  %key_ptr_non_null = icmp ne ptr %key_ptr, null
  %key_storage_ok = or i1 %key_empty, %key_ptr_non_null
  %key_ok = and i1 %key_len_non_negative, %key_storage_ok
  br i1 %key_ok, label %validate_value, label %invalid_key
validate_value:
  %value_ptr = extractvalue %kizu.slice.u8 %value, 0
  %value_len = extractvalue %kizu.slice.u8 %value, 1
  %value_size_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  %value_size = load i64, ptr %value_size_field
  %value_size_positive = icmp sgt i64 %value_size, 0
  %value_size_matches = icmp eq i64 %value_len, %value_size
  %value_ptr_ok = icmp ne ptr %value_ptr, null
  %value_ok_a = and i1 %value_size_positive, %value_size_matches
  %value_ok = and i1 %value_ok_a, %value_ptr_ok
  br i1 %value_ok, label %find, label %invalid_value
find:
  %found = call i64 @kizu_rt_map_find(ptr %raw, %kizu.slice.u8 %key)
  %exists = icmp sge i64 %found, 0
  br i1 %exists, label %update, label %append_prepare
update:
  %update_entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %update_entries = load ptr, ptr %update_entries_field
  %update_entry = getelementptr inbounds %kizu.rt.map.entry, ptr %update_entries, i64 %found
  %update_value_field = getelementptr inbounds %kizu.rt.map.entry, ptr %update_entry, i32 0, i32 2
  %stored_value = load ptr, ptr %update_value_field
  %stored_value_ok = icmp ne ptr %stored_value, null
  br i1 %stored_value_ok, label %update_copy, label %invalid_value
update_copy:
  call void @llvm.memmove.p0.p0.i64(ptr %stored_value, ptr %value_ptr, i64 %value_size, i1 false)
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
append_prepare:
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %len_bounded = icmp slt i64 %len, 384307168202282325
  br i1 %len_bounded, label %reserve, label %allocation_failed
reserve:
  %needed = add i64 %len, 1
  %reserved = call i1 @kizu_rt_map_reserve(ptr %raw, i64 %needed)
  br i1 %reserved, label %allocate_key_check, label %allocation_failed
allocate_key_check:
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  br i1 %key_empty, label %allocate_value, label %allocate_key
allocate_key:
  %key_copy = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %key_len)
  %key_allocated = icmp ne ptr %key_copy, null
  br i1 %key_allocated, label %copy_key, label %allocation_failed
copy_key:
  call void @llvm.memcpy.p0.p0.i64(ptr %key_copy, ptr %key_ptr, i64 %key_len, i1 false)
  br label %allocate_value
allocate_value:
  %stored_key = phi ptr [null, %allocate_key_check], [%key_copy, %copy_key]
  %value_copy = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %value_size)
  %value_allocated = icmp ne ptr %value_copy, null
  br i1 %value_allocated, label %copy_value, label %allocation_failed_free_key
copy_value:
  call void @llvm.memcpy.p0.p0.i64(ptr %value_copy, ptr %value_ptr, i64 %value_size, i1 false)
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %entries = load ptr, ptr %entries_field
  %entry_ptr = getelementptr inbounds %kizu.rt.map.entry, ptr %entries, i64 %len
  %entry_key_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 0
  store ptr %stored_key, ptr %entry_key_field
  %entry_key_len_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 1
  store i64 %key_len, ptr %entry_key_len_field
  %entry_value_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 2
  store ptr %value_copy, ptr %entry_value_field
  store i64 %needed, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
allocation_failed_free_key:
  %stored_key_present = icmp ne ptr %stored_key, null
  br i1 %stored_key_present, label %free_key, label %allocation_failed
free_key:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %stored_key)
  br label %allocation_failed
allocation_failed:
  %message_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.allocation_failed, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 17, 1
  %failed_base = insertvalue %kizu.error.void poison, i1 false, 0
  %failed = insertvalue %kizu.error.void %failed_base, %kizu.slice.u8 %message, 1
  ret %kizu.error.void %failed
invalid_key:
  %invalid_key_ptr = getelementptr inbounds [15 x i8], ptr @.kizu.rt.invalid_map_key, i64 0, i64 0
  %invalid_key_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_key_ptr, 0
  %invalid_key_message = insertvalue %kizu.slice.u8 %invalid_key_base, i64 15, 1
  %invalid_key_result_base = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_key_result = insertvalue %kizu.error.void %invalid_key_result_base, %kizu.slice.u8 %invalid_key_message, 1
  ret %kizu.error.void %invalid_key_result
invalid_value:
  %invalid_value_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.invalid_map_value, i64 0, i64 0
  %invalid_value_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_value_ptr, 0
  %invalid_value_message = insertvalue %kizu.slice.u8 %invalid_value_base, i64 17, 1
  %invalid_value_result_base = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_value_result = insertvalue %kizu.error.void %invalid_value_result_base, %kizu.slice.u8 %invalid_value_message, 1
  ret %kizu.error.void %invalid_value_result
}

define i1 @kizu_rt_map_key_equal(ptr %stored_key, i64 %stored_len, %kizu.slice.u8 %key) {
entry:
  %key_ptr = extractvalue %kizu.slice.u8 %key, 0
  %key_len = extractvalue %kizu.slice.u8 %key, 1
  %len_ok = icmp eq i64 %stored_len, %key_len
  br i1 %len_ok, label %check_empty, label %fail
check_empty:
  %empty = icmp eq i64 %stored_len, 0
  br i1 %empty, label %pass, label %check_ptrs
check_ptrs:
  %stored_ok = icmp ne ptr %stored_key, null
  %key_ok = icmp ne ptr %key_ptr, null
  %ptrs_ok = and i1 %stored_ok, %key_ok
  br i1 %ptrs_ok, label %loop, label %fail
loop:
  %index = phi i64 [0, %check_ptrs], [%next, %continue]
  %done = icmp eq i64 %index, %stored_len
  br i1 %done, label %pass, label %compare
compare:
  %stored_byte_ptr = getelementptr i8, ptr %stored_key, i64 %index
  %stored_byte = load i8, ptr %stored_byte_ptr
  %key_byte_ptr = getelementptr i8, ptr %key_ptr, i64 %index
  %key_byte = load i8, ptr %key_byte_ptr
  %same = icmp eq i8 %stored_byte, %key_byte
  br i1 %same, label %continue, label %fail
continue:
  %next = add i64 %index, 1
  br label %loop
pass:
  ret i1 true
fail:
  ret i1 false
}

define i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %raw_ok = icmp ne ptr %raw, null
  %key_ptr = extractvalue %kizu.slice.u8 %key, 0
  %key_len = extractvalue %kizu.slice.u8 %key, 1
  %key_len_non_negative = icmp sge i64 %key_len, 0
  %key_empty = icmp eq i64 %key_len, 0
  %key_ptr_non_null = icmp ne ptr %key_ptr, null
  %key_storage_ok = or i1 %key_empty, %key_ptr_non_null
  %key_ok_a = and i1 %key_len_non_negative, %key_storage_ok
  %key_ok = and i1 %raw_ok, %key_ok_a
  br i1 %key_ok, label %find, label %missing
find:
  %found = call i64 @kizu_rt_map_find(ptr %raw, %kizu.slice.u8 %key)
  %present = icmp sge i64 %found, 0
  ret i1 %present
missing:
  ret i1 false
}

define i64 @kizu_rt_map_len(%kizu.owned %map) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %load, label %empty
load:
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %len_ok = icmp sge i64 %len, 0
  %result = select i1 %len_ok, i64 %len, i64 0
  ret i64 %result
empty:
  ret i64 0
}

define %kizu.error.slice.u8 @kizu_rt_map_get(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %raw_ok = icmp ne ptr %raw, null
  %key_ptr = extractvalue %kizu.slice.u8 %key, 0
  %key_len = extractvalue %kizu.slice.u8 %key, 1
  %key_len_non_negative = icmp sge i64 %key_len, 0
  %key_empty = icmp eq i64 %key_len, 0
  %key_ptr_non_null = icmp ne ptr %key_ptr, null
  %key_storage_ok = or i1 %key_empty, %key_ptr_non_null
  %key_ok_a = and i1 %key_len_non_negative, %key_storage_ok
  %key_ok = and i1 %raw_ok, %key_ok_a
  br i1 %key_ok, label %find, label %invalid
find:
  %found = call i64 @kizu_rt_map_find(ptr %raw, %kizu.slice.u8 %key)
  %present = icmp sge i64 %found, 0
  br i1 %present, label %load_value, label %missing
load_value:
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %entries = load ptr, ptr %entries_field
  %entry_ptr = getelementptr inbounds %kizu.rt.map.entry, ptr %entries, i64 %found
  %value_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 2
  %value_ptr = load ptr, ptr %value_field
  %value_size_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  %value_size = load i64, ptr %value_size_field
  %value_ptr_ok = icmp ne ptr %value_ptr, null
  %value_size_ok = icmp sgt i64 %value_size, 0
  %value_ok = and i1 %value_ptr_ok, %value_size_ok
  br i1 %value_ok, label %success, label %invalid
success:
  %view_base = insertvalue %kizu.slice.u8 poison, ptr %value_ptr, 0
  %view = insertvalue %kizu.slice.u8 %view_base, i64 %value_size, 1
  %ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %with_value = insertvalue %kizu.error.slice.u8 %ok, %kizu.slice.u8 %view, 1
  %result = insertvalue %kizu.error.slice.u8 %with_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.slice.u8 %result
missing:
  %message_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_key_not_found, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 21, 1
  %missing_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %missing_value = insertvalue %kizu.error.slice.u8 %missing_ok, %kizu.slice.u8 zeroinitializer, 1
  %missing_result = insertvalue %kizu.error.slice.u8 %missing_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.slice.u8 %missing_result
invalid:
  %invalid_ptr = getelementptr inbounds [15 x i8], ptr @.kizu.rt.invalid_map_key, i64 0, i64 0
  %invalid_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_ptr, 0
  %invalid_message = insertvalue %kizu.slice.u8 %invalid_base, i64 15, 1
  %invalid_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.slice.u8 %invalid_ok, %kizu.slice.u8 zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.slice.u8 %invalid_value, %kizu.slice.u8 %invalid_message, 2
  ret %kizu.error.slice.u8 %invalid_result
}

define void @kizu_rt_map_deinit(%kizu.owned %map) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %load, label %done
load:
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %entries_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %entries = load ptr, ptr %entries_field
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %len_positive = icmp sgt i64 %len, 0
  %entries_present = icmp ne ptr %entries, null
  %has_entries = and i1 %len_positive, %entries_present
  br i1 %has_entries, label %loop, label %free_entries_check
loop:
  %index = phi i64 [0, %load], [%next, %next_entry]
  %finished = icmp eq i64 %index, %len
  br i1 %finished, label %free_entries_check, label %process_entry
process_entry:
  %entry_ptr = getelementptr inbounds %kizu.rt.map.entry, ptr %entries, i64 %index
  %key_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 0
  %key_ptr = load ptr, ptr %key_field
  %key_present = icmp ne ptr %key_ptr, null
  br i1 %key_present, label %free_key, label %value_check
free_key:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %key_ptr)
  br label %value_check
value_check:
  %value_field = getelementptr inbounds %kizu.rt.map.entry, ptr %entry_ptr, i32 0, i32 2
  %value_ptr = load ptr, ptr %value_field
  %value_present = icmp ne ptr %value_ptr, null
  br i1 %value_present, label %free_value, label %next_entry
free_value:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %value_ptr)
  br label %next_entry
next_entry:
  %next = add i64 %index, 1
  br label %loop
free_entries_check:
  %entries_non_null = icmp ne ptr %entries, null
  br i1 %entries_non_null, label %free_entries, label %free_map
free_entries:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %entries)
  br label %free_map
free_map:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  br label %done
done:
  ret void
}
; kizu hosted map runtime end

define %kizu.record.abi.summary @kizu_selfhost__abi_summary_make(
  %kizu.slice.u8 %name,
  i64 %tokens
) {
entry:
  %with_tokens = insertvalue %kizu.record.abi.summary poison, i64 %tokens, 0
  %record = insertvalue %kizu.record.abi.summary %with_tokens, %kizu.slice.u8 %name, 1
  ret %kizu.record.abi.summary %record
}

define %kizu.record.abi.summary @kizu_selfhost__abi_summary_passthrough(
  %kizu.record.abi.summary %record
) {
entry:
  ret %kizu.record.abi.summary %record
}

define %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_success(
  %kizu.slice.u8 %name,
  i64 %tokens
) {
entry:
  %record = call %kizu.record.abi.summary @kizu_selfhost__abi_summary_make(
    %kizu.slice.u8 %name,
    i64 %tokens
  )
  %ok = insertvalue %kizu.error.record.abi.summary poison, i1 true, 0
  %with_value = insertvalue %kizu.error.record.abi.summary %ok, %kizu.record.abi.summary %record, 1
  %result = insertvalue %kizu.error.record.abi.summary %with_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.record.abi.summary %result
}

define %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_failure() {
entry:
  %message_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.abi_failure, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 16, 1
  %failed = insertvalue %kizu.error.record.abi.summary poison, i1 false, 0
  %with_value = insertvalue %kizu.error.record.abi.summary %failed, %kizu.record.abi.summary zeroinitializer, 1
  %result = insertvalue %kizu.error.record.abi.summary %with_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.record.abi.summary %result
}

define i64 @kizu_selfhost__runtime_abi_roundtrip_smoke() {
entry:
  %name_ptr = getelementptr inbounds [5 x i8], ptr @.kizu.rt.abi_summary_name, i64 0, i64 0
  %name_base = insertvalue %kizu.slice.u8 poison, ptr %name_ptr, 0
  %name = insertvalue %kizu.slice.u8 %name_base, i64 5, 1
  %record = call %kizu.record.abi.summary @kizu_selfhost__abi_summary_make(
    %kizu.slice.u8 %name,
    i64 7
  )
  %record_tokens = extractvalue %kizu.record.abi.summary %record, 0
  %record_tokens_ok = icmp eq i64 %record_tokens, 7
  %record_name = extractvalue %kizu.record.abi.summary %record, 1
  %record_name_ok = call i1 @kizu_rt_map_key_equal(
    ptr %name_ptr,
    i64 5,
    %kizu.slice.u8 %record_name
  )
  %passed_record = call %kizu.record.abi.summary @kizu_selfhost__abi_summary_passthrough(
    %kizu.record.abi.summary %record
  )
  %passed_tokens = extractvalue %kizu.record.abi.summary %passed_record, 0
  %passed_tokens_ok = icmp eq i64 %passed_tokens, 7
  %passed_name = extractvalue %kizu.record.abi.summary %passed_record, 1
  %passed_name_ok = call i1 @kizu_rt_map_key_equal(
    ptr %name_ptr,
    i64 5,
    %kizu.slice.u8 %passed_name
  )
  %success = call %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_success(
    %kizu.slice.u8 %name,
    i64 11
  )
  %success_ok = extractvalue %kizu.error.record.abi.summary %success, 0
  %success_record = extractvalue %kizu.error.record.abi.summary %success, 1
  %success_tokens = extractvalue %kizu.record.abi.summary %success_record, 0
  %success_tokens_ok = icmp eq i64 %success_tokens, 11
  %success_name = extractvalue %kizu.record.abi.summary %success_record, 1
  %success_name_ok = call i1 @kizu_rt_map_key_equal(
    ptr %name_ptr,
    i64 5,
    %kizu.slice.u8 %success_name
  )
  %failure = call %kizu.error.record.abi.summary @kizu_selfhost__abi_summary_failure()
  %failure_ok = extractvalue %kizu.error.record.abi.summary %failure, 0
  %failure_rejected = icmp eq i1 %failure_ok, false
  %failure_message = extractvalue %kizu.error.record.abi.summary %failure, 2
  %failure_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.abi_failure, i64 0, i64 0
  %failure_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %failure_ptr,
    i64 16,
    %kizu.slice.u8 %failure_message
  )
  %record_ok = and i1 %record_tokens_ok, %record_name_ok
  %success_payload_ok = and i1 %success_tokens_ok, %success_name_ok
  %success_all_ok = and i1 %success_ok, %success_payload_ok
  %failure_all_ok = and i1 %failure_rejected, %failure_message_ok
  %passed_ok = and i1 %passed_tokens_ok, %passed_name_ok
  %direct_record_ok = and i1 %record_ok, %passed_ok
  %ok_a = and i1 %direct_record_ok, %success_all_ok
  %ok = and i1 %ok_a, %failure_all_ok
  br i1 %ok, label %pass, label %fail
pass:
  ret i64 0
fail:
  ret i64 1
}

define %kizu.error.owned @kizu_rt_box_new(%kizu.owned %allocator, %kizu.slice.u8 %payload) {
entry:
  %allocator_ptr = extractvalue %kizu.owned %allocator, 0
  %payload_ptr = extractvalue %kizu.slice.u8 %payload, 0
  %payload_len = extractvalue %kizu.slice.u8 %payload, 1
  %payload_len_ok = icmp sgt i64 %payload_len, 0
  %payload_ptr_ok = icmp ne ptr %payload_ptr, null
  %payload_ok = and i1 %payload_len_ok, %payload_ptr_ok
  br i1 %payload_ok, label %allocate_box, label %invalid
allocate_box:
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 24)
  %box_allocated = icmp ne ptr %raw, null
  br i1 %box_allocated, label %allocate_payload, label %allocation_failed
allocate_payload:
  %data = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %payload_len)
  %payload_allocated = icmp ne ptr %data, null
  br i1 %payload_allocated, label %copy_payload, label %allocation_failed_free_box
copy_payload:
  call void @llvm.memcpy.p0.p0.i64(ptr %data, ptr %payload_ptr, i64 %payload_len, i1 false)
  %allocator_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 1
  store ptr %data, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 2
  store i64 %payload_len, ptr %len_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  %ok_base = insertvalue %kizu.error.owned poison, i1 true, 0
  %ok_value = insertvalue %kizu.error.owned %ok_base, %kizu.owned %handle, 1
  %ok_result = insertvalue %kizu.error.owned %ok_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.owned %ok_result
allocation_failed_free_box:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  br label %allocation_failed
allocation_failed:
  %message_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.allocation_failed, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 17, 1
  %failed_base = insertvalue %kizu.error.owned poison, i1 false, 0
  %failed_value = insertvalue %kizu.error.owned %failed_base, %kizu.owned zeroinitializer, 1
  %failed = insertvalue %kizu.error.owned %failed_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.owned %failed
invalid:
  %invalid_ptr = getelementptr inbounds [11 x i8], ptr @.kizu.rt.invalid_box, i64 0, i64 0
  %invalid_message_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_ptr, 0
  %invalid_message = insertvalue %kizu.slice.u8 %invalid_message_base, i64 11, 1
  %invalid_base = insertvalue %kizu.error.owned poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.owned %invalid_base, %kizu.owned zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.owned %invalid_value, %kizu.slice.u8 %invalid_message, 2
  ret %kizu.error.owned %invalid_result
}

define %kizu.error.slice.u8 @kizu_rt_box_borrow(%kizu.owned %box) {
entry:
  %raw = extractvalue %kizu.owned %box, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %load_box, label %invalid
load_box:
  %data_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %len_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 2
  %len = load i64, ptr %len_field
  %data_ok = icmp ne ptr %data, null
  %len_ok = icmp sgt i64 %len, 0
  %ok = and i1 %data_ok, %len_ok
  br i1 %ok, label %valid, label %invalid
valid:
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %data, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %len, 1
  %valid_ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %valid_value = insertvalue %kizu.error.slice.u8 %valid_ok, %kizu.slice.u8 %slice, 1
  %valid_result = insertvalue %kizu.error.slice.u8 %valid_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.slice.u8 %valid_result
invalid:
  %message_ptr = getelementptr inbounds [11 x i8], ptr @.kizu.rt.invalid_box, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 11, 1
  %invalid_ok = insertvalue %kizu.error.slice.u8 poison, i1 false, 0
  %invalid_value = insertvalue %kizu.error.slice.u8 %invalid_ok, %kizu.slice.u8 zeroinitializer, 1
  %invalid_result = insertvalue %kizu.error.slice.u8 %invalid_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.slice.u8 %invalid_result
}

define void @kizu_rt_box_deinit(%kizu.owned %box) {
entry:
  %raw = extractvalue %kizu.owned %box, 0
  %raw_ok = icmp ne ptr %raw, null
  br i1 %raw_ok, label %load_box, label %done
load_box:
  %allocator_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %data_field = getelementptr inbounds %kizu.rt.box, ptr %raw, i32 0, i32 1
  %data = load ptr, ptr %data_field
  %has_data = icmp ne ptr %data, null
  br i1 %has_data, label %free_data, label %free_box
free_data:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %data)
  br label %free_box
free_box:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %raw)
  br label %done
done:
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
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 88)
  %allocator_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %len_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 1
  store i64 0, ptr %len_field
  %size_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 2
  store i64 %element_size, ptr %size_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %value) {
entry:
  %raw = extractvalue %kizu.owned %arena, 0
  %value_ptr = extractvalue %kizu.slice.u8 %value, 0
  %value_len = extractvalue %kizu.slice.u8 %value, 1
  %size_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 2
  %element_size = load i64, ptr %size_field
  %size_nonnegative = icmp sge i64 %element_size, 0
  %size_bounded = icmp sle i64 %element_size, 32
  %size_ok = icmp eq i64 %value_len, %element_size
  %positive_size = icmp sgt i64 %element_size, 0
  %value_ptr_ok = icmp ne ptr %value_ptr, null
  %empty_size = icmp eq i64 %element_size, 0
  %shape_ptr_ok = or i1 %value_ptr_ok, %empty_size
  %shape_size_bound_ok = and i1 %size_nonnegative, %size_bounded
  %shape_size_ok = and i1 %shape_size_bound_ok, %size_ok
  %shape_ok = and i1 %shape_size_ok, %shape_ptr_ok
  br i1 %shape_ok, label %load_len, label %invalid
load_len:
  %len_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 1
  %current = load i64, ptr %len_field
  %has_capacity = icmp slt i64 %current, 2
  br i1 %has_capacity, label %store_value, label %invalid
store_value:
  %inline_storage = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 3
  %offset = mul i64 %current, %element_size
  %slot = getelementptr i8, ptr %inline_storage, i64 %offset
  br i1 %positive_size, label %copy_value, label %finish
copy_value:
  call void @llvm.memcpy.p0.p0.i64(ptr %slot, ptr %value_ptr, i64 %element_size, i1 false)
  br label %finish
finish:
  %next = add i64 %current, 1
  store i64 %next, ptr %len_field
  %handle_base = insertvalue %kizu.handle poison, ptr %raw, 0
  %handle = insertvalue %kizu.handle %handle_base, i64 %current, 1
  ret %kizu.handle %handle
invalid:
  %invalid_base = insertvalue %kizu.handle poison, ptr %raw, 0
  %invalid_handle = insertvalue %kizu.handle %invalid_base, i64 -1, 1
  ret %kizu.handle %invalid_handle
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
  %inline_storage = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 3
  %size_field = getelementptr inbounds %kizu.rt.arena, ptr %raw, i32 0, i32 2
  %element_size = load i64, ptr %size_field
  %offset = mul i64 %index, %element_size
  %slot = getelementptr i8, ptr %inline_storage, i64 %offset
  %slice_base = insertvalue %kizu.slice.u8 poison, ptr %slot, 0
  %slice = insertvalue %kizu.slice.u8 %slice_base, i64 %element_size, 1
  %valid_ok = insertvalue %kizu.error.slice.u8 poison, i1 true, 0
  %valid_value = insertvalue %kizu.error.slice.u8 %valid_ok, %kizu.slice.u8 %slice, 1
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

define i1 @kizu_selfhost__runtime_array_first_payload_ok(%kizu.slice.u8 %value) {
entry:
  %ptr = extractvalue %kizu.slice.u8 %value, 0
  %len = extractvalue %kizu.slice.u8 %value, 1
  %len_ok = icmp eq i64 %len, 8
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
  %b0_ok = icmp eq i8 %b0, 97
  %b1_ok = icmp eq i8 %b1, 114
  %b2_ok = icmp eq i8 %b2, 114
  %b3_ok = icmp eq i8 %b3, 97
  %b4_ok = icmp eq i8 %b4, 121
  %b5_ok = icmp eq i8 %b5, 45
  %b6_ok = icmp eq i8 %b6, 111
  %b7_ok = icmp eq i8 %b7, 107
  %p0 = and i1 %b0_ok, %b1_ok
  %p1 = and i1 %b2_ok, %b3_ok
  %p2 = and i1 %b4_ok, %b5_ok
  %p3 = and i1 %b6_ok, %b7_ok
  %p4 = and i1 %p0, %p1
  %p5 = and i1 %p2, %p3
  %ok = and i1 %p4, %p5
  ret i1 %ok
fail:
  ret i1 false
}

define i1 @kizu_selfhost__runtime_array_second_payload_ok(%kizu.slice.u8 %value) {
entry:
  %ptr = extractvalue %kizu.slice.u8 %value, 0
  %len = extractvalue %kizu.slice.u8 %value, 1
  %len_ok = icmp eq i64 %len, 8
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
  %b0_ok = icmp eq i8 %b0, 112
  %b1_ok = icmp eq i8 %b1, 97
  %b2_ok = icmp eq i8 %b2, 121
  %b3_ok = icmp eq i8 %b3, 108
  %b4_ok = icmp eq i8 %b4, 111
  %b5_ok = icmp eq i8 %b5, 97
  %b6_ok = icmp eq i8 %b6, 100
  %b7_ok = icmp eq i8 %b7, 50
  %p0 = and i1 %b0_ok, %b1_ok
  %p1 = and i1 %b2_ok, %b3_ok
  %p2 = and i1 %b4_ok, %b5_ok
  %p3 = and i1 %b6_ok, %b7_ok
  %p4 = and i1 %p0, %p1
  %p5 = and i1 %p2, %p3
  %ok = and i1 %p4, %p5
  ret i1 %ok
fail:
  ret i1 false
}

define i1 @kizu_selfhost__runtime_invalid_array_element_message_ok(%kizu.slice.u8 %message) {
entry:
  %ptr = extractvalue %kizu.slice.u8 %message, 0
  %len = extractvalue %kizu.slice.u8 %message, 1
  %len_ok = icmp eq i64 %len, 21
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
  %b13p = getelementptr i8, ptr %ptr, i64 13
  %b13 = load i8, ptr %b13p
  %b14p = getelementptr i8, ptr %ptr, i64 14
  %b14 = load i8, ptr %b14p
  %b15p = getelementptr i8, ptr %ptr, i64 15
  %b15 = load i8, ptr %b15p
  %b16p = getelementptr i8, ptr %ptr, i64 16
  %b16 = load i8, ptr %b16p
  %b17p = getelementptr i8, ptr %ptr, i64 17
  %b17 = load i8, ptr %b17p
  %b18p = getelementptr i8, ptr %ptr, i64 18
  %b18 = load i8, ptr %b18p
  %b19p = getelementptr i8, ptr %ptr, i64 19
  %b19 = load i8, ptr %b19p
  %b20p = getelementptr i8, ptr %ptr, i64 20
  %b20 = load i8, ptr %b20p
  %b0_ok = icmp eq i8 %b0, 105
  %b1_ok = icmp eq i8 %b1, 110
  %b2_ok = icmp eq i8 %b2, 118
  %b3_ok = icmp eq i8 %b3, 97
  %b4_ok = icmp eq i8 %b4, 108
  %b5_ok = icmp eq i8 %b5, 105
  %b6_ok = icmp eq i8 %b6, 100
  %b7_ok = icmp eq i8 %b7, 32
  %b8_ok = icmp eq i8 %b8, 97
  %b9_ok = icmp eq i8 %b9, 114
  %b10_ok = icmp eq i8 %b10, 114
  %b11_ok = icmp eq i8 %b11, 97
  %b12_ok = icmp eq i8 %b12, 121
  %b13_ok = icmp eq i8 %b13, 32
  %b14_ok = icmp eq i8 %b14, 101
  %b15_ok = icmp eq i8 %b15, 108
  %b16_ok = icmp eq i8 %b16, 101
  %b17_ok = icmp eq i8 %b17, 109
  %b18_ok = icmp eq i8 %b18, 101
  %b19_ok = icmp eq i8 %b19, 110
  %b20_ok = icmp eq i8 %b20, 116
  %p0 = and i1 %b0_ok, %b1_ok
  %p1 = and i1 %b2_ok, %b3_ok
  %p2 = and i1 %b4_ok, %b5_ok
  %p3 = and i1 %b6_ok, %b7_ok
  %p4 = and i1 %b8_ok, %b9_ok
  %p5 = and i1 %b10_ok, %b11_ok
  %p6 = and i1 %b12_ok, %b13_ok
  %p7 = and i1 %b14_ok, %b15_ok
  %p8 = and i1 %b16_ok, %b17_ok
  %p9 = and i1 %b18_ok, %b19_ok
  %q0 = and i1 %p0, %p1
  %q1 = and i1 %p2, %p3
  %q2 = and i1 %p4, %p5
  %q3 = and i1 %p6, %p7
  %q4 = and i1 %p8, %p9
  %r0 = and i1 %q0, %q1
  %r1 = and i1 %q2, %q3
  %r2 = and i1 %q4, %b20_ok
  %s0 = and i1 %r0, %r1
  %ok = and i1 %s0, %r2
  ret i1 %ok
fail:
  ret i1 false
}

define i1 @kizu_selfhost__runtime_array_oob_message_ok(%kizu.slice.u8 %message) {
entry:
  %ptr = extractvalue %kizu.slice.u8 %message, 0
  %len = extractvalue %kizu.slice.u8 %message, 1
  %len_ok = icmp eq i64 %len, 25
  %ptr_ok = icmp ne ptr %ptr, null
  %base_ok = and i1 %len_ok, %ptr_ok
  br i1 %base_ok, label %bytes, label %fail
bytes:
  %b0p = getelementptr i8, ptr %ptr, i64 0
  %b0 = load i8, ptr %b0p
  %b6p = getelementptr i8, ptr %ptr, i64 6
  %b6 = load i8, ptr %b6p
  %b12p = getelementptr i8, ptr %ptr, i64 12
  %b12 = load i8, ptr %b12p
  %b19p = getelementptr i8, ptr %ptr, i64 19
  %b19 = load i8, ptr %b19p
  %b24p = getelementptr i8, ptr %ptr, i64 24
  %b24 = load i8, ptr %b24p
  %b0_ok = icmp eq i8 %b0, 97
  %b6_ok = icmp eq i8 %b6, 105
  %b12_ok = icmp eq i8 %b12, 111
  %b19_ok = icmp eq i8 %b19, 98
  %b24_ok = icmp eq i8 %b24, 115
  %p0 = and i1 %b0_ok, %b6_ok
  %p1 = and i1 %b12_ok, %b19_ok
  %p2 = and i1 %p0, %p1
  %ok = and i1 %p2, %b24_ok
  ret i1 %ok
fail:
  ret i1 false
}

define i64 @kizu_selfhost__runtime_storage_smoke() {
entry:
  %array = call %kizu.owned @kizu_rt_array_new(%kizu.owned zeroinitializer, i64 8)
  %array_input_ptr = getelementptr inbounds [8 x i8], ptr @.kizu.rt.array_smoke, i64 0, i64 0
  %array_input_base = insertvalue %kizu.slice.u8 poison, ptr %array_input_ptr, 0
  %array_input = insertvalue %kizu.slice.u8 %array_input_base, i64 8, 1
  %array_append = call %kizu.error.void @kizu_rt_array_append(%kizu.owned %array, %kizu.slice.u8 %array_input)
  %array_append_ok = extractvalue %kizu.error.void %array_append, 0
  %array_second_ptr = getelementptr inbounds [8 x i8], ptr @.kizu.rt.array_smoke_second, i64 0, i64 0
  %array_second_base = insertvalue %kizu.slice.u8 poison, ptr %array_second_ptr, 0
  %array_second = insertvalue %kizu.slice.u8 %array_second_base, i64 8, 1
  %array_append_second = call %kizu.error.void @kizu_rt_array_append(
    %kizu.owned %array,
    %kizu.slice.u8 %array_second
  )
  %array_append_second_ok = extractvalue %kizu.error.void %array_append_second, 0
  %array_len = call i64 @kizu_rt_array_len(%kizu.owned %array)
  %array_len_ok = icmp eq i64 %array_len, 2
  %array_view_result = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 0)
  %array_view_ok = extractvalue %kizu.error.slice.u8 %array_view_result, 0
  %array_view = extractvalue %kizu.error.slice.u8 %array_view_result, 1
  %array_first_ok = call i1 @kizu_selfhost__runtime_array_first_payload_ok(
    %kizu.slice.u8 %array_view
  )
  %array_second_result = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 1)
  %array_second_ok = extractvalue %kizu.error.slice.u8 %array_second_result, 0
  %array_second_view = extractvalue %kizu.error.slice.u8 %array_second_result, 1
  %array_second_payload_ok = call i1 @kizu_selfhost__runtime_array_second_payload_ok(
    %kizu.slice.u8 %array_second_view
  )
  %array_oob = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 2)
  %array_oob_ok = extractvalue %kizu.error.slice.u8 %array_oob, 0
  %array_oob_rejected = icmp eq i1 %array_oob_ok, false
  %array_oob_message = extractvalue %kizu.error.slice.u8 %array_oob, 2
  %array_oob_message_ok = call i1 @kizu_selfhost__runtime_array_oob_message_ok(
    %kizu.slice.u8 %array_oob_message
  )
  %array_invalid = call %kizu.error.void @kizu_rt_array_append(
    %kizu.owned %array,
    %kizu.slice.u8 zeroinitializer
  )
  %array_invalid_ok = extractvalue %kizu.error.void %array_invalid, 0
  %array_invalid_rejected = icmp eq i1 %array_invalid_ok, false
  %array_invalid_message = extractvalue %kizu.error.void %array_invalid, 1
  %array_invalid_message_ok = call i1 @kizu_selfhost__runtime_invalid_array_element_message_ok(
    %kizu.slice.u8 %array_invalid_message
  )
  %array_set_result = call %kizu.error.void @kizu_rt_array_set(%kizu.owned %array, i64 0, %kizu.slice.u8 %array_second)
  %array_set_ok = extractvalue %kizu.error.void %array_set_result, 0
  %array_set_view_result = call %kizu.error.slice.u8 @kizu_rt_array_at(%kizu.owned %array, i64 0)
  %array_set_view_ok = extractvalue %kizu.error.slice.u8 %array_set_view_result, 0
  %array_set_view = extractvalue %kizu.error.slice.u8 %array_set_view_result, 1
  %array_set_payload_ok = call i1 @kizu_selfhost__runtime_array_second_payload_ok(
    %kizu.slice.u8 %array_set_view
  )
  %array_set_oob = call %kizu.error.void @kizu_rt_array_set(%kizu.owned %array, i64 2, %kizu.slice.u8 %array_second)
  %array_set_oob_ok = extractvalue %kizu.error.void %array_set_oob, 0
  %array_set_oob_rejected = icmp eq i1 %array_set_oob_ok, false
  %array_set_null = call %kizu.error.void @kizu_rt_array_set(%kizu.owned %array, i64 0, %kizu.slice.u8 zeroinitializer)
  %array_set_null_ok = extractvalue %kizu.error.void %array_set_null, 0
  %array_set_null_rejected = icmp eq i1 %array_set_null_ok, false
  %array_set_ok_a = and i1 %array_set_ok, %array_set_view_ok
  %array_set_ok_b = and i1 %array_set_payload_ok, %array_set_oob_rejected
  %array_set_ok_c = and i1 %array_set_ok_a, %array_set_ok_b
  %array_set_all_ok = and i1 %array_set_ok_c, %array_set_null_rejected
  %array_append_all_ok = and i1 %array_append_ok, %array_append_second_ok
  %array_first_all_ok = and i1 %array_view_ok, %array_first_ok
  %array_second_all_ok = and i1 %array_second_ok, %array_second_payload_ok
  %array_oob_all_ok = and i1 %array_oob_rejected, %array_oob_message_ok
  %array_invalid_all_ok = and i1 %array_invalid_rejected, %array_invalid_message_ok
  %array_ok_a = and i1 %array_append_all_ok, %array_len_ok
  %array_ok_b = and i1 %array_first_all_ok, %array_second_all_ok
  %array_ok_c = and i1 %array_oob_all_ok, %array_invalid_all_ok
  %array_ok_d = and i1 %array_ok_a, %array_ok_b
  %array_ok_e = and i1 %array_ok_c, %array_ok_d
  %array_base_ok = and i1 %array_ok_e, %array_set_all_ok
  br i1 %array_base_ok, label %array_pass, label %array_fail
array_fail:
  call void @kizu_rt_array_deinit(%kizu.owned %array)
  ret i64 1
array_pass:
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
  %map = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer, i64 16)
  %map_alpha_ptr = getelementptr inbounds [5 x i8], ptr @.kizu.rt.map_key_alpha, i64 0, i64 0
  %map_alpha_base = insertvalue %kizu.slice.u8 poison, ptr %map_alpha_ptr, 0
  %map_alpha = insertvalue %kizu.slice.u8 %map_alpha_base, i64 5, 1
  %map_alpha_temp = alloca [5 x i8]
  call void @llvm.memcpy.p0.p0.i64(ptr %map_alpha_temp, ptr %map_alpha_ptr, i64 5, i1 false)
  %map_alpha_temp_base = insertvalue %kizu.slice.u8 poison, ptr %map_alpha_temp, 0
  %map_alpha_from_temp = insertvalue %kizu.slice.u8 %map_alpha_temp_base, i64 5, 1
  %map_beta_ptr = getelementptr inbounds [4 x i8], ptr @.kizu.rt.map_key_beta, i64 0, i64 0
  %map_beta_base = insertvalue %kizu.slice.u8 poison, ptr %map_beta_ptr, 0
  %map_beta = insertvalue %kizu.slice.u8 %map_beta_base, i64 4, 1
  %map_gamma_ptr = getelementptr inbounds [5 x i8], ptr @.kizu.rt.map_key_gamma, i64 0, i64 0
  %map_gamma_base = insertvalue %kizu.slice.u8 poison, ptr %map_gamma_ptr, 0
  %map_gamma = insertvalue %kizu.slice.u8 %map_gamma_base, i64 5, 1
  %map_delta_ptr = getelementptr inbounds [5 x i8], ptr @.kizu.rt.map_key_delta, i64 0, i64 0
  %map_delta_base = insertvalue %kizu.slice.u8 poison, ptr %map_delta_ptr, 0
  %map_delta = insertvalue %kizu.slice.u8 %map_delta_base, i64 5, 1
  %map_missing_key_ptr = getelementptr inbounds [7 x i8], ptr @.kizu.rt.map_key_missing, i64 0, i64 0
  %map_missing_key_base = insertvalue %kizu.slice.u8 poison, ptr %map_missing_key_ptr, 0
  %map_missing_key = insertvalue %kizu.slice.u8 %map_missing_key_base, i64 7, 1
  %map_alpha_value_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.map_value_alpha, i64 0, i64 0
  %map_alpha_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_alpha_value_ptr, 0
  %map_alpha_value = insertvalue %kizu.slice.u8 %map_alpha_value_base, i64 16, 1
  %map_beta_value_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.map_value_beta, i64 0, i64 0
  %map_beta_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_beta_value_ptr, 0
  %map_beta_value = insertvalue %kizu.slice.u8 %map_beta_value_base, i64 16, 1
  %map_gamma_value_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.map_value_gamma, i64 0, i64 0
  %map_gamma_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_gamma_value_ptr, 0
  %map_gamma_value = insertvalue %kizu.slice.u8 %map_gamma_value_base, i64 16, 1
  %map_delta_value_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.map_value_delta, i64 0, i64 0
  %map_delta_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_delta_value_ptr, 0
  %map_delta_value = insertvalue %kizu.slice.u8 %map_delta_value_base, i64 16, 1
  %map_update_value_ptr = getelementptr inbounds [16 x i8], ptr @.kizu.rt.map_value_update, i64 0, i64 0
  %map_update_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_update_value_ptr, 0
  %map_update_value = insertvalue %kizu.slice.u8 %map_update_value_base, i64 16, 1
  %map_insert_alpha = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha_from_temp,
    %kizu.slice.u8 %map_alpha_value
  )
  %map_insert_alpha_ok = extractvalue %kizu.error.void %map_insert_alpha, 0
  %map_alpha_temp_first = getelementptr i8, ptr %map_alpha_temp, i64 0
  store i8 88, ptr %map_alpha_temp_first
  %map_insert_beta = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_beta,
    %kizu.slice.u8 %map_beta_value
  )
  %map_insert_beta_ok = extractvalue %kizu.error.void %map_insert_beta, 0
  %map_insert_gamma = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_gamma,
    %kizu.slice.u8 %map_gamma_value
  )
  %map_insert_gamma_ok = extractvalue %kizu.error.void %map_insert_gamma, 0
  %map_insert_delta = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_delta,
    %kizu.slice.u8 %map_delta_value
  )
  %map_insert_delta_ok = extractvalue %kizu.error.void %map_insert_delta, 0
  %map_get_missing = call %kizu.error.slice.u8 @kizu_rt_map_get(
    %kizu.owned %map,
    %kizu.slice.u8 %map_missing_key
  )
  %map_get_missing_ok = extractvalue %kizu.error.slice.u8 %map_get_missing, 0
  %map_get_missing_rejected = icmp eq i1 %map_get_missing_ok, false
  %map_get_missing_message = extractvalue %kizu.error.slice.u8 %map_get_missing, 2
  %map_missing_message_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_key_not_found, i64 0, i64 0
  %map_missing_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_missing_message_ptr,
    i64 21,
    %kizu.slice.u8 %map_get_missing_message
  )
  %map_insert_fifth = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_missing_key,
    %kizu.slice.u8 %map_alpha_value
  )
  %map_insert_fifth_ok = extractvalue %kizu.error.void %map_insert_fifth, 0
  %map_len = call i64 @kizu_rt_map_len(%kizu.owned %map)
  %map_len_ok = icmp eq i64 %map_len, 5
  %map_contains_alpha = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %map_alpha)
  %map_contains_fifth = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %map_missing_key)
  %map_get_beta = call %kizu.error.slice.u8 @kizu_rt_map_get(%kizu.owned %map, %kizu.slice.u8 %map_beta)
  %map_get_beta_ok = extractvalue %kizu.error.slice.u8 %map_get_beta, 0
  %map_get_beta_value = extractvalue %kizu.error.slice.u8 %map_get_beta, 1
  %map_beta_value_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_beta_value_ptr,
    i64 16,
    %kizu.slice.u8 %map_get_beta_value
  )
  %map_update_alpha = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha,
    %kizu.slice.u8 %map_update_value
  )
  %map_update_alpha_ok = extractvalue %kizu.error.void %map_update_alpha, 0
  %map_get_alpha_updated = call %kizu.error.slice.u8 @kizu_rt_map_get(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha
  )
  %map_get_alpha_updated_ok = extractvalue %kizu.error.slice.u8 %map_get_alpha_updated, 0
  %map_get_alpha_updated_value = extractvalue %kizu.error.slice.u8 %map_get_alpha_updated, 1
  %map_alpha_updated_value_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_update_value_ptr,
    i64 16,
    %kizu.slice.u8 %map_get_alpha_updated_value
  )
  %map_short_value = insertvalue %kizu.slice.u8 %map_alpha_value_base, i64 15, 1
  %map_insert_short = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_gamma,
    %kizu.slice.u8 %map_short_value
  )
  %map_insert_short_ok = extractvalue %kizu.error.void %map_insert_short, 0
  %map_short_rejected = icmp eq i1 %map_insert_short_ok, false
  %map_insert_null = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_gamma,
    %kizu.slice.u8 zeroinitializer
  )
  %map_insert_null_ok = extractvalue %kizu.error.void %map_insert_null, 0
  %map_null_rejected = icmp eq i1 %map_insert_null_ok, false
  %map_zero = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer, i64 0)
  %map_zero_raw = extractvalue %kizu.owned %map_zero, 0
  %map_zero_rejected = icmp eq ptr %map_zero_raw, null
  %map_negative = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer, i64 -1)
  %map_negative_raw = extractvalue %kizu.owned %map_negative, 0
  %map_negative_rejected = icmp eq ptr %map_negative_raw, null
  %map_insert_ok_a = and i1 %map_insert_alpha_ok, %map_insert_beta_ok
  %map_insert_ok_b = and i1 %map_insert_gamma_ok, %map_insert_delta_ok
  %map_insert_ok_c = and i1 %map_insert_ok_a, %map_insert_ok_b
  %map_insert_ok = and i1 %map_insert_ok_c, %map_insert_fifth_ok
  %map_contains_ok = and i1 %map_contains_alpha, %map_contains_fifth
  %map_missing_ok_a = and i1 %map_get_missing_rejected, %map_missing_message_ok
  %map_beta_ok = and i1 %map_get_beta_ok, %map_beta_value_ok
  %map_update_ok_a = and i1 %map_update_alpha_ok, %map_get_alpha_updated_ok
  %map_update_ok = and i1 %map_update_ok_a, %map_alpha_updated_value_ok
  %map_invalid_value_ok = and i1 %map_short_rejected, %map_null_rejected
  %map_invalid_size_ok = and i1 %map_zero_rejected, %map_negative_rejected
  %map_ok_a = and i1 %map_insert_ok, %map_contains_ok
  %map_ok_b = and i1 %map_missing_ok_a, %map_beta_ok
  %map_ok_c = and i1 %map_update_ok, %map_invalid_value_ok
  %map_ok_d = and i1 %map_invalid_size_ok, %map_len_ok
  %map_ok_e = and i1 %map_ok_a, %map_ok_b
  %map_ok_f = and i1 %map_ok_c, %map_ok_d
  %map_ok = and i1 %map_ok_e, %map_ok_f
  br i1 %map_ok, label %map_pass, label %map_fail
map_fail:
  call void @kizu_rt_map_deinit(%kizu.owned %map)
  ret i64 1
map_pass:
  call void @kizu_rt_map_deinit(%kizu.owned %map)
  %map_byte = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer, i64 1)
  %map_byte_slot = alloca i8
  store i8 77, ptr %map_byte_slot
  %map_byte_value_base = insertvalue %kizu.slice.u8 poison, ptr %map_byte_slot, 0
  %map_byte_value = insertvalue %kizu.slice.u8 %map_byte_value_base, i64 1, 1
  %map_byte_insert = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map_byte,
    %kizu.slice.u8 %map_beta,
    %kizu.slice.u8 %map_byte_value
  )
  %map_byte_insert_ok = extractvalue %kizu.error.void %map_byte_insert, 0
  store i8 12, ptr %map_byte_slot
  %map_byte_get = call %kizu.error.slice.u8 @kizu_rt_map_get(
    %kizu.owned %map_byte,
    %kizu.slice.u8 %map_beta
  )
  %map_byte_get_ok = extractvalue %kizu.error.slice.u8 %map_byte_get, 0
  %map_byte_view = extractvalue %kizu.error.slice.u8 %map_byte_get, 1
  %map_byte_ptr = extractvalue %kizu.slice.u8 %map_byte_view, 0
  %map_byte_stored = load i8, ptr %map_byte_ptr
  %map_byte_value_ok = icmp eq i8 %map_byte_stored, 77
  %map_byte_ok_a = and i1 %map_byte_insert_ok, %map_byte_get_ok
  %map_byte_ok = and i1 %map_byte_ok_a, %map_byte_value_ok
  call void @kizu_rt_map_deinit(%kizu.owned %map_byte)
  br i1 %map_byte_ok, label %map_record_start, label %map_width_fail
map_record_start:
  %map_record = call %kizu.owned @kizu_rt_map_new(
    %kizu.owned zeroinitializer,
    i64 ptrtoint (ptr getelementptr (%kizu.record.abi.summary, ptr null, i32 1) to i64)
  )
  %map_record_slot = alloca %kizu.record.abi.summary
  %map_record_value_base = insertvalue %kizu.record.abi.summary poison, i64 73, 0
  %map_record_value = insertvalue %kizu.record.abi.summary %map_record_value_base, %kizu.slice.u8 %map_alpha, 1
  store %kizu.record.abi.summary %map_record_value, ptr %map_record_slot
  %map_record_view_base = insertvalue %kizu.slice.u8 poison, ptr %map_record_slot, 0
  %map_record_view = insertvalue %kizu.slice.u8 %map_record_view_base, i64 ptrtoint (ptr getelementptr (%kizu.record.abi.summary, ptr null, i32 1) to i64), 1
  %map_record_insert = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map_record,
    %kizu.slice.u8 %map_gamma,
    %kizu.slice.u8 %map_record_view
  )
  %map_record_insert_ok = extractvalue %kizu.error.void %map_record_insert, 0
  store %kizu.record.abi.summary zeroinitializer, ptr %map_record_slot
  %map_record_get = call %kizu.error.slice.u8 @kizu_rt_map_get(
    %kizu.owned %map_record,
    %kizu.slice.u8 %map_gamma
  )
  %map_record_get_ok = extractvalue %kizu.error.slice.u8 %map_record_get, 0
  %map_record_stored_view = extractvalue %kizu.error.slice.u8 %map_record_get, 1
  %map_record_stored_ptr = extractvalue %kizu.slice.u8 %map_record_stored_view, 0
  %map_record_stored = load %kizu.record.abi.summary, ptr %map_record_stored_ptr
  %map_record_tokens = extractvalue %kizu.record.abi.summary %map_record_stored, 0
  %map_record_tokens_ok = icmp eq i64 %map_record_tokens, 73
  %map_record_name = extractvalue %kizu.record.abi.summary %map_record_stored, 1
  %map_record_name_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_alpha_ptr,
    i64 5,
    %kizu.slice.u8 %map_record_name
  )
  %map_record_ok_a = and i1 %map_record_insert_ok, %map_record_get_ok
  %map_record_ok_b = and i1 %map_record_tokens_ok, %map_record_name_ok
  %map_record_ok = and i1 %map_record_ok_a, %map_record_ok_b
  call void @kizu_rt_map_deinit(%kizu.owned %map_record)
  br i1 %map_record_ok, label %map_width_pass, label %map_width_fail
map_width_fail:
  ret i64 1
map_width_pass:
  %diagnostics = call %kizu.owned @kizu_rt_diagnostic_buffer_new(%kizu.owned zeroinitializer)
  %diagnostic_push = call %kizu.error.void @kizu_rt_diagnostic_push(%kizu.owned %diagnostics, %kizu.slice.u8 zeroinitializer)
  call void @kizu_rt_diagnostic_buffer_deinit(%kizu.owned %diagnostics)
  %arena = call %kizu.owned @kizu_rt_arena_new(%kizu.owned zeroinitializer, i64 24)
  %arena_payload_ptr = getelementptr inbounds [24 x i8], ptr @.kizu.rt.arena_smoke, i64 0, i64 0
  %arena_payload_base = insertvalue %kizu.slice.u8 poison, ptr %arena_payload_ptr, 0
  %arena_payload = insertvalue %kizu.slice.u8 %arena_payload_base, i64 24, 1
  %node_id = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %arena_payload)
  %node = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %node_id)
  %node_ok = extractvalue %kizu.error.slice.u8 %node, 0
  %node_payload = extractvalue %kizu.error.slice.u8 %node, 1
  %arena_payload_ok = call i1 @kizu_rt_map_key_equal(
    ptr %arena_payload_ptr,
    i64 24,
    %kizu.slice.u8 %node_payload
  )
  %arena_payload2_ptr = getelementptr inbounds [24 x i8], ptr @.kizu.rt.arena_smoke_second, i64 0, i64 0
  %arena_payload2_base = insertvalue %kizu.slice.u8 poison, ptr %arena_payload2_ptr, 0
  %arena_payload2 = insertvalue %kizu.slice.u8 %arena_payload2_base, i64 24, 1
  %node2_id = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %arena_payload2)
  %node2 = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %node2_id)
  %node2_ok = extractvalue %kizu.error.slice.u8 %node2, 0
  %node2_payload = extractvalue %kizu.error.slice.u8 %node2, 1
  %arena_payload2_ok = call i1 @kizu_rt_map_key_equal(
    ptr %arena_payload2_ptr,
    i64 24,
    %kizu.slice.u8 %node2_payload
  )
  %node3_id = call %kizu.handle @kizu_rt_arena_add(%kizu.owned %arena, %kizu.slice.u8 %arena_payload)
  %node3_index = extractvalue %kizu.handle %node3_id, 1
  %arena_full_rejected = icmp eq i64 %node3_index, -1
  %node3 = call %kizu.error.slice.u8 @kizu_rt_arena_get(%kizu.owned %arena, %kizu.handle %node3_id)
  %node3_ok = extractvalue %kizu.error.slice.u8 %node3, 0
  %arena_full_get_rejected = icmp eq i1 %node3_ok, false
  %arena_full_message = extractvalue %kizu.error.slice.u8 %node3, 2
  %arena_invalid_ptr = getelementptr inbounds [20 x i8], ptr @.kizu.rt.arena_invalid_handle, i64 0, i64 0
  %arena_full_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %arena_invalid_ptr,
    i64 20,
    %kizu.slice.u8 %arena_full_message
  )
  %arena_raw = extractvalue %kizu.owned %arena, 0
  %arena_oob_base = insertvalue %kizu.handle poison, ptr %arena_raw, 0
  %arena_oob_handle = insertvalue %kizu.handle %arena_oob_base, i64 2, 1
  %arena_oob = call %kizu.error.slice.u8 @kizu_rt_arena_get(
    %kizu.owned %arena,
    %kizu.handle %arena_oob_handle
  )
  %arena_oob_ok = extractvalue %kizu.error.slice.u8 %arena_oob, 0
  %arena_oob_rejected = icmp eq i1 %arena_oob_ok, false
  %arena_oob_message = extractvalue %kizu.error.slice.u8 %arena_oob, 2
  %arena_oob_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %arena_invalid_ptr,
    i64 20,
    %kizu.slice.u8 %arena_oob_message
  )
  %arena_foreign_base = insertvalue %kizu.handle poison, ptr null, 0
  %arena_foreign_handle = insertvalue %kizu.handle %arena_foreign_base, i64 0, 1
  %arena_foreign = call %kizu.error.slice.u8 @kizu_rt_arena_get(
    %kizu.owned %arena,
    %kizu.handle %arena_foreign_handle
  )
  %arena_foreign_ok = extractvalue %kizu.error.slice.u8 %arena_foreign, 0
  %arena_foreign_rejected = icmp eq i1 %arena_foreign_ok, false
  %arena_foreign_message = extractvalue %kizu.error.slice.u8 %arena_foreign, 2
  %arena_foreign_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %arena_invalid_ptr,
    i64 20,
    %kizu.slice.u8 %arena_foreign_message
  )
  %arena_get_ok_a = and i1 %node_ok, %arena_payload_ok
  %arena_get_ok_b = and i1 %node2_ok, %arena_payload2_ok
  %arena_get_ok = and i1 %arena_get_ok_a, %arena_get_ok_b
  %arena_full_ok_a = and i1 %arena_full_rejected, %arena_full_get_rejected
  %arena_full_ok = and i1 %arena_full_ok_a, %arena_full_message_ok
  %arena_oob_all_ok = and i1 %arena_oob_rejected, %arena_oob_message_ok
  %arena_foreign_all_ok = and i1 %arena_foreign_rejected, %arena_foreign_message_ok
  %arena_invalid_ok_a = and i1 %arena_full_ok, %arena_oob_all_ok
  %arena_invalid_ok = and i1 %arena_invalid_ok_a, %arena_foreign_all_ok
  %arena_ok = and i1 %arena_get_ok, %arena_invalid_ok
  call void @kizu_rt_arena_deinit(%kizu.owned %arena)
  br i1 %arena_ok, label %arena_pass, label %arena_fail
arena_fail:
  ret i64 1
arena_pass:
  %box_payload_slot = alloca i64
  store i64 7, ptr %box_payload_slot
  %box_payload_base = insertvalue %kizu.slice.u8 poison, ptr %box_payload_slot, 0
  %box_payload = insertvalue %kizu.slice.u8 %box_payload_base, i64 8, 1
  %box_new = call %kizu.error.owned @kizu_rt_box_new(%kizu.owned zeroinitializer, %kizu.slice.u8 %box_payload)
  %box_new_ok = extractvalue %kizu.error.owned %box_new, 0
  br i1 %box_new_ok, label %box_borrow, label %box_fail
box_borrow:
  %box = extractvalue %kizu.error.owned %box_new, 1
  %box_view = call %kizu.error.slice.u8 @kizu_rt_box_borrow(%kizu.owned %box)
  %box_view_ok = extractvalue %kizu.error.slice.u8 %box_view, 0
  br i1 %box_view_ok, label %box_load, label %box_borrow_fail
box_borrow_fail:
  call void @kizu_rt_box_deinit(%kizu.owned %box)
  ret i64 1
box_load:
  %box_view_slice = extractvalue %kizu.error.slice.u8 %box_view, 1
  %box_view_ptr = extractvalue %kizu.slice.u8 %box_view_slice, 0
  %box_value = load i64, ptr %box_view_ptr
  %box_value_ok = icmp eq i64 %box_value, 7
  call void @kizu_rt_box_deinit(%kizu.owned %box)
  br i1 %box_value_ok, label %box_pass, label %box_fail
box_fail:
  ret i64 1
box_pass:
  %abi_roundtrip = call i64 @kizu_selfhost__runtime_abi_roundtrip_smoke()
  %abi_roundtrip_ok = icmp eq i64 %abi_roundtrip, 0
  br i1 %abi_roundtrip_ok, label %abi_pass, label %abi_fail
abi_fail:
  ret i64 1
abi_pass:
  ret i64 0
}
