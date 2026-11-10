// -*- mode: Bluespec; -*-
// The ownership checker as a model: which programs over one owned String the
// checker accepts (SPEC §6.3.1, §8, §9, §14.4), and what those programs do at
// run time. The state machine builds one program a statement at a time; when
// it finishes, `expected` holds the diagnostics the model predicts. The
// invariants say that every program the model accepts runs without a use after
// free, a double free, a leak or a write through a live shared borrow.
//
// The programs are rendered to Kizu by internal/ownership/mbt_test.go; the
// fixed frame around them is
//
//   fn main() -> !void {
//       let allocator = mem::page_allocator();
//       var s = try string::from_bytes(allocator, "ab");
//       <statements>
//       return;
//   }
//
// README.md in this directory says why the statements are these.
module ownership {
  // One statement of the body. Ids name bindings and are fresh per program.
  type Stmt =
    | View(int)       // let v<id> = s.as_bytes();
    | UseView(int)    // print(v<id>);
    | Borrow(int)     // let r<id> = &s;
    | UseBorrow(int)  // print(r<id>.len());
    | ReadOwner       // print(s.len());
    | Mutate          // try s.append_bytes(allocator, "x");
    | Deinit          // s.deinit(allocator);
    | MoveOut         // sink(allocator, move s);
    | DeferDeinit     // defer s.deinit(allocator);
    | Unrelated(int)  // var u<id> = string::new(allocator); defer u<id>.deinit(allocator);
    | LoopBegin(int)  // for 0..2 |i<id>| {
    | WhileBegin(int) // var i<id> = 0; while i<id> < 2 {     (the body ends with i<id> = i<id> + 1;)
    | LoopEnd         // }
    // Only in unrolled programs (the run-time model): the bounds of one turn.
    | TurnBegin
    | TurnEnd

  // The diagnostic classes the checker reports, by what they mean rather than
  // their wording. mbt_test.go maps the wording onto these.
  type Kind =
    | Borrowed      // a mutation, deinit or move of `s` while a view or borrow of it is live
    | UseAfterMove  // `s` used, deinitialized or deferred after it was consumed
    | DoubleDefer   // a second `defer s.deinit` while one is registered
    | Leak          // an exit (the end, or a `try`'s error exit) that leaves `s` unconsumed
    | LoopConsume   // a loop body that consumes `s`, declared outside it

  // What going wrong looks like at run time.
  type Fault =
    | UseAfterFree   // `s` read, viewed, borrowed or mutated after its buffer was released
    | DanglingView   // a view read after its buffer was released or reallocated
    | DanglingBorrow // a borrow read after `s` was released
    | SharedMutated  // a shared borrow read after `s` was mutated under it
    | DoubleFree     // the buffer released twice (also on a `try`'s error exit)
    | LeakAtExit     // the function returns with the buffer still held
    | LeakOnError    // a `try`'s error exit returns with the buffer still held

  // ---------------------------------------------------------------- shape

  pure def isUseOf(s: Stmt, id: int): bool = match s {
    | UseView(x) => x == id
    | UseBorrow(x) => x == id
    | _ => false
  }

  // The id a view or borrow binding introduces, or -1.
  pure def bindingId(s: Stmt): int = match s {
    | View(x) => x
    | Borrow(x) => x
    | _ => -1
  }

  // Whether the statement names `s` itself.
  pure def usesOwner(s: Stmt): bool = match s {
    | View(_) => true
    | Borrow(_) => true
    | ReadOwner => true
    | Mutate => true
    | Deinit => true
    | MoveOut => true
    | DeferDeinit => true
    | _ => false
  }

  // Whether the statement needs `s` exclusively: it may not run under a live view or borrow.
  pure def exclusive(s: Stmt): bool = s == Mutate or s == Deinit or s == MoveOut

  pure def consumes(s: Stmt): bool = s == Deinit or s == MoveOut

  pure def isLoopBegin(s: Stmt): bool = match s {
    | LoopBegin(_) => true
    | WhileBegin(_) => true
    | _ => false
  }

