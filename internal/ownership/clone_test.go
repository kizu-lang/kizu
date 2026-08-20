package ownership

import (
	"reflect"
	"testing"
)

// copiedBindingFacts names the map and slice facts scope.clone copies. It is
// the list the guard below compares the struct against, so it is also where a
// new fact announces that someone decided what clone does with it.
var copiedBindingFacts = map[string]bool{
	"fieldBorrows":    true,
	"fieldMutBorrows": true,
	"fieldDeinit":     true,
	"fieldArenaIDs":   true,
	"borrowTargets":   true,
}

// TestBindingFactsAreCopiedByClone fails when a map or slice appears on binding
// that clone was never told about. Sharing one lets a branch write into what
// the other branches read, which is a union merge nobody declared — the shape
// that let a deinit release a field on one path only and still pass.
//
// Copying is only half of what a new fact owes. The other half is what happens
// where branches meet, and clone's doc comment records the answer each existing
// fact gives.
func TestBindingFactsAreCopiedByClone(t *testing.T) {
	layout := reflect.TypeOf(binding{})
	for index := 0; index < layout.NumField(); index++ {
		field := layout.Field(index)
		switch field.Type.Kind() {
		case reflect.Map, reflect.Slice:
		default:
			continue
		}
		if !copiedBindingFacts[field.Name] {
			t.Fatalf("binding.%s is a %s that scope.clone does not copy;"+
				" decide what a branch does with it, copy it in clone, and add it"+
				" to copiedBindingFacts", field.Name, field.Type.Kind())
		}
	}
}

// TestCloneDoesNotShareFieldFacts checks the copies are real: a branch's writes
// stay in that branch.
func TestCloneDoesNotShareFieldFacts(t *testing.T) {
	origin := &scope{values: map[string]*binding{}}
	value := &binding{
		id:              1,
		name:            "self",
		fieldBorrows:    map[string]int{"a": 1},
		fieldMutBorrows: map[string]int{"a": 1},
		fieldDeinit:     map[string]bool{"a": true},
		fieldArenaIDs:   map[string]int{"a": 7},
		borrowTargets:   []borrowSource{{field: "a"}},
	}
	origin.values["self"] = value

	branch := origin.clone()
	changed := branch.values["self"]
	changed.fieldBorrows["b"] = 1
	changed.fieldMutBorrows["b"] = 1
	changed.markFieldDeinit("b")
	changed.fieldArenaIDs["b"] = 9
	changed.borrowTargets = append(changed.borrowTargets, borrowSource{field: "b"})

	if len(value.fieldBorrows) != 1 || len(value.fieldMutBorrows) != 1 {
		t.Fatal("a branch's field borrow counts reached the scope it was cloned from")
	}
	if len(value.fieldDeinit) != 1 {
		t.Fatal("a branch's field cleanup reached the scope it was cloned from")
	}
	if len(value.fieldArenaIDs) != 1 {
		t.Fatal("a branch's field arena identity reached the scope it was cloned from")
	}
	if len(value.borrowTargets) != 1 {
		t.Fatal("a branch's borrow targets reached the scope it was cloned from")
	}
}
