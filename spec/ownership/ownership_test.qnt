// -*- mode: Bluespec; -*-
// Known shapes for the ownership model: what it must accept and reject, and
// that the run-time model sees the fault each rejection is there to prevent.
// `quint test ownership_test.qnt`.
module ownership_test {
  import ownership.* from "./ownership"

  // #1913: a view read inside a loop, then the deferred deinit. The view's last
  // use is in the loop, so nothing is borrowed at the exit.
  run issue1913ViewReadInLoopThenDeferredDeinitTest = all {
    val p = List(DeferDeinit, View(0), LoopBegin(1), UseView(0), LoopEnd)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }

  // #1913 with the explicit deinit after the loop, and with an unrelated owner
  // in between, which must not change the verdict either way.
  run issue1913ExplicitDeinitAndUnrelatedOwnerTest = all {
    val bare = List(View(0), LoopBegin(1), UseView(0), LoopEnd, Deinit)
    val withUnrelated = List(View(0), Unrelated(2), LoopBegin(1), UseView(0), LoopEnd, Deinit)
    assert(errorsOf(bare) == Set() and errorsOf(withUnrelated) == Set()),
  }

  // examples/negative/std_string_deinit_while_viewed.kizu
  run deinitWhileViewedTest = all {
    val p = List(DeferDeinit, View(0), Deinit, UseView(0))
    assert(errorsOf(p).contains(Borrowed) and faultsOf(p).contains(DanglingView)),
  }

  run deinitWhileBorrowedTest = all {
    val p = List(Borrow(0), Deinit, UseBorrow(0))
    assert(errorsOf(p) == Set(Borrowed) and faultsOf(p).contains(DanglingBorrow)),
  }

  // examples/negative/borrow_loop_last_use.kizu: the next turn reads the view
  // again, so a mutation after the read in the same body is under a live view.
  // Without the carried liveness the model would accept a program that reads a
  // reallocated buffer.
  run loopCarriedViewTest = all {
    val p = List(DeferDeinit, View(0), LoopBegin(1), UseView(0), Mutate, LoopEnd)
    assert(errorsOf(p) == Set(Borrowed) and faultsOf(p).contains(DanglingView)),
  }

  // A view bound inside the body ends with the turn.
  run bodyLocalViewEndsWithTurnTest = all {
    val p = List(DeferDeinit, LoopBegin(0), View(1), UseView(1), Mutate, LoopEnd)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }

  // A read before the mutation in straight-line code has ended its borrow.
  run lastUseEndsBorrowTest = all {
    val p = List(DeferDeinit, Borrow(0), UseBorrow(0), Mutate, ReadOwner)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }

  run sharedBorrowSeesMutationTest = all {
    val p = List(DeferDeinit, Borrow(0), Mutate, UseBorrow(0))
    assert(errorsOf(p) == Set(Borrowed) and faultsOf(p).contains(SharedMutated)),
  }

  run useAfterMoveTest = all {
    val p = List(MoveOut, ReadOwner)
    assert(errorsOf(p) == Set(UseAfterMove) and faultsOf(p).contains(UseAfterFree)),
  }

  // examples/negative/defer_after_explicit_deinit.kizu
  run deferThenDeinitTest = all {
    val p = List(DeferDeinit, Deinit)
    assert(errorsOf(p) == Set(UseAfterMove) and faultsOf(p).contains(DoubleFree)),
  }

  run doubleDeferTest = all {
    val p = List(DeferDeinit, DeferDeinit)
    assert(errorsOf(p) == Set(DoubleDefer) and faultsOf(p).contains(DoubleFree)),
  }

  run neverDeinitializedTest = all {
    val p = List(ReadOwner)
    assert(errorsOf(p) == Set(Leak) and faultsOf(p).contains(LeakAtExit)),
  }

  // A `try` before any cleanup is registered leaks on its error exit.
  run tryBeforeDeferTest = all {
    val p = List(Mutate, DeferDeinit)
    assert(errorsOf(p) == Set(Leak) and faultsOf(p).contains(LeakOnError)),
  }

  run consumeInLoopTest = all {
    val p = List(LoopBegin(0), Deinit, LoopEnd)
    assert(errorsOf(p).contains(LoopConsume) and faultsOf(p).contains(DoubleFree)),
  }

  // A deferred deinit inside a body runs every turn.
  run deferInLoopTest = all {
    val p = List(LoopBegin(0), DeferDeinit, LoopEnd)
    assert(errorsOf(p).contains(LoopConsume) and faultsOf(p).contains(DoubleFree)),
  }

  // A deferred deinit in a body runs as the turn ends, while a view the code
  // after the loop reads is still live (SPEC §6.3.1).
  run deferInLoopWhileViewedTest = all {
    val p = List(View(0), LoopBegin(1), DeferDeinit, LoopEnd, UseView(0))
    assert(errorsOf(p).contains(Borrowed) and faultsOf(p).contains(DanglingView)),
  }

  // #1913 reported the same with `while`.
  run issue1913WhileTest = all {
    val p = List(DeferDeinit, View(0), WhileBegin(1), UseView(0), LoopEnd)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }

  // A view read in an inner loop is read again by every turn of the outer one,
  // so a mutation after the inner loop is under it.
  run nestedCarriedViewTest = all {
    val p = List(DeferDeinit, View(0), LoopBegin(1), WhileBegin(2), UseView(0), LoopEnd, Mutate, LoopEnd)
    assert(errorsOf(p) == Set(Borrowed) and faultsOf(p).contains(DanglingView)),
  }

  // A view bound in the outer body and read only in the inner loop ends with
  // the inner loop: the next outer turn binds a new one.
  run outerBodyViewEndsAfterInnerLoopTest = all {
    val p = List(DeferDeinit, LoopBegin(0), View(1), LoopBegin(2), UseView(1), LoopEnd, Mutate, LoopEnd)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }

  // A consume in an inner loop is a consume in the outer loop too.
  run nestedConsumeTest = all {
    val p = List(LoopBegin(0), LoopBegin(1), Deinit, LoopEnd, LoopEnd)
    assert(errorsOf(p).contains(LoopConsume) and faultsOf(p).contains(DoubleFree)),
  }

  run sharedViewAndBorrowCoexistTest = all {
    val p = List(View(0), Borrow(1), UseView(0), UseBorrow(1), Deinit)
    assert(errorsOf(p) == Set() and faultsOf(p) == Set()),
  }
}