  pure def last(l: List[a]): a = l[length(l) - 1]
  pure def pop(l: List[a]): List[a] = l.slice(0, length(l) - 1)
  pure def setLast(l: List[a], x: a): List[a] = l.replaceAt(length(l) - 1, x)
  pure def anyOf(l: List[bool]): bool = l.foldl(false, (acc, b) => acc or b)

  // The positions of the loop headers whose bodies hold position i, outermost first.
  pure def openLoops(p: List[Stmt], i: int): List[int] =
    range(0, i).foldl(List(), (open, j) =>
      if (isLoopBegin(p[j])) open.append(j) else if (p[j] == LoopEnd) pop(open) else open)

  // The position of the LoopEnd closing the loop that begins at b, or length(p)
  // while it is still open.
  pure def loopEndOf(p: List[Stmt], b: int): int = {
    val r = range(b + 1, length(p)).foldl({ depth: 0, end: length(p) }, (acc, j) =>
      if (acc.end < length(p)) acc
      else if (isLoopBegin(p[j])) { ...acc, depth: acc.depth + 1 }
      else if (p[j] == LoopEnd) (if (acc.depth == 0) { ...acc, end: j } else { ...acc, depth: acc.depth - 1 })
      else acc)
    r.end
  }

  // ------------------------------------------------------- checker model

  // SPEC §9: a local borrow binding is active until its last use in
  // straight-line code. A binding declared outside a loop and read inside it is
  // read again by the next turn, so it is live across the whole body
  // (examples/negative/borrow_loop_last_use.kizu) -- for each enclosing loop
  // the binding is declared outside of. A binding declared inside a body ends
  // with the turn.
  pure def liveAt(p: List[Stmt], id: int, decl: int, pos: int): bool = {
    val laterUse = (pos + 1).to(length(p) - 1).exists(q => isUseOf(p[q], id))
    val carried = openLoops(p, pos).foldl(false, (acc, b) =>
      acc or (decl < b and (b + 1).to(loopEndOf(p, b) - 1).exists(q => isUseOf(p[q], id))))
    laterUse or carried
  }

  // Whether some view or borrow of `s` is live at position pos.
  pure def borrowedAt(p: List[Stmt], pos: int): bool =
    p.indices().exists(d => d < pos and bindingId(p[d]) >= 0 and liveAt(p, bindingId(p[d]), d, pos))

  type CheckState = {
    consumed: bool,            // deinit or moved on the path so far
    outerDefer: bool,          // a `defer s.deinit` registered in the function body
    bodyDefer: List[bool],     // per open loop, innermost last: a `defer s.deinit` in its body
    bodyConsumes: List[bool],  // per open loop: its body consumes `s`
    errs: Set[Kind],
  }

