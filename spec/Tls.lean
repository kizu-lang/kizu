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

The tables come from `std::tls` itself: `spec/tls_machine.kizu` reads what
`advance` and `advance_server` do with every state and message, and
`scripts/machine-lean` writes that into the generated blocks below (`just
spec-table`). `table` is the client's transition table and `post` applies one
message to it; `ServerState`, `ServerEvent`, `serverTable` and `serverPost` are
the server's. `closed_absorbing`, `connected_only_by_finished`,
`data_needs_connection`, `verify_needs_certificate` and `refusal_closes` are
the properties every implementation has to keep, and the server has its own of
each, so a change to `std::tls` that breaks one fails here. `main` writes traces
of the client that `examples/tls_conformance.kizu` replays against `std::tls`.
-/
open Lean (Json toJson)

namespace Tls

/-- `applied` is a message taken as the table says; `refused` is one the
table does not list, answered with an `unexpected_message` alert; and
`unsupported` is `HelloRetryRequest`, which this client does not handle and
answers with `handshake_failure`. Both of the last close the connection. -/
inductive Outcome
  | applied
  | refused
  | unsupported
deriving Repr, DecidableEq

/-!
The client's machine. `State` is named as RFC 8446 §A.1 names the states, and
`closed` is after a `close_notify`, a fatal alert, or a refusal. `Event` is what
arrives from the server: a handshake message, application data, or an alert.
`table` is the moves taken as listed, with `none` a refusal, and every state but
`closed` takes `close_notify` and a fatal alert. `post` is every pair and where
it leaves the connection.
-/

-- BEGIN GENERATED client state=State event=Event table=table post=post applied=Applied
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

def table : State → Event → Option State
  | .waitServerHello, .serverHello => some .waitEncryptedExtensions
  | .waitServerHello, .closeNotify => some .closed
  | .waitServerHello, .fatalAlert => some .closed
  | .waitEncryptedExtensions, .encryptedExtensions => some .waitCertificateRequest
  | .waitEncryptedExtensions, .closeNotify => some .closed
  | .waitEncryptedExtensions, .fatalAlert => some .closed
  | .waitCertificateRequest, .certificateRequest => some .waitCertificate
  | .waitCertificateRequest, .certificate => some .waitCertificateVerify
  | .waitCertificateRequest, .closeNotify => some .closed
  | .waitCertificateRequest, .fatalAlert => some .closed
  | .waitCertificate, .certificate => some .waitCertificateVerify
  | .waitCertificate, .closeNotify => some .closed
  | .waitCertificate, .fatalAlert => some .closed
  | .waitCertificateVerify, .certificateVerify => some .waitFinished
  | .waitCertificateVerify, .closeNotify => some .closed
  | .waitCertificateVerify, .fatalAlert => some .closed
  | .waitFinished, .finished => some .connected
  | .waitFinished, .closeNotify => some .closed
  | .waitFinished, .fatalAlert => some .closed
  | .connected, .newSessionTicket => some .connected
  | .connected, .keyUpdate => some .connected
  | .connected, .applicationData => some .connected
  | .connected, .closeNotify => some .closed
  | .connected, .fatalAlert => some .closed
  | _, _ => none

