// -*- mode: Bluespec; -*-
// Known shapes for the slot model: what the lowerer must give storage, and
// that deciding by the method's name (slots.go before #1914) is caught.
// `quint test slots_test.qnt`.
module slots_test {
  import slots.* from "./slots"

  pure val POKE = 0
  pure val NOTE = 1
  pure val ABSENT: Decl = { declared: false, recv: RShared, arg: AVal }
  pure def decl(recv: Recv, arg: Arg): Decl = { declared: true, recv: recv, arg: arg }

  // tests/behavior/src/method_lend_scope: Slot.poke and Tally.note read,
  // while the methods of the same names on Counter take `&var` -- the
  // receiver of poke, the argument of note. Here Slot is A, Tally is B and
  // Counter is C; x0: Slot, x1: Tally, x2: Counter.
  pure val lendScope: (int, int) -> Decl = Map(
    (0, POKE) -> decl(RShared, AVal),  (0, NOTE) -> ABSENT,
    (1, POKE) -> ABSENT,               (1, NOTE) -> decl(RShared, AVal),
    (2, POKE) -> decl(RLent, AVal),    (2, NOTE) -> decl(RShared, ALent),
  )
  pure val lendScopeTypes = List(0, 1, 2)

  // `receiver.poke(1)` on the Slot and `tally.note(argument)` lend nothing.
  // By name, Counter's declarations would mark both.
  run issue1914SharedMethodsLendNothingTest = {
    val p = List(Call({ x: 0, m: POKE, k: 0 }), Call({ x: 1, m: NOTE, k: 1 }))
    all {
      assert(slotsOf(lendScope, lendScopeTypes, Identity, p) == Set()),
      assert(slotsOf(lendScope, lendScopeTypes, ByName, p) == Set(X(0), N(1))),
      assert(onlyReached(lendScope, lendScopeTypes, Identity, p)),
      assert(not(onlyReached(lendScope, lendScopeTypes, ByName, p))),
    }
  }

  // `counter.poke(5)` and `counter.note(&var total)`: Counter's own methods
  // lend, and both rules see it.
  run issue1914OwnMethodsStillLendTest = {
    val p = List(Call({ x: 2, m: POKE, k: 0 }), Call({ x: 2, m: NOTE, k: 1 }))
    all {
      assert(slotsOf(lendScope, lendScopeTypes, Identity, p) == Set(X(2), N(1))),
      assert(noLostWrite(lendScope, lendScopeTypes, Identity, p)),
      assert(onlyReached(lendScope, lendScopeTypes, Identity, p)),
    }
  }

  // A deferred call reads its receiver at the exit, so the receiver has
  // storage even when the method takes it by value.
  run deferredReceiverIsStorageTest = {
    val p = List(DeferCall({ x: 0, m: POKE, k: 0 }))
    assert(slotsOf(lendScope, lendScopeTypes, Identity, p) == Set(X(0)))
  }

  // The decision holds for the whole function: a local lent inside a block
  // has storage from its declaration on.
  run lentInsideBlockTest = {
    val p = List(IfBegin, LoopBegin(0), Bump(2), BlockEnd, BlockEnd, BumpExplicit(0))
    assert(slotsOf(lendScope, lendScopeTypes, Identity, p) == Set(N(0), N(2)))
  }

  // A rule that forgot the argument positions would lose the write: the
  // property that catches it is noLostWrite.
  run forgettingArgumentsLosesWritesTest = {
    val p = List(Call({ x: 2, m: NOTE, k: 1 }))
    val forgot = slotsOf(lendScope, lendScopeTypes, Identity, p).filter(l => match l { | X(_) => true | N(_) => false })
    assert(not(lentBy(lendScope, lendScopeTypes, p[0]).subseteq(forgot)))
  }
}
