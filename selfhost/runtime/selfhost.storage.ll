; kizu selfhost runtime storage ll v0
source_filename = "target/selfhost/selfhost.storage"

%kizu.slice.u8 = type { ptr, i64 }
%kizu.owned = type { ptr }
%kizu.handle = type { ptr, i64 }
%kizu.error.void = type { i1, %kizu.slice.u8 }
%kizu.error.i64 = type { i1, i64, %kizu.slice.u8 }
%kizu.error.slice.u8 = type { i1, %kizu.slice.u8, %kizu.slice.u8 }
%kizu.record.abi.summary = type { i64, %kizu.slice.u8 }
%kizu.error.record.abi.summary = type { i1, %kizu.record.abi.summary, %kizu.slice.u8 }
%kizu.rt.array = type { ptr, ptr, i64, i64, i64 }
%kizu.rt.string = type { ptr, ptr, i64, i64 }
%kizu.rt.map = type { ptr, i64, ptr, i64, i64, ptr, i64, i64 }
%kizu.rt.diagnostics = type { ptr, i64 }
%kizu.rt.arena = type { ptr, i64, i64, [48 x i8] }

@.kizu.rt.arena_invalid_handle = private unnamed_addr constant [20 x i8] c"invalid arena handle"
@.kizu.rt.allocation_failed = private unnamed_addr constant [17 x i8] c"allocation failed"
@.kizu.rt.invalid_slice = private unnamed_addr constant [13 x i8] c"invalid slice"
@.kizu.rt.invalid_array_element = private unnamed_addr constant [21 x i8] c"invalid array element"
@.kizu.rt.array_index_out_of_bounds = private unnamed_addr constant [25 x i8] c"array index out of bounds"
@.kizu.rt.array_smoke = private unnamed_addr constant [8 x i8] c"array-ok"
@.kizu.rt.array_smoke_second = private unnamed_addr constant [8 x i8] c"payload2"
@.kizu.rt.string_smoke = private unnamed_addr constant [3 x i8] c"kiz"
@.kizu.rt.invalid_map_key = private unnamed_addr constant [15 x i8] c"invalid map key"
@.kizu.rt.map_full = private unnamed_addr constant [21 x i8] c"map capacity exceeded"
@.kizu.rt.map_key_not_found = private unnamed_addr constant [21 x i8] c"Map.get key not found"
@.kizu.rt.map_key_alpha = private unnamed_addr constant [5 x i8] c"alpha"
@.kizu.rt.map_key_beta = private unnamed_addr constant [4 x i8] c"beta"
@.kizu.rt.map_key_gamma = private unnamed_addr constant [5 x i8] c"gamma"
@.kizu.rt.arena_smoke = private unnamed_addr constant [24 x i8] c"ast-node-payload-storage"
@.kizu.rt.arena_smoke_second = private unnamed_addr constant [24 x i8] c"ast-node-payload-second!"
@.kizu.rt.abi_summary_name = private unnamed_addr constant [5 x i8] c"token"
@.kizu.rt.abi_failure = private unnamed_addr constant [16 x i8] c"abi round failed"

declare ptr @kizu_rt_alloc(ptr, i64)
declare void @kizu_rt_free(ptr, ptr)
declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1 immarg)

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
  %bounds_ok = and i1 %index_nonnegative, %index_in_bounds
  %storage_ok = and i1 %data_ok, %size_ok
  %ok = and i1 %bounds_ok, %storage_ok
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
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 64)
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  store ptr %allocator_ptr, ptr %allocator_field
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  store i64 0, ptr %len_field
  %key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  store ptr null, ptr %key0_field
  %key0_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  store i64 0, ptr %key0_len_field
  %value0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  store i64 0, ptr %value0_field
  %key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  store ptr null, ptr %key1_field
  %key1_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 6
  store i64 0, ptr %key1_len_field
  %value1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 7
  store i64 0, ptr %value1_field
  %handle = insertvalue %kizu.owned poison, ptr %raw, 0
  ret %kizu.owned %handle
}

