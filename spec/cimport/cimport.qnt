// -*- mode: Bluespec; -*-
// The C header import (SPEC §12.2, `kizu import-c-header`) and the check of
// what it writes, as a model: which C declarations the importer converts, to
// which `extern "c" fn`, what the checker then says about those declarations,
// and what it says about a Kizu function that calls the first of them.
//
// A program is a header of one or two declarations and an optional caller:
//
//   <decl 1>;  [<decl 2>;]                       the header
//   fn use(a1: T1, .., an: Tn) -> R {            the caller, rendered by
//       // SAFETY: ..                            internal/cimport/mbt_test.go
//       return unsafe f(a1, .., an);
//   }
//
// The model has two layers.
//
// The importer layer (`importOf`) works on the words of the header the way the
// importer does: a parameter's last word is its name unless it is a `*`, the
// remaining words map through the SPEC §12.2 table, and the policy `POL` says
// how qualifiers and pointer results map. Its result, put to the checker layer
// (`checkErrs`, `useErrs`), is the set of diagnostic classes the pipeline may
// report, or the declarations it writes when it reports none.
//
// The truth layer (`truthOf`) reads the C type off the declaration's structure,
// not its words: which object each `const` and `volatile` qualifies, and that
// a C pointer result may be NULL. The invariants say every program the
// importer and checker accept writes declarations true to the C types, and
// that the caller cannot use them to write through a read-only pointer, to
// take a NULL result as non-null, or to call C with no marker.
//
// `spec` below fixes POL to the faithful mapping; `asImplemented` is the
// importer as it is (findings/cimport-quint-findings.md), and the mutants each
// break one rule, so `quint run --invariant soundness` must find a
// counterexample on each (README.md, "不変条件").
module cimport {
  type Policy = {
    // Any `const` in a parameter's type makes the innermost pointee const, and
    // no other level (the importer: findings F1).
    constCollapse: bool,
    // `volatile` is dropped wherever it is (the importer: findings F3).
    dropVolatile: bool,
    // A pointer result maps to a non-null `ptr<T>` (the importer: findings F2).
    retNonNull: bool,
    // A call of an `extern "c" fn` needs no marker (mutant).
    unmarkedCall: bool,
    // The table maps `unsigned int` to `i32` (mutant).
    signLoose: bool,
  }
  const POL: Policy

  // ---------------------------------------------------------------------------
  // The header.

  // A qualifier set written after a `*` (or before / after the base type).
  type Q = { c: bool, v: bool, r: bool }
  // A C type as written: the base type's words, its qualifiers and whether they
  // come after it (`int const`), and the qualifiers after each `*`, left to right.
  type CTy = { base: List[str], bc: bool, bv: bool, after: bool, stars: List[Q] }
  // One parameter. `name` "" is an unnamed one.
  //   typed     <ty> <name>
  //   array     <ty> <name>[8]
  //   ellipsis  ...
  //   fnptr     int (*cb)(int)
  //   empty     (nothing between two commas)
  type Param = { form: str, ty: CTy, name: str }
  // One declaration. kind:
  //   proto    <ret> <name>(<params>)      plist "void" / "empty" / "some"
  //   define   #define X 1                 no `;`, so it runs into the next declaration
  //   typedef  typedef int my_int
  //   struct   struct point { int x; }
  //   enum     enum color { RED }
  //   var      int counter
  // `glue` writes a pointer result's last `*` against the name (`int *f(..)`).
  type Decl = { kind: str, name: str, ret: CTy, glue: bool, plist: str, params: List[Param] }

  pure val NOQ: Q = { c: false, v: false, r: false }
  pure val INT: CTy = { base: List("int"), bc: false, bv: false, after: false, stars: List() }
  pure val NO_PARAM: Param = { form: "typed", ty: INT, name: "x" }
  pure val NO_DECL: Decl = { kind: "proto", name: "f", ret: INT, glue: false, plist: "void", params: List() }

  // The TypeScript simulator does not re-enter a definition that takes an
  // operator: a call of one of these inside the operator of another call of
  // the same one sees the outer operator. Nested walks use `foldl` directly.
  pure def lexists(l: List[a], pred: a => bool): bool = l.foldl(false, (leSeen, leX) => leSeen or pred(leX))
  pure def lmap(l: List[a], f: a => b): List[b] = l.foldl(List(), (lmAcc, lmX) => lmAcc.append(f(lmX)))
  pure def flat(l: List[List[a]]): List[a] = l.foldl(List(), (flAcc, flX) => flAcc.concat(flX))
  pure def count(l: List[str], w: str): int = l.foldl(0, (cnN, cnX) => if (cnX == w) cnN + 1 else cnN)

