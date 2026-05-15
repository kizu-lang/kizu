package interp

import "fmt"

type binding struct {
	value       Value
	mutable     bool
	fieldParent *binding
	fieldName   string
	arrayParent *Array
	arrayIndex  int
}

// Env stores lexical bindings for a function call or block execution.
type Env struct {
	parent   *Env
	bindings map[string]*binding
}

// NewEnv creates a root runtime environment.
func NewEnv() *Env {
	return &Env{bindings: map[string]*binding{}}
}

// Child creates a nested environment sharing outer bindings through parent.
func (e *Env) Child() *Env {
	return &Env{parent: e, bindings: map[string]*binding{}}
}

// Define creates a binding in the current environment.
func (e *Env) Define(name string, value Value, mutable bool) error {
	if _, ok := e.bindings[name]; ok {
		return fmt.Errorf("runtime error: `%s` is already defined", name)
	}
	e.bindings[name] = &binding{value: value, mutable: mutable}
	return nil
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