define %kizu.error.void @kizu_rt_map_insert(%kizu.owned %map, %kizu.slice.u8 %key, i64 %value) {
entry:
  %key_ptr = extractvalue %kizu.slice.u8 %key, 0
  %key_len = extractvalue %kizu.slice.u8 %key, 1
  %len_negative = icmp slt i64 %key_len, 0
  br i1 %len_negative, label %invalid_key, label %check_ptr
check_ptr:
  %has_key_bytes = icmp sgt i64 %key_len, 0
  %ptr_ok = icmp ne ptr %key_ptr, null
  %empty_key = icmp eq i64 %key_len, 0
  %key_ok = or i1 %ptr_ok, %empty_key
  br i1 %key_ok, label %load_map, label %invalid_key
load_map:
  %raw = extractvalue %kizu.owned %map, 0
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  %has_slot0 = icmp sgt i64 %len, 0
  br i1 %has_slot0, label %check_existing_slot0, label %check_capacity
check_existing_slot0:
  %existing_key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %existing_key0 = load ptr, ptr %existing_key0_field
  %existing_key0_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  %existing_key0_len = load i64, ptr %existing_key0_len_field
  %slot0_matches = call i1 @kizu_rt_map_key_equal(
    ptr %existing_key0,
    i64 %existing_key0_len,
    %kizu.slice.u8 %key
  )
  br i1 %slot0_matches, label %update_slot0, label %check_existing_slot1_capacity
update_slot0:
  %update_value0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  store i64 %value, ptr %update_value0_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
check_existing_slot1_capacity:
  %has_slot1 = icmp sgt i64 %len, 1
  br i1 %has_slot1, label %check_existing_slot1, label %check_capacity
check_existing_slot1:
  %existing_key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  %existing_key1 = load ptr, ptr %existing_key1_field
  %existing_key1_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 6
  %existing_key1_len = load i64, ptr %existing_key1_len_field
  %slot1_matches = call i1 @kizu_rt_map_key_equal(
    ptr %existing_key1,
    i64 %existing_key1_len,
    %kizu.slice.u8 %key
  )
  br i1 %slot1_matches, label %update_slot1, label %check_capacity
update_slot1:
  %update_value1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 7
  store i64 %value, ptr %update_value1_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
check_capacity:
  %has_capacity = icmp slt i64 %len, 2
  br i1 %has_capacity, label %copy_key, label %map_full
copy_key:
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %stored_key = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 %key_len)
  %allocated = icmp ne ptr %stored_key, null
  br i1 %allocated, label %copy_key_bytes_check, label %allocation_failed
copy_key_bytes_check:
  br i1 %has_key_bytes, label %copy_key_bytes, label %store_entry
copy_key_bytes:
  call void @llvm.memcpy.p0.p0.i64(ptr %stored_key, ptr %key_ptr, i64 %key_len, i1 false)
  br label %store_entry
store_entry:
  %slot0 = icmp eq i64 %len, 0
  br i1 %slot0, label %store_slot0, label %store_slot1
store_slot0:
  %key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  store ptr %stored_key, ptr %key0_field
  %key0_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  store i64 %key_len, ptr %key0_len_field
  %value0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  store i64 %value, ptr %value0_field
  br label %finish
store_slot1:
  %key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  store ptr %stored_key, ptr %key1_field
  %key1_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 6
  store i64 %key_len, ptr %key1_len_field
  %value1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 7
  store i64 %value, ptr %value1_field
  br label %finish
finish:
  %next = add i64 %len, 1
  store i64 %next, ptr %len_field
  ret %kizu.error.void { i1 true, %kizu.slice.u8 zeroinitializer }
allocation_failed:
  %message_ptr = getelementptr inbounds [17 x i8], ptr @.kizu.rt.allocation_failed, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 17, 1
  %failed_base = insertvalue %kizu.error.void poison, i1 false, 0
  %failed = insertvalue %kizu.error.void %failed_base, %kizu.slice.u8 %message, 1
  ret %kizu.error.void %failed