  // ---------------------------------------------------------------------------
  // The Kizu side.

  // A Kizu type: the base, a const flag per pointee level innermost first (empty
  // for a scalar), and whether the outermost pointer may be null. Base "slice"
  // is `[]u8`, which only a caller writes.
  type KTy = { base: str, consts: List[bool], nullable: bool }
  type KParam = { name: str, ty: KTy }
  type KDecl = { name: str, params: List[KParam], ret: KTy }

  pure def depth(t: KTy): int = t.consts.length()

  // The SPEC §12.2 table of C spellings the importer knows.
  pure val TABLE: str -> str = Map(
    "void" -> "void", "char" -> "i8", "signed char" -> "i8", "unsigned char" -> "u8",
    "short" -> "i16", "unsigned short" -> "u16", "int" -> "i32", "unsigned int" -> "u32",
    "long long" -> "i64", "unsigned long long" -> "u64",
    "int8_t" -> "i8", "uint8_t" -> "u8", "int16_t" -> "i16", "uint16_t" -> "u16",
    "int32_t" -> "i32", "uint32_t" -> "u32", "int64_t" -> "i64", "uint64_t" -> "u64",
    "size_t" -> "usize", "intptr_t" -> "isize", "float" -> "f32", "double" -> "f64",
  )

  // What each base spelling the generator writes is, as a C type on the target
  // (LP64, `char` signed): the truth the table is held to. "" is not a type.
  pure val TRUTH: str -> str = Map(
    "void" -> "void", "char" -> "i8", "signed char" -> "i8", "unsigned char" -> "u8",
    "short" -> "i16", "unsigned short" -> "u16", "int" -> "i32", "unsigned int" -> "u32",
    "long long" -> "i64", "unsigned long long" -> "u64",
    "int8_t" -> "i8", "uint8_t" -> "u8", "int16_t" -> "i16", "uint16_t" -> "u16",
    "int32_t" -> "i32", "uint32_t" -> "u32", "int64_t" -> "i64", "uint64_t" -> "u64",
    "size_t" -> "usize", "intptr_t" -> "isize", "float" -> "f32", "double" -> "f64",
    "long" -> "i64", "_Bool" -> "bool", "uintptr_t" -> "usize", "short int" -> "i16",
    "widget" -> "",
  )

  // Spellings as word lists: Quint strings do not concatenate, so a spelling
  // is a list and `spellOf` names it for the tables.
  pure def spellOf(w: List[str]): str =
    if (w == List("void")) "void" else if (w == List("char")) "char"
    else if (w == List("signed", "char")) "signed char" else if (w == List("unsigned", "char")) "unsigned char"
    else if (w == List("short")) "short" else if (w == List("unsigned", "short")) "unsigned short"
    else if (w == List("int")) "int" else if (w == List("unsigned", "int")) "unsigned int"
    else if (w == List("long", "long")) "long long" else if (w == List("unsigned", "long", "long")) "unsigned long long"
    else if (w == List("int8_t")) "int8_t" else if (w == List("uint8_t")) "uint8_t"
    else if (w == List("int16_t")) "int16_t" else if (w == List("uint16_t")) "uint16_t"
    else if (w == List("int32_t")) "int32_t" else if (w == List("uint32_t")) "uint32_t"
    else if (w == List("int64_t")) "int64_t" else if (w == List("uint64_t")) "uint64_t"
    else if (w == List("size_t")) "size_t" else if (w == List("intptr_t")) "intptr_t"
    else if (w == List("float")) "float" else if (w == List("double")) "double"
    else if (w == List("long")) "long" else if (w == List("_Bool")) "_Bool"
    else if (w == List("uintptr_t")) "uintptr_t" else if (w == List("short", "int")) "short int"
    else if (w == List("widget")) "widget" else ""

  // The importer's lookup: the table under the policy, "" for no entry.
  pure def lookup(pol: Policy, w: List[str]): str = {
    val s = spellOf(w)
    if (pol.signLoose and s == "unsigned int") "i32"
    else if (TABLE.keys().contains(s)) TABLE.get(s) else ""
  }

