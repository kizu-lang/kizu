package interp

import "fmt"

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
	kind      valueKind
	i         int64
	b         bool
	s         string
	typeName  string
	fields    map[string]Value
	arena     *Arena
	handle    Handle
	errUnion  *ErrorUnion
	enum      Enum
	union     Union
	taskGroup *TaskGroup
	task      *Task
	queue     *Queue
	channel   *Channel
	partition *Partition
	slot      PartitionSlot
	localBuf  LocalBuffer
	atomic    *Atomic
	mutex     *Mutex
	array     *Array
	mapValue  *Map
	box       *Box
	ref       *binding
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
		if v.errUnion.payload != nil {
			return "<error: " + v.errUnion.payload.String() + ">"
		}
		return "<error: " + v.errUnion.message + ">"
	case kindEnum:
		return v.enum.typeName + "::" + v.enum.tag
	case kindUnion:
		return v.union.typeName + "::" + v.union.tag
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
		return v.slot.partition.values[v.slot.index].String()
	case kindLocalBuffer:
		return "<localbuffer>"
	case kindAtomic:
		return "<atomic>"
	case kindMutex:
		return "<mutex>"
	case kindArray:
		return "<array>"
	case kindRef:
		return v.ref.value.String()
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

// structValue returns a runtime struct value.
func structValue(typeName string, fields map[string]Value) Value {
	return Value{kind: kindStruct, typeName: typeName, fields: fields}
}

// arenaValue returns an empty runtime arena.
func arenaValue() Value {
	return Value{kind: kindArena, arena: &Arena{}}
}

// handleValue returns an opaque handle runtime value.
func handleValue(arena *Arena, index int) Value {
	return Value{kind: kindHandle, handle: Handle{arena: arena, index: index}}
}

// errorUnionValue returns an error-union error runtime value.
func errorUnionValue(message string) Value {
	return Value{kind: kindErrorUnion, errUnion: &ErrorUnion{message: message}}
}

// typedErrorUnionValue returns a typed error runtime value.
func typedErrorUnionValue(payload Value) Value {
	return Value{kind: kindErrorUnion, errUnion: &ErrorUnion{payload: &payload}}
}

// enumValue returns a tag enum runtime value.
func enumValue(typeName string, tag string) Value {
	return Value{kind: kindEnum, enum: Enum{typeName: typeName, tag: tag}}
}

// unionValue returns a tagged union runtime value.
func unionValue(typeName string, tag string, payload *Value) Value {
	return Value{kind: kindUnion, union: Union{typeName: typeName, tag: tag, payload: payload}}
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
	return Value{kind: kindArray, typeName: typeName, array: &Array{}}
}

// mapValue returns an empty owned map value.
func mapValue(valueType string) Value {
	return Value{
		kind:     kindMap,
		typeName: fmt.Sprintf("std::map::Map<[]const u8, %s>", valueType),
		mapValue: &Map{
			valueType: valueType,
			entries:   map[string]Value{},
		},
	}
}

// boxValue returns one owned indirection value.
func boxValue(typeName string, value Value) Value {
	return Value{
		kind: kindBox, typeName: fmt.Sprintf("std::mem::Box<%s>", typeName),
		box: &Box{value: value},
	}
}

// taskGroupValue returns a structured task group value.
func taskGroupValue(io Value) Value {
	return Value{kind: kindTaskGroup, taskGroup: &TaskGroup{io: io}}
}

// completedTaskValue returns a completed task value.
func completedTaskValue(value Value, err error) Value {
	return Value{kind: kindTask, task: &Task{value: value, err: err}}
}

// runningTaskValue returns a task value backed by a running goroutine.
func runningTaskValue(result <-chan TaskResult) Value {
	return Value{kind: kindTask, task: &Task{result: result}}
}

// queueValue returns an empty deterministic task queue.
func queueValue() Value {
	return Value{kind: kindQueue, queue: &Queue{}}
}

// channelValue returns an empty owned-message channel.
func channelValue(typeName string) Value {
	return Value{kind: kindChannel, typeName: typeName, channel: &Channel{}}
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
	return Value{kind: kindPartition, partition: &Partition{values: values}}
}

// partitionSlotValue returns a mutable view into one disjoint partition slot.
func partitionSlotValue(partition *Partition, index int) Value {
	return Value{kind: kindPartitionSlot, slot: PartitionSlot{partition: partition, index: index}}
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
	return Value{kind: kindLocalBuffer, localBuf: LocalBuffer{values: values}}
}

// atomicValue returns a seq_cst primitive atomic value.
func atomicValue(typeName string, value Value) Value {
	return Value{kind: kindAtomic, typeName: typeName, atomic: &Atomic{value: value}}
}

// mutexValue returns a protected synchronous value.
func mutexValue(typeName string, value Value) Value {
	return Value{kind: kindMutex, typeName: typeName, mutex: &Mutex{value: value}}
}

// refValue returns a local borrow reference to a runtime binding.
func refValue(binding *binding) Value {
	return Value{kind: kindRef, ref: binding}
}

// functionNameValue carries a checked compile-time function reference through std wrappers.
func functionNameValue(name string) Value {
	return Value{kind: kindFunctionName, s: name}
}
