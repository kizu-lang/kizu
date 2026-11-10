// -*- mode: Bluespec; -*-
// The slot decision of internal/ir/slots.go as a model: which locals of a
// function the lowerer gives storage (a `local.slot`) because something hands
// them over as the caller's storage (`&var`). The decision is made once per
// function, before the body is lowered, and it holds for every use of the name.
//
// A program declares up to three struct types A, B, C, each with up to two
// methods named `poke` and `note`. Several types may declare a method of the
// same name with different receivers and parameters -- that is how #1914 met
// it: the lent positions were looked up by the method's name, so a call to
// B.poke (shared receiver) was decided as if it could reach A.poke (`&var`
// receiver). The model keeps the declarations apart and says which one each
// call reaches.
//
// The programs are rendered to Kizu by internal/ir/slots_mbt_test.go; the
// frame is
//
//   struct A { n: i64, }   (B, C the same)
//   fn (self: <recv> T) <m>(total: <arg>) -> void { ... }   (each declared method)
//   fn bump(total: &var i64) -> void { ... }
//   fn main() -> void {
//       var x0 = T0 { n: 100 };  var x1 = ...;  var x2 = ...;
//       var n0 = 200;  var n1 = 201;  var n2 = 202;
//       <statements>
//       print(...every local...);
//   }
//
// README.md in this directory says why the statements are these.
module slots {
  // How a method receives its receiver.
  type Recv =
    | RShared // fn (self: &T)
    | RLent   // fn (self: &var T)    -- the caller's storage
    | RValue  // fn (self: T)

  // How a method receives its one argument.
  type Arg =
    | AVal    // total: i64
    | ALent   // total: &var i64      -- the caller's storage

  type Decl = { declared: bool, recv: Recv, arg: Arg }

  // A local of main: a struct x<i> or an integer n<k>.
  type Local =
    | X(int)
    | N(int)

  // The receiver x<x>, the method (0 poke, 1 note) and the argument n<k>.
  type CallSite = { x: int, m: int, k: int }

  // One statement of main. Blocks nest; the slot decision is the same at
  // every depth.
  type Stmt =
    | Call(CallSite)            // x<i>.<method m>(n<k>);
    | DeferCall(CallSite)       // defer x<i>.<method m>(n<k>);
    | Bump(int)                 // bump(n<k>);          a free function's `&var` parameter
    | BumpExplicit(int)         // bump(&var n<k>);     the same, lent at the call site
    | IfBegin                   // if n0 > 0 {
    | LoopBegin(int)            // for 0..2 |i<id>| {
    | BlockEnd                  // }

  // Which declarations a method call is decided by.
  type Rule =
    | Identity // the declaration the call reaches: the receiver's type's method (slots.go since #1914)
    | ByName   // every declaration bearing the method's name (slots.go before #1914)

  pure val TYPES = 0.to(2)    // A, B, C
  pure val METHODS = 0.to(1)  // poke, note
  pure val XS = 0.to(2)
  pure val NS = 0.to(2)
  pure val MAX_LEN = 10

  pure val DECLS: Set[Decl] =
    tuples(Set(true, false), Set(RShared, RLent, RValue), Set(AVal, ALent))
      .map(t => { declared: t._1, recv: t._2, arg: t._3 })

  // ------------------------------------------------------------ the rule

  // The declarations a call x<i>.<m>() is decided by under a rule.
  pure def decidingDecls(
    decls: (int, int) -> Decl, xtype: List[int], rule: Rule, i: int, m: int
  ): Set[Decl] = match rule {
    | Identity => Set(decls.get((xtype[i], m)))
    | ByName => TYPES.filter(t => decls.get((t, m)).declared).map(t => decls.get((t, m)))
  }

