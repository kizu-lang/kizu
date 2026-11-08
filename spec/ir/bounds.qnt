// -*- mode: Bluespec; -*-
// Index checks on a std::array::Array (internal/ir/bounds.go): what an
// access does at run time, and a rule for when its check may be skipped.
//
// The optimizer may drop the check of an access it has already answered
// (EliminateBoundsChecks). Whatever it drops, an access whose index is out of
// range must still fail: `get_or_panic` stops the program, `set` returns
// `std::array::Error::OutOfBounds`. That is the property the replay test
// observes, by running each program on every input and comparing with
// `outcome` below -- an answered check that was wrong reads or writes past the
// array and the program goes on instead of stopping.
//
// `skippable` is a model of when a check is answered: the index is known not
// to be negative and known to be below the length, or the same index was
// checked since the header last could change. `skipSound` says the rule only
// answers accesses that are in range on every input. It is not compared with
// what the optimizer answers; it sorts the sampled programs into those where
// an answer is plausible and those where one would be wrong (README.md).
//
// The programs are rendered to Kizu by internal/ir/runtime_mbt_test.go:
//
//   fn caseN(a: &var array::Array<i64>, k: i64, allocator: Allocator) -> !i64 {
//       var s = 0;
//       <statements>
//       return s;
//   }
//
// main builds `a` as [10, 11, .., 10 + len - 1] from the input and prints the
// result and `a.len()`.
module bounds {
  // An index expression. I and I1 name the innermost loop's counter.
  type Idx =
    | K   // k, the input
    | I   // i<id>
    | I1  // i<id> + 1
    | C0  // 0
    | C2  // 2

  type Stmt =
    | Read(Idx)          // s = s + a.get_or_panic(<idx>);
    | Write(Idx)         // a.set(<idx>, s) catch return -7;
    | Pop                // let _ = a.pop();
    | Push               // try a.append(allocator, s);
    | GuardLt(Idx)       // if <idx> < a.len() {
    | GuardGe(Idx)       // if <idx> >= 0 {
    | ExitUnlessLt(Idx)  // if <idx> >= a.len() { return s; }
    | ForLen(int)        // let n<id> = a.len(); for 0..n<id> |i<id>| {
    | WhileLen(int)      // var i<id> = 0; while i<id> < a.len() {   ... i<id> = i<id> + 1; }
    | End                // }

  // Which facts survive an instruction that could change the header.
  type Rule =
    | Forget  // a pop or an append forgets the length and what was checked
    | Keep    // a rule that forgot nothing, for showing skipSound is not vacuous

  pure val LENS = 0.to(3)
  pure val KS = (-1).to(3)
  pure val INPUTS: Set[(int, int)] = tuples(LENS, KS)
  pure val MAX_LEN = 8
  pure val MAX_PUSH = 2
  pure val MAX_STEPS = 80
  pure val ERROR_RESULT = -7

  pure def isOpen(s: Stmt): bool = match s {
    | GuardLt(_) => true | GuardGe(_) => true | ForLen(_) => true | WhileLen(_) => true
    | _ => false
  }
  pure def isLoop(s: Stmt): bool = match s {
    | ForLen(_) => true | WhileLen(_) => true | _ => false
  }
  pure def changesHeader(s: Stmt): bool = match s { | Pop => true | Push => true | _ => false }

  // The index of the End that closes the block opened at pc.
  pure def endOf(p: List[Stmt], pc: int): int = {
    val walk = range(pc + 1, length(p)).foldl({ depth: 0, found: -1 }, (acc, j) =>
      if (acc.found >= 0) acc
      else if (isOpen(p[j])) { depth: acc.depth + 1, found: -1 }
      else if (p[j] == End and acc.depth == 0) { depth: 0, found: j }
      else if (p[j] == End) { depth: acc.depth - 1, found: -1 }
      else acc)
    walk.found
  }

  // Whether the block opened at pc holds an instruction that could change
  // the header.
  pure def bodyChangesHeader(p: List[Stmt], pc: int): bool =
    range(pc + 1, endOf(p, pc)).foldl(false, (acc, j) => acc or changesHeader(p[j]))

  // ------------------------------------------------------------ run time

  type Frame = { start: int, loop: bool, fixed: bool, i: int, bound: int }

