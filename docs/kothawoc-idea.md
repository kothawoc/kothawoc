# Kothawoc

## I don't like Bad Apple monopolies* like Google.
\- * *According to the American Government.*

## Idea

NNTP/Tor + Transparent security

Use the Internet, in the way the Internet was meant to be.

The Internet is a very centralised place, it was never meant to be. Big companies are dysoning/hoovering up everyone's information, and censoring those who they don't agree with. The Internet was meant to be open for all, but it is a SPAM and scam infested walled garden, only open to corporate ideas.

If only there was a system where you could have peer to peer forums, where you post messages and share them with friends and like minded individuals, whilst scaling, avoiding pitfalls of big tech, especially with privacy and moderation.

For sharing information, posts, there's already a simple protocol that exists. NNTP, why not? It's P2P, it does most stuff we need. It's well defined, and RFCd, it even handles intermittent connections, but there's the NAT issue, and people aren't going to run a NNTP server.

For NAT, well there's a simple solution to it. Lets just use Tor, it solves a lot of other problems too. Lets just not tell people as it has a bad reputation.

For SPAM and abuse, well there's social responsibility, you just allow your friends to connect. If your friend sends you undesired material, you may want to drop them as a friend, or report them to the authorities if it's that bad.

For servers, each client should work the same way, as an NNTP server, and offer anyone they connect to any groups they carry. Some people will agglomerate a lot of groups, choose to have high retention periods, and act as master nodes. People just don't think of it as a server, it's simpler than other p2p software people use.

Have the client display all of the feeds that a user subscribes to, like normal social media. Have a forum interface too.

## Aim

To create a user friendly discussion service which rivals the features of others, including some features of social networks, and forums to create a discussion platform for all, managed by the users and run by the users. It should be censorship resistant, yet be safe and easy to manage.
It will rely on social responsibility for the integrity of the network, and will be responsible for what they chose to host and how to deal with abuse. We should provide the tools, the participants will self censor.

## Implementation

### Stage 1

Create proof of concept:

* Create NNTP servers that talk over Tor.
* Add posting and propagation.
* Add posting rules
* Add private groups (effectively means multi-devices and PM too, like how Matrix works)
* Add subscriptions
* Add Basic UI

### Stage 2

* Market it, see if people are interested.

## Feature Ideas

* When the app is started, it connects to all their friends and ask for news lists.
* They then offer anything to other friends which they don't have.
* They can post to private groups to their friends as a private message.
* Anyone can host public or private groups.
* A user can chose whether to accept feeds for specific groups from friends.
* Add binary support, initially images.
* Video streaming capabilities, it could be a decent video CDN in the future.

## UI

The main page should be a feed of every public group they're subscribed to, chronologically newest at the top. Like a social media feed. Three should be a friends section and a subscribed groups section.
There should be an interface that's like either a forum or social media interface. There should be a discovery interface.
There should be a link to other posts, which can transcend groups, so introducing people to others public pages.
Hyperlinks to other content and groups should be supported.

Config:

There should be options on whether to accept all groups by default, or subscribed groups only.
Retention time/size for groups.
Caching policy for non-followed groups.
Private groups only for certain members, and never push, only allow pull.

## Authentication

Authentication is done with public key exchanges with signing. We know who's who according to the public keys we have.
We can extend this by having signed features for public keys, such as a group name they can access.

## Extended Protocol

When a client connects, they do a signature exchange handshake, so we know they are who they claim. This replaces other authentication.

There are special news groups, and messages that are used to control the service. This makes it very easy to use 3rd party NNTP clients.

The client ID is the Tor hash.


### Group Hierarchy

Maybe something like this. The only part of the hierarchy that is relevant is the client ID, which is integral.

* <client-id>.public.<group.name> -> A clients public profile. This is propagated.
* <client-id>.private.<group.name> -> private, only propagated to those with permissions.
* <client-id>.control -> ephemeral messages used for control.
* <client-id>.control.announce -> semi ephemeral messages propagated for global control, such as announcing new groups, or modifying them.

