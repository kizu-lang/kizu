package ownership

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/typ"
)

// checkStdMethodCall applies one std container method to its receiver. The
// method's signature says how the receiver and each argument are handed over
// and what comes back, as it does for a user method; the method's facts
// (stdmethod.Facts) say what the signature cannot — how the call touches the
// storage a live borrow may alias, whether the allocator it names must be the
// container's own, whether an element it hands out must be a copy, whether
// its result exists only in a capture or a `let`. Local receivers, direct
// field receivers, and the std primitives behind the wrappers all come here.
func (c *Checker) checkStdMethodCall(
	receiver *binding,
	base string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	container, ok := stdmethod.Lookup(base)
	if !ok {
		return "", errorf("move error: `%s` has no container access table", base)
	}
	facts, known := container.Methods[name]
	if !known || (facts.StdOnly && !c.currentStd) {
		return "", errorf("%s error: %s has no method `%s`", container.Kind, container.Label, name)
	}
	if err := checkContainerMethodAccess(base, receiver, name); err != nil {
		return "", err
	}
	method := c.stdMethodInfo(base, receiver.typeName, name)
	if method == nil {
		return "", errorf("%s error: %s has no method `%s`", container.Kind, container.Label, name)
	}
	if err := checkStdReceiverKind(container, method, receiver, name); err != nil {
		return "", err
	}
	if err := c.checkStdResultContext(container, facts, receiver, name); err != nil {
		return "", err
	}
	if len(args) != len(method.params)-1 {
		return "", errorf("%s error: `%s.%s` expects %d args, got %d",
			container.Kind, container.Label, name, len(method.params)-1, len(args))
	}
	if err := c.checkStdCopyElem(container, facts, receiver, name); err != nil {
		return "", err
	}
	if err := c.checkStdMethodArgs(container, facts, method, receiver, name, args, env); err != nil {
		return "", err
	}
	if facts.Access == stdmethod.AccessCleanup {
		if err := c.checkLazyDefaultConsume(receiver.name, "consumed", receiver.declSpan); err != nil {
			return "", err
		}
		// Releasing the value is the last thing done with it, so the binding
		// is consumed rather than left readable.
		receiver.moved = true
		releaseConsumedBorrows(receiver)
		if facts.Deinitializes {
			receiver.deinitialized = true
		}
	}
	return returnTypeName(method), nil
}

// stdMethodInfo is the signature one call to a std container method sees:
// the declaration's, with the container's type arguments substituted for its
// type parameters, so `T` reads as the element the receiver holds.
func (c *Checker) stdMethodInfo(base string, receiverType string, name string) *functionInfo {
	fn := c.implMethod(base, name)
	if fn == nil {
		return nil
	}
	params := fn.sig.TypeParamNames()
	if len(params) == 0 {
		return fn
	}
	_, argsText, ok := splitGenericType(receiverType)
	if !ok {
		return fn
	}
	args, err := typ.SplitArgs(argsText)
	if err != nil || len(args) != len(params) {
		return fn
	}
	subst := make(map[string]string, len(params))
	for idx, param := range params {
		subst[param] = args[idx]
	}
	return instantiateFunctionInfo(fn, subst)
}

// containerElemType is the type a container hands out by value: the element
// of an Array, Box, or Arena, the value of a Map.
func containerElemType(receiverType string) string {
	_, argsText, ok := splitGenericType(receiverType)
	if !ok {
		return receiverType
	}
	args, err := typ.SplitArgs(argsText)
	if err != nil || len(args) == 0 {
		return argsText
	}
	return args[len(args)-1]
}

// checkStdCopyElem refuses duplicating an owner element by value: only a copy
// element can leave the container this way, an owner needs per-type
// deep-copy logic (ADR-0124). A type parameter is left to its instantiation.
func (c *Checker) checkStdCopyElem(
	container stdmethod.Container,
	facts stdmethod.Facts,
	receiver *binding,
	name string,
) error {
	if !facts.CopyElem {
		return nil
	}
	elem := containerElemType(receiver.typeName)
	if isGenericParamName(elem) || c.isCopyType(elem) {
		return nil
	}
	return errorf("%s error: `%s.%s` requires copy %s",
		container.Kind, container.Label, name, container.Elem)
}

