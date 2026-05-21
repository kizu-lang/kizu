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
	value       Value
	mutable     bool
	fieldParent *binding
	fieldName   string
	arrayParent *Array
	arrayIndex  int
	boxParent   *Box
}

// Env stores lexical bindings for a function call or block execution.
type Env struct {
	parent     *Env
	bindings   map[string]*binding
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
	if env.bindings == nil {
		env.bindings = make(map[string]*binding, capacity)
	}
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
	if _, ok := e.bindings[name]; ok {
		return fmt.Errorf("runtime error: `%s` is already defined", name)
	}
	next := bindingPool.Get().(*binding)
	*next = binding{value: value, mutable: mutable}
	e.bindings[name] = next
	return nil
}

// Release returns an unborrowed environment and its bindings to reusable pools.
func (e *Env) Release() {
	if e == nil || e.hasBorrows {
		return
	}
	for name, cell := range e.bindings {
		delete(e.bindings, name)
		*cell = binding{}
		bindingPool.Put(cell)
	}
	e.parent = nil
	envPool.Put(e)
}

// SetMutable marks an existing binding as assignable in the nearest scope.
func (e *Env) SetMutable(name string) {
	if b, ok := e.bindings[name]; ok {
		b.mutable = true
		return
	}
	if e.parent != nil {
		e.parent.SetMutable(name)
	}
}

// Get returns a binding value from the nearest environment that defines it.
func (e *Env) Get(name string) (Value, bool) {
	if b, ok := e.bindings[name]; ok {
		return b.value, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return voidValue(), false
}

// Binding returns the mutable storage cell for a local name.
func (e *Env) Binding(name string) (*binding, bool) {
	if b, ok := e.bindings[name]; ok {
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
	if b, ok := e.bindings[name]; ok {
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