def post : State → Event → State × Outcome
  | .waitServerHello, .serverHello => (.waitEncryptedExtensions, .applied)
  | .waitServerHello, .helloRetryRequest => (.closed, .unsupported)
  | .waitServerHello, .encryptedExtensions => (.closed, .refused)
  | .waitServerHello, .certificateRequest => (.closed, .refused)
  | .waitServerHello, .certificate => (.closed, .refused)
  | .waitServerHello, .certificateVerify => (.closed, .refused)
  | .waitServerHello, .finished => (.closed, .refused)
  | .waitServerHello, .newSessionTicket => (.closed, .refused)
  | .waitServerHello, .keyUpdate => (.closed, .refused)
  | .waitServerHello, .applicationData => (.closed, .refused)
  | .waitServerHello, .closeNotify => (.closed, .applied)
  | .waitServerHello, .fatalAlert => (.closed, .applied)
  | .waitEncryptedExtensions, .serverHello => (.closed, .refused)
  | .waitEncryptedExtensions, .helloRetryRequest => (.closed, .refused)
  | .waitEncryptedExtensions, .encryptedExtensions => (.waitCertificateRequest, .applied)
  | .waitEncryptedExtensions, .certificateRequest => (.closed, .refused)
  | .waitEncryptedExtensions, .certificate => (.closed, .refused)
  | .waitEncryptedExtensions, .certificateVerify => (.closed, .refused)
  | .waitEncryptedExtensions, .finished => (.closed, .refused)
  | .waitEncryptedExtensions, .newSessionTicket => (.closed, .refused)
  | .waitEncryptedExtensions, .keyUpdate => (.closed, .refused)
  | .waitEncryptedExtensions, .applicationData => (.closed, .refused)
  | .waitEncryptedExtensions, .closeNotify => (.closed, .applied)
  | .waitEncryptedExtensions, .fatalAlert => (.closed, .applied)
  | .waitCertificateRequest, .serverHello => (.closed, .refused)
  | .waitCertificateRequest, .helloRetryRequest => (.closed, .refused)
  | .waitCertificateRequest, .encryptedExtensions => (.closed, .refused)
  | .waitCertificateRequest, .certificateRequest => (.waitCertificate, .applied)
  | .waitCertificateRequest, .certificate => (.waitCertificateVerify, .applied)
  | .waitCertificateRequest, .certificateVerify => (.closed, .refused)
  | .waitCertificateRequest, .finished => (.closed, .refused)
  | .waitCertificateRequest, .newSessionTicket => (.closed, .refused)
  | .waitCertificateRequest, .keyUpdate => (.closed, .refused)
  | .waitCertificateRequest, .applicationData => (.closed, .refused)
  | .waitCertificateRequest, .closeNotify => (.closed, .applied)
  | .waitCertificateRequest, .fatalAlert => (.closed, .applied)
  | .waitCertificate, .serverHello => (.closed, .refused)
  | .waitCertificate, .helloRetryRequest => (.closed, .refused)
  | .waitCertificate, .encryptedExtensions => (.closed, .refused)
  | .waitCertificate, .certificateRequest => (.closed, .refused)
  | .waitCertificate, .certificate => (.waitCertificateVerify, .applied)
  | .waitCertificate, .certificateVerify => (.closed, .refused)
  | .waitCertificate, .finished => (.closed, .refused)
  | .waitCertificate, .newSessionTicket => (.closed, .refused)
  | .waitCertificate, .keyUpdate => (.closed, .refused)
  | .waitCertificate, .applicationData => (.closed, .refused)
  | .waitCertificate, .closeNotify => (.closed, .applied)
  | .waitCertificate, .fatalAlert => (.closed, .applied)
  | .waitCertificateVerify, .serverHello => (.closed, .refused)
  | .waitCertificateVerify, .helloRetryRequest => (.closed, .refused)
  | .waitCertificateVerify, .encryptedExtensions => (.closed, .refused)
  | .waitCertificateVerify, .certificateRequest => (.closed, .refused)
  | .waitCertificateVerify, .certificate => (.closed, .refused)
  | .waitCertificateVerify, .certificateVerify => (.waitFinished, .applied)
  | .waitCertificateVerify, .finished => (.closed, .refused)
  | .waitCertificateVerify, .newSessionTicket => (.closed, .refused)
  | .waitCertificateVerify, .keyUpdate => (.closed, .refused)
  | .waitCertificateVerify, .applicationData => (.closed, .refused)
  | .waitCertificateVerify, .closeNotify => (.closed, .applied)
  | .waitCertificateVerify, .fatalAlert => (.closed, .applied)
  | .waitFinished, .serverHello => (.closed, .refused)
  | .waitFinished, .helloRetryRequest => (.closed, .refused)
  | .waitFinished, .encryptedExtensions => (.closed, .refused)
  | .waitFinished, .certificateRequest => (.closed, .refused)
  | .waitFinished, .certificate => (.closed, .refused)
  | .waitFinished, .certificateVerify => (.closed, .refused)
  | .waitFinished, .finished => (.connected, .applied)
  | .waitFinished, .newSessionTicket => (.closed, .refused)
  | .waitFinished, .keyUpdate => (.closed, .refused)
  | .waitFinished, .applicationData => (.closed, .refused)
  | .waitFinished, .closeNotify => (.closed, .applied)
  | .waitFinished, .fatalAlert => (.closed, .applied)
  | .connected, .serverHello => (.closed, .refused)
  | .connected, .helloRetryRequest => (.closed, .refused)
  | .connected, .encryptedExtensions => (.closed, .refused)
  | .connected, .certificateRequest => (.closed, .refused)
  | .connected, .certificate => (.closed, .refused)
  | .connected, .certificateVerify => (.closed, .refused)
  | .connected, .finished => (.closed, .refused)
  | .connected, .newSessionTicket => (.connected, .applied)
  | .connected, .keyUpdate => (.connected, .applied)
  | .connected, .applicationData => (.connected, .applied)
  | .connected, .closeNotify => (.closed, .applied)
  | .connected, .fatalAlert => (.closed, .applied)
  | .closed, .serverHello => (.closed, .refused)
  | .closed, .helloRetryRequest => (.closed, .refused)
  | .closed, .encryptedExtensions => (.closed, .refused)
  | .closed, .certificateRequest => (.closed, .refused)
  | .closed, .certificate => (.closed, .refused)
  | .closed, .certificateVerify => (.closed, .refused)
  | .closed, .finished => (.closed, .refused)
  | .closed, .newSessionTicket => (.closed, .refused)
  | .closed, .keyUpdate => (.closed, .refused)
  | .closed, .applicationData => (.closed, .refused)
  | .closed, .closeNotify => (.closed, .refused)
  | .closed, .fatalAlert => (.closed, .refused)
