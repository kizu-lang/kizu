import Lean
/-!
# TLS 1.3 handshake

A TLS 1.3 client (RFC 8446 §A.1) is a machine that waits for the server's
messages in one order and no other, and a server (§A.2) one that waits for
the client's. The transition tables below are the whole of what each side
may do with a message from the other: a message the table does not list
for the current state is an `unexpected_message` alert, and the connection
is closed. Pre-shared keys, early data and client certificates are not
modelled: the client offers none of them, so the server's
`CertificateRequest` is acknowledged and the client's own `Certificate` is
empty; the server asks for none, sends its whole flight on the
ClientHello, and refuses a client's `Certificate`, `CertificateVerify` and
`EndOfEarlyData`.

This file is the specification. `table` is the client's transition table
and `post` applies one message to it; `ServerState`, `ServerEvent`,
`serverTable` and `serverPost` are the server's. `closed_absorbing`,
`connected_only_by_finished`, `data_needs_connection`,
`verify_needs_certificate` and `refusal_closes` are the properties every
implementation has to keep, and the server has its own of each. `main`
writes traces of the client that `examples/tls_conformance.kizu` replays
against `std::tls`.
-/
open Lean (Json toJson)

namespace Tls

/-- The client's states, as RFC 8446 §A.1 names them. `closed` is after a
`close_notify`, a fatal alert, or a refusal. -/
inductive State
  | waitServerHello
  | waitEncryptedExtensions
  | waitCertificateRequest
  | waitCertificate
  | waitCertificateVerify
  | waitFinished
  | connected
  | closed
deriving Repr, DecidableEq

/-- What arrives from the server: a handshake message, application data, or
an alert. -/
inductive Event
  | serverHello
  | helloRetryRequest
  | encryptedExtensions
  | certificateRequest
  | certificate
  | certificateVerify
  | finished
  | newSessionTicket
  | keyUpdate
  | applicationData
  | closeNotify
  | fatalAlert
deriving Repr, DecidableEq

/-- `applied` is a message taken as the table says; `refused` is one the
table does not list, answered with an `unexpected_message` alert; and
`unsupported` is `HelloRetryRequest`, which this client does not handle and
answers with `handshake_failure`. Both of the last close the connection. -/
inductive Outcome
  | applied
  | refused
  | unsupported
deriving Repr, DecidableEq

/-- The transition table: `none` is a refusal. Every state but `closed`
takes `close_notify` and a fatal alert. -/
def table : State → Event → Option State
  | .waitServerHello, .serverHello => some .waitEncryptedExtensions
  | .waitEncryptedExtensions, .encryptedExtensions => some .waitCertificateRequest
  | .waitCertificateRequest, .certificateRequest => some .waitCertificate
  | .waitCertificateRequest, .certificate => some .waitCertificateVerify
  | .waitCertificate, .certificate => some .waitCertificateVerify
  | .waitCertificateVerify, .certificateVerify => some .waitFinished
  | .waitFinished, .finished => some .connected
  | .connected, .newSessionTicket => some .connected
  | .connected, .keyUpdate => some .connected
  | .connected, .applicationData => some .connected
  | .closed, _ => none
  | _, .closeNotify => some .closed
  | _, .fatalAlert => some .closed
  | _, _ => none

def post (s : State) (e : Event) : State × Outcome :=
  match s, e with
  | .waitServerHello, .helloRetryRequest => (.closed, .unsupported)
  | _, _ =>
    match table s e with
    | some next => (next, .applied)
    | none => (.closed, .refused)

def run (s : State) : List Event → State
  | [] => s
  | e :: es => run (post s e).1 es

/-! ## Properties -/

/-- Nothing happens to a closed connection, whatever arrives. -/
theorem closed_absorbing (e : Event) : post .closed e = (.closed, .refused) := by
  cases e <;> rfl

theorem closed_stays (es : List Event) : run .closed es = .closed := by
  induction es with
  | nil => rfl
  | cons e es ih => simp [run, closed_absorbing, ih]

/-- A connection is connected only because the server's `Finished` arrived
where it was waited for, or was connected already. -/
theorem connected_only_by_finished (s : State) (e : Event) (h : (post s e).1 = .connected) :
    s = .connected ∨ (s = .waitFinished ∧ e = .finished) := by
  cases s <;> cases e <;> simp_all [post, table]

