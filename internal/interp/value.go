package interp

import (
	"fmt"
	"sort"
)

type valueKind int

const (
	kindVoid valueKind = iota
	kindInt
	kindBool
	kindString
	kindStruct
	kindArena
	kindHandle
	kindErrorUnion
	kindEnum
	kindUnion
	kindIo
	kindAllocator
	kindTaskGroup
	kindTask
	kindQueue
	kindChannel
	kindPartition
	kindPartitionSlot
	kindLocalBuffer
	kindAtomic
	kindMutex
	kindArray
	kindMap
	kindBox
	kindRef
	kindFunctionName
	kindType
)

// Value is a runtime value produced by the Phase 2 interpreter.
type Value struct {
	kind     valueKind
	i        int64
	b        bool
	s        string
	typeName string
	object   any
}

// StructLayout stores field indexes shared by runtime struct instances.
type StructLayout struct {
	names  []string
	index  map[string]int
	sorted []string
}

// StructFields stores one runtime struct instance.
type StructFields struct {
	layout *StructLayout
	values []Value
	mapped map[string]Value
}

type StructFields1 struct {
	layout *StructLayout
	v0     Value
}

type StructFields2 struct {
	layout *StructLayout
	v0     Value
	v1     Value
}

type StructFields3 struct {
	layout *StructLayout
	v0     Value
	v1     Value
	v2     Value
}

type StructFields4 struct {
	layout *StructLayout
	v0     Value
	v1     Value
	v2     Value
	v3     Value
}

// Arena stores values and gives out opaque handles.
type Arena struct {
	values []Value
	deinit bool
}

// Handle identifies an arena element without exposing a raw pointer.
type Handle struct {
	arena *Arena
	index int
}

// Array stores owned contiguous values for the v0.2 std::array prototype.
type Array struct {
	values []Value
	deinit bool
}

// Map stores owned key/value entries for the v0.2 std::map prototype.
type Map struct {
	valueType string
	entries   map[string]Value
	deinit    bool
}

// Box stores one owned heap-like value for explicit indirection.
type Box struct {
	value  Value
	deinit bool
}

// ErrorUnion owns an error value for !T runtime propagation.
type ErrorUnion struct {
	message string
	payload *Value
}

// TaskGroup owns one structured task scope and its I/O runtime mode.
type TaskGroup struct {
	io Value
}

// TaskResult stores the result produced by a running task.
type TaskResult struct {
	value Value
	err   error
}

type taskState int

const (
	taskOpen taskState = iota
	taskAwaited
	taskCanceled
)

// Task stores the result of a spawned interpreter task.
type Task struct {
	value  Value
	err    error
	state  taskState
	result <-chan TaskResult
}

// Queue stores deterministic deferred jobs for the v0.1 task API.
type Queue struct {
	jobs []QueuedJob
}

// QueuedJob stores a function call captured by a Queue value.
type QueuedJob struct {
	name string
	args []Value
}

// Channel stores owned messages for the v0.1 synchronous channel model.
type Channel struct {
	values []Value
	closed bool
}

// Partition stores bounded disjoint output slots for safe data-parallel examples.
type Partition struct {
	values []Value
}

// PartitionSlot identifies one mutable slot inside a Partition.
type PartitionSlot struct {
	partition *Partition
	index     int
}

// LocalBuffer stores per-worker scratch slots for the v0.1 sequential model.
type LocalBuffer struct {
	values []Value
}

// Atomic stores one primitive value with seq_cst-only semantics in v0.1.
type Atomic struct {
	value Value
}

// Mutex stores one protected value for the v0.1 synchronous model.
type Mutex struct {
	value Value
}

// Enum stores a Zig/C-style enum tag value.
type Enum struct {
	typeName string
	tag      string
}

// Union stores a tagged union runtime value.
type Union struct {
	typeName string
	tag      string
	payload  *Value
}

// String formats a value for the print builtin and test assertions.
func (v Value) String() string {
	if out, ok := v.scalarString(); ok {
		return out
	}
	return v.objectString()
}

