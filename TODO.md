# TODO:

## Stage 1

- [x] Basic Tor connections.
- [x] Store messages.
- [x] Sign all local messages.
- [x] Reject all non signed and verified message.
- [x] Search and read messages.
- [x] Connection authentication.
- [ ] Security groups and flags.
- [ ] Identity group access control.
- [\] Control message;- delegate permissions/authority to post, delete, and delegate authority.
- [x] Control message;- sending.
- [x] Control message;- receiving and processing.
- [x] Control message;- Create group.
- [x] Control message;- Add peer.
- [x] Control message;- Remove peer.
- [\] Peering.

## Stage 2

- [\] Identity vcard support, so a node/person can call them selves something, and possibly redirect them to other nodes.
- [?] Support Distribution header (and require it for security, really?).
- [ ] Support Expires header (and require it for control messages).
- [ ] Support Supersedes header.
- [ ] Control message;- Subscribe to peer's group.
- [x] Control message;- Cancel /delete message.
- [x] Control message;- Add group identity/vcard.
- [ ] Control message;- Delete group.
- [ ] Control message;- Unsubscribe from peer's group.


## Extra Ideas

- [ ] Message size limits, per group, and accepted over the connection.
- [ ] Overall size policies.
- [ ] Group retention policies and server policies.
- [ ] Group content post policies (images/video etc).
- [ ] RAM backed ephemeral groups for chatting, possibly only allowing the subject line?.
- [ ] Reply only group policy (so people can make public posts, and others can reply).
- [ ] TLS/ssh connections over Tor, I know this isn't necessary, but maybe a good idea and useful for TCP comms, this could be a random public key exchanged in the handshake.
- [ ] Allow peers to connect locally over TCP, if you're on the same LAN. Such as a mobile phone to a laptop, desktop, home server or visiting friend.
- [ ] Use an arbitrary group (maybe define it), as a synced structured repository to hold vcard, and ical files, for external name recognition in news readers, and general address book, and a synced calendar server. These could be in private groups for personal devices, or shared for families and friends etc.
- [ ] A SMTP/POP3 interface so people can send email directly instead of using public servers.
- [ ] 
