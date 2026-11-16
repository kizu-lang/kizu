\* What a run of the client side of the handshake has to keep. tls_client is
\* generated from std::tls; `just spec-verify` checks every reachable state with
\* TLC, and fails if notConnected holds, since a machine that never connects
\* would satisfy the properties about connecting.
---- MODULE tls_client_properties ----
EXTENDS tls_client

notConnected == state /= "Connected"

connectedOnlyByFinished ==
    state = "Connected" => (previous = "Connected" \/ (previous = "WaitFinished" /\ event = "Finished"))

closedStaysClosed == previous = "Closed" => state = "Closed"

dataNeedsConnection ==
    (moved /\ event = "ApplicationData" /\ outcome = "Applied") => previous = "Connected"
====