  // The parser's keywords among the names the generator writes.
  pure val KEYWORDS: Set[str] = Set("fn", "match")
  pure val QUALS: Set[str] = Set("const", "volatile", "restrict")
  pure val AUTO: List[str] = List("p1", "p2", "p3")

  // ---------------------------------------------------------------------------
  // Words: the header as the importer splits it.

  pure def qwords(c: bool, v: bool, r: bool): List[str] =
    (if (c) List("const") else List()).concat(if (v) List("volatile") else List())
      .concat(if (r) List("restrict") else List())

  pure def tyWords(t: CTy): List[str] = {
    val q = qwords(t.bc, t.bv, false)
    val base = if (t.after) t.base.concat(q) else q.concat(t.base)
    base.concat(flat(lmap(t.stars, s => List("*").concat(qwords(s.c, s.v, s.r)))))
  }

  pure def paramWords(p: Param): List[str] =
    tyWords(p.ty).concat(if (p.name == "") List() else List(p.name))

  // ---------------------------------------------------------------------------
  // The importer layer.

  // The pointee qualifiers of a type's words, innermost first: the words before
  // the first `*`, then the words after each `*` but the last (the pointer
  // itself, passed by value).
  pure def segments(w: List[str]): List[List[str]] =
    w.foldl(List(List()), (acc, x) =>
      if (x == "*") acc.append(List())
      else acc.replaceAt(acc.length() - 1, acc[acc.length() - 1].append(x)))

  type Mapped = { err: str, ty: KTy }

  // One type's words to a Kizu type, or the class of the error.
  pure def mapWords(pol: Policy, w: List[str], isRet: bool): Mapped = {
    val n = count(w, "*")
    val kept = w.select(x => x != "*" and not(QUALS.contains(x)))
    val base = lookup(pol, kept)
    val segs = segments(w)
    val pointees = segs.slice(0, n)
    val consts =
      if (pol.constCollapse) range(0, n).foldl(List(), (acc, i) => acc.append(i == 0 and anyConst(w)))
      else lmap(pointees, s => s.foldl(false, (seen, x) => seen or x == "const"))
    val volatilePointee = lexists(pointees, s => s.foldl(false, (seen, x) => seen or x == "volatile"))
    if (base == "") { err: "Import:type", ty: { base: "", consts: List(), nullable: false } }
    else if (not(pol.dropVolatile) and volatilePointee) { err: "Import:volatile", ty: { base: "", consts: List(), nullable: false } }
    else { err: "", ty: { base: base, consts: consts, nullable: isRet and n > 0 and not(pol.retNonNull) } }
  }

  pure def anyConst(w: List[str]): bool = w.foldl(false, (seen, x) => seen or x == "const")

  type PResult = { errs: Set[str], param: KParam }

  // One parameter. The last word names it unless it is a `*`; then it is the
  // generated `p<i>`.
  pure def importParam(pol: Policy, p: Param, i: int): PResult = {
    val none = { name: "", ty: { base: "", consts: List(), nullable: false } }
    if (p.form == "empty") { errs: Set("Import:empty_param"), param: none }
    else if (p.form == "array") { errs: Set("Import:array"), param: none }
    else if (p.form != "typed") { errs: Set(), param: none }
    else {
      val w = paramWords(p)
      val last = w[w.length() - 1]
      val named = last != "*"
      val name = if (named) last else AUTO[i]
      val tw = if (named) w.slice(0, w.length() - 1) else w
      val m = mapWords(pol, tw, false)
      { errs: if (m.err == "") Set() else Set(m.err), param: { name: name, ty: m.ty } }
    }
  }

  // The parameter list, `void` and `()` both empty. A lone unnamed `void`
  // written as a parameter is the same `(void)`, and a lone empty parameter
  // the same `()`: the importer looks at the text between the parentheses.
  pure def effectiveParams(d: Decl): List[Param] =
    if (d.plist != "some") List()
    else if (d.params == List({ form: "typed", ty: { ...INT, base: List("void") }, name: "" })) List()
    else if (d.params.length() == 1 and d.params[0].form == "empty") List()
    else d.params

  type DResult = { errs: Set[str], decl: KDecl }