  type Machine = {
    pc: int, arr: List[int], s: int, frames: List[Frame],
    status: str,  // "run", "ok", "panic", "error"
    oob: int,     // the access that failed, or -1
  }

  pure def counter(m: Machine): int =
    m.frames.foldl(0, (acc, f) => if (f.loop) f.i else acc)

  pure def eval(m: Machine, k: int, x: Idx): int = match x {
    | K => k
    | I => counter(m)
    | I1 => counter(m) + 1
    | C0 => 0
    | C2 => 2
  }

  pure def inRange(m: Machine, v: int): bool = 0 <= v and v < length(m.arr)

  pure def pushFrame(m: Machine, f: Frame): Machine = { ...m, frames: m.frames.append(f), pc: m.pc + 1 }
  pure def skipBlock(p: List[Stmt], m: Machine): Machine = { ...m, pc: endOf(p, m.pc) + 1 }
  pure def top(m: Machine): Frame = m.frames[length(m.frames) - 1]
  pure def popFrame(m: Machine): Machine =
    { ...m, frames: m.frames.slice(0, length(m.frames) - 1), pc: m.pc + 1 }
  pure def setTop(m: Machine, f: Frame): Machine =
    { ...m, frames: m.frames.replaceAt(length(m.frames) - 1, f) }

  pure def exec(p: List[Stmt], k: int, m: Machine): Machine =
    if (m.status != "run") m
    else if (m.pc >= length(p)) { ...m, status: "ok" }
    else match p[m.pc] {
      | Read(x) =>
        val v = eval(m, k, x)
        if (inRange(m, v)) { ...m, s: m.s + m.arr[v], pc: m.pc + 1 }
        else { ...m, status: "panic", oob: m.pc }
      | Write(x) =>
        val v = eval(m, k, x)
        if (inRange(m, v)) { ...m, arr: m.arr.replaceAt(v, m.s), pc: m.pc + 1 }
        else { ...m, s: ERROR_RESULT, status: "error", oob: m.pc }
      | Pop =>
        if (length(m.arr) > 0) { ...m, arr: m.arr.slice(0, length(m.arr) - 1), pc: m.pc + 1 }
        else { ...m, pc: m.pc + 1 }
      | Push => { ...m, arr: m.arr.append(m.s), pc: m.pc + 1 }
      | GuardLt(x) =>
        if (eval(m, k, x) < length(m.arr)) pushFrame(m, { start: m.pc, loop: false, fixed: false, i: 0, bound: 0 })
        else skipBlock(p, m)
      | GuardGe(x) =>
        if (eval(m, k, x) >= 0) pushFrame(m, { start: m.pc, loop: false, fixed: false, i: 0, bound: 0 })
        else skipBlock(p, m)
      | ExitUnlessLt(x) =>
        if (eval(m, k, x) >= length(m.arr)) { ...m, status: "ok" } else { ...m, pc: m.pc + 1 }
      | ForLen(_) =>
        // The bound is read once, before the first turn.
        if (0 < length(m.arr)) pushFrame(m, { start: m.pc, loop: true, fixed: true, i: 0, bound: length(m.arr) })
        else skipBlock(p, m)
      | WhileLen(_) =>
        if (0 < length(m.arr)) pushFrame(m, { start: m.pc, loop: true, fixed: false, i: 0, bound: 0 })
        else skipBlock(p, m)
      | End =>
        val f = top(m)
        if (not(f.loop)) popFrame(m)
        else {
          val turn = f.i + 1
          val again = if (f.fixed) turn < f.bound else turn < length(m.arr)
          if (again) { ...setTop(m, { ...f, i: turn }), pc: f.start + 1 } else popFrame(m)
        }
    }

  pure def runOn(p: List[Stmt], len: int, k: int): Machine = {
    val arr0 = range(0, len).foldl(List(), (acc, j) => acc.append(10 + j))
    val m0: Machine = { pc: 0, arr: arr0, s: 0, frames: List(), status: "run", oob: -1 }
    range(0, MAX_STEPS).foldl(m0, (m, _) => exec(p, k, m))
  }

  // What main prints for a program on one input: the result and the
  // length, unless the program stopped.
  type Outcome = { stopped: bool, result: int, len: int }

