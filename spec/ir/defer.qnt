// -*- mode: Bluespec; -*-
// Where deferred cleanups run (compiler/src/internal/ir/defer.kizu,
// SPEC §6.3.1): a `defer` runs when its block exits, an `errdefer` only when
// the block exits on an error return, both in the reverse of registration
// order, on every way out -- the end of the block, `break`, `continue`,
// `return`, and a `try` that fails.
//
// `run` interprets a program on one input and records what it prints; the
// replay test (compiler/src/ir_runtime_mbt_test.kizu) runs the lowered
// program on the same input and compares. The interpreter also keeps a ledger
// of every registration and every cleanup that ran, from which `exitsSound`
// states the rules above without reading the interpreter's unwinding.
//
// A program is the body of
//
//   fn caseN(x: i64) -> Exit!i64 { <statements> return 0; }
//
// where `bit(x, b)` is bit b of the input. main prints -1 for an error.
module defer {
  type Stmt =
    | Defer(int)      // defer note(<id>);
    | ErrDefer(int)   // errdefer note(<id>);
    | Note(int)       // note(<id>);
    | IfBegin(int)    // if bit(x, <b>) {
    | LoopBegin(int)  // for 0..2 |i<id>| {
    | End             // }
    | Return(int)     // if bit(x, <b>) { return 0; }
    | Fail(int)       // try fail(bit(x, <b>));
    | Break(int)      // if bit(x, <b>) { break; }
    | Continue(int)   // if bit(x, <b>) { continue; }

  // How a cleanup frame was left.
  type Exit = | Normal | Errored

  // How the interpreter leaves frames. Spec is §6.3.1; the others are
  // lowerings that forget a way out, for showing exitsSound is not vacuous.
  type Rule =
    | Spec
    | FallthroughOnly  // cleanups run at the end of a block, not on a jump out of it
    | ErrDeferAlways   // errdefer entries run on every exit

  pure val BITS = 0.to(3)
  pure val INPUTS = 0.to(15)
  pure val TURNS = 2
  pure val MAX_LEN = 10
  pure val MAX_STEPS = 120
  pure val ERROR_RESULT = -1

  pure def bit(x: int, b: int): bool = (x / (2 ^ b)) % 2 == 1

  pure def isOpen(s: Stmt): bool = match s { | IfBegin(_) => true | LoopBegin(_) => true | _ => false }

  pure def endOf(p: List[Stmt], pc: int): int = {
    val walk = range(pc + 1, length(p)).foldl({ depth: 0, found: -1 }, (acc, j) =>
      if (acc.found >= 0) acc
      else if (isOpen(p[j])) { depth: acc.depth + 1, found: -1 }
      else if (p[j] == End and acc.depth == 0) { depth: 0, found: j }
      else if (p[j] == End) { depth: acc.depth - 1, found: -1 }
      else acc)
    walk.found
  }

  // ------------------------------------------------------------ run time

  // A registered cleanup: its serial in the ledger, the id it prints, and
  // whether it is an errdefer.
  type Entry = { serial: int, id: int, onError: bool }
  type Frame = { start: int, loop: bool, turn: int, entries: List[Entry] }

  type Machine = {
    pc: int,
    frames: List[Frame],
    out: List[int],
    status: str,           // "run", "ok", "error"
    serial: int,
    // The ledger: for each serial, the id it prints, whether it is an
    // errdefer, how its frame was left (when it was), and how many times it ran.
    registered: int -> Entry,
    left: int -> Exit,
    ran: int -> int,
    // The serials run at each exit, in the order they ran.
    batches: List[List[int]],
  }

  pure def fnFrame: Frame = { start: -1, loop: false, turn: 0, entries: List() }

  // Leave the innermost frame: run its entries that apply, newest first,
  // and note in the ledger how it was left.
  pure def leaveTop(rule: Rule, m: Machine, how: Exit, jump: bool): Machine = {
    val f = m.frames[length(m.frames) - 1]
    val n = length(f.entries)
    val order = range(0, n).foldl(List(), (acc, j) => acc.append(f.entries[n - 1 - j]))
    val runs = order.select(e =>
      (match rule {
        | Spec => true
        | FallthroughOnly => not(jump)
        | ErrDeferAlways => true
      }) and (not(e.onError) or how == Errored or rule == ErrDeferAlways))
    {
      ...m,
      frames: m.frames.slice(0, length(m.frames) - 1),
      out: runs.foldl(m.out, (acc, e) => acc.append(e.id)),
      left: f.entries.foldl(m.left, (acc, e) => acc.put(e.serial, how)),
      ran: runs.foldl(m.ran, (acc, e) => acc.set(e.serial, acc.get(e.serial) + 1)),
      batches: m.batches.append(runs.foldl(List(), (acc, e) => acc.append(e.serial))),
    }
  }