/-- Application data is taken only on a connected connection. -/
theorem data_needs_connection (s : State) (h : (post s .applicationData).2 = .applied) :
    s = .connected := by
  cases s <;> simp_all [post, table]

/-- A `CertificateVerify` is taken only after a `Certificate`, and the state
that waits for it is reached only by one. -/
theorem verify_needs_certificate (s : State) (h : (post s .certificateVerify).2 = .applied) :
    s = .waitCertificateVerify := by
  cases s <;> simp_all [post, table]

theorem certificate_first (s : State) (e : Event) (h : (post s e).1 = .waitCertificateVerify) :
    e = .certificate := by
  cases s <;> cases e <;> simp_all [post, table]

/-- A client that refuses a message, or cannot handle one, is done: it
sends its alert and closes. -/
theorem refusal_closes (s : State) (e : Event) (h : (post s e).2 ≠ .applied) :
    (post s e).1 = .closed := by
  cases s <;> cases e <;> simp_all [post, table]

/-- The server's handshake messages are taken in the order RFC 8446 §2 lists
them: whatever the path, the states visit them in this order. -/
theorem finished_needs_verify (s : State) (h : (post s .finished).2 = .applied) :
    s = .waitFinished := by
  cases s <;> simp_all [post, table]

theorem wait_finished_by_verify (s : State) (e : Event) (h : (post s e).1 = .waitFinished) :
    s = .waitCertificateVerify ∧ e = .certificateVerify := by
  cases s <;> cases e <;> simp_all [post, table]

/-! ## The server -/

/-- The server's states, as RFC 8446 §A.2 names them, less what it never
does: it sends its whole flight on the ClientHello and then waits for the
client's `Finished`. -/
inductive ServerState
  | waitClientHello
  | waitFinished
  | connected
  | closed
deriving Repr, DecidableEq

/-- What arrives from the client: one of the handshake messages a client
can send, application data, or an alert. -/
inductive ServerEvent
  | clientHello
  | endOfEarlyData
  | certificate
  | certificateVerify
  | finished
  | keyUpdate
  | applicationData
  | closeNotify
  | fatalAlert
deriving Repr, DecidableEq

/-- The server's transition table: `none` is a refusal. A `Certificate` or
`CertificateVerify` is refused since none was asked for, and
`EndOfEarlyData` since no early data was accepted. -/
def serverTable : ServerState → ServerEvent → Option ServerState
  | .waitClientHello, .clientHello => some .waitFinished
  | .waitFinished, .finished => some .connected
  | .connected, .keyUpdate => some .connected
  | .connected, .applicationData => some .connected
  | .closed, _ => none
  | _, .closeNotify => some .closed
  | _, .fatalAlert => some .closed
  | _, _ => none

def serverPost (s : ServerState) (e : ServerEvent) : ServerState × Outcome :=
  match serverTable s e with
  | some next => (next, .applied)
  | none => (.closed, .refused)

theorem server_closed_absorbing (e : ServerEvent) :
    serverPost .closed e = (.closed, .refused) := by
  cases e <;> rfl

/-- The server is connected only because the client's `Finished` arrived
where it was waited for, or was connected already. -/
theorem server_connected_only_by_finished (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).1 = .connected) :
    s = .connected ∨ (s = .waitFinished ∧ e = .finished) := by
  cases s <;> cases e <;> simp_all [serverPost, serverTable]

theorem server_data_needs_connection (s : ServerState)
    (h : (serverPost s .applicationData).2 = .applied) : s = .connected := by
  cases s <;> simp_all [serverPost, serverTable]

/-- The client's `Finished` is taken only after the server sent its own
flight, which is what `waitFinished` means, and that state is reached only
by a `ClientHello`. -/
theorem server_finished_needs_hello (s : ServerState)
    (h : (serverPost s .finished).2 = .applied) : s = .waitFinished := by
  cases s <;> simp_all [serverPost, serverTable]

theorem server_wait_finished_by_hello (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).1 = .waitFinished) :
    s = .waitClientHello ∧ e = .clientHello := by
  cases s <;> cases e <;> simp_all [serverPost, serverTable]

