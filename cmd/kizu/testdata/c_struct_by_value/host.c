#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
typedef struct { uint16_t a, b; uint64_t c; } O16;
typedef struct { int32_t a; } I4;
typedef struct { uint8_t a, b, c; } B3;
typedef struct { int32_t a, b, c; } I12;
typedef struct { float a, b; } F2;
typedef struct { float a, b, c; } F3;
typedef struct { double a, b; } D2;
typedef struct { double a, b, c, d; } D4;
typedef struct { int64_t a, b, c; } L3;
typedef struct { double a; int32_t b; } DI;
typedef struct { float a; int32_t b; double c; } FID;
typedef struct { bool a; const uint8_t *b; } BP;
typedef struct { F2 a; float b; } NF;
static const uint8_t seven = 7;
O16 make_o16(int64_t k) { return (O16){k, k + 1, k + 2}; }
int64_t take_o16(O16 v) { return v.a + 10 * v.b + 100 * v.c; }
I4 make_i4(int64_t k) { return (I4){k}; }
int64_t take_i4(I4 v) { return 3 * v.a; }
B3 make_b3(int64_t k) { return (B3){k, k + 1, k + 2}; }
int64_t take_b3(B3 v) { return v.a + 10 * v.b + 100 * v.c; }
I12 make_i12(int64_t k) { return (I12){k, k + 1, k + 2}; }
int64_t take_i12(I12 v) { return v.a + 10 * v.b + 100 * v.c; }
F2 make_f2(int64_t k) { return (F2){k + 0.5f, k + 0.25f}; }
int64_t take_f2(F2 v) { return (int64_t)(4 * v.a + 40 * v.b); }
F3 make_f3(int64_t k) { return (F3){k + 0.5f, k + 0.25f, k + 0.75f}; }
int64_t take_f3(F3 v) { return (int64_t)(4 * v.a + 40 * v.b + 400 * v.c); }
D2 make_d2(int64_t k) { return (D2){k + 0.5, k + 0.25}; }
int64_t take_d2(D2 v) { return (int64_t)(4 * v.a + 40 * v.b); }
D4 make_d4(int64_t k) { return (D4){k + 0.5, k + 0.25, k + 0.75, k + 1.5}; }
int64_t take_d4(D4 v) { return (int64_t)(4 * v.a + 40 * v.b + 400 * v.c + 4000 * v.d); }
L3 make_l3(int64_t k) { return (L3){k, k + 1, k + 2}; }
int64_t take_l3(L3 v) { return v.a + 10 * v.b + 100 * v.c; }
DI make_di(int64_t k) { return (DI){k + 0.5, k + 1}; }
int64_t take_di(DI v) { return (int64_t)(4 * v.a) + 10 * v.b; }
FID make_fid(int64_t k) { return (FID){k + 0.5f, k + 1, k + 0.25}; }
int64_t take_fid(FID v) { return (int64_t)(4 * v.a + 40 * v.c) + 10 * v.b; }
BP make_bp(int64_t k) { return (BP){k != 0, &seven}; }
int64_t take_bp(BP v) { return v.a + 10 * *v.b; }
NF make_nf(int64_t k) { return (NF){{k + 0.5f, k + 0.25f}, k + 0.75f}; }
int64_t take_nf(NF v) { return (int64_t)(4 * v.a.a + 40 * v.a.b + 400 * v.b); }
/* Five integer arguments leave x86-64 one register, too few for O16. */
int64_t take_late(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, O16 o, D2 f) {
    return a + b + c + d + e + take_o16(o) + take_d2(f);
}
L3 pass_l3(L3 v, int64_t k) { return (L3){v.a + k, v.b + k, v.c + k}; }
int64_t lib_run(void);
int64_t reference(void) {
    int64_t total = 0;
    for (int64_t k = 1; k <= 3; k++) {
        total += take_o16(make_o16(k)) + take_i4(make_i4(k)) + take_b3(make_b3(k)) +
                 take_i12(make_i12(k)) + take_f2(make_f2(k)) + take_f3(make_f3(k)) +
                 take_d2(make_d2(k)) + take_d4(make_d4(k)) + take_l3(make_l3(k)) +
                 take_di(make_di(k)) + take_fid(make_fid(k)) + take_bp(make_bp(k)) +
                 take_nf(make_nf(k));
        O16 o = make_o16(k);
        L3 l = pass_l3(make_l3(k), k);
        total += o.a + o.b + o.c + l.a + l.b + l.c;
        total += take_late(k, k, k, k, k, make_o16(k), make_d2(k));
    }
    return total;
}
int main(void) {
    int64_t want = reference();
    int64_t got = lib_run();
    printf(got == want ? "ok %lld\n" : "got %lld want %lld\n", (long long)got, (long long)want);
    return 0;
}
