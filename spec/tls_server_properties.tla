\* What a run of the server side of the handshake has to keep. See
\* tls_client_properties.tla.
---- MODULE tls_server_properties ----
EXTENDS tls_server

notConnected == state /= "Connected"

connectedOnlyByFinished ==
    state = "Connected" => (previous = "Connected" \/ (previous = "WaitFinished" /\ event = "Finished"))

closedStaysClosed == previous = "Closed" => state = "Closed"
====