// scalarString formats primitive runtime values.
func (v Value) scalarString() (string, bool) {
	switch v.kind {
	case kindVoid:
		return "void", true
	case kindInt:
		return fmt.Sprintf("%d", v.i), true
	case kindBool:
		if v.b {
			return "true", true
		}
		return "false", true
	case kindString:
		return v.s, true
	case kindType:
		return v.typeName, true
	default:
		return "", false
	}
}

// objectString formats aggregate and capability runtime values.
func (v Value) objectString() string {
	switch v.kind {
	case kindStruct:
		return "<struct>"
	case kindArena:
		return "<arena>"
	case kindHandle:
		return "<handle>"
	case kindErrorUnion:
		errUnion := v.errUnionPayload()
		if errUnion.payload != nil {
			return "<error: " + errUnion.payload.String() + ">"
		}
		return "<error: " + errUnion.message + ">"
	case kindEnum:
		enum := v.enumPayload()
		return enum.typeName + "::" + enum.tag
	case kindUnion:
		union := v.unionPayload()
		return union.typeName + "::" + union.tag
	case kindMap:
		return "<map>"
	case kindBox:
		return "<box>"
	default:
		return v.capabilityString()
	}
}

// capabilityString formats capability and concurrency runtime values.
func (v Value) capabilityString() string {
	switch v.kind {
	case kindIo:
		return "<io:" + v.typeName + ">"
	case kindAllocator:
		return "<allocator:" + v.typeName + ">"
	case kindTaskGroup:
		return "<taskgroup>"
	case kindTask:
		return "<task>"
	case kindQueue:
		return "<queue>"
	case kindChannel:
		return "<channel>"
	case kindPartition:
		return "<partition>"
	case kindPartitionSlot:
		slot := v.slotPayload()
		return slot.partition.values[slot.index].String()
	case kindLocalBuffer:
		return "<localbuffer>"
	case kindAtomic:
		return "<atomic>"
	case kindMutex:
		return "<mutex>"
	case kindArray:
		return "<array>"
	case kindRef:
		return v.refPayload().value.String()
	default:
		return "<invalid>"
	}
}

// voidValue returns the singleton void runtime value.
func voidValue() Value {
	return Value{kind: kindVoid}
}

// intValue returns an integer runtime value.
func intValue(v int64) Value {
	return Value{kind: kindInt, i: v}
}

// boolValue returns a boolean runtime value.
func boolValue(v bool) Value {
	return Value{kind: kindBool, b: v}
}

// stringValue returns a string runtime value.
func stringValue(v string) Value {
	return Value{kind: kindString, s: v}
}

// typeValue returns a compile-time type runtime value.
func typeValue(name string) Value {
	return Value{kind: kindType, typeName: name}
}

// structValue returns a map-backed runtime struct value.
func structValue(typeName string, fields map[string]Value) Value {
	return Value{
		kind:     kindStruct,
		typeName: typeName,
		object:   &StructFields{mapped: fields},
	}
}

// structLayoutValue returns a layout-backed runtime struct value.
func structLayoutValue(typeName string, layout *StructLayout) Value {
	var object any
	switch len(layout.names) {
	case 1:
		object = &StructFields1{layout: layout}
	case 2:
		object = &StructFields2{layout: layout}
	case 3:
		object = &StructFields3{layout: layout}
	case 4:
		object = &StructFields4{layout: layout}
	default:
		object = &StructFields{layout: layout, values: make([]Value, len(layout.names))}
	}
	return Value{
		kind:     kindStruct,
		typeName: typeName,
		object:   object,
	}
}

// getStructField returns a named field from either runtime struct storage form.
func (v Value) getStructField(name string) (Value, bool) {
	if v.object == nil {
		return voidValue(), false
	}
	switch fields := v.object.(type) {
	case *StructFields:
		return fields.get(name)
	case *StructFields1:
		if fields.layout.names[0] == name {
			return fields.v0, true
		}
	case *StructFields2:
		return getStructField2(fields.layout, fields.v0, fields.v1, name)
	case *StructFields3:
		return getStructField3(fields.layout, fields.v0, fields.v1, fields.v2, name)
	case *StructFields4:
		return getStructField4(fields.layout, fields.v0, fields.v1, fields.v2, fields.v3, name)
	}
	return voidValue(), false
}