// checkStdReceiverKind holds the receiver to what the signature asks of it:
// a `&var self` method needs a receiver that can be mutated, and a method that
// takes its receiver over needs one the caller owns, since a lender would
// still hold the released value.
func checkStdReceiverKind(
	container stdmethod.Container,
	method *functionInfo,
	receiver *binding,
	name string,
) error {
	if len(method.params) == 0 {
		return errorf("move error: method `%s` must have self parameter", method.name)
	}
	self := method.params[0]
	if functionConsumesReceiver(method) && receiver.borrowedParam {
		return errorf("%s error: `%s.%s` requires owned %s receiver",
			container.Kind, container.Label, name, container.Label)
	}
	if self.borrow && self.mutBorrow && receiver.borrowedParam && !receiver.mutBorrow {
		return errorf("%s error: `%s.%s` requires mutable %s receiver",
			container.Kind, container.Label, name, container.Label)
	}
	return nil
}

// checkStdResultContext refuses a result that exists only in one place: a
// borrow optional outside the capture condition that consumes it, and a view
// outside the `let` that binds it. A mutable capture also needs a mutable
// place to borrow from.
func (c *Checker) checkStdResultContext(
	container stdmethod.Container,
	facts stdmethod.Facts,
	receiver *binding,
	name string,
) error {
	switch facts.Access {
	case stdmethod.AccessCapture:
		if !c.captureCondition && !c.borrowReturn {
			return errorf("%s error: `%s.%s` must be consumed by a capture"+
				" (`if value.%s(...) |name|` or `while value.%s(...) |name|`)",
				container.Kind, container.Label, name, name, name)
		}
		if strings.HasSuffix(name, "_mut") && !mutablePlace(receiver) {
			return errorf("%s error: `%s.%s` requires mutable %s binding",
				container.Kind, container.Label, name, container.Kind)
		}
	case stdmethod.AccessView:
		return errorf("%s error: `%s.%s` must be bound with `let name = value.%s()`",
			container.Kind, container.Label, name, name)
	}
	return nil
}

// checkStdMethodArgs applies the signature to the arguments, then what the
// facts add: a growth's allocator and a cleanup's must be the container's own,
// and an arena handle must belong to the arena it is used on. A capture
// accessor reads its arguments with the capture context off, so a nested
// accessor in argument position refuses as usual.
func (c *Checker) checkStdMethodArgs(
	container stdmethod.Container,
	facts stdmethod.Facts,
	method *functionInfo,
	receiver *binding,
	name string,
	args []ast.Expression,
	env *scope,
) error {
	check := func(idx int, arg ast.Expression) error {
		if facts.Lent == idx+1 {
			_, err := c.readExpr(arg, env)
			return err
		}
		return c.checkImplMethodArg(method, idx+1, arg, env)
	}
	savedCapture, savedReturn := c.captureCondition, c.borrowReturn
	if facts.Access == stdmethod.AccessCapture {
		c.captureCondition, c.borrowReturn = false, false
	}
	err := c.checkMethodArgs(method, receiver, args, env, false, check)
	c.captureCondition, c.borrowReturn = savedCapture, savedReturn
	if err != nil {
		return err
	}
	label := container.Label + "." + name
	if facts.Grows {
		if err := c.checkReleaseTie(label, receiver, args[0], env); err != nil {
			return err
		}
	}
	if facts.Access == stdmethod.AccessCleanup && len(args) == 1 &&
		method.params[1].typeName == "Allocator" {
		if err := c.checkReleaseTie(label, receiver, args[0], env); err != nil {
			return err
		}
	}
	for idx, arg := range args {
		if base, _, ok := splitGenericType(method.params[idx+1].typeName); ok &&
			base == "std::arena::Handle" {
			if err := c.checkKnownHandleProvenance(receiver, arg, env); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkContainerMethodAccess refuses a container method whose access to the
// storage collides with a live borrow of it: a read waits for mutable
// borrows, a mutation or cleanup for any borrow. A method the container does
// not have is refused here too.
func checkContainerMethodAccess(base string, value *binding, name string) error {
	container, ok := stdmethod.Lookup(base)
	if !ok {
		return errorf("move error: `%s` has no container access table", base)
	}
	facts, known := container.Methods[name]
	if !known {
		return errorf("%s error: %s has no method `%s`", container.Kind, container.Label, name)
	}
	switch facts.Access {
	case stdmethod.AccessRead:
		if value.activeMutBorrows > 0 {
			return errorf("%s error: `%s.%s` cannot read while mutably borrowed",
				container.Kind, container.Label, name)
		}
	case stdmethod.AccessMutate, stdmethod.AccessCleanup:
		if value.hasAnyBorrow() {
			return errorf("%s error: `%s.%s` cannot run while %s is borrowed",
				container.Kind, container.Label, name, container.Kind)
		}
	}
	return nil
}