invalid_key:
  %invalid_ptr = getelementptr inbounds [15 x i8], ptr @.kizu.rt.invalid_map_key, i64 0, i64 0
  %invalid_base = insertvalue %kizu.slice.u8 poison, ptr %invalid_ptr, 0
  %invalid_message = insertvalue %kizu.slice.u8 %invalid_base, i64 15, 1
  %invalid_result_base = insertvalue %kizu.error.void poison, i1 false, 0
  %invalid_result = insertvalue %kizu.error.void %invalid_result_base, %kizu.slice.u8 %invalid_message, 1
  ret %kizu.error.void %invalid_result
map_full:
  %full_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_full, i64 0, i64 0
  %full_base = insertvalue %kizu.slice.u8 poison, ptr %full_ptr, 0
  %full_message = insertvalue %kizu.slice.u8 %full_base, i64 21, 1
  %full_result_base = insertvalue %kizu.error.void poison, i1 false, 0
  %full_result = insertvalue %kizu.error.void %full_result_base, %kizu.slice.u8 %full_message, 1
  ret %kizu.error.void %full_result
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

define i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  %has_slot0 = icmp sgt i64 %len, 0
  br i1 %has_slot0, label %check_slot0, label %missing
check_slot0:
  %key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %key0 = load ptr, ptr %key0_field
  %key0_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  %key0_len = load i64, ptr %key0_len_field
  %slot0_matches = call i1 @kizu_rt_map_key_equal(ptr %key0, i64 %key0_len, %kizu.slice.u8 %key)
  br i1 %slot0_matches, label %found, label %check_slot1_capacity
check_slot1_capacity:
  %has_slot1 = icmp sgt i64 %len, 1
  br i1 %has_slot1, label %check_slot1, label %missing
check_slot1:
  %key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  %key1 = load ptr, ptr %key1_field
  %key1_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 6
  %key1_len = load i64, ptr %key1_len_field
  %slot1_matches = call i1 @kizu_rt_map_key_equal(ptr %key1, i64 %key1_len, %kizu.slice.u8 %key)
  br i1 %slot1_matches, label %found, label %missing
found:
  ret i1 true
missing:
  ret i1 false
}

define %kizu.error.i64 @kizu_rt_map_get_i64(%kizu.owned %map, %kizu.slice.u8 %key) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 1
  %len = load i64, ptr %len_field
  %has_slot0 = icmp sgt i64 %len, 0
  br i1 %has_slot0, label %check_slot0, label %missing
check_slot0:
  %key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %key0 = load ptr, ptr %key0_field
  %key0_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 3
  %key0_len = load i64, ptr %key0_len_field
  %slot0_matches = call i1 @kizu_rt_map_key_equal(ptr %key0, i64 %key0_len, %kizu.slice.u8 %key)
  br i1 %slot0_matches, label %found_slot0, label %check_slot1_capacity
found_slot0:
  %value0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 4
  %value0 = load i64, ptr %value0_field
  br label %found
check_slot1_capacity:
  %has_slot1 = icmp sgt i64 %len, 1
  br i1 %has_slot1, label %check_slot1, label %missing
check_slot1:
  %key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  %key1 = load ptr, ptr %key1_field
  %key1_len_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 6
  %key1_len = load i64, ptr %key1_len_field
  %slot1_matches = call i1 @kizu_rt_map_key_equal(ptr %key1, i64 %key1_len, %kizu.slice.u8 %key)
  br i1 %slot1_matches, label %found_slot1, label %missing
found_slot1:
  %value1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 7
  %value1 = load i64, ptr %value1_field
  br label %found
found:
  %value = phi i64 [%value0, %found_slot0], [%value1, %found_slot1]
  %ok = insertvalue %kizu.error.i64 poison, i1 true, 0
  %with_value = insertvalue %kizu.error.i64 %ok, i64 %value, 1
  %result = insertvalue %kizu.error.i64 %with_value, %kizu.slice.u8 zeroinitializer, 2
  ret %kizu.error.i64 %result
