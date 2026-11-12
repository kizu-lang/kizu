// -*- mode: Bluespec; -*-
// Known shapes for the C header import model: what SPEC §12.2 and the
// importer's tests say about each, under the faithful mapping and under the
// importer as it is, and which property each divergence breaks.
// `quint test cimport_test.qnt --main cimport_test`.
//
// compiler/src/cimport_mbt_test.kizu puts the same shapes to the importer and
// the checker.
module cimport_test {
  import cimport(POL = { constCollapse: false, dropVolatile: false, retNonNull: false,
    unmarkedCall: false, signLoose: false }).* from "./cimport"

  pure def ty(base: List[str], stars: List[Q]): CTy = { ...INT, base: base, stars: stars }
  pure def cty(base: List[str], stars: List[Q]): CTy = { ...ty(base, stars), bc: true }
  pure val STAR: Q = NOQ
  pure val CSTAR: Q = { ...NOQ, c: true }
  pure val VSTAR: Q = { ...NOQ, v: true }
  pure def param(t: CTy, name: str): Param = { form: "typed", ty: t, name: name }
  pure def proto(name: str, ret: CTy, params: List[Param]): Decl =
    { ...NO_DECL, name: name, ret: ret, plist: if (params == List()) "void" else "some", params: params }
  pure def only(d: Decl): List[Decl] = List(d)
  pure def call(args: List[str], ret: str): Use = { call: true, marked: true, args: args, ret: ret }

  pure def k(base: str, consts: List[bool], nullable: bool): KTy = { base: base, consts: consts, nullable: nullable }
  pure def kp(name: str, t: KTy): KParam = { name: name, ty: t }
  pure val I32: KTy = k("i32", List(), false)
  pure val VOID: KTy = k("void", List(), false)

  pure val IMPL_POL: Policy = IMPL
  pure def specOut(h: List[Decl], u: Use): Outcome = outcomeOf(POL, h, u)
  pure def implOut(h: List[Decl], u: Use): Outcome = outcomeOf(IMPL_POL, h, u)
  // Both mappings write the same and accept.
  pure def agreeAccept(h: List[Decl], u: Use): bool =
    specOut(h, u) == implOut(h, u) and specOut(h, u).errs == Set() and broken(h, u, specOut(h, u)) == Set()
  pure def rejects(h: List[Decl], u: Use, e: Set[str]): bool =
    specOut(h, u).errs == e and implOut(h, u).errs == e

  // SPEC §12.2: `int puts(const char *s);` → `extern "c" fn puts(s: ptr<const i8>) -> i32`.
  run putsTest = {
    val h = only(proto("f", INT, List(param(cty(List("char"), List(STAR)), "s"))))
    all {
      assert(agreeAccept(h, NO_USE)),
      assert(specOut(h, NO_USE).decls == List({ name: "f", params: List(kp("s", k("i8", List(true), false))), ret: I32 })),
    }
  }

  // `void write_byte(unsigned char *p, unsigned char value);`
  run writeByteTest = {
    val h = only(proto("f", ty(List("void"), List()), List(
      param(ty(List("unsigned", "char"), List(STAR)), "p"), param(ty(List("unsigned", "char"), List()), "value"))))
    all {
      assert(agreeAccept(h, call(List("faithful", "faithful"), "faithful"))),
      assert(specOut(h, NO_USE).decls[0].params == List(kp("p", k("u8", List(false), false)), kp("value", k("u8", List(), false)))),
    }
  }

  // `const char **p`: the const is on the innermost pointee, both ways.
  run innermostConstTest =
    assert(agreeAccept(only(proto("f", INT, List(param(cty(List("char"), List(STAR, STAR)), "p")))), NO_USE))

  // `size_t len(const uint8_t *data, size_t n);` called with its own types.
  run lenCallTest = assert(agreeAccept(
    only(proto("f", ty(List("size_t"), List()), List(param(cty(List("uint8_t"), List(STAR)), "data"),
      param(ty(List("size_t"), List()), "n")))),
    call(List("faithful", "faithful"), "faithful")))

  // An unnamed pointer parameter is `p<i>`.
  run unnamedPointerTest = {
    val h = only(proto("f", INT, List(param(cty(List("int32_t"), List(STAR)), ""))))
    assert(agreeAccept(h, NO_USE) and specOut(h, NO_USE).decls[0].params[0].name == "p1")
  }

