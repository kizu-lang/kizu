// -*- mode: Bluespec; -*-
// Known shapes for the defer model. `quint test defer_test.qnt`.
module defer_test {
  import defer.* from "./defer"

  pure def out(p: List[Stmt], x: int): List[int] = printed(runOn(Spec, p, x))

  // A loop body's defer runs at the end of every turn, on `continue` and on
  // `break`; the function's defer runs at the return, its errdefer only on
  // the error return. Run by hand as Kizu on wasm before the model existed:
  //   x=0: 100 3 100 3 101 1 0    x=1: 3 3 101 1 0    x=2: 3 101 1 0
  //   x=4: 100 3 100 3 1 0        x=8: 100 3 100 3 2 1 -1
  pure val loopAndExits = List(
    Defer(1), ErrDefer(2),
    LoopBegin(5), Defer(3), Continue(0), Break(1), Note(100), End,
    Return(2), Fail(3), Note(101),
  )

  run loopAndExitsTest = all {
    assert(out(loopAndExits, 0) == List(100, 3, 100, 3, 101, 1, 0)),
    assert(out(loopAndExits, 1) == List(3, 3, 101, 1, 0)),
    assert(out(loopAndExits, 2) == List(3, 101, 1, 0)),
    assert(out(loopAndExits, 4) == List(100, 3, 100, 3, 1, 0)),
    assert(out(loopAndExits, 8) == List(100, 3, 100, 3, 2, 1, -1)),
    assert(INPUTS.forall(x => exitsSound(runOn(Spec, loopAndExits, x)))),
  }

  // An errdefer of a loop turn that ended normally does not run when a
  // later turn fails.
  run errDeferOfFinishedTurnTest = {
    val p = List(LoopBegin(1), ErrDefer(2), Note(103), End, Fail(0))
    all {
      assert(out(p, 0) == List(103, 103, 0)),
      assert(out(p, 1) == List(103, 103, -1)),
    }
  }

  // A failing `try` in a nested block unwinds the inner frames first.
  run nestedErrorUnwindTest = {
    val p = List(Defer(1), IfBegin(0), ErrDefer(2), LoopBegin(3), Defer(4), Fail(1), End, End)
    all {
      assert(out(p, 3) == List(4, 2, 1, -1)),
      assert(out(p, 1) == List(4, 4, 1, 0)),
      assert(out(p, 0) == List(1, 0)),
    }
  }

  // The mutant rules break exitsSound on a shape each one gets wrong.
  run mutantsAreCaughtTest = {
    val early = List(Defer(1), Return(0), Note(102))
    val errd = List(ErrDefer(1), Note(102))
    all {
      assert(exitsSound(runOn(Spec, early, 1))),
      assert(not(exitsSound(runOn(FallthroughOnly, early, 1)))),
      assert(exitsSound(runOn(Spec, errd, 0))),
      assert(not(exitsSound(runOn(ErrDeferAlways, errd, 0)))),
    }
  }
}