  // The checker on one statement. The model does not say which error comes
  // first: it collects every rule a statement breaks, continuing as though the
  // statement ran, and the checker's first report must be one of them.
  pure def checkStmt(p: List[Stmt], st: CheckState, i: int): CheckState = {
    val s = p[i]
    val inLoop = length(st.bodyDefer) > 0
    val deferred = st.outerDefer or anyOf(st.bodyDefer)
    val afterMove = if (usesOwner(s) and st.consumed) Set(UseAfterMove) else Set()
    val borrowed = if (exclusive(s) and borrowedAt(p, i)) Set(Borrowed) else Set()
    // SPEC §8: an owner must be consumed on every exit, and a `try` is one.
    val leak = if (s == Mutate and not(st.consumed) and not(deferred)) Set(Leak) else Set()
    // SPEC §8: a loop body may not consume an owner declared outside it.
    val consumesInLoop = inLoop and (consumes(s) or s == DeferDeinit)
    val loopConsume = if (consumesInLoop) Set(LoopConsume) else Set()
    val doubleDefer = if (s == DeferDeinit and deferred) Set(DoubleDefer) else Set()
    val errs = st.errs.union(afterMove).union(borrowed).union(leak).union(loopConsume).union(doubleDefer)
    if (s == LoopEnd)
      // The body's defers run as the turn ends, and SPEC §6.3.1 refuses a
      // deferred deinit whose owner is borrowed when it runs: a view the next
      // turn or the code after the loop reads is live there. A body that
      // consumed `s` meets it consumed on the next turn, which the checker may
      // report first; and the loop around it consumes `s` too.
      val ownDefer = last(st.bodyDefer)
      val ownConsumes = last(st.bodyConsumes)
      val deferBorrowed = if (ownDefer and borrowedAt(p, i)) Set(Borrowed) else Set()
      val nextTurn = if (ownConsumes) Set(UseAfterMove) else Set()
      val outer = pop(st.bodyConsumes)
      {
        consumed: st.consumed or ownDefer,
        outerDefer: st.outerDefer,
        bodyDefer: pop(st.bodyDefer),
        bodyConsumes: if (length(outer) > 0 and ownConsumes) setLast(outer, true) else outer,
        errs: errs.union(deferBorrowed).union(nextTurn),
      }
    else if (isLoopBegin(s))
      { ...st, bodyDefer: st.bodyDefer.append(false), bodyConsumes: st.bodyConsumes.append(false) }
    else
      {
        consumed: st.consumed or consumes(s),
        outerDefer: st.outerDefer or (s == DeferDeinit and not(inLoop)),
        bodyDefer: if (s == DeferDeinit and inLoop) setLast(st.bodyDefer, true) else st.bodyDefer,
        bodyConsumes: if (consumesInLoop) setLast(st.bodyConsumes, true) else st.bodyConsumes,
        errs: errs,
      }
  }

  pure val checkStart: CheckState =
    { consumed: false, outerDefer: false, bodyDefer: List(), bodyConsumes: List(), errs: Set() }

  pure def checkPrefix(p: List[Stmt]): CheckState =
    range(0, length(p)).foldl(checkStart, (st, i) => checkStmt(p, st, i))

  // Every diagnostic class the checker may report for a finished program; the
  // empty set means it must accept.
  pure def errorsOf(p: List[Stmt]): Set[Kind] = {
    val st = checkPrefix(p)
    // SPEC §6.3.1: the deferred deinit runs at the exit and must find `s` unconsumed.
    val exitAfterMove = if (st.consumed and st.outerDefer) Set(UseAfterMove) else Set()
    val exitLeak = if (not(st.consumed) and not(st.outerDefer)) Set(Leak) else Set()
    st.errs.union(exitAfterMove).union(exitLeak)
  }

  // ------------------------------------------------------ run-time model

  // Each innermost loop's body repeated `turns` times, every turn between
  // TurnBegin and TurnEnd. Loops nest at most twice, so two passes unroll all.
  pure def unrollInnermost(p: List[Stmt], turns: int): List[Stmt] = {
    val r = p.foldl({ out: List(), pending: List() }, (acc, s) =>
      if (isLoopBegin(s))
        { out: acc.out.concat(acc.pending), pending: List(s) }
      else if (s == LoopEnd and length(acc.pending) > 0)
        {
          out: range(0, turns).foldl(acc.out, (o, _) =>
            o.append(TurnBegin).concat(acc.pending.slice(1, length(acc.pending))).append(TurnEnd)),
          pending: List(),
        }
      else if (length(acc.pending) > 0)
        { ...acc, pending: acc.pending.append(s) }
      else
        { ...acc, out: acc.out.append(s) })
    r.out.concat(r.pending)
  }

  pure def unroll(p: List[Stmt], turns: int): List[Stmt] =
    unrollInnermost(unrollInnermost(p, turns), turns)

  type RunState = {
    alive: bool,                 // `s` owns a live buffer
    gen: int,                    // bumped by every append, which may reallocate
    viewGen: int -> int,         // the buffer generation each view points into
    borrowGen: int -> int,       // the generation each borrow last saw
    outerDefers: int,            // deferred deinits registered in the function body
    turnDefers: List[int],       // per running turn, innermost last: deferred deinits it registered
    faults: Set[Fault],
  }