  // An unnamed `int *const`: the qualifier is taken as the name; the type is right.
  run unnamedTrailingQualifierTest = {
    val h = only(proto("f", INT, List(param(ty(List("int"), List(CSTAR)), ""))))
    assert(agreeAccept(h, NO_USE) and specOut(h, NO_USE).decls[0].params[0] == kp("const", k("i32", List(false), false)))
  }

  // An unnamed scalar: its type is taken as the name, and nothing is left.
  run unnamedScalarTest = assert(rejects(only(proto("f", INT, List(param(INT, "")))), NO_USE, Set("Import:type")))

  // F1: `char *const p` — the pointer is const, the chars are not. The importer
  // writes `ptr<const i8>`, so read-only memory reaches a parameter C writes through.
  run constPointerTest = {
    val h = only(proto("f", ty(List("void"), List()), List(param(ty(List("char"), List(CSTAR)), "p"))))
    val u = call(List("readonly"), "faithful")
    all {
      assert(specOut(h, NO_USE).decls[0].params[0].ty == k("i8", List(false), false)),
      assert(implOut(h, NO_USE).decls[0].params[0].ty == k("i8", List(true), false)),
      assert(specOut(h, u).errs == Set("Use:arg_type")),
      assert(implOut(h, u).errs == Set()),
      assert(broken(h, u, implOut(h, u)) == Set("declsTrue", "noWriteThroughConst")),
    }
  }

  // F1: `char *const *p` — the const is on the middle pointer, not the chars.
  run middleConstTest = {
    val h = only(proto("f", ty(List("void"), List()), List(param(ty(List("char"), List(CSTAR, STAR)), "p"))))
    all {
      assert(specOut(h, NO_USE).decls[0].params[0].ty == k("i8", List(false, true), false)),
      assert(implOut(h, NO_USE).decls[0].params[0].ty == k("i8", List(true, false), false)),
      assert(broken(h, NO_USE, implOut(h, NO_USE)) == Set("declsTrue")),
    }
  }

  // F2: `char* getenv(const char* name);` — C may return NULL; the importer writes `ptr<i8>`.
  run pointerResultTest = {
    val h = only(proto("f", ty(List("char"), List(STAR)), List(param(cty(List("char"), List(STAR)), "name"))))
    val u = call(List("faithful"), "nonnull")
    all {
      assert(specOut(h, NO_USE).decls[0].ret == k("i8", List(false), true)),
      assert(implOut(h, NO_USE).decls[0].ret == k("i8", List(false), false)),
      assert(specOut(h, u).errs == Set("Use:ret_type")),
      assert(implOut(h, u).errs == Set()),
      assert(broken(h, u, implOut(h, u)) == Set("declsTrue", "noNullLaundering")),
      // A caller that checks for null is accepted either way.
      assert(specOut(h, call(List("faithful"), "nullable")).errs == Set()),
      assert(implOut(h, call(List("faithful"), "nullable")).errs == Set()),
    }
  }

  // `int *f(void)`: the `*` against the name is not a prototype to the importer.
  run gluedResultTest =
    assert(rejects(only({ ...proto("f", ty(List("int"), List(STAR)), List()), glue: true }), NO_USE, Set("Import:declaration")))

  // F3: `volatile int *x` — Kizu has no volatile pointee; the importer drops it.
  run volatileTest = {
    val h = only(proto("f", ty(List("void"), List()), List(param({ ...ty(List("int"), List(STAR)), bv: true }, "x"))))
    all {
      assert(specOut(h, NO_USE).errs == Set("Import:volatile")),
      assert(implOut(h, NO_USE).decls[0].params[0].ty == k("i32", List(false), false)),
      assert(broken(h, NO_USE, implOut(h, NO_USE)) == Set("declsTrue")),
    }
  }

  // A volatile pointer passed by value is fine.
  run topVolatileTest =
    assert(agreeAccept(only(proto("f", INT, List(param(ty(List("int"), List(VSTAR)), "x")))), NO_USE))

