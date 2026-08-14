package ir

// An env binds the names in scope to the SSA values they currently hold, and
// keeps the order the names were bound in.
//
// The order is the point. A phi node is created by walking the names a merge
// has to reconcile, and each phi takes the next SSA number as it is created, so
// whatever order that walk runs in becomes the order of the instructions in the
// block. A Go map has no order, and reading one back gives a different order
// every run, which made the same source lower to a different module each time.
// Keeping the order the source bound the names in makes the walk deterministic,
// and puts the phis of a block in the order a reader looks for them.
type env struct {
	order  []string
	values map[string]Value
}

// newEnv returns an environment with nothing bound.
func newEnv() *env {
	return &env{values: map[string]Value{}}
}

// get returns the value bound to name.
func (e *env) get(name string) (Value, bool) {
	value, ok := e.values[name]
	return value, ok
}

// set binds name to value. Rebinding a name that is already bound leaves it
// where it is, so an assignment in a loop body does not move the name to the
// end and reorder the header phis.
func (e *env) set(name string, value Value) {
	if _, bound := e.values[name]; !bound {
		e.order = append(e.order, name)
	}
	e.values[name] = value
}

// remove unbinds name, which is what a scope does to its declarations when it
// ends.
func (e *env) remove(name string) {
	if _, bound := e.values[name]; !bound {
		return
	}
	delete(e.values, name)
	kept := make([]string, 0, len(e.order)-1)
	for _, bound := range e.order {
		if bound != name {
			kept = append(kept, bound)
		}
	}
	e.order = kept
}

// names returns the bound names in the order they were bound. The result is
// the environment's own order and is meant to be read, not written.
func (e *env) names() []string {
	return e.order
}

// clone returns an independent copy, for lowering a branch that must not
// disturb the environment its siblings start from.
func (e *env) clone() *env {
	out := &env{
		order:  append([]string(nil), e.order...),
		values: make(map[string]Value, len(e.values)),
	}
	for name, value := range e.values {
		out.values[name] = value
	}
	return out
}
