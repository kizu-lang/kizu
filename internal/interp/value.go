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
	kindTaskGroup
	kindTask
	kindQueue
	kindChannel
	kindPartition
	kindLocalBuffer
	kindAtomic
	kindMutex
	kindRef
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
	task      *Task
	queue     *Queue
	channel   *Channel
	partition Partition
	localBuf  LocalBuffer
	atomic    *Atomic
	mutex     *Mutex
	ref       *binding
}

// Arena stores values and gives out opaque handles.
type Arena struct {
	values []Value
}

// Handle identifies an arena element without exposing a raw pointer.
type Handle struct {
	arena *Arena
	index int
}

// ErrorUnion stores an error value for !T runtime propagation.
type ErrorUnion struct {
	message string
}

// Task stores the synchronous result of a spawned interpreter task.
type Task struct {
	value Value
	done  bool
}

// Queue stores deterministic deferred jobs for the v0.1 task API.
type Queue struct {
	jobs []QueuedJob
}

// QueuedJob stores a function call captured by std.task.Queue.enqueue.
type QueuedJob struct {
	name string
	args []Value
}

// Channel stores owned messages for the v0.1 synchronous channel model.
type Channel struct {
	values []Value
	closed bool
}

// Partition stores a bounded index partition used by safe data-parallel examples.
type Partition struct {
	count int64
}

// LocalBuffer stores per-worker scratch slots for the v0.1 sequential model.
type LocalBuffer struct {
	values []Value
}

// Atomic stores one integer with seq_cst-only semantics in v0.1.
type Atomic struct {
	value int64
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
		return "<error: " + v.errUnion.message + ">"
	case kindEnum:
		return v.enum.typeName + "." + v.enum.tag
	case kindUnion:
		return v.union.typeName + "." + v.union.tag
	default:
		return v.capabilityString()
	}
}

// capabilityString formats capability and concurrency runtime values.
func (v Value) capabilityString() string {
	switch v.kind {
	case kindIo:
		return "<io>"
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
	case kindLocalBuffer:
		return "<localbuffer>"
	case kindAtomic:
		return "<atomic>"
	case kindMutex:
		return "<mutex>"
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

// enumValue returns a tag enum runtime value.
func enumValue(typeName string, tag string) Value {
	return Value{kind: kindEnum, enum: Enum{typeName: typeName, tag: tag}}
}

// unionValue returns a tagged union runtime value.
func unionValue(typeName string, tag string, payload *Value) Value {
	return Value{kind: kindUnion, union: Union{typeName: typeName, tag: tag, payload: payload}}
}

// ioValue returns an explicit I/O capability value.
func ioValue() Value {
	return Value{kind: kindIo}
}

// taskGroupValue returns a structured task group value.
func taskGroupValue() Value {
	return Value{kind: kindTaskGroup}
}

// taskValue returns a completed synchronous task value.
func taskValue(value Value) Value {
	return Value{kind: kindTask, task: &Task{value: value}}
}

// queueValue returns an empty deterministic task queue.
func queueValue() Value {
	return Value{kind: kindQueue, queue: &Queue{}}
}

// channelValue returns an empty owned-message channel.
func channelValue() Value {
	return Value{kind: kindChannel, channel: &Channel{}}
}

// partitionValue returns a bounded partition marker.
func partitionValue(count int64) Value {
	return Value{kind: kindPartition, partition: Partition{count: count}}
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

// atomicValue returns a seq_cst integer atomic value.
func atomicValue(value int64) Value {
	return Value{kind: kindAtomic, atomic: &Atomic{value: value}}
}

// mutexValue returns a protected synchronous value.
func mutexValue(value Value) Value {
	return Value{kind: kindMutex, mutex: &Mutex{value: value}}
}

// refValue returns a local borrow reference to a runtime binding.
func refValue(binding *binding) Value {
	return Value{kind: kindRef, ref: binding}
}