  pure def toOutcome(m: Machine): Outcome = { stopped: m.status == "panic", result: m.s, len: length(m.arr) }

  pure def outcome(p: List[Stmt], len: int, k: int): Outcome = toOutcome(runOn(p, len, k))

  // ------------------------------------------------------------ the rule

  type Facts = { nonneg: Set[Idx], below: Set[Idx], checked: Set[Idx] }

  type Walk = { facts: Facts, saved: List[(Facts, int)], skip: Set[int] }

  pure val NO_FACTS: Facts = { nonneg: Set(C0, C2), below: Set(), checked: Set() }

  pure def enterBlock(w: Walk, pc: int, g: Facts): Walk =
    { ...w, facts: g, saved: w.saved.append((w.facts, pc)) }

  pure def forget(rule: Rule, f: Facts): Facts = match rule {
    | Forget => { ...f, below: Set(), checked: Set() }
    | Keep => f
  }

  // The accesses the rule answers, by position. The walk follows the text
  // once: a block's facts are dropped at its end, and a block that could
  // change the header leaves the facts after it forgotten.
  pure def skippable(rule: Rule, p: List[Stmt]): Set[int] = {
    val w0: Walk = { facts: NO_FACTS, saved: List(), skip: Set() }
    val w = range(0, length(p)).foldl(w0, (w, pc) => {
      val f = w.facts
      match p[pc] {
        | Read(x) =>
          val ok = f.checked.contains(x) or (f.nonneg.contains(x) and f.below.contains(x))
          { ...w, facts: { ...f, checked: f.checked.union(Set(x)) }, skip: if (ok) w.skip.union(Set(pc)) else w.skip }
        | Write(x) =>
          val ok = f.checked.contains(x) or (f.nonneg.contains(x) and f.below.contains(x))
          { ...w, facts: { ...f, checked: f.checked.union(Set(x)) }, skip: if (ok) w.skip.union(Set(pc)) else w.skip }
        | Pop => { ...w, facts: forget(rule, f) }
        | Push => { ...w, facts: forget(rule, f) }
        | GuardLt(x) => enterBlock(w, pc, { ...f, below: f.below.union(Set(x)) })
        | GuardGe(x) => enterBlock(w, pc, { ...f, nonneg: f.nonneg.union(Set(x)) })
        | ExitUnlessLt(x) => { ...w, facts: { ...f, below: f.below.union(Set(x)) } }
        | ForLen(_) =>
          // The counter starts at 0 and steps below the bound read at the
          // entry, which is the length while nothing in the body changes it.
          val outer = if (bodyChangesHeader(p, pc)) forget(rule, f) else f
          val counterFacts = { ...outer, nonneg: (outer.nonneg.exclude(Set(I, I1))).union(Set(I)),
                               below: outer.below.exclude(Set(I, I1)), checked: outer.checked.exclude(Set(I, I1)) }
          val withBound = if (bodyChangesHeader(p, pc) and rule == Forget) counterFacts
                          else { ...counterFacts, below: counterFacts.below.union(Set(I)) }
          enterBlock(w, pc, withBound)
        | WhileLen(_) =>
          // The condition is read before each turn, so the counter is below
          // the length at the top of the body whatever the body does.
          val outer = if (bodyChangesHeader(p, pc)) forget(rule, f) else f
          enterBlock(w, pc, { ...outer, nonneg: (outer.nonneg.exclude(Set(I, I1))).union(Set(I)),
                  below: (outer.below.exclude(Set(I, I1))).union(Set(I)), checked: outer.checked.exclude(Set(I, I1)) })
        | End =>
          val last = w.saved[length(w.saved) - 1]
          val before = last._1
          val after = if (bodyChangesHeader(p, last._2)) forget(rule, before) else before
          { ...w, facts: after, saved: w.saved.slice(0, length(w.saved) - 1) }
      }
    })
    w.skip
  }

  // ------------------------------------------------------- the generator

  var prog: List[Stmt]
  var open: List[Stmt]
  var nextId: int
  var pushes: int
  var rule: Rule
  var done: bool
  // When done: what main prints on each input, and the accesses the rule
  // answers.
  var expected: (int, int) -> Outcome
  var answered: Set[int]
  // When done: whether every input ended within the step budget and no
  // access the rule answers went out of range (see soundness).
  var sound: bool

