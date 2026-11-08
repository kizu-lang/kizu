// -*- mode: Bluespec; -*-
// The `unsafe` marker check as a model (SPEC §12): which operations the
// compiler cannot prove, which `unsafe` marker covers each, and what the
// checker must report when a marker is missing, covers nothing, has no
// `// SAFETY:` comment, or sits in the wrong file for an `unsafe struct`.
//
// A program is two functions over the same frame. `g` comes first and `f` may
// call it; each is a `fn` or an `unsafe fn`, in the file that declares the
// `unsafe struct Buf` (Home) or in a second file of the same module (Away):
//
//   fn g(p: ptr<i64>, b: &var Buf, fu: unsafe fn() -> i64,
//        fe: extern "c" fn() -> i64, fs: fn() -> i64) -> i64 {
//       <statements>
//       return 0;
//   }
//
// The prelude internal/unsafecap/mbt_test.go puts around them declares the
// callees (`ext` / `ev` extern "c", `uf` / `uv` unsafe fn, `sf` / `sv` fn),
// `pid(q: ptr<i64>) -> i64`, `blen(x: Buf) -> i64` and `setn(x: &var i64)`.
//
// The model has two layers. The checker layer (`errorsOf`) follows SPEC §12 the
// way the checker walks a statement: each operation is charged to the
// innermost marker around it, so a marker is "used" or not. The obligation
// layer (`obligationsOf`) says what runs: the operations a call through a
// function pointer, a deferred call or a callee actually performs, each with
// the markers written around it in its own function. The invariants say every
// program the checker layer accepts discharges every obligation it runs.
//
// The constants switch one rule each to a broken variant; `spec` below fixes
// them to SPEC. The mutant modules at the end of the file are the same model
// with one rule broken, so `quint run --invariant soundness` on each must find
// a counterexample (README.md, "不変条件").
module unsafecap {
  // A marker written at a call site also covers the callee's body.
  const INHERIT_CALLER_MARK: bool
  // An `unsafe fn` body is covered as a whole.
  const UNSAFE_FN_BODY_COVERED: bool
  // A function pointer binding takes any function, whatever its type says.
  const LOOSE_FN_TYPES: bool
  // Lending `&var b.n` is not a write to `b.n` (what the checker does: findings F1).
  const BORROW_IS_NOT_WRITE: bool
  // Whether the generator writes `setn(&var b.n)` at all.
  const GEN_FIELD_BORROW: bool

  // The operation kinds of SPEC §12's table, which the diagnostic names in its
  // `help:` line (internal/unsafecap).
  type Cap =
    | PtrRead | PtrWrite | PtrDeref | PtrCast | PtrIntCast | PtrOffset | PtrView
    | ExternCall | UnsafeCall | StructInvariant | Volatile

  // The diagnostic classes, by rule.
  type Kind =
    | Missing(Cap)    // unsafe.missing_marker: an operation no marker covers
    | Unused          // unsafe.unused_marker: a marker that covers no operation
    | NoSafety        // unsafe.missing_safety_comment
    | Boundary        // unsafe.invariant_file_boundary
    | FnTypeMismatch  // a function bound at a function pointer type it is not

  // One i64 operand. The comment is its Kizu.
  type Atom =
    | Lit          // 1
    | SafeCall     // sf()
    | SafeFnPtr    // fs()
    | FieldRead    // b.n                         reading an unsafe struct field is safe
    | Read         // ptr_read(p)
    | Deref        // p.*
    | VolRead      // volatile_read(p)
    | Offset       // pid(ptr_offset(p, 0))
    | Cast         // pid(cast<ptr<i64>>(p))
    | IntCast      // cast<i64>(int_from_ptr<usize>(p))
    | ExtCall      // ext()
    | UfCall       // uf()
    | UnsafeFnPtr  // fu()
    | ExternFnPtr  // fe()
    | Build        // blen(Buf { data: p, n: 1 })
    | CallG        // g(p, b, fu, fe, fs)          only in f

  // An operand and the markers written right before it.
  type Leaf = { m: int, a: Atom }