  // One declaration statement: every class it can be rejected with (which one
  // the importer names first is not SPEC's to say), or what it writes.
  pure def importDecl(pol: Policy, d: Decl): DResult = {
    val noRet = { base: "", consts: List(), nullable: false }
    if (d.kind == "define") { errs: Set("Import:preprocessor"), decl: { name: "", params: List(), ret: noRet } }
    else if (d.kind == "typedef") { errs: Set("Import:typedef"), decl: { name: "", params: List(), ret: noRet } }
    else if (d.kind == "struct") { errs: Set("Import:struct"), decl: { name: "", params: List(), ret: noRet } }
    else if (d.kind == "enum") { errs: Set("Import:enum"), decl: { name: "", params: List(), ret: noRet } }
    else if (d.kind == "var") { errs: Set("Import:declaration"), decl: { name: "", params: List(), ret: noRet } }
    else {
      val ps = effectiveParams(d)
      val stmtErrs =
        (if (lexists(ps, p => p.form == "ellipsis")) Set("Import:variadic") else Set())
          .union(if (lexists(ps, p => p.form == "fnptr")) Set("Import:fnptr") else Set())
      val stars = d.ret.stars
      val glued = d.glue and stars.length() > 0 and stars[stars.length() - 1] == NOQ
      val ret = mapWords(pol, tyWords(d.ret), true)
      val results = range(0, ps.length()).foldl(List(), (acc, i) => acc.append(importParam(pol, ps[i], i)))
      val paramErrs = results.foldl(Set(), (acc, r) => acc.union(r.errs))
      val errs =
        if (stmtErrs != Set()) stmtErrs
        else if (glued) Set("Import:declaration")
        else (if (ret.err == "") Set() else Set(ret.err)).union(paramErrs)
      { errs: errs, decl: { name: d.name, params: results.foldl(List(), (acc, r) => acc.append(r.param)), ret: ret.ty } }
    }
  }

  // The statements of a header: a `#define` has no `;` of its own, so it and
  // the declaration after it are one statement, rejected as the directive.
  pure def statements(h: List[Decl]): List[Decl] =
    if (h.length() == 2 and h[0].kind == "define") List(h[0]) else h

  type IResult = { errs: Set[str], decls: List[KDecl] }

  // The header: the first rejected statement ends the import.
  pure def importOf(pol: Policy, h: List[Decl]): IResult = {
    val rs = statements(h).foldl(List(), (rsAcc, d) => rsAcc.append(importDecl(pol, d)))
    val failed = rs.select(r => r.errs != Set())
    if (failed.length() > 0) { errs: failed[0].errs, decls: List() }
    else { errs: Set(), decls: rs.foldl(List(), (dsAcc, r) => dsAcc.append(r.decl)) }
  }

  // ---------------------------------------------------------------------------
  // The checker layer: what `kizu check` says about the written declarations.

  pure def checkErrs(ds: List[KDecl]): Set[str] = {
    val names = ds.foldl(List(), (nmAcc, d) => nmAcc.append(d.name))
    val parse = ds.foldl(false, (seen, d) => seen or KEYWORDS.contains(d.name) or lexists(d.params, p => KEYWORDS.contains(p.name)))
    val voidParam = ds.foldl(false, (seen, d) => seen or lexists(d.params, p => p.ty.base == "void" and depth(p.ty) == 0))
    val dup = ds.length() == 2 and names[0] == names[1]
    val e1: Set[str] = if (parse) Set("Check:parse") else Set()
    e1.union(if (voidParam) Set("Check:void_param") else Set())
      .union(if (dup) Set("Check:duplicate") else Set())
  }

  // ---------------------------------------------------------------------------
  // The truth layer: the C type a declaration's structure names.

  type Truth = { known: bool, ty: KTy, volatilePointee: bool }

  pure def truthOf(t: CTy, isRet: bool): Truth = {
    val n = t.stars.length()
    val pointeeQs = List({ c: t.bc, v: t.bv, r: false }).concat(t.stars).slice(0, n)
    val s = spellOf(t.base)
    val base = if (TRUTH.keys().contains(s)) TRUTH.get(s) else ""
    {
      known: base != "",
      ty: { base: base, consts: lmap(pointeeQs, q => q.c), nullable: isRet and n > 0 },
      volatilePointee: lexists(pointeeQs, q => q.v),
    }
  }

  // ---------------------------------------------------------------------------
  // The caller.