// setStructField writes a named field in either runtime struct storage form.
func (v *Value) setStructField(name string, value Value) bool {
	if v.object == nil {
		return false
	}
	switch fields := v.object.(type) {
	case *StructFields:
		return fields.set(name, value)
	case *StructFields1:
		if fields.layout.names[0] != name {
			return false
		}
		fields.v0 = value
		return true
	case *StructFields2:
		return setStructField2(fields, name, value)
	case *StructFields3:
		return setStructField3(fields, name, value)
	case *StructFields4:
		return setStructField4(fields, name, value)
	default:
		return false
	}
}

// getStructField2 returns one field from a two-field layout-backed struct.
func getStructField2(
	layout *StructLayout,
	v0 Value,
	v1 Value,
	name string,
) (Value, bool) {
	idx, ok := layout.index[name]
	if !ok {
		return voidValue(), false
	}
	if idx == 0 {
		return v0, true
	}
	if idx == 1 {
		return v1, true
	}
	return voidValue(), false
}

// getStructField3 returns one field from a three-field layout-backed struct.
func getStructField3(
	layout *StructLayout,
	v0 Value,
	v1 Value,
	v2 Value,
	name string,
) (Value, bool) {
	idx, ok := layout.index[name]
	if !ok {
		return voidValue(), false
	}
	switch idx {
	case 0:
		return v0, true
	case 1:
		return v1, true
	case 2:
		return v2, true
	default:
		return voidValue(), false
	}
}

// getStructField4 returns one field from a four-field layout-backed struct.
func getStructField4(
	layout *StructLayout,
	v0 Value,
	v1 Value,
	v2 Value,
	v3 Value,
	name string,
) (Value, bool) {
	idx, ok := layout.index[name]
	if !ok {
		return voidValue(), false
	}
	switch idx {
	case 0:
		return v0, true
	case 1:
		return v1, true
	case 2:
		return v2, true
	case 3:
		return v3, true
	default:
		return voidValue(), false
	}
}

// set writes one field in map-backed or large layout-backed struct storage.
func (fields *StructFields) set(name string, value Value) bool {
	if fields.mapped != nil {
		if _, ok := fields.mapped[name]; !ok {
			return false
		}
		fields.mapped[name] = value
		return true
	}
	idx, ok := fields.layout.index[name]
	if !ok || idx < 0 || idx >= len(fields.values) {
		return false
	}
	fields.values[idx] = value
	return true
}

// setStructField2 writes one field in a two-field layout-backed struct.
func setStructField2(fields *StructFields2, name string, value Value) bool {
	idx, ok := fields.layout.index[name]
	if !ok {
		return false
	}
	if idx == 0 {
		fields.v0 = value
		return true
	}
	if idx == 1 {
		fields.v1 = value
		return true
	}
	return false
}

// setStructField3 writes one field in a three-field layout-backed struct.
func setStructField3(fields *StructFields3, name string, value Value) bool {
	idx, ok := fields.layout.index[name]
	if !ok {
		return false
	}
	switch idx {
	case 0:
		fields.v0 = value
	case 1:
		fields.v1 = value
	case 2:
		fields.v2 = value
	default:
		return false
	}
	return true
}

// setStructField4 writes one field in a four-field layout-backed struct.
func setStructField4(fields *StructFields4, name string, value Value) bool {
	idx, ok := fields.layout.index[name]
	if !ok {
		return false
	}
	switch idx {
	case 0:
		fields.v0 = value
	case 1:
		fields.v1 = value
	case 2:
		fields.v2 = value
	case 3:
		fields.v3 = value
	default:
		return false
	}
	return true
}