  type Shape =
    | Single  // M L
    | Sum     // M (L + R)                         with each operand in parentheses
    | Block   // M if true { let t = L; t + R } else { 0 }
  // An expression: its own markers, its shape and its operands. In a Block the
  // two inner statements carry their own `// SAFETY:` (lSafety, rSafety).
  type Expr = { m: int, shape: Shape, l: Leaf, r: Leaf, lSafety: bool, rSafety: bool }

  type FnTy = SafeTy | UnsafeTy | ExternTy  // fn() -> i64 / unsafe fn() -> i64 / extern "c" fn() -> i64
  type FnName = Sf | Uf | Ext               // the function bound to it
  type VoidCall = Ev | Uv | Sv              // ev() / uv() / sv()

  // The form of a statement. `M` is the statement's own markers (`m`).
  type Form =
    | Print        // print(E);
    | PtrWriteS    // M ptr_write(p, E);        the marker covers E too
    | VolWriteS    // M volatile_write(p, E);
    | DerefSet     // M p.* = E;                the marker covers the target only (SPEC §12)
    | FieldSet     // M b.n = E;
    | ViewS        // let v = M view_from_ptr(p, 1); print(v[0]);
    | DerefBorrow  // let r = M &var p.*; print(r.*);
    | DeferS       // defer M <void>();
    | Bind         // let h: <ty> = <name>; print(M h());
    | FieldBorrow  // M setn(&var b.n);
  // One statement. The fields a form does not use hold their defaults (`mkStmt`),
  // so equal statements are equal values. `safety` is the `// SAFETY:` line
  // right before the statement.
  type Stmt = { form: Form, m: int, e: Expr, ty: FnTy, name: FnName, void: VoidCall, safety: bool }

  pure val NO_EXPR: Expr =
    { m: 0, shape: Single, l: { m: 0, a: Lit }, r: { m: 0, a: Lit }, lSafety: false, rSafety: false }

  pure def hasExpr(f: Form): bool = Set(Print, PtrWriteS, VolWriteS, DerefSet, FieldSet).contains(f)

  // A lone operand's markers are all the operand's (`unsafe unsafe x`), and a
  // Single has no right operand.
  pure def normalize(e: Expr): Expr =
    if (e.shape == Single) { ...NO_EXPR, l: { m: e.l.m + e.m, a: e.l.a } } else e

  pure def mkStmt(form: Form, m: int, e: Expr, ty: FnTy, name: FnName, void: VoidCall, safety: bool): Stmt = {
    form: form,
    m: if (form == Print) 0 else m,
    e: if (hasExpr(form)) normalize(e) else NO_EXPR,
    ty: if (form == Bind) ty else SafeTy,
    name: if (form == Bind) name else Sf,
    void: if (form == DeferS) void else Sv,
    safety: safety,
  }

  type FnKind = SafeFn | UnsafeFn
  type File = Home | Away
  type Func = { kind: FnKind, file: File, body: List[Stmt] }
  type Prog = { g: Func, f: Func }

  // ---------------------------------------------------------------------------
  // Where the markers and operations of a statement are.
  //
  // A statement has four marker positions: 0 its operation, 1 its expression,
  // 2 and 3 the two operands. An operation site lists the positions around it,
  // innermost first. `caps` is what the checker charges there; `runs` is what
  // the operation does when it runs, which differs only for a call through a
  // function pointer bound at the wrong type, and for a lent field.

  type Site = { caps: Set[Cap], runs: Set[Cap], invariant: bool, callsG: bool, chain: List[int] }
  type Shape4 = {
    marks: List[int],       // markers at each position
    sites: List[Site],
    outer: Set[int],        // positions in the statement itself, not in a Block's inner statements
    mismatch: bool,
  }

  pure def one(c: Cap): Set[Cap] = Set(c)

  pure def lexists(l: List[a], pred: a => bool): bool = l.foldl(false, (seen, x) => seen or pred(x))