  // How the caller types each argument, from the C type as a reader of the
  // header sees it:
  //   faithful  the C type, non-null
  //   readonly  the same pointer with a const pointee (read-only memory)
  //   nullable  the same pointer, maybe null
  //   wide      a scalar of another width (`i64`, or `u8` for an `i64`)
  //   slice     `[]u8` for a pointer
  // and its result: "faithful" (a scalar or `void`), "nonnull" or "nullable" (a pointer).
  type Use = { call: bool, marked: bool, args: List[str], ret: str }
  pure val NO_USE: Use = { call: false, marked: true, args: List(), ret: "faithful" }

  pure def argTy(t: CTy, sel: str): KTy = {
    val f = truthOf(t, false).ty
    if (sel == "readonly" and depth(f) > 0) { ...f, consts: f.consts.replaceAt(depth(f) - 1, true) }
    else if (sel == "nullable" and depth(f) > 0) { ...f, nullable: true }
    else if (sel == "wide" and depth(f) == 0) { ...f, base: if (f.base == "i64") "u8" else "i64" }
    else if (sel == "slice" and depth(f) > 0) { base: "slice", consts: List(), nullable: false }
    else f
  }

  pure def retTy(t: CTy, sel: str): KTy = {
    val f = truthOf(t, true).ty
    if (depth(f) > 0) { ...f, nullable: sel == "nullable" } else f
  }

  // A value of type `from` is accepted where `to` is expected: the same type,
  // or a non-null pointer where the same pointer may be null.
  pure def fits(src: KTy, dst: KTy): bool =
    src.base == dst.base and src.consts == dst.consts and (src.nullable == dst.nullable or dst.nullable)

  pure def useErrs(pol: Policy, d: Decl, k: KDecl, u: Use): Set[str] = {
    if (not(u.call)) Set()
    else {
      val ps = effectiveParams(d)
      val badArg = range(0, ps.length()).foldl(false, (seen, i) =>
        seen or not(fits(argTy(ps[i].ty, u.args[i]), k.params[i].ty)))
      val badRet = not(fits(k.ret, retTy(d.ret, u.ret)))
      val missing: Set[str] = if (not(u.marked) and not(pol.unmarkedCall)) Set("Use:missing_marker") else Set()
      missing.union(if (badArg) Set("Use:arg_type") else Set())
        .union(if (badRet) Set("Use:ret_type") else Set())
    }
  }

  type Outcome = { errs: Set[str], decls: List[KDecl] }

  // The whole pipeline: import, check the declarations, check the caller.
  pure def outcomeOf(pol: Policy, h: List[Decl], u: Use): Outcome = {
    val i = importOf(pol, h)
    if (i.errs != Set()) { errs: i.errs, decls: List() }
    else {
      val c = checkErrs(i.decls)
      val e = c.union(useErrs(pol, h[0], i.decls[0], u))
      { errs: e, decls: i.decls }
    }
  }

  // ---------------------------------------------------------------------------
  // The obligations of an accepted program.

  // Each written declaration is the C type: the same base, the same const at
  // every pointee level, a pointer result that may be null, no volatile pointee
  // (Kizu has no type for it). A parameter may be non-null: a caller that
  // cannot pass NULL asks less of C, not more.
  pure def paramTrue(p: Param, k: KParam): bool = {
    val t = truthOf(p.ty, false)
    t.known and not(t.volatilePointee) and k.ty.base == t.ty.base and k.ty.consts == t.ty.consts
  }
  pure def declTrue(d: Decl, k: KDecl): bool = {
    val ps = effectiveParams(d)
    val t = truthOf(d.ret, true)
    t.known and not(t.volatilePointee) and k.ret == t.ty
      and ps.length() == k.params.length()
      and range(0, ps.length()).foldl(true, (ok, i) => ok and paramTrue(ps[i], k.params[i]))
  }

  // ---------------------------------------------------------------------------
  // The generator.

  var hdr: List[Decl]
  var use: Use
  var done: bool
  // The pipeline's outcome under POL, and under the importer as it is.
  var expected: Outcome
  var implemented: Outcome
  // The caller's parameter and result types, for the renderer.
  var caller: { args: List[KTy], ret: KTy }
  // The findings that each make the importer as it is diverge here: the
  // faithful mapping with that one rule as the importer has it.
  var why: Set[str]

  pure def whyOf(h: List[Decl], u: Use): Set[str] = {
    val base = outcomeOf(POL, h, u)
    val f1: Set[str] = if (outcomeOf({ ...POL, constCollapse: true }, h, u) != base) Set("F1") else Set()
    val f2: Set[str] = if (outcomeOf({ ...POL, retNonNull: true }, h, u) != base) Set("F2") else Set()
    val f3: Set[str] = if (outcomeOf({ ...POL, dropVolatile: true }, h, u) != base) Set("F3") else Set()
    f1.union(f2).union(f3)
  }

