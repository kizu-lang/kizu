// What a run of each side of the handshake has to keep. The machines are
// generated from std::tls; `just spec-quint` searches random runs of them, and
// fails if the runs never reach `reachedConnected`, since a search that stops short
// of the state a property is about says nothing.
module tls_client_properties {
  import tls_client.* from "./tls_client"

  val reachedConnected = state == Connected

  val notConnected = not(reachedConnected)

  val connectedOnlyByFinished =
    state == Connected implies (previous == Connected or (previous == WaitFinished and event == Finished))

  val closedStaysClosed = previous == Closed implies state == Closed

  val dataNeedsConnection =
    moved and event == ApplicationData and outcome == Applied implies previous == Connected
}

module tls_server_properties {
  import tls_server.* from "./tls_server"

  val reachedConnected = state == Connected

  val notConnected = not(reachedConnected)

  val connectedOnlyByFinished =
    state == Connected implies (previous == Connected or (previous == WaitFinished and event == Finished))

  val closedStaysClosed = previous == Closed implies state == Closed
}