missing:
  %message_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_key_not_found, i64 0, i64 0
  %message_base = insertvalue %kizu.slice.u8 poison, ptr %message_ptr, 0
  %message = insertvalue %kizu.slice.u8 %message_base, i64 21, 1
  %missing_ok = insertvalue %kizu.error.i64 poison, i1 false, 0
  %missing_value = insertvalue %kizu.error.i64 %missing_ok, i64 0, 1
  %missing_result = insertvalue %kizu.error.i64 %missing_value, %kizu.slice.u8 %message, 2
  ret %kizu.error.i64 %missing_result
}

define void @kizu_rt_map_deinit(%kizu.owned %map) {
entry:
  %raw = extractvalue %kizu.owned %map, 0
  %allocator_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 0
  %allocator_ptr = load ptr, ptr %allocator_field
  %key0_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 2
  %key0 = load ptr, ptr %key0_field
  %key0_present = icmp ne ptr %key0, null
  br i1 %key0_present, label %free_key0, label %check_key1
free_key0:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %key0)
  br label %check_key1
check_key1:
  %key1_field = getelementptr inbounds %kizu.rt.map, ptr %raw, i32 0, i32 5
  %key1 = load ptr, ptr %key1_field
  %key1_present = icmp ne ptr %key1, null
  br i1 %key1_present, label %free_key1, label %free_map
free_key1:
  call void @kizu_rt_free(ptr %allocator_ptr, ptr %key1)
  br label %free_map