  pure val IMPL: Policy =
    { constCollapse: true, dropVolatile: true, retNonNull: true, unmarkedCall: false, signLoose: false }

  pure val GOOD_BASES: Set[List[str]] = Set(
    List("void"), List("char"), List("signed", "char"), List("unsigned", "char"),
    List("short"), List("unsigned", "short"), List("int"), List("unsigned", "int"),
    List("long", "long"), List("unsigned", "long", "long"),
    List("int8_t"), List("uint8_t"), List("int16_t"), List("uint16_t"),
    List("int32_t"), List("uint32_t"), List("int64_t"), List("uint64_t"),
    List("size_t"), List("intptr_t"), List("float"), List("double"),
  )
  pure val BAD_BASES: Set[List[str]] =
    Set(List("long"), List("_Bool"), List("uintptr_t"), List("short", "int"), List("widget"))
  // Qualifiers. `volatile` has no Kizu type, so it is a mistake slot of its
  // own (19); when it is drawn it mixes with the others rather than replacing them.
  pure val STAR_QS: Set[Q] = Set(NOQ, { ...NOQ, c: true }, { ...NOQ, r: true }, { ...NOQ, c: true, r: true })
  pure val VOLATILE_QS: Set[Q] = Set({ ...NOQ, v: true }, { ...NOQ, c: true, v: true })
  pure val GOOD_NAMES: Set[str] = Set("x", "s", "n", "p1", "p2")
  pure val BAD_NAMES: Set[str] = Set("fn", "match", "")
  pure val BOOLS: Set[bool] = Set(false, true)

  // The slots a mistake can go in, and the mode: 0 none, 1 at `slot`, 2
  // anywhere, 3 none but read-only arguments.
  pure def pick(mode: int, slot: int, k: int, good: Set[a], bad: Set[a]): Set[a] =
    if (mode == 2) good.union(bad) else if (mode == 1 and slot == k) bad else good
  pure def mix(mode: int, slot: int, k: int, good: Set[a], bad: Set[a]): Set[a] =
    if (mode == 2 or (mode == 1 and slot == k)) good.union(bad) else good

  pure def mkTy(base: List[str], bq: int, after: bool, n: int, q1: Q, q2: Q, q3: Q): CTy =
    {
      base: base, bc: bq == 1 or bq == 3, bv: bq == 2 or bq == 3, after: after,
      stars: List(q1, q2, q3).slice(0, n),
    }