  // Leave every frame above the innermost loop (not the loop itself).
  pure def leaveToLoop(rule: Rule, m: Machine): Machine =
    range(0, MAX_LEN).foldl(m, (acc, _) =>
      if (acc.frames[length(acc.frames) - 1].loop) acc else leaveTop(rule, acc, Normal, true))

  pure def leaveAll(rule: Rule, m: Machine, how: Exit): Machine =
    range(0, MAX_LEN + 1).foldl(m, (acc, _) =>
      if (length(acc.frames) == 0) acc else leaveTop(rule, acc, how, true))

  pure def register(m: Machine, id: int, onError: bool): Machine = {
    val e = { serial: m.serial, id: id, onError: onError }
    val f = m.frames[length(m.frames) - 1]
    {
      ...m,
      frames: m.frames.replaceAt(length(m.frames) - 1, { ...f, entries: f.entries.append(e) }),
      serial: m.serial + 1,
      registered: m.registered.put(e.serial, e),
      ran: m.ran.put(e.serial, 0),
      pc: m.pc + 1,
    }
  }

  // The end of a loop turn: the body frame is left, and the next turn opens
  // a fresh one.
  pure def endTurn(rule: Rule, p: List[Stmt], m: Machine, jump: bool): Machine = {
    val f = m.frames[length(m.frames) - 1]
    val left = leaveTop(rule, m, Normal, jump)
    if (f.turn + 1 < TURNS)
      { ...left, frames: left.frames.append({ ...f, turn: f.turn + 1, entries: List() }), pc: f.start + 1 }
    else { ...left, pc: endOf(p, f.start) + 1 }
  }

  pure def exec(rule: Rule, p: List[Stmt], x: int, m: Machine): Machine =
    if (m.status != "run") m
    else if (m.pc >= length(p)) { ...leaveAll(rule, m, Normal), status: "ok" }
    else match p[m.pc] {
      | Defer(id) => register(m, id, false)
      | ErrDefer(id) => register(m, id, true)
      | Note(id) => { ...m, out: m.out.append(id), pc: m.pc + 1 }
      | IfBegin(b) =>
        if (bit(x, b)) { ...m, frames: m.frames.append({ start: m.pc, loop: false, turn: 0, entries: List() }), pc: m.pc + 1 }
        else { ...m, pc: endOf(p, m.pc) + 1 }
      | LoopBegin(_) =>
        { ...m, frames: m.frames.append({ start: m.pc, loop: true, turn: 0, entries: List() }), pc: m.pc + 1 }
      | End =>
        if (m.frames[length(m.frames) - 1].loop) endTurn(rule, p, m, false)
        else { ...leaveTop(rule, m, Normal, false), pc: m.pc + 1 }
      | Return(b) =>
        if (bit(x, b)) { ...leaveAll(rule, m, Normal), status: "ok" } else { ...m, pc: m.pc + 1 }
      | Fail(b) =>
        if (bit(x, b)) { ...leaveAll(rule, m, Errored), status: "error" }
        else { ...m, pc: m.pc + 1 }
      | Break(b) =>
        if (bit(x, b)) {
          val inner = leaveToLoop(rule, m)
          val loopStart = inner.frames[length(inner.frames) - 1].start
          { ...leaveTop(rule, inner, Normal, true), pc: endOf(p, loopStart) + 1 }
        } else { ...m, pc: m.pc + 1 }
      | Continue(b) =>
        if (bit(x, b)) endTurn(rule, p, leaveToLoop(rule, m), true) else { ...m, pc: m.pc + 1 }
    }

  pure def runOn(rule: Rule, p: List[Stmt], x: int): Machine = {
    val m0: Machine = {
      pc: 0, frames: List(fnFrame), out: List(), status: "run", serial: 0,
      registered: Map(), left: Map(), ran: Map(), batches: List(),
    }
    range(0, MAX_STEPS).foldl(m0, (acc, _) => exec(rule, p, x, acc))
  }