  pure def lookup(m: int -> int, k: int): int = if (m.keys().contains(k)) m.get(k) else -1

  // Releases the buffer `n` times, as `n` deferred deinits do.
  pure def release(st: RunState, n: int): RunState =
    range(0, n).foldl(st, (acc, _) =>
      { ...acc, alive: false, faults: if (acc.alive) acc.faults else acc.faults.union(Set(DoubleFree)) })

  pure def runStmt(st: RunState, s: Stmt): RunState = {
    val dead = if (st.alive) Set() else Set(UseAfterFree)
    match s {
      | View(id) => { ...st, viewGen: st.viewGen.put(id, st.gen), faults: st.faults.union(dead) }
      | UseView(id) =>
        { ...st, faults: if (st.alive and lookup(st.viewGen, id) == st.gen) st.faults
                         else st.faults.union(Set(DanglingView)) }
      | Borrow(id) => { ...st, borrowGen: st.borrowGen.put(id, st.gen), faults: st.faults.union(dead) }
      | UseBorrow(id) =>
        { ...st, faults: st.faults
            .union(if (st.alive) Set() else Set(DanglingBorrow))
            .union(if (lookup(st.borrowGen, id) == st.gen) Set() else Set(SharedMutated)) }
      | ReadOwner => { ...st, faults: st.faults.union(dead) }
      | Mutate =>
        // If the append fails, the error exit runs every registered defer.
        val pending = st.outerDefers + st.turnDefers.foldl(0, (n, k) => n + k)
        val onError =
          if (pending == 0) (if (st.alive) Set(LeakOnError) else Set())
          else if (not(st.alive) or pending >= 2) Set(DoubleFree)
          else Set()
        { ...st, gen: st.gen + 1, faults: st.faults.union(dead).union(onError) }
      | Deinit => release(st, 1)
      | MoveOut => release(st, 1)   // `sink` deinitializes what it receives
      | DeferDeinit =>
        if (length(st.turnDefers) > 0) { ...st, turnDefers: setLast(st.turnDefers, last(st.turnDefers) + 1) }
        else { ...st, outerDefers: st.outerDefers + 1 }
      | TurnBegin => { ...st, turnDefers: st.turnDefers.append(0) }
      | TurnEnd => { ...release(st, last(st.turnDefers)), turnDefers: pop(st.turnDefers) }
      | _ => st
    }
  }

  pure def runWith(p: List[Stmt], turns: int): Set[Fault] = {
    val start: RunState = {
      alive: true, gen: 0, viewGen: Map(), borrowGen: Map(),
      outerDefers: 0, turnDefers: List(), faults: Set(),
    }
    val st = unroll(p, turns).foldl(start, (acc, s) => runStmt(acc, s))
    val exited = release(st, st.outerDefers)
    exited.faults.union(if (exited.alive) Set(LeakAtExit) else Set())
  }

  // Faults over every number of turns a loop may take. The rendered loops take
  // two, but the checker does not count turns, so neither does the property.
  pure def faultsOf(p: List[Stmt]): Set[Fault] =
    Set(0, 1, 2).fold(Set(), (acc, k) => acc.union(runWith(p, k)))

  // ------------------------------------------------------------ generator

  pure val MAX_LEN = 10
  pure val MAX_DEPTH = 2

  var prog: List[Stmt]
  var nextId: int
  var views: Set[int]            // view ids in scope
  var borrows: Set[int]          // borrow ids in scope
  var bodyNames: List[Set[int]]  // per open loop, innermost last: the ids its body declared
  var unused: Set[int]           // ids declared but not yet read (the type checker refuses those)
  var budget: int                // how many statements may break a rule the program kept so far
  var allowed: Set[Kind]         // the rules a statement may break at all
  var exact: bool                // the finished program must break exactly the allowed rules
  var mistakes: int              // how many did
  var broken: Set[Kind]          // the rules prog breaks so far: checkPrefix(prog).errs, kept to save its cost
  var done: bool
  var expected: Set[Kind]
  var faults: Set[Fault]

