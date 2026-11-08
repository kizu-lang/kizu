// -*- mode: Bluespec; -*-
// Known shapes for the bounds model: what main prints, and which accesses
// the rule answers. `quint test bounds_test.qnt`.
module bounds_test {
  import bounds.* from "./bounds"

  pure def printed(result: int, len: int): Outcome = { stopped: false, result: result, len: len }
  pure val STOPPED: Outcome = { stopped: true, result: 0, len: 0 }
  pure def same(got: Outcome, want: Outcome): bool =
    if (want.stopped) got.stopped else got == want

  // A loop over the length read once, with nothing changing the header:
  // the check is answered, and the sum is the elements'.
  run forOverLengthTest = {
    val p = List(ForLen(0), Read(I), End)
    all {
      assert(skippable(Forget, p) == Set(1)),
      assert(same(outcome(p, 3, 0), printed(33, 3))),
      assert(same(outcome(p, 0, 0), printed(0, 0))),
    }
  }

  // The same loop popping in its body: the bound read at the entry is no
  // longer the length, and the third turn is out of range.
  run forOverLengthWithPopTest = {
    val p = List(ForLen(0), Read(I), Pop, End)
    all {
      assert(skippable(Forget, p) == Set()),
      assert(skippable(Keep, p) == Set(1)),
      assert(same(outcome(p, 3, 0), STOPPED)),
      assert(not(skipSound(Keep, p))),
      assert(skipSound(Forget, p)),
    }
  }

  // A `while` reads the length before each turn, so the pop is harmless.
  run whileOverLengthWithPopTest = {
    val p = List(WhileLen(0), Read(I), Pop, End)
    all {
      assert(skippable(Forget, p) == Set(1)),
      assert(same(outcome(p, 3, 0), printed(21, 1))),
    }
  }

  // A guard answers the first read; a pop between makes the second one need
  // its check again.
  run guardThenPopTest = {
    val p = List(GuardGe(K), GuardLt(K), Read(K), Pop, Read(K), End, End)
    all {
      assert(skippable(Forget, p) == Set(2)),
      assert(same(outcome(p, 2, 1), STOPPED)),
      assert(same(outcome(p, 2, 0), printed(20, 1))),
      assert(same(outcome(p, 2, -1), printed(0, 2))),
    }
  }

  // An early exit leaves the fall-through below the length.
  run exitUnlessBelowTest = {
    val p = List(GuardGe(K), ExitUnlessLt(K), Read(K), End)
    all {
      assert(skippable(Forget, p) == Set(2)),
      assert(same(outcome(p, 2, 2), printed(0, 2))),
      assert(same(outcome(p, 2, 1), printed(11, 2))),
    }
  }

  // The second access of the same index was checked by the first.
  run checkedOnceTest = {
    val p = List(Read(K), Read(K))
    all {
      assert(skippable(Forget, p) == Set(1)),
      assert(same(outcome(p, 1, 0), printed(20, 1))),
      assert(same(outcome(p, 1, 1), STOPPED)),
    }
  }

  // A `set` out of range is an error the program handles, not a stop.
  run writeOutOfRangeTest = {
    val p = List(Write(K), Read(K))
    all {
      assert(same(outcome(p, 1, 1), printed(-7, 1))),
      assert(same(outcome(p, 1, 0), printed(0, 1))),
      assert(skippable(Forget, p) == Set(1)),
    }
  }

  // Not knowing the index is not negative leaves the check in.
  run belowIsNotEnoughTest = {
    val p = List(GuardLt(K), Read(K), End)
    all {
      assert(skippable(Forget, p) == Set()),
      assert(same(outcome(p, 2, -1), STOPPED)),
    }
  }

  // `i + 1` is not below the length because `i` is: the last turn of a
  // loop over the length reads one past the end.
  run nextIndexTest = {
    val p = List(ForLen(0), Read(I1), End)
    all {
      assert(skippable(Forget, p) == Set()),
      assert(same(outcome(p, 3, 0), STOPPED)),
      assert(same(outcome(p, 0, 0), printed(0, 0))),
    }
  }
}