### Messaging Protocol

NNTP already supports the "Control:" header and control messages. We'll keep using them, however we'll require extensions.

#### Control message expiry

With the "Expires" header, you can make all control messages disapear. This way stale groups will expire, and they need to be updated with a "Supersedes" header to continue operation. This can easily be automatically managed by the client.

#### Restricted distribution messages

With the "Distribution:" header, we can use that to control group distribution, and we can use the clients public key to determine who's allowed to read/post. Is this even needed or helpful? Can't we just rely on groups?

## Reasoning

The network is built up of social links between the participants. 

### Bad forks (and good ones)

Hopefully we get lots of good forks and new implementations, however the rules need to be followed. If people break stuff, if the community doesn't like it, it will just drop them. Hopefully it'll be self policing.

### Where the RFC went wrong

Section 1.3 of RFC977 got it totally wrong (well it is wrong now), we don't want the centralised storage of news. We don't need it, storage is huge, bandwidth is cheap and connections are fast. Because of this, we lost out with centralised proprietary services.

### Financing

Whilst the big social networks need a lot of money, this is essentially free. The costs are the resources on the user devices and how much they decide to allocate, development costs and the Tor network.
There is no profit incentive or monetisation model for this service, which means that it doesn't need to collect your personal data or sell adverts. The idea here is that companies, governments and other organisations can't control what is on the network, or control your personal data.
If it becomes popular, incentives will appear to pay for development, and enhance the Tor network.

#### Commercialisation

There's no reason why it can't be used for commercial reasons. There's nothing to stop people from building special systems for e-commerce, or having paid for groups, where the only members are the ones that have paid.

### Disk/Storage usage

Most of the storage space used by companies to host systems is dedicated to support and monitoring. Generally only a small amount is used for data, unless video/images are involved. Given that modern games regularly clock in at 100GB and regularly surpass 200GB, it's reasonable to take up that amount of space for storage. We don't need to worry about storage space.

### Bandwidth usage

Unless it turns into a binaries service, given that people use YT, netflix, prime, etc all the time, the bandwidth usage will be miniscule in comparison.

### Sharing newsgroups

By default, it should share all the newsgroups from peers (starting with their ID), and all group control messages. The user can then choose to subscribe to any more groups their peers advertise. The user will by default advertise all groups as active or inactive.
A client can chose which groups they propagate and block.
A super spreader may choose to subscribe to all groups and make them available instantly.

### Minimising on data transfer and non real-time routing

Group control messages can be propagated to all clients, this way they know about a group, and will get the "Path" header. If a node knows about a newsgroup, it can advertise it and mark it as inactive. If a user tries to access that news group, it can proxy requests to where it found out about the group.
If a user subscribes to the group, the group will become active and request a feed from the upstream(subscribe to it), and store the data. This will happen up the line until the source is found.

### Real time Routing, like BGP ?

We get a soft routing pattern from any messages we receive, however there may be multiple routes between two people. If the main route goes down, you'll never see the other routes.
By being a member of the network, you're effectively announcing your presence to anyone in the network listening. Adding some routing like BGP would allow you to easily find a path to another should somebody in your friendship chain disappears.

### Self censorship and freedom of speech, SPAM and criminality

We don't want the network to be criminal, we want it to be open and foster discussion. Laws are different in different places and people have different values and morals.

Because everyone knows everyone through their mutual connection tree, there will be jurisdiction issues where some things are illegal in some places but not others. You may chose to moderate the content that passes through your systems to avoid breaking the law, or you may just ignore it. You have to choose who you connect to and share those posts with, so don't connect to people wanting to spoil everybodies fun.

SPAM shouldn't be a problem, because if I'm getting SPAM from you, I'm going to drop you as a friend. If they don't want to be dropped by you, they'll either drop their upstream source of SPAM, or get them to drop their source.

### Complexity, Simplicity and a codebase

