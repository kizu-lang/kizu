package stdmethod

// Access classifies what a container method does to the storage a live borrow
// may alias: reads wait for mutable borrows, mutations and cleanup wait for any
// borrow, capture accessors yield a borrow optional that exists only in a
// capture condition, and view producers yield a view that exists only where a
// `let` binds it.
type Access int

const (
	// AccessRead reads the storage.
	AccessRead Access = iota
	// AccessMutate changes the storage.
	AccessMutate
	// AccessCleanup releases the storage and consumes the receiver.
	AccessCleanup
	// AccessCapture yields a borrow optional a capture consumes.
	AccessCapture
	// AccessView yields a view a `let` binds.
	AccessView
)

// Facts is what a std container method's signature does not say. The
// signature carries how the receiver and each argument are handed over and
// what comes back; these say how the call touches the storage a borrow may
// alias, whether the allocator it names must be the container's own, whether
// an element it hands out by value must be a copy, and who may call it.
type Facts struct {
	Access Access
	// Grows marks a call that buys storage: its first argument names the
	// allocator, which must be the one the container was built from, since
	// the container's cleanup names one allocator for all of it (ADR-0132).
	Grows bool
	// CopyElem marks a call that duplicates an element by value, which only a
	// copy element allows; an owner needs per-type deep-copy logic
	// (ADR-0124).
	CopyElem bool
	// Lent is the position, counted from 1 among the arguments after the
	// receiver, of a parameter the container copies or only compares, so a
	// view handed to it is lent, not kept: a Map stores its own copy of a
	// key and looks one up by comparison. Zero when there is none.
	Lent int
	// StdOnly marks a helper reserved to std source.
	StdOnly bool
	// Deinitializes marks a cleanup after which a use reports the cleanup,
	// not a move.
	Deinitializes bool
}

// Container is one std container and how its diagnostics spell it: Kind is
// the error prefix and the noun for the storage, Label the type as a method
// call spells it, Elem the noun for what it holds.
type Container struct {
	Kind    string
	Label   string
	Elem    string
	Methods map[string]Facts
}

// containers is the one classification the checkers share. A method missing
// here is refused at dispatch, so a new method has to name what it does to
// storage before any check sees it.
var containers = map[string]Container{
	"std::array::Array": {Kind: "array", Label: "Array", Elem: "element", Methods: map[string]Facts{
		"append":       {Access: AccessMutate, Grows: true},
		"append_bytes": {Access: AccessMutate, Grows: true},
		"reserve":      {Access: AccessMutate, Grows: true},
		"set":          {Access: AccessMutate},
		"swap":         {Access: AccessMutate},
		"pop":          {Access: AccessMutate},
		"pop_or_panic": {Access: AccessMutate},
		"truncate":     {Access: AccessMutate, StdOnly: true},
		"clear":        {Access: AccessMutate, StdOnly: true},
		"len":          {Access: AccessRead},
		"capacity":     {Access: AccessRead},
		"get":          {Access: AccessRead, CopyElem: true},
		"get_or_panic": {Access: AccessRead, CopyElem: true},
		"clone":        {Access: AccessRead, CopyElem: true},
		// Unlike String's, Array's as_bytes/as_mut_bytes are std-internal
		// calls guarded as reads; String's form view bindings and are
		// guarded where the binding forms.
		"as_bytes":     {Access: AccessRead, StdOnly: true},
		"as_mut_bytes": {Access: AccessRead, StdOnly: true},
		"at":           {Access: AccessCapture},
		"at_mut":       {Access: AccessCapture},
		"deinit":       {Access: AccessCleanup},
	}},
	"std::map::Map": {Kind: "map", Label: "Map", Elem: "value", Methods: map[string]Facts{
		"insert":   {Access: AccessMutate, Grows: true, Lent: 2},
		"get":      {Access: AccessRead, CopyElem: true, Lent: 1},
		"key_at":   {Access: AccessRead},
		"contains": {Access: AccessRead, Lent: 1},
		"len":      {Access: AccessRead},
		"at":       {Access: AccessCapture, Lent: 1},
		"at_mut":   {Access: AccessCapture, Lent: 1},
		"deinit":   {Access: AccessCleanup},
	}},
	"std::string::String": {Kind: "string", Label: "String", Elem: "byte", Methods: map[string]Facts{
		"append_bytes":  {Access: AccessMutate, Grows: true},
		"append_byte":   {Access: AccessMutate, Grows: true},
		"append_string": {Access: AccessMutate, Grows: true},
		"reserve":       {Access: AccessMutate, Grows: true},
		"truncate":      {Access: AccessMutate},
		"clear":         {Access: AccessMutate},
		"len":           {Access: AccessRead},
		"capacity":      {Access: AccessRead},
		"as_bytes":      {Access: AccessView},
		"as_mut_bytes":  {Access: AccessView},
		"deinit":        {Access: AccessCleanup},
	}},
	"std::mem::Box": {Kind: "box", Label: "Box", Elem: "value", Methods: map[string]Facts{
		"borrow":     {Access: AccessView},
		"borrow_mut": {Access: AccessView},
		"take":       {Access: AccessCleanup},
		"deinit":     {Access: AccessCleanup},
	}},
	"std::arena::Arena": {Kind: "arena", Label: "Arena", Elem: "element", Methods: map[string]Facts{
		"add":    {Access: AccessMutate, Grows: true},
		"at":     {Access: AccessRead},
		"at_mut": {Access: AccessCapture},
		"deinit": {Access: AccessCleanup, Deinitializes: true},
	}},
}

// Lookup returns the container filed under base.
func Lookup(base string) (Container, bool) {
	container, ok := containers[base]
	return container, ok
}

// MethodFacts returns the facts of one container method.
func MethodFacts(base string, name string) (Facts, bool) {
	container, ok := containers[base]
	if !ok {
		return Facts{}, false
	}
	facts, ok := container.Methods[name]
	return facts, ok
}