  // Whether a program that extends prog and has the diagnostics qErrs breaks a
  // rule prog did not.
  def breaksNewRule(qErrs: Set[Kind]): bool = not(qErrs.subseteq(broken))

  // Each mode has a budget of rule breaks: none gives accepted programs, one
  // gives programs with a single mistake, which say the most about which rule
  // the checker applied, and a large one gives anything. A targeted mode also
  // narrows which rules may break, and finishes only on exactly those.
  pure val ALL_KINDS: Set[Kind] = Set(Borrowed, UseAfterMove, DoubleDefer, Leak, LoopConsume)

  // The verdicts the targeted mode aims at, since a run that may break any rule
  // mostly breaks the common ones (Leak, UseAfterMove) and seldom stops at one
  // Borrowed, one DoubleDefer, or a pair of the rare ones. Every set of classes
  // is a verdict some program has, except that a loop body's consume always
  // comes with UseAfterMove (the next turn meets `s` consumed).
  pure val TARGETS: Set[Set[Kind]] = ALL_KINDS.powerset().filter(k =>
    k != Set() and (k.contains(LoopConsume) implies k.contains(UseAfterMove)))

  // The single-class verdicts among them, which say the most about which rule
  // the checker applied (it reports one diagnostic, and with several the
  // report may be any of them).
  pure val SINGLE_TARGETS: Set[Set[Kind]] =
    TARGETS.filter(k => k.size() == 1 or k == Set(LoopConsume, UseAfterMove))

  def room: bool = length(prog) < MAX_LEN
  def depth: int = length(bodyNames)

  action take(s: Stmt): bool = all {
    not(done),
    val errs = checkPrefix(prog.append(s)).errs
    val m = breaksNewRule(errs)
    all {
      errs.subseteq(allowed),
      not(m) or mistakes < budget,
      mistakes' = if (m) mistakes + 1 else mistakes,
      broken' = errs,
    },
    prog' = prog.append(s),
  }

  action keepNames: bool = all {
    nextId' = nextId, views' = views, borrows' = borrows, bodyNames' = bodyNames, unused' = unused,
  }

  action keepResult: bool = all {
    budget' = budget, allowed' = allowed, exact' = exact, done' = done, expected' = expected, faults' = faults,
  }

  action declare(s: Stmt, isView: bool): bool = all {
    room,
    take(s),
    nextId' = nextId + 1,
    views' = if (isView) views.union(Set(nextId)) else views,
    borrows' = if (isView) borrows else borrows.union(Set(nextId)),
    bodyNames' = if (depth > 0) setLast(bodyNames, last(bodyNames).union(Set(nextId))) else bodyNames,
    unused' = unused.union(Set(nextId)),
    keepResult,
  }

  action read(s: Stmt, id: int): bool = all {
    room or unused.contains(id),
    take(s),
    nextId' = nextId, views' = views, borrows' = borrows, bodyNames' = bodyNames,
    unused' = unused.exclude(Set(id)),
    keepResult,
  }

  action plain(s: Stmt): bool = all { room, take(s), keepNames, keepResult }

  action unrelated: bool = all {
    room,
    take(Unrelated(nextId)),
    nextId' = nextId + 1,
    views' = views, borrows' = borrows, bodyNames' = bodyNames, unused' = unused,
    keepResult,
  }

  action openLoop(isWhile: bool): bool = all {
    room,
    depth < MAX_DEPTH,
    take(if (isWhile) WhileBegin(nextId) else LoopBegin(nextId)),
    nextId' = nextId + 1,
    views' = views, borrows' = borrows, bodyNames' = bodyNames.append(Set()), unused' = unused,
    keepResult,
  }

  action closeLoop: bool = all {
    depth > 0,
    unused.intersect(last(bodyNames)) == Set(),
    take(LoopEnd),
    nextId' = nextId,
    views' = views.exclude(last(bodyNames)),
    borrows' = borrows.exclude(last(bodyNames)),
    bodyNames' = pop(bodyNames),
    unused' = unused,
    keepResult,
  }

