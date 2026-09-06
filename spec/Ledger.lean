import Lean
/-!
# Ledger

A ledger moves money between a fixed number of accounts. Amounts are
integers in the smallest unit, never floats. Every transfer carries an id so
that a client whose reply was lost can send the same transfer again, and the
second copy must not move money.

This file is the specification. `Book.post` says what one transfer does.
`total_reachable` and `retry_after` are the two properties every
implementation has to keep: money is neither created nor destroyed, and a
transfer that was applied once is a duplicate whenever it arrives again.
`main` writes traces of the specification that
`examples/ledger_conformance.kizu` replays against implementations.
-/
open Lean (Json ToJson toJson)

namespace Ledger

def accounts : Nat := 4
def opening : Int := 1000

structure Transfer where
  id : Nat
  source : Fin accounts
  target : Fin accounts
  amount : Int
deriving Repr, DecidableEq

instance : Inhabited Transfer := ⟨⟨0, ⟨0, by decide⟩, ⟨0, by decide⟩, 0⟩⟩

inductive Outcome
  | applied
  | duplicate
  | invalid
  | insufficient
deriving Repr, DecidableEq

/-- One balance per account, and the id of every transfer applied so far. -/
structure Book where
  balances : List Int
  applied : List Nat
  wf : balances.length = accounts

def Book.init : Book := ⟨List.replicate accounts opening, [], by simp⟩

def Book.total (b : Book) : Int := b.balances.sum

/-- Add `amount` to the balance at `i`. -/
def credit (l : List Int) (i : Nat) (amount : Int) : List Int :=
  l.set i (l.getD i 0 + amount)

theorem length_credit (l : List Int) (i : Nat) (amount : Int) :
    (credit l i amount).length = l.length := by
  simp [credit]

def Book.post (b : Book) (t : Transfer) : Book × Outcome :=
  if t.id ∈ b.applied then (b, .duplicate)
  else if t.amount ≤ 0 ∨ t.source = t.target then (b, .invalid)
  else if b.balances.getD t.source 0 < t.amount then (b, .insufficient)
  else
    ({ balances := credit (credit b.balances t.source (-t.amount)) t.target t.amount,
       applied := t.id :: b.applied,
       wf := by simp [length_credit, b.wf] },
     .applied)

def Book.run (b : Book) : List Transfer → Book
  | [] => b
  | t :: ts => (b.post t).1.run ts

/-! ## Money is conserved -/

theorem sum_credit (l : List Int) (i : Nat) (amount : Int) (h : i < l.length) :
    (credit l i amount).sum = l.sum + amount := by
  induction l generalizing i with
  | nil => simp at h
  | cons x xs ih =>
    cases i with
    | zero => simp [credit]; omega
    | succ n =>
      have := ih n (by simpa using h)
      simp only [credit, List.getD_cons_succ, List.set_cons_succ, List.sum_cons] at this ⊢
      omega

theorem post_total (b : Book) (t : Transfer) : (b.post t).1.total = b.total := by
  unfold Book.post
  split
  · rfl
  split
  · rfl
  split
  · rfl
  · simp only [Book.total]
    rw [sum_credit, sum_credit]
    · omega
    · rw [b.wf]; exact t.source.isLt
    · rw [length_credit, b.wf]; exact t.target.isLt

inductive Reachable : Book → Prop
  | init : Reachable Book.init
  | step {b : Book} (t : Transfer) : Reachable b → Reachable (b.post t).1

theorem init_total : Book.init.total = accounts * opening := by decide

theorem total_reachable {b : Book} (h : Reachable b) : b.total = accounts * opening := by
  induction h with
  | init => exact init_total
  | step t _ ih => rw [post_total]; exact ih

/-! ## A retry moves nothing -/

theorem retry_duplicate (b : Book) (t : Transfer) (h : t.id ∈ b.applied) :
    b.post t = (b, .duplicate) := by
  simp [Book.post, h]