  action start(r: Rule): bool = all {
    prog' = List(), open' = List(), nextId' = 0, pushes' = 0, rule' = r, done' = false,
    expected' = Map(), answered' = Set(), sound' = true,
  }
  action init = start(Forget)
  action initKeep = start(Keep)

  pure def inLoop(o: List[Stmt]): bool = o.foldl(false, (acc, s) => acc or isLoop(s))
  pure def inWhile(o: List[Stmt]): bool = o.foldl(false, (acc, s) => acc or (match s { | WhileLen(_) => true | _ => false }))

  action add(s: Stmt, o: List[Stmt], id: int, pu: int): bool = all {
    not(done),
    length(prog) < MAX_LEN,
    prog' = prog.append(s), open' = o, nextId' = id, pushes' = pu,
    rule' = rule, done' = false, expected' = expected, answered' = answered, sound' = sound,
  }

  action addAccess = {
    nondet x = Set(K, I, I1, C0, C2).oneOf()
    nondet write = Set(true, false).oneOf()
    all {
      inLoop(open) or (x != I and x != I1),
      add(if (write) Write(x) else Read(x), open, nextId, pushes),
    }
  }

  action addHeader = {
    nondet push = Set(true, false).oneOf()
    all {
      // An append in a `while` over the length would never end.
      not(push) or (pushes < MAX_PUSH and not(inWhile(open))),
      add(if (push) Push else Pop, open, nextId, if (push) pushes + 1 else pushes),
    }
  }

  action addGuard = {
    nondet x = Set(K, I, I1, C0, C2).oneOf()
    nondet kind = 0.to(2).oneOf()
    val s = if (kind == 0) GuardLt(x) else if (kind == 1) GuardGe(x) else ExitUnlessLt(x)
    all {
      inLoop(open) or (x != I and x != I1),
      length(open) < 2 or kind == 2,
      add(s, if (kind == 2) open else open.append(s), nextId, pushes),
    }
  }

  action addLoop = {
    nondet fixed = Set(true, false).oneOf()
    val s = if (fixed) ForLen(nextId) else WhileLen(nextId)
    all { length(open) < 2, not(inLoop(open)), add(s, open.append(s), nextId + 1, pushes) }
  }

  action addEnd = all {
    length(open) > 0,
    // A block holds something.
    not(isOpen(prog[length(prog) - 1])),
    add(End, open.slice(0, length(open) - 1), nextId, pushes),
  }

  action finish = {
    val runs = INPUTS.mapBy(v => runOn(prog, v._1, v._2))
    val skip = skippable(rule, prog)
    all {
      not(done),
      length(open) == 0,
      length(prog) > 0,
      done' = true,
      expected' = INPUTS.mapBy(v => toOutcome(runs.get(v))),
      answered' = skip,
      sound' = INPUTS.forall(v => runs.get(v).status != "run" and not(skip.contains(runs.get(v).oob))),
      prog' = prog, open' = open, nextId' = nextId, pushes' = pushes, rule' = rule,
    }
  }

  action halt = all {
    done or (length(prog) >= MAX_LEN and length(open) > 0),
    prog' = prog, open' = open, nextId' = nextId, pushes' = pushes, rule' = rule,
    done' = done, expected' = expected, answered' = answered, sound' = sound,
  }

  action step = any { addAccess, addAccess, addHeader, addGuard, addLoop, addEnd, addEnd, finish, halt }

  // ------------------------------------------------------------ properties

  // Every program ends within the step budget on every input.
  pure def terminates(p: List[Stmt]): bool =
    INPUTS.forall(v => runOn(p, v._1, v._2).status != "run")

  // An access the rule answers is in range on every input that reaches it.
  pure def skipSound(r: Rule, p: List[Stmt]): bool = {
    val skip = skippable(r, p)
    INPUTS.forall(v => not(skip.contains(runOn(p, v._1, v._2).oob)))
  }

  // `sound` is terminates and skipSound, computed once when the program is
  // finished.
  val soundness: bool = done implies sound

  // Witnesses: each is expected to be violated by some run.
  val reachedAnsweredAccess: bool = not(done and answered.size() > 0)
  val reachedOutOfRange: bool = not(done and INPUTS.exists(v => expected.get(v).stopped))
}