  pure def atomCaps(gKind: FnKind, a: Atom): Set[Cap] = match a {
    | Read => one(PtrRead)
    | Deref => one(PtrDeref)
    | VolRead => one(Volatile)
    | Offset => one(PtrOffset)
    | Cast => one(PtrCast)
    | IntCast => one(PtrIntCast)
    | ExtCall => one(ExternCall)
    | UfCall => one(UnsafeCall)
    | UnsafeFnPtr => one(UnsafeCall)
    | ExternFnPtr => one(ExternCall)
    | Build => one(StructInvariant)
    | CallG => if (gKind == UnsafeFn) one(UnsafeCall) else Set()
    | _ => Set()
  }

  pure def atomSite(gKind: FnKind, a: Atom, chain: List[int]): Site = {
    val c = atomCaps(gKind, a)
    { caps: c, runs: c, invariant: a == Build, callsG: a == CallG, chain: chain }
  }

  pure def opSite(c: Set[Cap], invariant: bool): Site =
    { caps: c, runs: c, invariant: invariant, callsG: false, chain: List(0) }

  // The operand sites of an expression. `stmtCovers` says whether the
  // statement's own markers (position 0) are around the expression.
  pure def exprSites(gKind: FnKind, e: Expr, stmtCovers: bool): List[Site] = {
    val around = if (stmtCovers) List(1, 0) else List(1)
    val left = atomSite(gKind, e.l.a, List(2).concat(around))
    val right = atomSite(gKind, e.r.a, List(3).concat(around))
    if (e.shape == Single) List(left) else List(left, right)
  }

  pure def exprOuter(e: Expr): Set[int] =
    if (e.shape == Block) Set(0, 1) else Set(0, 1, 2, 3)

  pure def fnTyCaps(t: FnTy): Set[Cap] = match t {
    | SafeTy => Set()
    | UnsafeTy => one(UnsafeCall)
    | ExternTy => one(ExternCall)
  }

  pure def nameTy(n: FnName): FnTy = match n {
    | Sf => SafeTy
    | Uf => UnsafeTy
    | Ext => ExternTy
  }

  pure def voidCaps(v: VoidCall): Set[Cap] = match v {
    | Ev => one(ExternCall)
    | Uv => one(UnsafeCall)
    | Sv => Set()
  }

  pure def noMarks(m0: int): List[int] = List(m0, 0, 0, 0)
  pure def exprMarks(m0: int, e: Expr): List[int] =
    List(m0, e.m, e.l.m, if (e.shape == Single) 0 else e.r.m)

  pure def withExpr(m0: int, top: Site, e: Expr, gKind: FnKind, covers: bool): Shape4 = {
    marks: exprMarks(m0, e),
    sites: List(top).concat(exprSites(gKind, e, covers)),
    outer: exprOuter(e),
    mismatch: false,
  }

  pure def plainShape(m: int, site: Site): Shape4 =
    { marks: noMarks(m), sites: List(site), outer: Set(0), mismatch: false }

  pure def layout(gKind: FnKind, s: Stmt): Shape4 = match s.form {
    | Print => {
        marks: exprMarks(0, s.e), sites: exprSites(gKind, s.e, false), outer: exprOuter(s.e), mismatch: false,
      }
    | PtrWriteS => withExpr(s.m, opSite(one(PtrWrite), false), s.e, gKind, true)
    | VolWriteS => withExpr(s.m, opSite(one(Volatile), false), s.e, gKind, true)
    | DerefSet => withExpr(s.m, opSite(one(PtrDeref), false), s.e, gKind, false)
    | FieldSet => withExpr(s.m, opSite(one(StructInvariant), true), s.e, gKind, false)
    | ViewS => plainShape(s.m, opSite(one(PtrView), false))
    | DerefBorrow => plainShape(s.m, opSite(one(PtrDeref), false))
    | DeferS => plainShape(s.m, opSite(voidCaps(s.void), false))
    | Bind => {
        marks: noMarks(s.m),
        // The checker reads the declared type; what runs is the function bound.
        sites: List({
          caps: fnTyCaps(s.ty), runs: fnTyCaps(nameTy(s.name)), invariant: false, callsG: false, chain: List(0),
        }),
        outer: Set(0),
        mismatch: s.ty != nameTy(s.name),
      }
    | FieldBorrow => plainShape(s.m, {
        // Lending `&var b.n` to `setn` writes the field: SPEC §12 puts every
        // field write of an unsafe struct under `unsafe`.
        caps: if (BORROW_IS_NOT_WRITE) Set() else one(StructInvariant),
        runs: one(StructInvariant),
        invariant: true,
        callsG: false,
        chain: List(0),
      })
  }