-- END GENERATED

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
  cases s <;> cases e <;> simp_all [post]

/-- Application data is taken only on a connected connection. -/
theorem data_needs_connection (s : State) (h : (post s .applicationData).2 = .applied) :
    s = .connected := by
  cases s <;> simp_all [post]

/-- A `CertificateVerify` is taken only after a `Certificate`, and the state
that waits for it is reached only by one. -/
theorem verify_needs_certificate (s : State) (h : (post s .certificateVerify).2 = .applied) :
    s = .waitCertificateVerify := by
  cases s <;> simp_all [post]

theorem certificate_first (s : State) (e : Event) (h : (post s e).1 = .waitCertificateVerify) :
    e = .certificate := by
  cases s <;> cases e <;> simp_all [post]

/-- A client that refuses a message, or cannot handle one, is done: it
sends its alert and closes. -/
theorem refusal_closes (s : State) (e : Event) (h : (post s e).2 ≠ .applied) :
    (post s e).1 = .closed := by
  cases s <;> cases e <;> simp_all [post]

/-- The server's handshake messages are taken in the order RFC 8446 §2 lists
them: whatever the path, the states visit them in this order. -/
theorem finished_needs_verify (s : State) (h : (post s .finished).2 = .applied) :
    s = .waitFinished := by
  cases s <;> simp_all [post]

theorem wait_finished_by_verify (s : State) (e : Event) (h : (post s e).1 = .waitFinished) :
    s = .waitCertificateVerify ∧ e = .certificateVerify := by
  cases s <;> cases e <;> simp_all [post]

/-! ## The server -/

/-!
The server's machine. `ServerState` is named as RFC 8446 §A.2 names the states,
less what the server never does: it sends its whole flight on the ClientHello and
then waits for the client's `Finished`. `ServerEvent` is what arrives from the
client: one of the handshake messages a client can send, application data, or an
alert. In `serverTable`, `none` is a refusal: a `Certificate` or
`CertificateVerify` is refused since none was asked for, and `EndOfEarlyData`
since no early data was accepted.
-/

-- BEGIN GENERATED server state=ServerState event=ServerEvent table=serverTable post=serverPost applied=Applied
inductive ServerState
  | waitClientHello
  | waitFinished
  | connected
  | closed
deriving Repr, DecidableEq

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

def serverTable : ServerState → ServerEvent → Option ServerState
  | .waitClientHello, .clientHello => some .waitFinished
  | .waitClientHello, .closeNotify => some .closed
  | .waitClientHello, .fatalAlert => some .closed
  | .waitFinished, .finished => some .connected
  | .waitFinished, .closeNotify => some .closed
  | .waitFinished, .fatalAlert => some .closed
  | .connected, .keyUpdate => some .connected
  | .connected, .applicationData => some .connected
  | .connected, .closeNotify => some .closed
  | .connected, .fatalAlert => some .closed
  | _, _ => none