  // Ends the program, optionally with a closing deinit so that runs reach
  // accepted programs instead of leaking. The program must name `s` somewhere
  // and read every binding, or the type checker refuses it before ownership
  // is asked.
  action finish(closing: List[Stmt]): bool = all {
    not(done),
    depth == 0,
    unused == Set(),
    val p = prog.concat(closing)
    val errs = errorsOf(p)
    val m = breaksNewRule(errs)
    all {
      p.foldl(false, (named, s) => named or usesOwner(s)),
      if (exact) errs == allowed else errs.subseteq(allowed),
      not(m) or mistakes < budget,
      mistakes' = if (m) mistakes + 1 else mistakes,
      prog' = p,
      broken' = errs,
      done' = true,
      expected' = errs,
      faults' = faultsOf(p),
    },
    keepNames,
    budget' = budget, allowed' = allowed, exact' = exact,
  }

  action stutter: bool = all {
    done,
    prog' = prog, mistakes' = mistakes, broken' = broken, keepNames, keepResult,
  }

  action start(rules: int, kinds: Set[Kind], exactly: bool): bool = all {
    prog' = List(),
    nextId' = 0,
    views' = Set(),
    borrows' = Set(),
    bodyNames' = List(),
    unused' = Set(),
    budget' = rules,
    allowed' = kinds,
    exact' = exactly,
    mistakes' = 0,
    broken' = Set(),
    done' = false,
    expected' = Set(),
    faults' = Set(),
  }

  action init = nondet b = Set(0, 1, MAX_LEN).oneOf() start(b, ALL_KINDS, false)
  action initGuided = start(0, Set(), false)
  action initOneMistake = start(1, ALL_KINDS, false)
  action initFree = start(MAX_LEN, ALL_KINDS, false)
  // Half the runs aim at a single class, half at any verdict: oneOf picks
  // alike among the elements of a set, so the roll is the weighting.
  action initTargeted = {
    nondet roll = Set(0, 1).oneOf()
    nondet single = SINGLE_TARGETS.oneOf()
    nondet some = TARGETS.oneOf()
    start(MAX_LEN, if (roll == 0) single else some, true)
  }

  // The statements that name an existing binding or `s` alone.
  def readable: Set[Stmt] =
    Set(ReadOwner, Mutate, Deinit, MoveOut, DeferDeinit)
      .union(views.map(id => UseView(id)))
      .union(borrows.map(id => UseBorrow(id)))

  // Whether appending s breaks a rule of the goal the program has not broken yet.
  def advances(s: Stmt): bool = {
    val errs = checkPrefix(prog.append(s)).errs
    errs.subseteq(allowed) and breaksNewRule(errs)
  }

  // A targeted run picks among all statements alike, so a goal that needs one
  // particular statement at one particular point (a second `defer`, a read that
  // makes an earlier `mutate` borrowed) is seldom finished within MAX_LEN. This
  // branch picks a statement that moves the program toward its goal.
  // `TurnBegin` is the "none" choice (oneOf needs a value).
  action aim: bool = all {
    exact,
    nondet s = readable.filter(advances).union(Set(TurnBegin)).oneOf()
    all {
      s != TurnBegin,
      match s {
        | UseView(id) => read(s, id)
        | UseBorrow(id) => read(s, id)
        | _ => plain(s)
      },
    },
  }

  // What a goal's rule needs before it can break at all: a registered `defer`
  // for DoubleDefer, a view or borrow for Borrowed, a loop for LoopConsume, an
  // unprotected `try` for Leak (which aim already offers), a consume for
  // UseAfterMove. Without it a targeted run spends its MAX_LEN statements on
  // others and ends with no way to finish.
  action prepare: bool = all {
    exact,
    any {
      all { allowed.contains(DoubleDefer), plain(DeferDeinit) },
      all { allowed.contains(Borrowed), declare(View(nextId), true) },
      all { allowed.contains(Borrowed), declare(Borrow(nextId), false) },
      all { allowed.contains(LoopConsume), openLoop(false) },
      all { allowed.contains(LoopConsume), openLoop(true) },
      all { allowed.contains(UseAfterMove), plain(Deinit) },
      all { allowed.contains(UseAfterMove), plain(MoveOut) },
    },
  }