  // The first position along the chain that has a marker, or -1.
  pure def innermost(marks: List[int], chain: List[int]): int =
    chain.foldl(-1, (found, p) => if (found >= 0) found else if (marks[p] > 0) p else -1)

  pure def enclosed(marks: List[int], chain: List[int]): bool =
    chain.foldl(false, (seen, p) => seen or marks[p] > 0)

  // ---------------------------------------------------------------------------
  // The checker layer.

  // Where a function is checked: its kind and file, and whether something
  // outside the body covers it (only the broken variants say so).
  type Ctx = { gKind: FnKind, file: File, covered: bool }

  pure def stmtErrors(ctx: Ctx, s: Stmt): Set[Kind] = {
    val sh = layout(ctx.gKind, s)
    val charged = sh.sites.select(site => site.caps != Set())
    // SPEC §12: an operation the compiler cannot prove needs a marker around it.
    val missing = charged
      .select(site => innermost(sh.marks, site.chain) < 0 and not(ctx.covered))
      .foldl(Set(), (acc, site) => acc.union(site.caps.map(c => Missing(c))))
    // A use is charged to the innermost marker; a marker nothing is charged to
    // covers nothing. Of two markers in a row, the outer one never is.
    val used = charged.foldl(Set(), (acc, site) => acc.union(Set(innermost(sh.marks, site.chain))))
    val unused = 0.to(3).exists(p => sh.marks[p] >= 2 or (sh.marks[p] == 1 and not(used.contains(p))))
    // A statement with a marker needs `// SAFETY:`; a Block's inner statements
    // need their own (an outer comment does not reach an inner statement).
    val outerMarked = sh.outer.exists(p => sh.marks[p] > 0)
    val nested = s.e
    val nestedUnsafe = nested.shape == Block and (
      (sh.marks[2] > 0 and not(nested.lSafety)) or (sh.marks[3] > 0 and not(nested.rSafety))
    )
    val noSafety = (outerMarked and not(s.safety)) or nestedUnsafe
    // An unsafe struct is constructed and written only in its own file, marker or not.
    val boundary = ctx.file == Away and lexists(sh.sites, site => site.invariant and site.caps != Set())
    Set()
      .union(missing)
      .union(if (unused) Set(Unused) else Set())
      .union(if (noSafety) Set(NoSafety) else Set())
      .union(if (boundary) Set(Boundary) else Set())
      .union(if (sh.mismatch and not(LOOSE_FN_TYPES)) Set(FnTypeMismatch) else Set())
  }

  pure def bodyErrors(ctx: Ctx, body: List[Stmt]): Set[Kind] =
    body.foldl(Set(), (acc, s) => acc.union(stmtErrors(ctx, s)))

  // Whether f calls g somewhere a marker covers.
  pure def marksCallOfG(p: Prog): bool =
    p.f.body.foldl(false, (seen, s) => seen or {
      val sh = layout(p.g.kind, s)
      sh.sites.foldl(false, (a, site) => a or (site.callsG and enclosed(sh.marks, site.chain)))
    })

  pure def ctxOf(p: Prog, fn: Func, isG: bool): Ctx = {
    gKind: p.g.kind,
    file: fn.file,
    covered: (UNSAFE_FN_BODY_COVERED and fn.kind == UnsafeFn)
      or (INHERIT_CALLER_MARK and isG and marksCallOfG(p)),
  }

  pure def errorsOf(p: Prog): Set[Kind] =
    bodyErrors(ctxOf(p, p.g, true), p.g.body).union(bodyErrors(ctxOf(p, p.f, false), p.f.body))

