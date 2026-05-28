package interp

import (
	"fmt"
	"sync"
)

var (
	envPool = sync.Pool{
		New: func() any { return &Env{} },
	}
	bindingPool = sync.Pool{
		New: func() any { return &binding{} },
	}
)

type binding struct {
	name    string
	value   Value
	alias   *bindingAlias
	mutable bool
}

// bindingAlias records where a borrow reference must write back. It is set only
// on Ref payloads that alias a struct field, array element, or box, and stays
// nil for ordinary environment cells so the hot path keeps binding small.
type bindingAlias struct {
	fieldParent *binding
	fieldName   string
	arrayParent *Array
	arrayIndex  int
	boxParent   *Box
}

// Env stores lexical bindings for a function call or block execution.
type Env struct {
	parent     *Env
	cache      map[string]*binding
	inline     [8]binding
	cells      []binding
	pooled     []*binding
	hasBorrows bool
}

// NewEnv creates a root runtime environment.
func NewEnv() *Env {
	return NewEnvWithCapacity(0)
}

// NewEnvWithCapacity creates a root environment sized for expected locals.
func NewEnvWithCapacity(capacity int) *Env {
	env := envPool.Get().(*Env)
	env.parent = nil
	env.hasBorrows = false
	if capacity <= len(env.inline) {
		env.cells = env.inline[:0]
	} else if cap(env.cells) < capacity {
		env.cells = make([]binding, 0, capacity)
	} else {
		env.cells = env.cells[:0]
	}
	env.pooled = env.pooled[:0]
	return env
}

// Child creates a nested environment sharing outer bindings through parent.
func (e *Env) Child() *Env {
	child := NewEnvWithCapacity(4)
	child.parent = e
	return child
}

// Define creates a binding in the current environment.
func (e *Env) Define(name string, value Value, mutable bool) error {
	if _, ok := e.localBinding(name); ok {
		return fmt.Errorf("runtime error: `%s` is already defined", name)
	}
	if e.cache != nil {
		delete(e.cache, name)
	}
	next := e.nextBinding()
	*next = binding{name: name, value: value, mutable: mutable}
	return nil
}

// bindingAt returns the binding at a precomputed lexical address, walking
// depth parents then indexing inline cell storage. It returns nil when the
// address falls outside cells (overflowed into the pool, or not yet defined)
// so callers fall back to name lookup.
func (e *Env) bindingAt(depth, index int) *binding {
	for d := 0; d < depth; d++ {
		if e == nil {
			return nil
		}
		e = e.parent
	}
	if e == nil || index < 0 || index >= len(e.cells) {
		return nil
	}
	return &e.cells[index]
}

// localBinding scans this environment's own bindings for a name. Scopes are
// small, so a linear scan over stable cell storage beats string-keyed hashing.
func (e *Env) localBinding(name string) (*binding, bool) {
	for i := range e.cells {
		if e.cells[i].name == name {
			return &e.cells[i], true
		}
	}
	for _, b := range e.pooled {
		if b.name == name {
			return b, true
		}
	}
	return nil, false
}

// nextBinding returns stable storage for one binding in this environment.
func (e *Env) nextBinding() *binding {
	if len(e.cells) < cap(e.cells) {
		index := len(e.cells)
		e.cells = e.cells[:index+1]
		return &e.cells[index]
	}
	next := bindingPool.Get().(*binding)
	e.pooled = append(e.pooled, next)
	return next
}

// Release returns an unborrowed environment and its bindings to reusable pools.
func (e *Env) Release() {
	if e == nil || e.hasBorrows {
		return
	}
	for name := range e.cache {
		delete(e.cache, name)
	}
	for index := range e.cells {
		e.cells[index] = binding{}
	}
	for _, cell := range e.pooled {
		*cell = binding{}
		bindingPool.Put(cell)
	}
	e.pooled = e.pooled[:0]
	e.parent = nil
	envPool.Put(e)
}

// SetMutable marks an existing binding as assignable in the nearest scope.
func (e *Env) SetMutable(name string) {
	if b, ok := e.localBinding(name); ok {
		b.mutable = true
		return
	}
	if e.parent != nil {
		e.parent.SetMutable(name)
	}
}

// Get returns a binding value from the nearest environment that defines it.
func (e *Env) Get(name string) (Value, bool) {
	if b, ok := e.localBinding(name); ok {
		return b.value, true
	}
	if e.parent != nil {
		if b, ok := e.cache[name]; ok {
			return b.value, true
		}
		if b, ok := e.parent.resolve(name); ok {
			if e.cache == nil {
				e.cache = make(map[string]*binding, 4)
			}
			e.cache[name] = b
			return b.value, true
		}
	}
	return voidValue(), false
}

// resolve returns a binding from this environment or an ancestor without side effects.
func (e *Env) resolve(name string) (*binding, bool) {
	if b, ok := e.localBinding(name); ok {
		return b, true
	}
	if e.parent != nil {
		if b, ok := e.cache[name]; ok {
			return b, true
		}
		return e.parent.resolve(name)
	}
	return nil, false
}

// Binding returns the mutable storage cell for a local name.
func (e *Env) Binding(name string) (*binding, bool) {
	if b, ok := e.localBinding(name); ok {
		e.hasBorrows = true
		return b, true
	}
	if e.parent != nil {
		return e.parent.Binding(name)
	}
	return nil, false
}

// Assign updates a mutable binding in the nearest environment that defines it.
func (e *Env) Assign(name string, value Value) error {
	if b, ok := e.localBinding(name); ok {
		if !b.mutable {
			return fmt.Errorf("runtime error: cannot assign to immutable binding `%s`", name)
		}
		b.value = value
		return nil
	}
	if e.parent != nil {
		return e.parent.Assign(name, value)
	}
	return fmt.Errorf("runtime error: undefined binding `%s`", name)
}
