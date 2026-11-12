// -*- mode: Bluespec; -*-
// Known shapes for the unsafe marker model: what SPEC §12 and the examples say
// the checker accepts and rejects, and that the obligation layer sees the
// operation each rejection is there for. `quint test unsafecap_test.qnt`.
//
// compiler/src/unsafecap_mbt_test.kizu puts the same shapes to the checker.
module unsafecap_test {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = false,
  ).* from "./unsafecap"

  pure def leaf(m: int, a: Atom): Leaf = { m: m, a: a }
  pure def single(m: int, a: Atom): Expr = { ...NO_EXPR, l: leaf(m, a) }
  pure def sum(m: int, l: Leaf, r: Leaf): Expr = { ...NO_EXPR, m: m, shape: Sum, l: l, r: r }
  pure def block(m: int, l: Leaf, ls: bool, r: Leaf, rs: bool): Expr =
    { m: m, shape: Block, l: l, r: r, lSafety: ls, rSafety: rs }

  pure def print(e: Expr, safety: bool): Stmt = mkStmt(Print, 0, e, SafeTy, Sf, Sv, safety)
  pure def form(f: Form, m: int, e: Expr, safety: bool): Stmt = mkStmt(f, m, e, SafeTy, Sf, Sv, safety)
  pure def bind(t: FnTy, n: FnName, m: int, safety: bool): Stmt = mkStmt(Bind, m, NO_EXPR, t, n, Sv, safety)
  pure def deferCall(m: int, v: VoidCall, safety: bool): Stmt = mkStmt(DeferS, m, NO_EXPR, SafeTy, Sf, v, safety)

  pure def func(k: FnKind, file: File, body: List[Stmt]): Func = { kind: k, file: file, body: body }
  pure val EMPTY_G: Func = func(SafeFn, Home, List())
  // A program whose g is empty: f alone.
  pure def onlyF(k: FnKind, file: File, body: List[Stmt]): Prog = { g: EMPTY_G, f: func(k, file, body) }
  pure def fn1(body: List[Stmt]): Prog = onlyF(SafeFn, Home, body)

  pure def allDischarged(p: Prog): bool = obligationsOf(p).forall(o => o.discharged)

  // SPEC §12: `unsafe ptr_write(p, 20);` with its `// SAFETY:`.
  run markedOperationTest = all {
    val p = fn1(List(form(PtrWriteS, 1, single(0, Lit), true)))
    assert(errorsOf(p) == Set() and allDischarged(p)),
  }

  // tests/check/neg_ptr_read_without_unsafe.kizu
  run unmarkedReadTest = all {
    val p = fn1(List(print(single(0, Read), false)))
    assert(errorsOf(p) == Set(Missing(PtrRead)) and not(allDischarged(p))),
  }

  // Each operation of SPEC §12's table names its kind when it has no marker.
  run everyKindIsNamedTest = all {
    val atoms = Set(
      (Read, PtrRead), (Deref, PtrDeref), (VolRead, Volatile), (Offset, PtrOffset), (Cast, PtrCast),
      (IntCast, PtrIntCast), (ExtCall, ExternCall), (UfCall, UnsafeCall), (UnsafeFnPtr, UnsafeCall),
      (ExternFnPtr, ExternCall), (Build, StructInvariant),
    )
    assert(atoms.forall(((a, c)) => errorsOf(fn1(List(print(single(0, a), false)))) == Set(Missing(c)))),
  }

  // What is not an unproven operation needs no marker, and a marker on it covers nothing.
  run safeOperandsTest = all {
    val atoms = Set(Lit, SafeCall, SafeFnPtr, FieldRead)
    assert(atoms.forall(a => and {
      errorsOf(fn1(List(print(single(0, a), false)))) == Set(),
      errorsOf(fn1(List(print(single(1, a), true)))) == Set(Unused),
    })),
  }

  // SPEC §12: one marker covers every operation in what it covers.
  run oneMarkerCoversBothTest = all {
    val p = fn1(List(form(PtrWriteS, 1, single(0, Read), true)))
    assert(errorsOf(p) == Set()),
  }

  // tests/check/neg_redundant_nested_unsafe_marker.kizu: an inner marker over
  // the only operation leaves the outer one with nothing.
  run redundantOuterMarkerTest = all {
    val nested = fn1(List(form(PtrWriteS, 1, single(1, Read), true)))
    val twice = fn1(List(print(single(2, Read), true)))
    val outerOverInner = fn1(List(print(sum(1, leaf(1, Read), leaf(1, ExtCall)), true)))
    assert(and {
      // ptr_write is the outer marker's own operation.
      errorsOf(nested) == Set(),
      errorsOf(twice) == Set(Unused),
      errorsOf(outerOverInner) == Set(Unused),
    }),
  }

  // An outer marker that covers one operand, an inner one over the other: both used.
  run outerAndInnerBothUsedTest = all {
    val p = fn1(List(print(sum(1, leaf(1, Read), leaf(0, ExtCall)), true)))
    assert(errorsOf(p) == Set()),
  }

  // SPEC §12: `unsafe p.* = value` covers the target; the value is another expression.
  run assignmentMarkerCoversTargetTest = all {
    val bare = fn1(List(form(DerefSet, 1, single(0, Read), true)))
    val marked = fn1(List(form(DerefSet, 1, single(1, Read), true)))
    assert(errorsOf(bare) == Set(Missing(PtrRead)) and errorsOf(marked) == Set()),
  }

  // tests/check/neg_unsafe_without_safety_comment.kizu
  run safetyCommentTest = all {
    val p = fn1(List(print(single(1, Read), false)))
    assert(errorsOf(p) == Set(NoSafety)),
  }

  // tests/check/neg_unsafe_safety_comment_does_not_reach_nested.kizu, as a
  // block: the outer statement's comment does not reach the inner statement.
  run safetyDoesNotReachInnerStatementTest = all {
    val p = fn1(List(print(block(0, leaf(1, Read), false, leaf(0, Lit), false), true)))
    val own = fn1(List(print(block(0, leaf(1, Read), true, leaf(0, Lit), false), false)))
    assert(errorsOf(p) == Set(NoSafety) and errorsOf(own) == Set()),
  }

  // A marker over the whole `if` covers the operations of its inner statements
  // (SPEC §12: everything in what it covers), and those statements have no
  // marker of their own, so they need no comment.
  run outerMarkerCoversInnerStatementsTest = all {
    val p = fn1(List(print(block(1, leaf(0, Read), false, leaf(0, ExtCall), false), true)))
    assert(errorsOf(p) == Set() and allDischarged(p)),
  }

  // SPEC §12: an unsafe fn body is not covered implicitly, and calling it needs a marker.
  run unsafeFnBodyIsNotCoveredTest = all {
    val body = onlyF(UnsafeFn, Home, List(print(single(0, Read), false)))
    val callee = {
      g: func(UnsafeFn, Home, List(print(single(1, Read), true))),
      f: func(SafeFn, Home, List(print(single(0, CallG), false))),
    }
    assert(errorsOf(body) == Set(Missing(PtrRead)) and errorsOf(callee) == Set(Missing(UnsafeCall))),
  }

  // A marker at the call does not reach into the callee: a safe g that does an
  // unmarked read is rejected in g, and the marker at the call covers nothing.
  run markerDoesNotLeakIntoCalleeTest = all {
    val p = {
      g: func(SafeFn, Home, List(print(single(0, Read), false))),
      f: func(SafeFn, Home, List(print(single(1, CallG), true))),
    }
    val unsafeG = {
      g: func(UnsafeFn, Home, List(print(single(0, Read), false))),
      f: func(SafeFn, Home, List(print(single(1, CallG), true))),
    }
    assert(and {
      errorsOf(p) == Set(Missing(PtrRead), Unused),
      errorsOf(unsafeG) == Set(Missing(PtrRead)),
      not(allDischarged(unsafeG)),
    }),
  }

  // SPEC §12: through `fn(...)` no marker, through `unsafe fn(...)` and
  // `extern "c" fn(...)` a marker; and a function is bound only at its own type.
  run functionPointersTest = all {
    assert(and {
      errorsOf(fn1(List(bind(SafeTy, Sf, 0, false)))) == Set(),
      errorsOf(fn1(List(bind(UnsafeTy, Uf, 1, true)))) == Set(),
      errorsOf(fn1(List(bind(ExternTy, Ext, 1, true)))) == Set(),
      errorsOf(fn1(List(bind(UnsafeTy, Uf, 0, false)))) == Set(Missing(UnsafeCall)),
      errorsOf(fn1(List(bind(ExternTy, Ext, 0, false)))) == Set(Missing(ExternCall)),
      // laundering: an unsafe fn at a safe pointer type.
      errorsOf(fn1(List(bind(SafeTy, Uf, 0, false)))) == Set(FnTypeMismatch),
      errorsOf(fn1(List(bind(SafeTy, Ext, 0, false)))) == Set(FnTypeMismatch),
      not(allDischarged(fn1(List(bind(SafeTy, Uf, 0, false))))),
    }),
  }

  run deferredCallTest = all {
    assert(and {
      errorsOf(fn1(List(deferCall(1, Ev, true)))) == Set(),
      errorsOf(fn1(List(deferCall(0, Ev, false)))) == Set(Missing(ExternCall)),
      errorsOf(fn1(List(deferCall(1, Sv, true)))) == Set(Unused),
    }),
  }

  // SPEC §12: unsafe struct construction and field writes need a marker, and
  // are confined to the declaring file even with one.
  run unsafeStructTest = all {
    val write = form(FieldSet, 1, single(0, Lit), true)
    val build = print(single(1, Build), true)
    assert(and {
      errorsOf(fn1(List(write, build))) == Set(),
      errorsOf(fn1(List(form(FieldSet, 0, single(0, Lit), false)))) == Set(Missing(StructInvariant)),
      errorsOf(onlyF(SafeFn, Away, List(write))) == Set(Boundary),
      errorsOf(onlyF(SafeFn, Away, List(build))) == Set(Boundary),
      // reading a field is safe anywhere
      errorsOf(onlyF(SafeFn, Away, List(print(single(0, FieldRead), false)))) == Set(),
    }),
  }

  // SPEC §12 counts every field write, so lending the field to a writer is one
  // (findings/unsafecap-quint-findings.md).
  run fieldBorrowIsAWriteTest = all {
    val lent = mkStmt(FieldBorrow, 0, NO_EXPR, SafeTy, Sf, Sv, false)
    val marked = mkStmt(FieldBorrow, 1, NO_EXPR, SafeTy, Sf, Sv, true)
    assert(and {
      errorsOf(fn1(List(lent))) == Set(Missing(StructInvariant)),
      errorsOf(fn1(List(marked))) == Set(),
      errorsOf(onlyF(SafeFn, Away, List(marked))) == Set(Boundary),
    }),
  }

  // The view and the raw borrow are let-initializer operations of their own.
  // The checker names the form, not the kind, for the unmarked raw borrow (findings F2).
  run viewAndRawBorrowTest = all {
    assert(and {
      errorsOf(fn1(List(form(ViewS, 1, NO_EXPR, true)))) == Set(),
      errorsOf(fn1(List(form(ViewS, 0, NO_EXPR, false)))) == Set(Missing(PtrView)),
      errorsOf(fn1(List(form(DerefBorrow, 0, NO_EXPR, false)))) == Set(Missing(PtrDeref)),
    }),
  }

  // A marker changes neither the type nor the value of what it covers (SPEC
  // §12), so a let initializer or a deferred call under two markers is still
  // that form, and the outer marker is merely unused. The checker reads these
  // forms with exactly one marker and reports a form error instead (findings
  // F3); the generator does not draw them, mbt_test.go pins them.
  run stackedMarkersOnFormsTest = all {
    val stacked = Set(form(ViewS, 2, NO_EXPR, true), form(DerefBorrow, 2, NO_EXPR, true), deferCall(2, Ev, true))
    assert(stacked.forall(s => and {
      errorsOf(fn1(List(s))) == Set(Unused),
      allDischarged(fn1(List(s))),
    })),
  }

  // The repair the generator uses yields an accepted statement from any drawn one.
  run repairIsAcceptedTest = all {
    val drawn = Set(
      print(sum(2, leaf(0, Read), leaf(2, Build)), false),
      form(FieldSet, 0, block(0, leaf(0, CallG), false, leaf(1, Deref), false), false),
      bind(SafeTy, Uf, 2, false),
      deferCall(2, Sv, true),
      form(VolWriteS, 0, sum(1, leaf(1, UfCall), leaf(0, Lit)), false),
    )
    val ctxs = Set(
      { gKind: UnsafeFn, file: Away, covered: false },
      { gKind: SafeFn, file: Home, covered: false },
    )
    assert(drawn.forall(s => ctxs.forall(c => Set(false, true).forall(style =>
      stmtErrors(c, repair(c, s, style)) == Set()
    )))),
  }
}