  // ---------------------------------------------------------------------------
  // The obligation layer: what runs, and whether a marker written in the same
  // function is around it. This is said without "innermost": any marker of the
  // operation's own function discharges it, and nothing outside does.

  type Obligation = { fn: str, runs: Set[Cap], invariant: bool, discharged: bool, file: File, viaPointer: bool }

  pure def obligationsOfBody(name: str, gKind: FnKind, fn: Func): Set[Obligation] =
    fn.body.foldl(Set(), (acc, s) => {
      val sh = layout(gKind, s)
      val isBind = s.form == Bind
      acc.union(sh.sites.select(site => site.runs != Set()).foldl(Set(), (os, site) => os.union(Set({
        fn: name,
        runs: site.runs,
        invariant: site.invariant,
        discharged: enclosed(sh.marks, site.chain),
        file: fn.file,
        viaPointer: isBind,
      }))))
    })

  pure def obligationsOf(p: Prog): Set[Obligation] =
    obligationsOfBody("g", p.g.kind, p.g).union(obligationsOfBody("f", p.g.kind, p.f))

  // ---------------------------------------------------------------------------
  // The generator.

  pure val MAX_G = 2
  pure val MAX_F = 3

  var prog: Prog
  var building: str           // "g", then "f"
  var budget: int             // how many statements may break a rule the program kept so far
  var mistakes: int
  var done: bool
  var expected: Set[Kind]

  pure val ATOMS_F = Set(Lit, SafeCall, SafeFnPtr, FieldRead, Read, Deref, VolRead, Offset, Cast,
    IntCast, ExtCall, UfCall, UnsafeFnPtr, ExternFnPtr, Build, CallG)
  pure val ATOMS_G = ATOMS_F.exclude(Set(CallG))
  pure val MARKS = Set(0, 1, 2)
  pure val BOOLS = Set(false, true)

  // A statement with every marker where it is needed, a `// SAFETY:` on every
  // statement that has one, and nothing a file or a type refuses. `outerStyle`
  // picks between one marker over the expression and one per operand.
  pure def repair(ctx: Ctx, s: Stmt, outerStyle: bool): Stmt = {
    val away = ctx.file == Away
    pure def needs(a: Atom): bool = atomCaps(ctx.gKind, a) != Set()
    pure def fixAtom(a: Atom): Atom = if (away and a == Build) FieldRead else a
    pure def fixExpr(e: Expr, covered: bool): Expr = {
      val l = fixAtom(e.l.a)
      val r = if (e.shape == Single) Lit else fixAtom(e.r.a)
      val needed = needs(l) or needs(r)
      val outer = not(covered) and outerStyle and needed
      val lm = if (not(covered) and not(outer) and needs(l)) 1 else 0
      val rm = if (not(covered) and not(outer) and needs(r)) 1 else 0
      {
        m: if (outer) 1 else 0,
        shape: e.shape,
        l: { m: lm, a: l },
        r: { m: rm, a: r },
        lSafety: e.shape == Block and lm > 0,
        rSafety: e.shape == Block and rm > 0,
      }
    }
    val fixedExpr = fixExpr(s.e, s.form == PtrWriteS or s.form == VolWriteS)
    val form = if (away and s.form == FieldSet) Print
      else if (away and s.form == FieldBorrow) DerefBorrow
      else s.form
    val m = match form {
      | DeferS => if (voidCaps(s.void) != Set()) 1 else 0
      | Bind => if (fnTyCaps(nameTy(s.name)) != Set()) 1 else 0
      | _ => 1
    }
    val unmarked = mkStmt(form, m, fixedExpr, nameTy(s.name), s.name, s.void, false)
    val sh = layout(ctx.gKind, unmarked)
    { ...unmarked, safety: sh.outer.exists(p => sh.marks[p] > 0) }
  }

  def current: Func = if (building == "g") prog.g else prog.f
  def currentCtx: Ctx = ctxOf(prog, current, building == "g")

  def withStmt(s: Stmt): Prog =
    if (building == "g") { ...prog, g: { ...prog.g, body: prog.g.body.append(s) } }
    else { ...prog, f: { ...prog.f, body: prog.f.body.append(s) } }