  // Slots: 0 ret base, 1 glue, 2..4 param bases, 5..7 param names, 8..10 param
  // forms, 11 parameter list, 12 second declaration, 13..15 argument types,
  // 16 result type, 17 marker, 18 function name, 19 volatile. A mistake slot past the
  // program's size draws nothing bad, so some one-mistake programs have none.
  action start(mode: int): bool = {
    nondet slot = 0.to(19).oneOf()
    nondet rb = pick(mode, slot, 0, GOOD_BASES, BAD_BASES).oneOf()
    nondet rbq = mix(mode, slot, 19, Set(0, 1), Set(2, 3)).oneOf()
    nondet rafter = BOOLS.oneOf()
    nondet rn = Set(0, 0, 1, 2).oneOf()
    nondet r1 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet r2 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet glue = pick(mode, slot, 1, Set(false), Set(true)).oneOf()
    nondet np = Set(0, 1, 2, 3).oneOf()
    nondet b1 = pick(mode, slot, 2, GOOD_BASES, BAD_BASES).oneOf()
    nondet b2 = pick(mode, slot, 3, GOOD_BASES, BAD_BASES).oneOf()
    nondet b3 = pick(mode, slot, 4, GOOD_BASES, BAD_BASES).oneOf()
    nondet bq1 = mix(mode, slot, 19, Set(0, 1), Set(2, 3)).oneOf()
    nondet bq2 = mix(mode, slot, 19, Set(0, 1), Set(2, 3)).oneOf()
    nondet bq3 = mix(mode, slot, 19, Set(0, 1), Set(2, 3)).oneOf()
    nondet a1 = BOOLS.oneOf()
    nondet a2 = BOOLS.oneOf()
    nondet a3 = BOOLS.oneOf()
    nondet n1 = Set(0, 1, 1, 2, 3).oneOf()
    nondet n2 = Set(0, 1, 2).oneOf()
    nondet n3 = Set(0, 1, 2).oneOf()
    nondet q11 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet q12 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet q13 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet q21 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet q22 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet q31 = mix(mode, slot, 19, STAR_QS, VOLATILE_QS).oneOf()
    nondet nm1 = pick(mode, slot, 5, GOOD_NAMES, BAD_NAMES).oneOf()
    nondet nm2 = pick(mode, slot, 6, GOOD_NAMES, BAD_NAMES).oneOf()
    nondet nm3 = pick(mode, slot, 7, GOOD_NAMES, BAD_NAMES).oneOf()
    nondet f1 = pick(mode, slot, 8, Set("typed"), Set("array", "ellipsis", "fnptr", "empty")).oneOf()
    nondet f2 = pick(mode, slot, 9, Set("typed"), Set("array", "ellipsis", "fnptr", "empty")).oneOf()
    nondet f3 = pick(mode, slot, 10, Set("typed"), Set("array", "ellipsis", "fnptr", "empty")).oneOf()
    nondet plist = pick(mode, slot, 11, Set("some", "some", "void"), Set("empty")).oneOf()
    nondet second = pick(mode, slot, 12, Set("none", "other"),
      Set("dup", "define", "typedef", "struct", "enum", "var")).oneOf()
    nondet secondFirst = BOOLS.oneOf()
    nondet s1 = (if (mode == 3) Set("faithful", "readonly")
      else pick(mode, slot, 13, Set("faithful"), Set("readonly", "nullable", "wide", "slice"))).oneOf()
    nondet s2 = (if (mode == 3) Set("faithful", "readonly")
      else pick(mode, slot, 14, Set("faithful"), Set("readonly", "nullable", "wide", "slice"))).oneOf()
    nondet s3 = (if (mode == 3) Set("faithful", "readonly")
      else pick(mode, slot, 15, Set("faithful"), Set("readonly", "nullable", "wide", "slice"))).oneOf()
    nondet rs = pick(mode, slot, 16, Set("nullable"), Set("nonnull")).oneOf()
    nondet marked = pick(mode, slot, 17, Set(true), Set(false)).oneOf()
    nondet call = Set(true, true, false).oneOf()
    nondet fname = pick(mode, slot, 18, Set("f"), Set("fn")).oneOf()
    val params = List(
      { form: f1, ty: mkTy(b1, bq1, a1, n1, q11, q12, q13), name: nm1 },
      { form: f2, ty: mkTy(b2, bq2, a2, n2, q21, q22, q22), name: nm2 },
      { form: f3, ty: mkTy(b3, bq3, a3, n3, q31, q31, q31), name: nm3 },
    ).slice(0, if (plist == "some") (if (np == 0) 1 else np) else 0)
    val first = { kind: "proto", name: fname, ret: mkTy(rb, rbq, rafter, rn, r1, r2, r2), glue: glue,
      plist: plist, params: params }
    val other = if (second == "other") { ...NO_DECL, name: "g" }
      else if (second == "dup") { ...NO_DECL, name: fname }
      else { ...NO_DECL, kind: second }
    val h =
      if (second == "none") List(first)
      else if (secondFirst and second != "dup" and second != "other") List(other, first)
      else List(first, other)
    val ps = effectiveParams(first)
    val u = { call: call, marked: marked, args: List(s1, s2, s3).slice(0, ps.length()),
      ret: if (first.ret.stars.length() > 0) rs else "faithful" }
    all {
      hdr' = h,
      use' = u,
      done' = true,
      expected' = outcomeOf(POL, h, u),
      implemented' = outcomeOf(IMPL, h, u),
      caller' = {
        args: range(0, u.args.length()).foldl(List(), (acc, i) => acc.append(argTy(ps[i].ty, u.args[i]))),
        ret: retTy(first.ret, u.ret),
      },
      why' = whyOf(h, u),
    }
  }

  action init = nondet m = Set(0, 1, 2).oneOf() start(m)
  action initGuided = start(0)
  action initOneMistake = start(1)
  action initFree = start(2)
  // No mistake but the caller's: it holds read-only memory and passes it
  // where it can, so const on every level is tried against what C writes.
  action initReadonly = start(3)

  action step = all {
    hdr' = hdr, use' = use, done' = done, expected' = expected, implemented' = implemented,
    caller' = caller, why' = why,
  }

  // ---------------------------------------------------------------------------
  // Properties.

  // The properties an accepted program `h` / `u` with outcome `o` breaks.
  //   declsTrue            every declaration written is true to its C type
  //   noWriteThroughConst  read-only memory never reaches a parameter C may write through
  //   noNullLaundering     a C pointer result, which may be NULL, is never taken as non-null
  //   noUnmarkedCall       every call of C is under a marker
  pure def broken(h: List[Decl], u: Use, o: Outcome): Set[str] = {
    if (o.errs != Set()) Set()
    else {
      val ps = effectiveParams(h[0])
      val declsOk = range(0, o.decls.length()).foldl(true, (ok, i) =>
        ok and (h[i].kind == "proto" implies declTrue(h[i], o.decls[i])))
      val constOk = not(u.call) or range(0, ps.length()).foldl(true, (ok, i) => ok and (
        (u.args[i] == "readonly" and ps[i].ty.stars.length() > 0)
          implies truthOf(ps[i].ty, false).ty.consts[ps[i].ty.stars.length() - 1]))
      val nullOk = not(u.call) or h[0].ret.stars.length() == 0 or u.ret == "nullable"
      val markOk = not(u.call) or u.marked
      val first: Set[str] = if (declsOk) Set() else Set("declsTrue")
      first.union(if (constOk) Set() else Set("noWriteThroughConst"))
        .union(if (nullOk) Set() else Set("noNullLaundering"))
        .union(if (markOk) Set() else Set("noUnmarkedCall"))
    }
  }

  def accepted: bool = done and expected.errs == Set()
  def brokenNow: Set[str] = if (done) broken(hdr, use, expected) else Set()

  val declsTrue: bool = not(brokenNow.contains("declsTrue"))
  val noWriteThroughConst: bool = not(brokenNow.contains("noWriteThroughConst"))
  val noNullLaundering: bool = not(brokenNow.contains("noNullLaundering"))
  val noUnmarkedCall: bool = not(brokenNow.contains("noUnmarkedCall"))
  val soundness: bool = brokenNow == Set()

  // Witnesses.
  val reachedAccept: bool = accepted
  val reachedAcceptCall: bool = accepted and use.call and hdr[0].params.length() >= 2
  val reachedAcceptPtrRet: bool = accepted and use.call and hdr[0].ret.stars.length() > 0
  val reachedAcceptInnerConst: bool = accepted and lexists(hdr[0].params, p =>
    p.ty.stars.length() >= 2 and p.ty.stars[0].c)
  val reachedVolatile: bool = done and expected.errs == Set("Import:volatile")
  val reachedDiverge: bool = done and expected != implemented
  val reachedCheck: bool = done and expected.errs.exists(e => e == "Check:parse")
  val reachedUse: bool = done and expected.errs == Set("Use:arg_type")
  // The importer as it is lets read-only memory reach a parameter C writes through.
  val reachedConstLaundering: bool = done and implemented.errs == Set() and expected.errs == Set("Use:arg_type")
}