The idea is to KISS, this is a POC, and as little code should be written to build the basic system as possible. It should be simple and easy to replicate in other languages, and use existing standards so existing libraries can be re-used, there's no invention necessary, and easy to understand.

### Scaling

The architecture is designed to be highly scalable, as in mostly work on the tiny to small scale using few resource, but be able to expand to the big scale if required. If it ever gets to the massive scale, I expect there'd be some help.

The assumption is that most groups and discussions will likely be held in a fairly localised environment. Thus, if we modify the server to allow lazy groups and posting, we can have a full group list and ignore 99% of the content. Just taking control messages so we know groups exist.

If a user wants to access a post on a remote server, they can ask their local server, which will proxy the request down the chain until everyone has a copy of the data. That data is then cached, so each friend will only ask for the data once.

This causes data fanout, and is highly scalable.

If a user subscribes to a feed, it will ask the friend who introduced the feed to provide it. They will then ask their source and so on, until the hosting server provides a feed to their clients. This will provide fast propagation of data to all the clients who are interested.

### Bots

Yes please, the system is simple enough that it should be fairly trivial to write bots, and help to improve it. 

### Meritocratic Democracy

People vote for the best idea by using it, and it wins.

## Branding

I think it's important to brand this as community groups, rather than an anonymous free for all criminal laced hell-scape, as is associated with Tor. The facts don't really matter, peoples perception does. And when they realise it's actually not that bad, they may appreciate it more.

It should be marketed as relatively safe, as it should be, considering everyone knows everyone on the network, and it should be self policing.

It should be marketed as free community discussions, hosted locally by the community, free from interference by big tech.

Try to put in place safety features, such as instructions on how to deal with illegal content.



### Marketing Technical

NNTP is an old and simple Internet protocol, and a lot of modern social media works in a similar but centralised way. You could consider it one of first CDNs.

NNTP has most of the properties used in social networks. It is designed to be P2P, the weakness of NNTP was authentication, and SPAM.

Today bandwidth and storage is cheap, meaning most people could run an NNTP server from a RaspberryPI hanging off WIFI if they wanted to.

Using TOR to bypass NAT, and to authenticate, you can just authenticate friends, and have a closed private network. As groups enlarge, network will join together. This creates a P2P social network which is censorship resistant.

SPAM and abuse control is provided by social contract. If a node SPAMs and a downstream node forwards their articles, downstream nodes will be pissed off at the offender. Upset nodes will stop the upstream node, or drop them, so they don't get dropped by a downstream node.

Providing a simple familiar GUI, to exploit the strengths of NNTP and USENET, we can give a different, but in many ways superior experience to other services. Integrating an NNTP server, and Tor connectivity without the user needing to know.

Stick to the standards, with as few additions or deviations as possible, though not necessarily implementing them all.

Easy to replicate, hack and improve. Hopefully with the simplicity of it all, people will make their own better clients.

Exploits the current AI trend by **NOT** using it or advertising it as a service. If it ever gets used, it'll be for something sensible, not marketing.


#### Summary

Create an infrastructure-less P2P NNTP network over Tor, using Tor for authentication with a built in GUI and enhanced peering.

### Marketing Social

A private and free social network for you and your family and friends. No payment required, no MEGACORP servers hosting your private information and harvesting it. No mega brains or complex setups required to use it. Just install it and connect to friends.

Post your news, updates and messages, and automatically feed it to your friends, or friends of friends who're interested. Don't connect to people who you don't trust or like.

Don't get reported to the police for a private message, have private groups without those sort of people, and be anonymous to strangers.

Doesn't include AI crap.

#### Summary

A private social network without the spying or adverts. Designed for use not profit.



## Terminology

All of these are used interchangeably and mean basically the same for our purposes: node, server, host, device, computer, user, client, software.
They may refer to different aspects, to make topics more understandable, however every users piece of client software is a host device, a server, a node in the network, whether it's a computer or mobile. That device is always managed by a person.