  // The simulator picks among the enabled branches of `any` alike, so the
  // nesting is the weighting: in a targeted run aim and prepare are taken about
  // as often as all the other statements together; in the other modes they are
  // disabled and the step is the inner `any` alone.
  action step = any {
    aim,
    prepare,
    any {
      declare(View(nextId), true),
      declare(Borrow(nextId), false),
      nondet id = views.union(Set(-1)).oneOf()
        all { id >= 0, read(UseView(id), id) },
      nondet id = borrows.union(Set(-1)).oneOf()
        all { id >= 0, read(UseBorrow(id), id) },
      plain(ReadOwner),
      plain(Mutate),
      plain(Deinit),
      plain(MoveOut),
      plain(DeferDeinit),
      unrelated,
      openLoop(false),
      openLoop(true),
      closeLoop,
      finish(List()),
      finish(List(Deinit)),
      stutter,
    },
  }

  // ----------------------------------------------------------- properties

  def accepted: bool = done and expected == Set()

  // Soundness of the checker model, one property per promise of
  // docs/memory-safety.md, each over every program the model accepts.
  // A moved or deinitialized `s` is not used again.
  val noUseAfterMove: bool = accepted implies not(faults.contains(UseAfterFree))
  // `s` is not deinitialized or moved while a view or borrow of it is still read:
  // a view never outlives its owner's buffer, nor survives a reallocation.
  val noDanglingView: bool = accepted implies not(faults.contains(DanglingView))
  val noDanglingBorrow: bool = accepted implies not(faults.contains(DanglingBorrow))
  // A shared borrow does not see `s` change under it.
  val noSharedMutation: bool = accepted implies not(faults.contains(SharedMutated))
  // The buffer is released at most once, on every exit.
  val atMostOneDeinit: bool = accepted implies not(faults.contains(DoubleFree))
  // ... and at least once (SPEC §8: no implicit leak).
  val noLeak: bool = accepted implies faults.intersect(Set(LeakAtExit, LeakOnError)) == Set()

  val soundness: bool = and {
    noUseAfterMove, noDanglingView, noDanglingBorrow, noSharedMutation, atMostOneDeinit, noLeak,
  }

  // Witnesses: a property search that never reaches these says nothing.
  pure def loopDepth(p: List[Stmt]): int =
    p.foldl({ d: 0, most: 0 }, (acc, s) =>
      if (isLoopBegin(s)) { d: acc.d + 1, most: if (acc.d + 1 > acc.most) acc.d + 1 else acc.most }
      else if (s == LoopEnd) { ...acc, d: acc.d - 1 }
      else acc).most

  val reachedAccept: bool = accepted
  val reachedAcceptWithLoop: bool = accepted and loopDepth(prog) >= 1
  val reachedAcceptNested: bool = accepted and loopDepth(prog) >= 2
  val reachedBorrowed: bool = done and expected.contains(Borrowed)
  val reachedUseAfterMove: bool = done and expected.contains(UseAfterMove)
  val reachedLeak: bool = done and expected.contains(Leak)
  val reachedLoopConsume: bool = done and expected.contains(LoopConsume)
  val reachedDoubleDefer: bool = done and expected.contains(DoubleDefer)
  // A program whose only diagnostic is one class: the shapes that say the most
  // about which rule the checker applied, and the rarest under random runs.
  val reachedOnlyBorrowed: bool = done and expected == Set(Borrowed)
  val reachedOnlyDoubleDefer: bool = done and expected == Set(DoubleDefer)
  val reachedOnlyLoopConsume: bool = done and expected == Set(LoopConsume, UseAfterMove)
}