  // A repaired statement with the markers at one position taken away, or with
  // its `// SAFETY:` dropped (`pos` 4). A raw draw breaks several rules at once
  // more often than not, and a checker that reports the first of them can hide
  // a rule it skips behind the others; a slip breaks one.
  pure def slip(s: Stmt, pos: int): Stmt =
    if (pos == 0) { ...s, m: 0 }
    else if (pos == 1) { ...s, e: { ...s.e, m: 0 } }
    else if (pos == 2) { ...s, e: { ...s.e, l: { ...s.e.l, m: 0 }, lSafety: false } }
    else if (pos == 3) { ...s, e: { ...s.e, r: { ...s.e.r, m: 0 }, rSafety: false } }
    else if (pos == 4) { ...s, safety: false }
    else s

  // Adds the drawn statement, or its repair when the drawn one breaks a new
  // rule and the budget is spent (or `raw` is false). With budget left, a
  // repair may come with one slip (`slipAt` >= 0).
  action add(s: Stmt, raw: bool, outerStyle: bool, slipAt: int): bool = all {
    not(done),
    length(current.body) < (if (building == "g") MAX_G else MAX_F),
    val fixed = repair(currentCtx, s, outerStyle)
    val before = errorsOf(prog)
    def breaks(t: Stmt): bool = not(errorsOf(withStmt(t)).subseteq(before))
    val take = raw and (not(breaks(s)) or mistakes < budget)
    val slipped = not(take) and slipAt >= 0 and mistakes < budget
    val chosen = if (take) s else if (slipped) slip(fixed, slipAt) else fixed
    all {
      prog' = withStmt(chosen),
      mistakes' = if ((take or slipped) and breaks(chosen)) mistakes + 1 else mistakes,
    },
    building' = building,
    budget' = budget,
    done' = done,
    expected' = expected,
  }

  action switchToF: bool = all {
    not(done),
    building == "g",
    building' = "f",
    prog' = prog, budget' = budget, mistakes' = mistakes, done' = done, expected' = expected,
  }

  action finish: bool = all {
    not(done),
    building == "f",
    length(prog.f.body) > 0,
    done' = true,
    expected' = errorsOf(prog),
    prog' = prog, building' = building, budget' = budget, mistakes' = mistakes,
  }

  action stutter: bool = all {
    done,
    prog' = prog, building' = building, budget' = budget, mistakes' = mistakes,
    done' = done, expected' = expected,
  }

  action start(rules: int): bool = all {
    nondet gk = Set(SafeFn, UnsafeFn).oneOf()
    nondet fk = Set(SafeFn, UnsafeFn).oneOf()
    nondet gf = Set(Home, Away).oneOf()
    nondet ff = Set(Home, Away).oneOf()
    prog' = { g: { kind: gk, file: gf, body: List() }, f: { kind: fk, file: ff, body: List() } },
    building' = "g",
    budget' = rules,
    mistakes' = 0,
    done' = false,
    expected' = Set(),
  }

  action init = nondet b = Set(0, 1, 9).oneOf() start(b)
  action initGuided = start(0)
  action initOneMistake = start(1)
  action initFree = start(9)

