import Lean
/-!
# Agreement

An agreement between two parties moves through a fixed set of states, and
the transition table below is the whole of what an implementation may do.
An event the table does not list for the current state is refused and
changes nothing.

This file is the specification. `table` is the transition table, `post`
applies one event. `ended_absorbing`, `active_only_by_acceptance` and
`terminate_needs_agreement` are the properties every implementation has to
keep. `main` writes traces that `examples/agreement_conformance.kizu`
replays against implementations.
-/
open Lean (Json toJson)

namespace Agreement

inductive State
  | draft
  | offered
  | active
  | suspended
  | ended
deriving Repr, DecidableEq

inductive Event
  | offer
  | accept
  | reject
  | suspend
  | resume
  | terminate
deriving Repr, DecidableEq

inductive Outcome
  | applied
  | refused
deriving Repr, DecidableEq

/-- The transition table: `none` is a refusal. -/
def table : State → Event → Option State
  | .draft, .offer => some .offered
  | .offered, .accept => some .active
  | .offered, .reject => some .draft
  | .active, .suspend => some .suspended
  | .active, .terminate => some .ended
  | .suspended, .resume => some .active
  | .suspended, .terminate => some .ended
  | _, _ => none

def post (s : State) (e : Event) : State × Outcome :=
  match table s e with
  | some next => (next, .applied)
  | none => (s, .refused)

def run (s : State) : List Event → State
  | [] => s
  | e :: es => run (post s e).1 es

/-! ## Properties -/

/-- Nothing happens to an ended agreement, whatever is sent. -/
theorem ended_absorbing (e : Event) : post .ended e = (.ended, .refused) := by
  cases e <;> rfl

theorem ended_stays (es : List Event) : run .ended es = .ended := by
  induction es with
  | nil => rfl
  | cons e es ih => simp [run, ended_absorbing, ih]

/-- An agreement is active only because it was accepted or resumed, or was
active already. -/
theorem active_only_by_acceptance (s : State) (e : Event) (h : (post s e).1 = .active) :
    s = .active ∨ (s = .offered ∧ e = .accept) ∨ (s = .suspended ∧ e = .resume) := by
  cases s <;> cases e <;> simp_all [post, table]

/-- Terminate is applied only to an agreement both parties entered. -/
theorem terminate_needs_agreement (s : State) (h : (post s .terminate).2 = .applied) :
    s = .active ∨ s = .suspended := by
  cases s <;> simp_all [post, table]

/-! ## Traces for implementations -/

def State.name : State → String
  | .draft => "draft"
  | .offered => "offered"
  | .active => "active"
  | .suspended => "suspended"
  | .ended => "ended"

def Event.name : Event → String
  | .offer => "offer"
  | .accept => "accept"
  | .reject => "reject"
  | .suspend => "suspend"
  | .resume => "resume"
  | .terminate => "terminate"

def Outcome.name : Outcome → String
  | .applied => "applied"
  | .refused => "refused"

def events : List Event := [.offer, .accept, .reject, .suspend, .resume, .terminate]

structure Step where
  event : Event
  outcome : Outcome
  state : State

def Step.json (s : Step) : Json :=
  Json.mkObj [
    ("event", toJson s.event.name),
    ("outcome", toJson s.outcome.name),
    ("state", toJson s.state.name)]

/-- Two events in three are ones the table lists for the current state, so a
trace moves, and among those terminate is rare so it moves for a while; the
third is any event, so refusals show up too. -/
def nextEvent (rng : StdGen) (s : State) : Event × StdGen :=
  let (pick, rng) := randNat rng 0 2
  let (keep, rng) := randNat rng 0 7
  let listed := events.filter fun e => (table s e).isSome
  let listed := if keep = 0 then listed else listed.filter (· != .terminate)
  let pool := if pick < 2 ∧ listed ≠ [] then listed else events
  let (k, rng) := randNat rng 0 (pool.length - 1)
  (pool.getD k .offer, rng)

/-- A trace runs until three events after the agreement ended, or `limit`
events, whichever comes first. -/
def trace (seed limit : Nat) : List Step := Id.run do
  let mut rng := mkStdGen seed
  let mut state := State.draft
  let mut out : Array Step := #[]
  let mut after_end := 0
  for _ in [0:limit] do
    if after_end ≥ 3 then break
    let (e, next) := nextEvent rng state
    rng := next
    let (after, outcome) := post state e
    state := after
    out := out.push { event := e, outcome, state }
    if state = .ended then after_end := after_end + 1
  return out.toList

def main : IO Unit := do
  let seeds := [1, 2, 3]
  IO.println "{\"initial\": \"draft\", \"traces\": ["
  for seed in seeds, i in [0:seeds.length] do
    IO.println ("  {\"seed\": " ++ toString seed ++ ", \"steps\": [")
    let steps := trace seed 40
    for step in steps, j in [0:steps.length] do
      let tail := if j + 1 < steps.length then "," else ""
      IO.println ("    " ++ step.json.compress ++ tail)
    let tail := if i + 1 < seeds.length then "," else ""
    IO.println ("  ]}" ++ tail)
  IO.println "]}"

end Agreement

def main : IO Unit := Agreement.main