// get returns a field from a runtime struct storage object.
func (fields *StructFields) get(name string) (Value, bool) {
	if fields.mapped != nil {
		value, ok := fields.mapped[name]
		return value, ok
	}
	idx, ok := fields.layout.index[name]
	if !ok || idx < 0 || idx >= len(fields.values) {
		return voidValue(), false
	}
	return fields.values[idx], true
}

// sortedNames returns deterministic field names without allocating for layout-backed structs.
func (fields *StructFields) sortedNames() []string {
	if fields.mapped == nil {
		return fields.layout.sorted
	}
	names := make([]string, 0, len(fields.mapped))
	for name := range fields.mapped {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// structFieldNames returns deterministic field names for a runtime struct value.
func (v Value) structFieldNames() []string {
	switch fields := v.object.(type) {
	case *StructFields:
		return fields.sortedNames()
	case *StructFields1:
		return fields.layout.sorted
	case *StructFields2:
		return fields.layout.sorted
	case *StructFields3:
		return fields.layout.sorted
	case *StructFields4:
		return fields.layout.sorted
	default:
		return nil
	}
}

// arenaPayload returns the Arena payload for an arena value.
func (v Value) arenaPayload() *Arena {
	return v.object.(*Arena)
}

// handlePayload returns the Handle payload for a handle value.
func (v Value) handlePayload() Handle {
	return v.object.(Handle)
}

// errUnionPayload returns the error-union payload for an error value.
func (v Value) errUnionPayload() *ErrorUnion {
	return v.object.(*ErrorUnion)
}

// enumPayload returns the enum payload for an enum value.
func (v Value) enumPayload() Enum {
	return v.object.(Enum)
}

// unionPayload returns the union payload for a union value.
func (v Value) unionPayload() Union {
	return v.object.(Union)
}

// taskGroupPayload returns the task-group payload for a task-group value.
func (v Value) taskGroupPayload() *TaskGroup {
	return v.object.(*TaskGroup)
}

// taskPayload returns the task payload for a task value.
func (v Value) taskPayload() *Task {
	return v.object.(*Task)
}

// queuePayload returns the queue payload for a queue value.
func (v Value) queuePayload() *Queue {
	return v.object.(*Queue)
}

// channelPayload returns the channel payload for a channel value.
func (v Value) channelPayload() *Channel {
	return v.object.(*Channel)
}

// partitionPayload returns the partition payload for a partition value.
func (v Value) partitionPayload() *Partition {
	return v.object.(*Partition)
}

// slotPayload returns the partition-slot payload for a slot value.
func (v Value) slotPayload() PartitionSlot {
	return v.object.(PartitionSlot)
}

// localBufferPayload returns the local-buffer payload for a local-buffer value.
func (v Value) localBufferPayload() LocalBuffer {
	return v.object.(LocalBuffer)
}

// atomicPayload returns the atomic payload for an atomic value.
func (v Value) atomicPayload() *Atomic {
	return v.object.(*Atomic)
}

// mutexPayload returns the mutex payload for a mutex value.
func (v Value) mutexPayload() *Mutex {
	return v.object.(*Mutex)
}

// arrayPayload returns the array payload for an array value.
func (v Value) arrayPayload() *Array {
	return v.object.(*Array)
}

// mapPayload returns the map payload for a map value.
func (v Value) mapPayload() *Map {
	return v.object.(*Map)
}

// boxPayload returns the box payload for a box value.
func (v Value) boxPayload() *Box {
	return v.object.(*Box)
}

// refPayload returns the borrow-reference payload for a ref value.
func (v Value) refPayload() *binding {
	return v.object.(*binding)
}

// arenaValue returns an empty runtime arena.
func arenaValue() Value {
	return Value{kind: kindArena, object: &Arena{}}
}

// handleValue returns an opaque handle runtime value.
func handleValue(arena *Arena, index int) Value {
	return Value{kind: kindHandle, object: Handle{arena: arena, index: index}}
}

// errorUnionValue returns an error-union error runtime value.
func errorUnionValue(message string) Value {
	return Value{kind: kindErrorUnion, object: &ErrorUnion{message: message}}
}

// typedErrorUnionValue returns a typed error runtime value.
func typedErrorUnionValue(payload Value) Value {
	return Value{kind: kindErrorUnion, object: &ErrorUnion{payload: &payload}}
}

// enumValue returns a tag enum runtime value.
func enumValue(typeName string, tag string) Value {
	return Value{kind: kindEnum, object: Enum{typeName: typeName, tag: tag}}
}

// unionValue returns a tagged union runtime value.
func unionValue(typeName string, tag string, payload *Value) Value {
	return Value{kind: kindUnion, object: Union{typeName: typeName, tag: tag, payload: payload}}
}

// ioValue returns an explicit I/O capability value.
func ioValue(mode string) Value {
	return Value{kind: kindIo, typeName: mode}
}

// allocatorValue returns an explicit allocator capability value.
func allocatorValue(name string) Value {
	return Value{kind: kindAllocator, typeName: name}
}

// arrayValue returns an empty owned array value.
func arrayValue(typeName string) Value {
	return Value{kind: kindArray, typeName: typeName, object: &Array{}}
}

// mapValue returns an empty owned map value.
func mapValue(valueType string) Value {
	return Value{
		kind:     kindMap,
		typeName: fmt.Sprintf("std::map::Map<[]u8, %s>", valueType),
		object: &Map{
			valueType: valueType,
			entries:   map[string]Value{},
		},
	}
}

// boxValue returns one owned indirection value.
func boxValue(typeName string, value Value) Value {
	return Value{
		kind: kindBox, typeName: fmt.Sprintf("std::mem::Box<%s>", typeName),
		object: &Box{value: value},
	}
}

// taskGroupValue returns a structured task group value.
func taskGroupValue(io Value) Value {
	return Value{kind: kindTaskGroup, object: &TaskGroup{io: io}}
}

// completedTaskValue returns a completed task value.
func completedTaskValue(value Value, err error) Value {
	return Value{kind: kindTask, object: &Task{value: value, err: err}}
}

// runningTaskValue returns a task value backed by a running goroutine.
func runningTaskValue(result <-chan TaskResult) Value {
	return Value{kind: kindTask, object: &Task{result: result}}
}

// queueValue returns an empty deterministic task queue.
func queueValue() Value {
	return Value{kind: kindQueue, object: &Queue{}}
}

// channelValue returns an empty owned-message channel.
func channelValue(typeName string) Value {
	return Value{kind: kindChannel, typeName: typeName, object: &Channel{}}
}

// partitionValue returns a bounded partition initialized with copied values.
func partitionValue(init Value, count int64) Value {
	if count < 0 {
		count = 0
	}
	values := make([]Value, 0, count)
	for idx := int64(0); idx < count; idx++ {
		values = append(values, init)
	}
	return Value{kind: kindPartition, object: &Partition{values: values}}
}

// partitionSlotValue returns a mutable view into one disjoint partition slot.
func partitionSlotValue(partition *Partition, index int) Value {
	return Value{kind: kindPartitionSlot, object: PartitionSlot{partition: partition, index: index}}
}

// localBufferValue returns worker-local scratch slots.
func localBufferValue(count int64, init Value) Value {
	if count < 0 {
		count = 0
	}
	values := make([]Value, 0, count)
	for idx := int64(0); idx < count; idx++ {
		values = append(values, init)
	}
	return Value{kind: kindLocalBuffer, object: LocalBuffer{values: values}}
}

// atomicValue returns a seq_cst primitive atomic value.
func atomicValue(typeName string, value Value) Value {
	return Value{kind: kindAtomic, typeName: typeName, object: &Atomic{value: value}}
}

// mutexValue returns a protected synchronous value.
func mutexValue(typeName string, value Value) Value {
	return Value{kind: kindMutex, typeName: typeName, object: &Mutex{value: value}}
}

// refValue returns a local borrow reference to a runtime binding.
func refValue(binding *binding) Value {
	return Value{kind: kindRef, object: binding}
}

// functionNameValue carries a checked compile-time function reference through std wrappers.
func functionNameValue(name string) Value {
	return Value{kind: kindFunctionName, s: name}
}