  action step = {
    val atoms = if (building == "g") ATOMS_G else ATOMS_F
    nondet la = atoms.oneOf()
    nondet ra = atoms.oneOf()
    nondet m0 = MARKS.oneOf()
    nondet em = MARKS.oneOf()
    nondet lm = MARKS.oneOf()
    nondet rm = MARKS.oneOf()
    nondet shape = Set(Single, Sum, Block).oneOf()
    nondet ls = BOOLS.oneOf()
    nondet rs = BOOLS.oneOf()
    nondet safety = BOOLS.oneOf()
    nondet raw = BOOLS.oneOf()
    nondet outerStyle = BOOLS.oneOf()
    nondet slipAt = Set(-1, 0, 1, 2, 3, 4).oneOf()
    nondet ty = Set(SafeTy, UnsafeTy, ExternTy).oneOf()
    nondet name = Set(Sf, Uf, Ext).oneOf()
    nondet void = Set(Ev, Uv, Sv).oneOf()
    val e = {
      m: em,
      shape: shape,
      l: { m: lm, a: la },
      r: { m: rm, a: ra },
      lSafety: shape == Block and ls,
      rSafety: shape == Block and rs,
    }
    val forms = Set(Print, PtrWriteS, VolWriteS, DerefSet, FieldSet, ViewS, DerefBorrow, DeferS, Bind)
      .union(if (GEN_FIELD_BORROW) Set(FieldBorrow) else Set())
    nondet form = forms.oneOf()
    // `let v = unsafe view_from_ptr(..)`, `let r = unsafe &var p.*` and
    // `defer unsafe f()` are forms the checker reads with one marker; two in a
    // row are a form error there, not an unused marker (findings F3).
    val formMarks = if (Set(ViewS, DerefBorrow, DeferS).contains(form) and m0 > 1) 1 else m0
    any {
      add(mkStmt(form, formMarks, e, ty, name, void, safety), raw, outerStyle, slipAt),
      switchToF,
      finish,
      stutter,
    }
  }

  // ---------------------------------------------------------------------------
  // Properties.

  def accepted: bool = done and expected == Set()
  def obligations: Set[Obligation] = obligationsOf(prog)

  // Every operation the compiler cannot prove has a marker of its own function around it.
  val noUncoveredOp: bool = accepted implies obligations.forall(o => o.discharged)
  // A marker at a call does not cover what the callee does: g discharges its own.
  val noCalleeLeak: bool = accepted implies obligations.forall(o => o.fn == "g" implies o.discharged)
  // A call through a function pointer is covered as what it calls, not as what
  // the pointer type claims.
  val noLaundering: bool = accepted implies obligations.forall(o => o.viaPointer implies o.discharged)
  // An unsafe struct's invariant is changed only under a marker, in its own file.
  val invariantAudited: bool = accepted implies
    obligations.forall(o => o.invariant implies (o.discharged and o.file == Home))

  val soundness: bool = and { noUncoveredOp, noCalleeLeak, noLaundering, invariantAudited }

  // Witnesses: `quint run --witnesses ...` counts the runs that reach each,
  // so the generator is seen to reach accepted programs of each kind and each
  // diagnostic class.
  def everywhere(pred: Stmt => bool): bool =
    lexists(prog.g.body, pred) or lexists(prog.f.body, pred)
  val reachedAccept: bool = accepted
  val reachedAcceptUnsafeFn: bool = accepted and prog.f.kind == UnsafeFn and prog.f.body.length() >= 2
  val reachedAcceptBlock: bool = accepted and everywhere(s => s.form == Print and s.e.shape == Block and s.e.m > 0)
  val reachedAcceptAway: bool = accepted and prog.f.file == Away and prog.g.file == Away
  val reachedUnused: bool = done and expected == Set(Unused)
  val reachedBoundary: bool = done and expected.contains(Boundary)
  val reachedMismatch: bool = done and expected == Set(FnTypeMismatch)
  val reachedNoSafety: bool = done and expected == Set(NoSafety)
}

// The model as SPEC states it.
module spec {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = false,
  ).*
}

// SPEC plus `setn(&var b.n)` in the generator: what a fixed checker would agree with.
module specBorrow {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = true,
  ).*
}

// Each mutant breaks one rule; `soundness` must fail on each.
module mutantInherit {
  import unsafecap(
    INHERIT_CALLER_MARK = true,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = false,
  ).*
}

module mutantUnsafeFnBody {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = true,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = false,
  ).*
}

module mutantLooseFnTypes {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = true,
    BORROW_IS_NOT_WRITE = false,
    GEN_FIELD_BORROW = false,
  ).*
}

// The checker as it is: lending `&var b.n` is not charged as a write.
module asImplemented {
  import unsafecap(
    INHERIT_CALLER_MARK = false,
    UNSAFE_FN_BODY_COVERED = false,
    LOOSE_FN_TYPES = false,
    BORROW_IS_NOT_WRITE = true,
    GEN_FIELD_BORROW = true,
  ).*
}