  // The unsupported features of SPEC §12.2.
  run unsupportedTest = all {
    assert(rejects(only({ ...NO_DECL, kind: "define" }), NO_USE, Set("Import:preprocessor"))),
    assert(rejects(only({ ...NO_DECL, kind: "typedef" }), NO_USE, Set("Import:typedef"))),
    assert(rejects(only({ ...NO_DECL, kind: "struct" }), NO_USE, Set("Import:struct"))),
    assert(rejects(only({ ...NO_DECL, kind: "enum" }), NO_USE, Set("Import:enum"))),
    assert(rejects(only({ ...NO_DECL, kind: "var" }), NO_USE, Set("Import:declaration"))),
    assert(rejects(only(proto("f", INT, List(param(INT, "x"), { ...NO_PARAM, form: "ellipsis" }))), NO_USE,
      Set("Import:variadic"))),
    assert(rejects(only(proto("f", INT, List({ ...NO_PARAM, form: "fnptr" }))), NO_USE, Set("Import:fnptr"))),
    assert(rejects(only(proto("f", INT, List({ ...NO_PARAM, form: "array" }))), NO_USE, Set("Import:array"))),
    assert(rejects(only(proto("f", INT, List(param(INT, "x"), { ...NO_PARAM, form: "empty" }))), NO_USE,
      Set("Import:empty_param"))),
    assert(rejects(only(proto("f", ty(List("long"), List()), List())), NO_USE, Set("Import:type"))),
    assert(rejects(only(proto("f", INT, List(param(ty(List("_Bool"), List()), "b")))), NO_USE, Set("Import:type"))),
  }

  // A `#define` runs into the declaration after it.
  run defineSwallowsNextTest = {
    val h = List({ ...NO_DECL, kind: "define" }, proto("f", INT, List()))
    assert(rejects(h, NO_USE, Set("Import:preprocessor")))
  }

  // The first rejected statement ends the import.
  run firstRejectionTest = {
    val h = List(proto("f", INT, List(param(INT, ""))), { ...NO_DECL, kind: "typedef" })
    assert(rejects(h, NO_USE, Set("Import:type")))
  }

  // What the importer writes and the checker then rejects.
  run checkerTest = all {
    assert(rejects(only(proto("fn", INT, List())), NO_USE, Set("Check:parse"))),
    assert(rejects(only(proto("f", INT, List(param(INT, "match")))), NO_USE, Set("Check:parse"))),
    assert(rejects(only(proto("f", INT, List(param(ty(List("void"), List()), "x")))), NO_USE, Set("Check:void_param"))),
    assert(rejects(List(proto("f", INT, List()), proto("f", ty(List("long", "long"), List()), List())), NO_USE,
      Set("Check:duplicate"))),
  }

  // `(void)`, `()` and a lone unnamed `void` are all no parameters.
  run noParamsTest = all {
    assert(agreeAccept(only(proto("f", INT, List())), call(List(), "faithful"))),
    assert(agreeAccept(only({ ...proto("f", INT, List()), plist: "empty" }), call(List(), "faithful"))),
    assert(agreeAccept(only({ ...proto("f", INT, List(param(ty(List("void"), List()), ""))) }), call(List(), "faithful"))),
  }

  // The caller.
  run callerTest = {
    val h = only(proto("f", INT, List(param(INT, "x"), param(ty(List("int"), List(STAR)), "p"))))
    all {
      assert(rejects(h, { ...call(List("faithful", "faithful"), "faithful"), marked: false }, Set("Use:missing_marker"))),
      assert(rejects(h, call(List("wide", "faithful"), "faithful"), Set("Use:arg_type"))),
      assert(rejects(h, call(List("faithful", "nullable"), "faithful"), Set("Use:arg_type"))),
      assert(rejects(h, call(List("faithful", "readonly"), "faithful"), Set("Use:arg_type"))),
      assert(rejects(h, call(List("faithful", "slice"), "faithful"), Set("Use:arg_type"))),
      assert(agreeAccept(h, call(List("faithful", "faithful"), "faithful"))),
    }
  }

  // A read-only pointer to a `const` parameter is what const is for.
  run readonlyToConstTest =
    assert(agreeAccept(only(proto("f", INT, List(param(cty(List("char"), List(STAR)), "s")))), call(List("readonly"), "faithful")))

  // The mutants break what they are for.
  run mutantTest = {
    val sign = { ...POL, signLoose: true }
    val unmarked = { ...POL, unmarkedCall: true }
    val hs = only(proto("f", ty(List("unsigned", "int"), List()), List()))
    val hu = only(proto("f", INT, List()))
    val u = { ...call(List(), "faithful"), marked: false }
    all {
      assert(broken(hs, NO_USE, outcomeOf(sign, hs, NO_USE)) == Set("declsTrue")),
      assert(broken(hu, u, outcomeOf(unmarked, hu, u)) == Set("noUnmarkedCall")),
    }
  }
}