  // The locals a method call marks under a rule: a position is marked when
  // any declaration the rule reads receives it as the caller's storage.
  pure def callMarks(decls: (int, int) -> Decl, xtype: List[int], rule: Rule, c: CallSite): Set[Local] = {
    val ds = decidingDecls(decls, xtype, rule, c.x, c.m)
    val recv = if (ds.exists(d => d.recv == RLent)) Set(X(c.x)) else Set()
    val arg = if (ds.exists(d => d.arg == ALent)) Set(N(c.k)) else Set()
    recv.union(arg)
  }

  // The locals one statement marks under a rule.
  pure def marks(decls: (int, int) -> Decl, xtype: List[int], rule: Rule, s: Stmt): Set[Local] =
    match s {
      | Call(c) => callMarks(decls, xtype, rule, c)
      // A cleanup runs on the value its receiver holds at the exit
      // (SPEC §6.3.1), so a deferred call's receiver is storage whatever the
      // method takes; its arguments are decided like a direct call's.
      | DeferCall(c) => callMarks(decls, xtype, rule, c).union(Set(X(c.x)))
      | Bump(k) => Set(N(k))
      | BumpExplicit(k) => Set(N(k))
      | _ => Set()
    }

  // The locals a function's statements give storage to under a rule.
  pure def slotsOf(decls: (int, int) -> Decl, xtype: List[int], rule: Rule, p: List[Stmt]): Set[Local] =
    p.foldl(Set(), (acc, s) => acc.union(marks(decls, xtype, rule, s)))

  // --------------------------------------------------- what must hold

  // The positions the declaration a statement reaches receives as the
  // caller's storage: a write there has to land in the local.
  pure def reachedLent(decls: (int, int) -> Decl, xtype: List[int], c: CallSite): Set[Local] = {
    val d = decls.get((xtype[c.x], c.m))
    (if (d.recv == RLent) Set(X(c.x)) else Set()).union(if (d.arg == ALent) Set(N(c.k)) else Set())
  }

  pure def lentBy(decls: (int, int) -> Decl, xtype: List[int], s: Stmt): Set[Local] =
    match s {
      | Call(c) => reachedLent(decls, xtype, c)
      | DeferCall(c) => reachedLent(decls, xtype, c)
      | Bump(k) => Set(N(k))
      | BumpExplicit(k) => Set(N(k))
      | _ => Set()
    }

  // The receivers a deferred call reads at the exit rather than now.
  pure def readAtExit(s: Stmt): Set[Local] = match s {
    | DeferCall(c) => Set(X(c.x))
    | _ => Set()
  }

  // Every local some reached declaration writes through has storage:
  // otherwise the callee writes into a copy and the caller reads the value it
  // started with.
  pure def noLostWrite(decls: (int, int) -> Decl, xtype: List[int], rule: Rule, p: List[Stmt]): bool =
    p.foldl(true, (ok, s) => ok and lentBy(decls, xtype, s).subseteq(slotsOf(decls, xtype, rule, p)))

  // A local has storage only for a declaration a call to it reaches (or a
  // deferred read). A local marked for another type's method of the same
  // name is not only slower: a match through it moves the payload out
  // instead of binding it where it lies (#1914).
  pure def onlyReached(decls: (int, int) -> Decl, xtype: List[int], rule: Rule, p: List[Stmt]): bool = {
    val needed = p.foldl(Set(), (acc, s) => acc.union(lentBy(decls, xtype, s)).union(readAtExit(s)))
    slotsOf(decls, xtype, rule, p).subseteq(needed)
  }

  // ------------------------------------------------------- the generator

  var decls: (int, int) -> Decl
  var xtype: List[int]
  var prog: List[Stmt]
  var depth: int
  var nextId: int
  var rule: Rule
  var done: bool
  // When done: the locals the rule gives storage, and whether deciding by
  // name would give a different answer (the programs #1914 is about).
  var expected: Set[Local]
  var nameDiffers: bool