theorem applied_mono (b : Book) (t u : Transfer) (h : t.id ∈ b.applied) :
    t.id ∈ (b.post u).1.applied := by
  unfold Book.post
  split
  · exact h
  split
  · exact h
  split
  · exact h
  · exact List.mem_cons_of_mem _ h

theorem applied_stays (b : Book) (t : Transfer) (ts : List Transfer) (h : t.id ∈ b.applied) :
    t.id ∈ (b.run ts).applied := by
  induction ts generalizing b with
  | nil => exact h
  | cons u us ih => exact ih _ (applied_mono b t u h)

theorem applied_marks (b : Book) (t : Transfer) (h : (b.post t).2 = .applied) :
    t.id ∈ (b.post t).1.applied := by
  unfold Book.post at h ⊢
  split
  · simp_all
  split
  · simp_all
  split
  · simp_all
  · exact List.mem_cons_self

/-- Once a transfer was applied, sending it again after any other transfers
is a duplicate and leaves the book as it was. -/
theorem retry_after (b : Book) (t : Transfer) (ts : List Transfer)
    (h : (b.post t).2 = .applied) :
    ((b.post t).1.run ts).post t = ((b.post t).1.run ts, .duplicate) :=
  retry_duplicate _ _ (applied_stays _ _ _ (applied_marks b t h))

/-! ## Traces for implementations -/

def Outcome.name : Outcome → String
  | .applied => "applied"
  | .duplicate => "duplicate"
  | .invalid => "invalid"
  | .insufficient => "insufficient"

structure Step where
  transfer : Transfer
  outcome : Outcome
  balances : List Int

def Step.json (s : Step) : Json :=
  Json.mkObj [
    ("id", toJson s.transfer.id),
    ("source", toJson (s.transfer.source : Nat)),
    ("target", toJson (s.transfer.target : Nat)),
    ("amount", toJson s.transfer.amount),
    ("outcome", toJson s.outcome.name),
    ("balances", toJson s.balances)]

def account (n : Nat) : Fin accounts := ⟨n % accounts, Nat.mod_lt _ (by decide)⟩

/-- One transfer in three retries one the book already applied; the rest are
new, and their accounts and amounts are drawn so that self-transfers and
overdrafts happen now and then. -/
def nextTransfer (rng : StdGen) (applied : List Transfer) : Transfer × StdGen :=
  let (pick, rng) := randNat rng 0 2
  if pick = 0 ∧ applied ≠ [] then
    let (k, rng) := randNat rng 0 (applied.length - 1)
    (applied.getD k default, rng)
  else
    let (id, rng) := randNat rng 0 999
    let (source, rng) := randNat rng 0 (accounts - 1)
    let (target, rng) := randNat rng 0 (accounts - 1)
    let (amount, rng) := randNat rng 0 600
    ({ id, source := account source, target := account target, amount }, rng)

def trace (seed steps : Nat) : List Step := Id.run do
  let mut rng := mkStdGen seed
  let mut book := Book.init
  let mut applied : List Transfer := []
  let mut out : Array Step := #[]
  for _ in [0:steps] do
    let (t, next) := nextTransfer rng applied
    rng := next
    let (after, outcome) := book.post t
    if outcome = .applied then
      applied := applied ++ [t]
    book := after
    out := out.push { transfer := t, outcome, balances := book.balances }
  return out.toList

def main : IO Unit := do
  let seeds := [1, 2, 3]
  IO.println ("{\"accounts\": " ++ toString accounts ++ ", \"opening\": " ++ toString opening ++ ", \"traces\": [")
  for seed in seeds, i in [0:seeds.length] do
    IO.println ("  {\"seed\": " ++ toString seed ++ ", \"steps\": [")
    let steps := trace seed 50
    for step in steps, j in [0:steps.length] do
      let tail := if j + 1 < steps.length then "," else ""
      IO.println ("    " ++ step.json.compress ++ tail)
    let tail := if i + 1 < seeds.length then "," else ""
    IO.println ("  ]}" ++ tail)
  IO.println "]}"

end Ledger

def main : IO Unit := Ledger.main