/-- A client's certificate is never taken: the server asked for none. -/
theorem server_takes_no_certificate (s : ServerState) :
    (serverPost s .certificate).2 = .refused := by
  cases s <;> rfl

theorem server_refusal_closes (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).2 ≠ .applied) : (serverPost s e).1 = .closed := by
  cases s <;> cases e <;> simp_all [serverPost, serverTable]

/-! ## Traces for implementations -/

def State.name : State → String
  | .waitServerHello => "wait_server_hello"
  | .waitEncryptedExtensions => "wait_encrypted_extensions"
  | .waitCertificateRequest => "wait_certificate_request"
  | .waitCertificate => "wait_certificate"
  | .waitCertificateVerify => "wait_certificate_verify"
  | .waitFinished => "wait_finished"
  | .connected => "connected"
  | .closed => "closed"

def Event.name : Event → String
  | .serverHello => "server_hello"
  | .helloRetryRequest => "hello_retry_request"
  | .encryptedExtensions => "encrypted_extensions"
  | .certificateRequest => "certificate_request"
  | .certificate => "certificate"
  | .certificateVerify => "certificate_verify"
  | .finished => "finished"
  | .newSessionTicket => "new_session_ticket"
  | .keyUpdate => "key_update"
  | .applicationData => "application_data"
  | .closeNotify => "close_notify"
  | .fatalAlert => "fatal_alert"

def Outcome.name : Outcome → String
  | .applied => "applied"
  | .refused => "refused"
  | .unsupported => "unsupported"

def events : List Event := [.serverHello, .helloRetryRequest, .encryptedExtensions,
  .certificateRequest, .certificate, .certificateVerify, .finished, .newSessionTicket,
  .keyUpdate, .applicationData, .closeNotify, .fatalAlert]

structure Step where
  event : Event
  outcome : Outcome
  state : State

def Step.json (s : Step) : Json :=
  Json.mkObj [
    ("event", toJson s.event.name),
    ("outcome", toJson s.outcome.name),
    ("state", toJson s.state.name)]

/-- Nine times in ten the next event is one the table lists for the
current state, so a trace walks through the handshake and on into the
connection, and closing is rare among those so a connection lasts; the
tenth time it is one the table does not list, so refusals from every
state show up, and the retry request with them. -/
def nextEvent (rng : StdGen) (s : State) : Event × StdGen :=
  let (pick, rng) := randNat rng 0 9
  let (keep, rng) := randNat rng 0 11
  let listed := events.filter fun e => (table s e).isSome
  let unlisted := events.filter fun e => (table s e).isNone
  let listed := if keep = 0 then listed
    else listed.filter fun e => e != .closeNotify ∧ e != .fatalAlert
  let pool := if pick < 9 ∧ listed ≠ [] then listed else unlisted
  let (k, rng) := randNat rng 0 (pool.length - 1)
  (pool.getD k .serverHello, rng)

/-- A trace runs until three events after the connection closed, or `limit`
events, whichever comes first. -/
def trace (seed limit : Nat) : List Step := Id.run do
  let mut rng := mkStdGen seed
  let mut state := State.waitServerHello
  let mut out : Array Step := #[]
  let mut after_close := 0
  for _ in [0:limit] do
    if after_close ≥ 3 then break
    let (e, next) := nextEvent rng state
    rng := next
    let (after, outcome) := post state e
    state := after
    out := out.push { event := e, outcome, state }
    if state = .closed then after_close := after_close + 1
  return out.toList

def main : IO Unit := do
  let seeds := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]
  IO.println "{\"initial\": \"wait_server_hello\", \"traces\": ["
  for seed in seeds, i in [0:seeds.length] do
    IO.println ("  {\"seed\": " ++ toString seed ++ ", \"steps\": [")
    let steps := trace seed 40
    for step in steps, j in [0:steps.length] do
      let tail := if j + 1 < steps.length then "," else ""
      IO.println ("    " ++ step.json.compress ++ tail)
    let tail := if i + 1 < seeds.length then "," else ""
    IO.println ("  ]}" ++ tail)
  IO.println "]}"

end Tls

def main : IO Unit := Tls.main