  action start(r: Rule): bool = {
    nondet a0 = DECLS.oneOf()
    nondet a1 = DECLS.oneOf()
    nondet b0 = DECLS.oneOf()
    nondet b1 = DECLS.oneOf()
    nondet c0 = DECLS.oneOf()
    nondet c1 = DECLS.oneOf()
    nondet t0 = TYPES.oneOf()
    nondet t1 = TYPES.oneOf()
    nondet t2 = TYPES.oneOf()
    all {
      decls' = Map((0, 0) -> a0, (0, 1) -> a1, (1, 0) -> b0, (1, 1) -> b1, (2, 0) -> c0, (2, 1) -> c1),
      xtype' = List(t0, t1, t2),
      prog' = List(),
      depth' = 0,
      nextId' = 0,
      rule' = r,
      done' = false,
      expected' = Set(),
      nameDiffers' = false,
    }
  }

  action init = start(Identity)
  // The rule before #1914, for showing that onlyReached is not vacuous.
  action initByName = start(ByName)

  action add(s: Stmt): bool = all {
    not(done),
    length(prog) < MAX_LEN,
    prog' = prog.append(s),
    done' = false,
    expected' = expected,
    nameDiffers' = nameDiffers,
    decls' = decls,
    xtype' = xtype,
    rule' = rule,
  }

  action addCall = {
    nondet i = XS.oneOf()
    nondet m = METHODS.oneOf()
    nondet k = NS.oneOf()
    all { decls.get((xtype[i], m)).declared, add(Call({ x: i, m: m, k: k })), depth' = depth, nextId' = nextId }
  }

  action addDefer = {
    nondet i = XS.oneOf()
    nondet m = METHODS.oneOf()
    nondet k = NS.oneOf()
    all { decls.get((xtype[i], m)).declared, add(DeferCall({ x: i, m: m, k: k })), depth' = depth, nextId' = nextId }
  }

  action addBump = {
    nondet k = NS.oneOf()
    nondet explicit = Set(true, false).oneOf()
    all { add(if (explicit) BumpExplicit(k) else Bump(k)), depth' = depth, nextId' = nextId }
  }

  action openBlock = {
    nondet loop = Set(true, false).oneOf()
    all {
      depth < 2,
      add(if (loop) LoopBegin(nextId) else IfBegin),
      depth' = depth + 1,
      nextId' = nextId + 1,
    }
  }

  action closeBlock = all { depth > 0, add(BlockEnd), depth' = depth - 1, nextId' = nextId }

  action finish = all {
    not(done),
    depth == 0,
    length(prog) > 0,
    done' = true,
    expected' = slotsOf(decls, xtype, rule, prog),
    nameDiffers' = slotsOf(decls, xtype, Identity, prog) != slotsOf(decls, xtype, ByName, prog),
    decls' = decls,
    xtype' = xtype,
    prog' = prog,
    depth' = depth,
    nextId' = nextId,
    rule' = rule,
  }

  // A program that cannot finish (too long with a block open) stops here.
  action stuck = all {
    not(done),
    length(prog) >= MAX_LEN,
    depth > 0,
    decls' = decls, xtype' = xtype, prog' = prog, depth' = depth, nextId' = nextId,
    rule' = rule, done' = done, expected' = expected, nameDiffers' = nameDiffers,
  }

  action halt = all {
    done,
    decls' = decls, xtype' = xtype, prog' = prog, depth' = depth, nextId' = nextId,
    rule' = rule, done' = done, expected' = expected, nameDiffers' = nameDiffers,
  }

  action step = any {
    addCall, addCall, addDefer, addBump, openBlock, closeBlock, finish, stuck, halt,
  }

  // ------------------------------------------------------------ properties

  val soundness: bool = done implies and {
    noLostWrite(decls, xtype, rule, prog),
    onlyReached(decls, xtype, rule, prog),
  }

  // Witnesses: each is expected to be violated by some run (it is reachable).
  val reachedNameDiffers: bool = not(done and nameDiffers)
  val reachedLentReceiver: bool =
    not(done and expected.exists(l => match l { | X(_) => true | N(_) => false }))
}
