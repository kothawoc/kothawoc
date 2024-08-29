# Kothawoc

An authenticated NNTP server, that only accepts authenticated peers.
It is based on the Tor network, uses node-ids as addresses, and the
included crypto keys for signing and authenticating.

This enables you to run a SPAM free p2p network with your friends and peers,
with accountability going to the source, who you may block.
All messages going into the network are signed by the source, if the signature
doesn't match it is dropped.
It enables you to expand on control messages, as they are authentic, and should
be honoured if they have the right permissions.
This can be group management, moderation etc and you can trust headers like
"Control", "Distribution", "Expires", "Supersedes" header will be honoured, if 
the "From" and "Approved" headers match. Then you can automate
their actions.

This is designed to work as a headless server node, with a GUI for easy management.