  // What main prints: the program's lines, then 0 or -1.
  pure def printed(m: Machine): List[int] =
    m.out.append(if (m.status == "error") ERROR_RESULT else 0)

  // ------------------------------------------------------------ properties

  // §6.3.1 over the ledger of one finished run:
  //  * every frame is left: each registration has an exit;
  //  * a defer ran exactly once; an errdefer exactly once when its frame was
  //    left on an error return, and never otherwise;
  //  * the cleanups of one exit ran newest registration first.
  pure def exitsSound(m: Machine): bool = and {
    m.status != "run",
    m.registered.keys().forall(r => m.left.keys().contains(r)),
    m.registered.keys().forall(r => {
      val e = m.registered.get(r)
      val want = if (not(e.onError) or m.left.get(r) == Errored) 1 else 0
      m.ran.get(r) == want
    }),
    m.batches.foldl(true, (ok, batch) =>
      ok and range(1, length(batch)).foldl(true, (o, j) => o and batch[j - 1] > batch[j])),
  }

  // ------------------------------------------------------- the generator

  var prog: List[Stmt]
  var open: List[Stmt]
  var nextId: int
  var rule: Rule
  var done: bool
  // When done: what main prints on each input, and whether exitsSound holds
  // on every input.
  var expected: int -> List[int]
  var sound: bool

  action start(r: Rule): bool = all {
    prog' = List(), open' = List(), nextId' = 1, rule' = r, done' = false,
    expected' = Map(), sound' = true,
  }
  action init = start(Spec)
  action initFallthroughOnly = start(FallthroughOnly)
  action initErrDeferAlways = start(ErrDeferAlways)

  pure def inLoop(o: List[Stmt]): bool =
    o.foldl(false, (acc, s) => acc or (match s { | LoopBegin(_) => true | _ => false }))

  action add(s: Stmt, o: List[Stmt]): bool = all {
    not(done),
    length(prog) < MAX_LEN,
    prog' = prog.append(s), open' = o, nextId' = nextId + 1,
    rule' = rule, done' = false, expected' = expected, sound' = sound,
  }

  action addCleanup = {
    nondet err = Set(true, false).oneOf()
    add(if (err) ErrDefer(nextId) else Defer(nextId), open)
  }

  action addNote = add(Note(100 + nextId), open)

  action addExit = {
    nondet b = BITS.oneOf()
    nondet kind = 0.to(3).oneOf()
    all {
      kind < 2 or inLoop(open),
      add(if (kind == 0) Return(b) else if (kind == 1) Fail(b) else if (kind == 2) Break(b) else Continue(b), open),
    }
  }

  action openBlock = {
    nondet b = BITS.oneOf()
    nondet loop = Set(true, false).oneOf()
    val s = if (loop) LoopBegin(nextId) else IfBegin(b)
    all { length(open) < 2, add(s, open.append(s)) }
  }

  action closeBlock = all {
    length(open) > 0,
    not(isOpen(prog[length(prog) - 1])),
    add(End, open.slice(0, length(open) - 1)),
  }

  action finish = {
    val runs = INPUTS.mapBy(x => runOn(rule, prog, x))
    all {
      not(done),
      length(open) == 0,
      length(prog) > 0,
      done' = true,
      expected' = INPUTS.mapBy(x => printed(runs.get(x))),
      sound' = INPUTS.forall(x => exitsSound(runs.get(x))),
      prog' = prog, open' = open, nextId' = nextId, rule' = rule,
    }
  }

  action halt = all {
    done or (length(prog) >= MAX_LEN and length(open) > 0),
    prog' = prog, open' = open, nextId' = nextId, rule' = rule, done' = done,
    expected' = expected, sound' = sound,
  }

  action step = any {
    addCleanup, addCleanup, addNote, addExit, addExit, openBlock, closeBlock, closeBlock, finish, halt,
  }

  val soundness: bool = done implies sound

  // Witnesses: each is expected to be violated by some run.
  val reachedErrorExit: bool = not(done and INPUTS.exists(x => {
    val out = expected.get(x)
    out[length(out) - 1] == ERROR_RESULT
  }))
}