free_map:
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
  %raw = call ptr @kizu_rt_alloc(ptr %allocator_ptr, i64 72)
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
  %size_bounded = icmp sle i64 %element_size, 24
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
  %array_append_all_ok = and i1 %array_append_ok, %array_append_second_ok
  %array_first_all_ok = and i1 %array_view_ok, %array_first_ok
  %array_second_all_ok = and i1 %array_second_ok, %array_second_payload_ok
  %array_oob_all_ok = and i1 %array_oob_rejected, %array_oob_message_ok
  %array_invalid_all_ok = and i1 %array_invalid_rejected, %array_invalid_message_ok
  %array_ok_a = and i1 %array_append_all_ok, %array_len_ok
  %array_ok_b = and i1 %array_first_all_ok, %array_second_all_ok
  %array_ok_c = and i1 %array_oob_all_ok, %array_invalid_all_ok
  %array_ok_d = and i1 %array_ok_a, %array_ok_b
  %array_base_ok = and i1 %array_ok_c, %array_ok_d
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
  %map = call %kizu.owned @kizu_rt_map_new(%kizu.owned zeroinitializer)
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
  %map_insert_alpha = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha_from_temp,
    i64 11
  )
  %map_insert_alpha_ok = extractvalue %kizu.error.void %map_insert_alpha, 0
  %map_alpha_temp_first = getelementptr i8, ptr %map_alpha_temp, i64 0
  store i8 88, ptr %map_alpha_temp_first
  %map_insert_beta = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_beta,
    i64 22
  )
  %map_insert_beta_ok = extractvalue %kizu.error.void %map_insert_beta, 0
  %map_contains_alpha = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %map_alpha)
  %map_contains_beta = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %map_beta)
  %map_contains_gamma = call i1 @kizu_rt_map_contains(%kizu.owned %map, %kizu.slice.u8 %map_gamma)
  %map_gamma_missing = icmp eq i1 %map_contains_gamma, false
  %map_get_alpha = call %kizu.error.i64 @kizu_rt_map_get_i64(%kizu.owned %map, %kizu.slice.u8 %map_alpha)
  %map_get_alpha_ok = extractvalue %kizu.error.i64 %map_get_alpha, 0
  %map_get_alpha_value = extractvalue %kizu.error.i64 %map_get_alpha, 1
  %map_alpha_value_ok = icmp eq i64 %map_get_alpha_value, 11
  %map_get_beta = call %kizu.error.i64 @kizu_rt_map_get_i64(%kizu.owned %map, %kizu.slice.u8 %map_beta)
  %map_get_beta_ok = extractvalue %kizu.error.i64 %map_get_beta, 0
  %map_get_beta_value = extractvalue %kizu.error.i64 %map_get_beta, 1
  %map_beta_value_ok = icmp eq i64 %map_get_beta_value, 22
  %map_update_alpha = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha,
    i64 44
  )
  %map_update_alpha_ok = extractvalue %kizu.error.void %map_update_alpha, 0
  %map_get_alpha_updated = call %kizu.error.i64 @kizu_rt_map_get_i64(
    %kizu.owned %map,
    %kizu.slice.u8 %map_alpha
  )
  %map_get_alpha_updated_ok = extractvalue %kizu.error.i64 %map_get_alpha_updated, 0
  %map_get_alpha_updated_value = extractvalue %kizu.error.i64 %map_get_alpha_updated, 1
  %map_alpha_updated_value_ok = icmp eq i64 %map_get_alpha_updated_value, 44
  %map_get_gamma = call %kizu.error.i64 @kizu_rt_map_get_i64(%kizu.owned %map, %kizu.slice.u8 %map_gamma)
  %map_get_gamma_ok = extractvalue %kizu.error.i64 %map_get_gamma, 0
  %map_get_gamma_rejected = icmp eq i1 %map_get_gamma_ok, false
  %map_get_gamma_message = extractvalue %kizu.error.i64 %map_get_gamma, 2
  %map_missing_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_key_not_found, i64 0, i64 0
  %map_missing_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_missing_ptr,
    i64 21,
    %kizu.slice.u8 %map_get_gamma_message
  )
  %map_insert_gamma = call %kizu.error.void @kizu_rt_map_insert(
    %kizu.owned %map,
    %kizu.slice.u8 %map_gamma,
    i64 33
  )
  %map_insert_gamma_ok = extractvalue %kizu.error.void %map_insert_gamma, 0
  %map_full_rejected = icmp eq i1 %map_insert_gamma_ok, false
  %map_full_message = extractvalue %kizu.error.void %map_insert_gamma, 1
  %map_full_ptr = getelementptr inbounds [21 x i8], ptr @.kizu.rt.map_full, i64 0, i64 0
  %map_full_message_ok = call i1 @kizu_rt_map_key_equal(
    ptr %map_full_ptr,
    i64 21,
    %kizu.slice.u8 %map_full_message
  )
  %map_insert_ok_a = and i1 %map_insert_alpha_ok, %map_insert_beta_ok
  %map_contains_ok_a = and i1 %map_contains_alpha, %map_contains_beta
  %map_contains_ok = and i1 %map_contains_ok_a, %map_gamma_missing
  %map_get_alpha_all_ok = and i1 %map_get_alpha_ok, %map_alpha_value_ok
  %map_get_beta_all_ok = and i1 %map_get_beta_ok, %map_beta_value_ok
  %map_get_ok_a = and i1 %map_get_alpha_all_ok, %map_get_beta_all_ok
  %map_update_all_ok = and i1 %map_update_alpha_ok, %map_get_alpha_updated_ok
  %map_update_ok = and i1 %map_update_all_ok, %map_alpha_updated_value_ok
  %map_missing_ok_a = and i1 %map_get_gamma_rejected, %map_missing_message_ok
  %map_full_ok_a = and i1 %map_full_rejected, %map_full_message_ok
  %map_ok_a = and i1 %map_insert_ok_a, %map_contains_ok
  %map_get_and_update_ok = and i1 %map_get_ok_a, %map_update_ok
  %map_ok_b = and i1 %map_get_and_update_ok, %map_missing_ok_a
  %map_ok_c = and i1 %map_ok_a, %map_ok_b
  %map_ok = and i1 %map_ok_c, %map_full_ok_a
  br i1 %map_ok, label %map_pass, label %map_fail
map_fail:
  call void @kizu_rt_map_deinit(%kizu.owned %map)
  ret i64 1
map_pass:
  call void @kizu_rt_map_deinit(%kizu.owned %map)
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
  %abi_roundtrip = call i64 @kizu_selfhost__runtime_abi_roundtrip_smoke()
  %abi_roundtrip_ok = icmp eq i64 %abi_roundtrip, 0
  br i1 %abi_roundtrip_ok, label %abi_pass, label %abi_fail
abi_fail:
  ret i64 1
abi_pass:
  ret i64 0
}