// The faithful mapping: every qualifier where C has it, a pointer result nullable.
module spec {
  import cimport(POL = { constCollapse: false, dropVolatile: false, retNonNull: false,
    unmarkedCall: false, signLoose: false }).*
}

// The importer as it is (findings F1–F3).
module asImplemented {
  import cimport(POL = { constCollapse: true, dropVolatile: true, retNonNull: true,
    unmarkedCall: false, signLoose: false }).*
}

// Each mutant breaks one rule; `soundness` must fail on each.
module mutantConstCollapse {
  import cimport(POL = { constCollapse: true, dropVolatile: false, retNonNull: false,
    unmarkedCall: false, signLoose: false }).*
}

module mutantDropVolatile {
  import cimport(POL = { constCollapse: false, dropVolatile: true, retNonNull: false,
    unmarkedCall: false, signLoose: false }).*
}

module mutantRetNonNull {
  import cimport(POL = { constCollapse: false, dropVolatile: false, retNonNull: true,
    unmarkedCall: false, signLoose: false }).*
}

module mutantUnmarked {
  import cimport(POL = { constCollapse: false, dropVolatile: false, retNonNull: false,
    unmarkedCall: true, signLoose: false }).*
}

module mutantSign {
  import cimport(POL = { constCollapse: false, dropVolatile: false, retNonNull: false,
    unmarkedCall: false, signLoose: true }).*
}