def serverPost : ServerState → ServerEvent → ServerState × Outcome
  | .waitClientHello, .clientHello => (.waitFinished, .applied)
  | .waitClientHello, .endOfEarlyData => (.closed, .refused)
  | .waitClientHello, .certificate => (.closed, .refused)
  | .waitClientHello, .certificateVerify => (.closed, .refused)
  | .waitClientHello, .finished => (.closed, .refused)
  | .waitClientHello, .keyUpdate => (.closed, .refused)
  | .waitClientHello, .applicationData => (.closed, .refused)
  | .waitClientHello, .closeNotify => (.closed, .applied)
  | .waitClientHello, .fatalAlert => (.closed, .applied)
  | .waitFinished, .clientHello => (.closed, .refused)
  | .waitFinished, .endOfEarlyData => (.closed, .refused)
  | .waitFinished, .certificate => (.closed, .refused)
  | .waitFinished, .certificateVerify => (.closed, .refused)
  | .waitFinished, .finished => (.connected, .applied)
  | .waitFinished, .keyUpdate => (.closed, .refused)
  | .waitFinished, .applicationData => (.closed, .refused)
  | .waitFinished, .closeNotify => (.closed, .applied)
  | .waitFinished, .fatalAlert => (.closed, .applied)
  | .connected, .clientHello => (.closed, .refused)
  | .connected, .endOfEarlyData => (.closed, .refused)
  | .connected, .certificate => (.closed, .refused)
  | .connected, .certificateVerify => (.closed, .refused)
  | .connected, .finished => (.closed, .refused)
  | .connected, .keyUpdate => (.connected, .applied)
  | .connected, .applicationData => (.connected, .applied)
  | .connected, .closeNotify => (.closed, .applied)
  | .connected, .fatalAlert => (.closed, .applied)
  | .closed, .clientHello => (.closed, .refused)
  | .closed, .endOfEarlyData => (.closed, .refused)
  | .closed, .certificate => (.closed, .refused)
  | .closed, .certificateVerify => (.closed, .refused)
  | .closed, .finished => (.closed, .refused)
  | .closed, .keyUpdate => (.closed, .refused)
  | .closed, .applicationData => (.closed, .refused)
  | .closed, .closeNotify => (.closed, .refused)
  | .closed, .fatalAlert => (.closed, .refused)
-- END GENERATED

theorem server_closed_absorbing (e : ServerEvent) :
    serverPost .closed e = (.closed, .refused) := by
  cases e <;> rfl

/-- The server is connected only because the client's `Finished` arrived
where it was waited for, or was connected already. -/
theorem server_connected_only_by_finished (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).1 = .connected) :
    s = .connected ∨ (s = .waitFinished ∧ e = .finished) := by
  cases s <;> cases e <;> simp_all [serverPost]

theorem server_data_needs_connection (s : ServerState)
    (h : (serverPost s .applicationData).2 = .applied) : s = .connected := by
  cases s <;> simp_all [serverPost]

/-- The client's `Finished` is taken only after the server sent its own
flight, which is what `waitFinished` means, and that state is reached only
by a `ClientHello`. -/
theorem server_finished_needs_hello (s : ServerState)
    (h : (serverPost s .finished).2 = .applied) : s = .waitFinished := by
  cases s <;> simp_all [serverPost]

theorem server_wait_finished_by_hello (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).1 = .waitFinished) :
    s = .waitClientHello ∧ e = .clientHello := by
  cases s <;> cases e <;> simp_all [serverPost]

/-- A client's certificate is never taken: the server asked for none. -/
theorem server_takes_no_certificate (s : ServerState) :
    (serverPost s .certificate).2 = .refused := by
  cases s <;> rfl

theorem server_refusal_closes (s : ServerState) (e : ServerEvent)
    (h : (serverPost s e).2 ≠ .applied) : (serverPost s e).1 = .closed := by
  cases s <;> cases e <;> simp_all [serverPost]

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
