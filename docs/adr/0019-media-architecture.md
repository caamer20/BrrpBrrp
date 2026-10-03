# ADR 0019 — Media architecture: extend the embedded SFU, replace its publication model

**Status:** decided, **conditional on a measurement** (see §6)
**Revises nothing.** Extends ADR 0009 (authority ordering) and 0003 (roles).
**Basis:** the M4 architecture spike, cited throughout as *file:line*.

## 1. What exists

BrrpBrrp already has a real in-process SFU for community voice, and it is **hub-and-spoke,
not a mesh**: clients never peer with each other. Each account gets exactly one
server-side `PeerConnection` (`internal/rtc/sfu.go:145`), and a second tab is refused
outright (`:123-125`).

It is not, however, a *general* SFU. It is a pre-allocated, fixed-size Opus mixer:

- one `recvonly` audio input for the publisher (`internal/rtc/sfu.go:151`);
- **nine pre-created `sendonly` output tracks** named `voice-0 … voice-8` (`:156-157`),
  which the client mirrors positionally (`web/src/lib/voice.svelte.ts:369-371`);
- Opus is the only registered codec (`:101`);
- `OnTrack` hard-rejects any non-audio track and any *second* track (`:199`);
- `Signal` accepts an offer only when no remote description exists yet (`:275`), so
  **there is no renegotiation at all**.

Everything runs in the one binary: HTTP, WebSocket, SQLite, the web bundle, the SFU and a
TURN relay (`internal/api/router.go:171,177`; `Dockerfile:44` is `FROM scratch` with one
volume).

## 2. What is missing, and what is structurally impossible today

| Capability | State | Evidence |
| --- | --- | --- |
| Community voice | Present and tested | `internal/rtc/sfu.go`, `web/e2e/voice.spec.mjs` |
| **Private calls** | **Structurally impossible** | `internal/api/voice_access.go:118` rejects anything whose channel `Type != "voice"`; a private conversation is a `conversations` row with `channel_id IS NULL` (`internal/db/migration11.sql:49-50`) |
| **Screen video** | **Absent** | no `getDisplayMedia`, no `RTPCodecTypeVideo`, `video:false` at `web/src/lib/voice.svelte.ts:172,577` and `web/src/lib/components/AudioSettings.svelte:68` |
| **Screen audio** | **Absent** | same |
| TURN/relay | Present, **IPv4/UDP only** | `internal/rtc/turn.go:58,129` — `turn:…?transport=udp`, udp4 listener |
| **UDP-blocked fallback** | **Absent** | no `transport=tcp` is ever offered |
| Renegotiation | Absent | `internal/rtc/sfu.go:275` |

Two findings deserve their own weight:

**Private calls are not "a feature to add" but a structural gap.** The single admission
path resolves a `db.Channel` and refuses anything that is not a voice channel
(`voice_access.go:118`), and audience resolution goes through `channelAudienceIDs`
(`internal/api/authz.go:560-570`). A private call needs the *membership-based* resolver
that already exists at `internal/api/conversation_access.go:52-73`. Both paths are in this
codebase; only one is wired to media.

**`voice.share_screen` already exists and is enforced by nothing.** It is declared
(`internal/authz/authz.go:35`), catalogued (`:69`), seeded into three roles
(`internal/db/migration3.sql:89,92,95`) and described to users as "Publish a screen or
window" (`web/src/lib/permissions.ts:37-41`) — but no code outside tests reads it. The
product currently promises a capability it does not have.

## 3. The decision: extend, do not replace

**Keep the embedded Pion implementation and rewrite its publication model.** The
alternative — a separately self-hosted SFU — was considered and rejected for reasons that
are visible in the code, not by preference:

1. **The control plane is the valuable part, and it is already built and tested.**
   Per-channel publication limits (`voice_access.go:64-83`), resume-token ownership
   (`internal/rtc/rooms.go:68-110`), TURN credential issuance *and revocation*
   (`internal/rtc/turn.go:113,135`), non-cooperative speaker enforcement at the RTP
   boundary (`internal/api/voice_capability_test.go`), and moderated rooms
   (`internal/rtc/sfu.go:351-363`) all live on the same side as the RTC machinery. An
   external SFU would mean re-implementing every one of them against a network boundary,
   and each re-implementation is a new way for authorization to disagree with itself.

2. **The deployment model is a selling point.** One binary, one volume, `FROM scratch`,
   no second process to install, patch, back up, monitor or explain to an operator. That
   is most of what makes the product approachable, and giving it up for a general SFU
   would trade the project's strongest property for features the current code can be made
   to have.

3. **The gap is in the media core, not in the deployment shape.** What is missing —
   track identity, renegotiation, video, transport fallback — is all *inside*
   `internal/rtc/sfu.go`. Replacing the process does not avoid rewriting that logic; it
   adds a network hop to it.

The cost of this decision is stated plainly: **an in-process SFU shares fate with the
application.** Media load competes with SQLite, HTTP and WebSocket for the same CPU, and
there is no independent way to scale or restart the media plane. That is acceptable at
the project's support profile and would not be at a much larger one.

## 4. The rewrite: a typed publication model

The fixed `voice-0..8` slot array is the thing that has to go. It is not a limitation;
it is an assumption that a participant has exactly one track, forever, negotiated once,
in a fixed order. Screen sharing breaks all three at once.

Replace it with:

- **A publication is a first-class object** with a stable id, an owner, a kind
  (`microphone` | `screen_video` | `screen_audio`), and a lifecycle. A screen track is
  never stored in a variable named for audio, so it cannot masquerade as microphone
  state.
- **Renegotiation is allowed**, and is the only correct way to add a track to a live
  call. The current one-offer gate (`sfu.go:275`) is replaced with a serialized
  negotiation queue per participant, so two simultaneous adds cannot interleave.
- **A participant may hold several publications at once** — a microphone and a screen —
  and each is forwarded, muted and revoked independently.
- **Bounds are per publication kind and serverwide**, so "one screen share per room, two
  per server" is expressible and enforceable rather than a count of slots.
- **Two clocks and one direction are separated**: the current forwarder rewrites
  sequence and timestamp with hard-coded `48000/960` audio constants
  (`internal/rtc/sfu.go:439-451`). A video publication has a different clock rate and
  must not inherit audio's, and the implementation has to refuse a rewrite it cannot
  perform correctly.

## 5. Private calls: same machinery, different admission

A private call is not a second media stack. It is the same SFU, the same publications and
the same TURN, behind a different admission decision:

- the room key becomes a **conversation id**, not a channel id;
- admission uses `conversationAudienceIDs`-style membership resolution
  (`internal/api/conversation_access.go:52-73`) rather than `channelAudienceIDs`;
- the call requires *current* membership, re-checked in the transaction, exactly as a
  private message does — so removing somebody from a group ends their call the way it
  ends their read access, not on a timer;
- ringing is an ordinary activity source through the mechanism ADR 0015 already defines,
  so a private call reaches its recipient's inbox and their notification preferences
  rather than needing a new delivery path.

**A call is not a channel.** `internal/db/migration11.sql:49-50` already forbids a
message from carrying a private conversation's scope on a channel row; the same
separation applies to media, and a private room must not be reachable by guessing a
channel id.

## 6. The measurement this decision is conditional on — now made, and what it changed

`docs/v1-implementation-plan.md:812-815` records the arithmetic: two 2.5 Mbit/s screen
shares forwarded to nine viewers is roughly **45 Mbit/s of egress** from one process that
is also serving HTTP, SQLite and WebSocket. The plan also says: "Do not label this
arithmetic a load-test result."

**The measurement has now been made** and is recorded in
`planning/m4-checkpoint/video-forwarding.json`. One publisher to nine viewers at
2,500 kbit/s for fifteen seconds on a sixteen-core development host: 36,369 packets
forwarded, **mean egress 23.28 Mbit/s** against the plan's arithmetic of 22.50 for the
same combination, peak server heap 37.4 MiB, mean 1,114 goroutines. The measured rate
lands 3.5% above the estimate, which is what a path that copies every packet should
look like.

**The decision stands for audio and for the forwarding shape of video, on three
grounds that the measurement supports:**

- The forwarding cost is not a bottleneck at this scale. Twenty-two Mbit/s through one
  Go process's RTP path, with the heap under forty megabytes, leaves the HTTP, SQLite
  and WebSocket work room on the allocation the plan names.
- The measurement is the *server's* work, which is exactly the work an external SFU
  would move off this host. So the measurement is a reason to keep the work here.
- Nothing in it favours a different deployment: an external SFU would add a network
  hop and a process to do work this one is measured doing cheaply.

**Two capacity gaps it found, which are targets in the plan and not limits in the
product yet:**

- `MaxParticipants` bounds the **whole server** at ten. The plan targets twenty
  serverwide, at most ten in one room. Two publishers with nine viewers each is
  therefore not reachable today, and no amount of forwarding headroom changes that —
  it is a bound, not a cost.
- ~~A client cannot currently ask to *watch* a screen.~~ **Fixed.** A second offer
  that adds only a receiving track is now negotiated without a publication, and a
  viewer is not recorded as a publisher or charged against the room's screen bound.

**How the viewer is told from a publisher.** It could not be read from the offer, and
two attempts failed before this one, both of which are worth recording because each
looks correct:

- A transceiver created with `AddTransceiverFromKind` produces a media section with
  **no direction attribute at all**, which the SDP default makes `sendrecv`. A viewer
  and a publisher therefore produce byte-identical descriptions, and any check that
  reads the text is reading nothing.
- Pion **reuses a same-kind transceiver** rather than creating a second one, so a bare
  `recvonly` video offer matches the server's existing `sendonly` screen output and the
  direction stays `sendonly`. This is why the first attempt concluded that every viewer
  was a publisher.

What works is the **negotiated transceiver list**: a viewer's own track arrives on a
separate `recvonly` video transceiver, distinct from the server's output. A client marks
its intent with a stream id (`ViewStreamID`), and the server reads the marker off the
track — because a description is validated against the offer that produced it, so the
marker cannot be written into the SDP after the fact.

The refusal is kept for everything else. An offer that names no publication and no
marker is still refused, so the receive path never becomes a way to publish by
omitting a field.

**What the measurement does not establish**, and says inside the report itself:
synthetic RTP in one process, so the forwarding path rather than an encoded video
pipeline, a network, a browser or a codec; loopback, so no NIC and no TURN relay; no
decoder, so nothing here says a picture arrives intact; and **not on the reference
allocation** (four cores, 8 GiB) or on either required deployment. A rerun there is
required before the video profile is qualified, and it is listed with the host
qualification in `docs/host-qualification.md` §3.

Two things cannot be measured here and are not claimed:

- **Physical media quality.** The only recorded workload is 10 local Pion peers sending
  60-byte synthetic payloads, peak combined heap 45.44 MB, explicitly not physical audio
  or cross-network (`docs/acceptance.md:127-130`). Synthetic RTP proves the forwarding
  path; it proves nothing about a microphone or a lossy path.
- **The browser CI path.** `web/e2e/voice.spec.mjs:41-43` monkey-patches `getUserMedia`
  to audio-only and *throws* on a video constraint, so no video publication is exercised
  anywhere in CI. Browser lifecycle, capture denial, and a 100-cycle start/stop run are
  physical-device work.

## 7. What TURN needs

`internal/rtc/turn.go` is IPv4/UDP only: a `udp4` listener (`:58`) and a URL forced to
`transport=udp` (`:129`). A client behind a network that blocks UDP currently has **no
path at all** — which is the ordinary case on corporate and some mobile networks.

- Offer `turn:…?transport=tcp` alongside the UDP URL, and let ICE choose.
- Add `turns:` (TLS on 5349) for networks that block the TCP fallback too.

**Both done**, and the second one was written and then never switched on.
`ConfigureWithFallback` already took a TLS port and a certificate and built a
`tls.Listen` for them; the call site passed a zero port and a `nil` certificate, so
the transport was unreachable. A client behind a network that blocks both UDP and
plain TCP had no path, with the code to give it one sitting unused beside it.

The TLS transport reuses the server's own certificate — the one the HTTPS listener
already serves. It is offered only when that certificate is actually readable: a
`turns:` URL with no certificate hands the client something it cannot connect to, and
ICE reports that as a network fault rather than a misconfiguration. When it is
requested and the certificate cannot be read, the relay is still offered over UDP and
plain TCP and the reason is logged. A fallback that silently does not exist is the
failure this exists to prevent.

**A field that was named for the wrong transport.** `turn_tls_port` has always held
the *plain TCP* port. The name is kept rather than fixed, because renaming a JSON key
silently resets an operator's saved setting to the default — and for a port that is a
relay that no longer listens where it did. The TLS listener has its own field,
`turn_tls_listener_port`. The code says which is which, so the next reader is not sent
looking for a TLS listener behind a TCP port.

All three listeners are distinct, and the validation refuses a collision: UDP, plain
TCP and TLS cannot share a port, and a setting that validates and then fails to bind
at startup is discovered at the worst possible moment. The validation and the listener
share one helper for "which port will this bind", because two copies of that question
is how they come to disagree.
- Make the advertised relay address **configurable**, because a container or a host
  behind NAT that advertises a private address produces a call that works locally and
  fails for everyone else.
- Record the **actual candidate type chosen** per client. "Tailscale and HTTPS both
  worked" says nothing about whether media established.

**Done.** On connecting, the server reads the nominated candidate pair once and logs
the local and remote types with a `relayed` boolean. The types are reported by name —
`host`, `srflx`, `prflx`, `relay` — because that is the token an operator will search
a log for, and an id nobody can resolve after the fact is not a measurement.

The read is a single settled lookup, not a poll: the pair is chosen at connect time and
does not usefully change afterwards. A peer with no connection yet reports that rather
than reading statistics from nothing — the caller is a logging goroutine, and a panic
there would take the server down over a diagnostic.

This closes the gap §7 named for TURN specifically: an operator can now tell a
deployment whose calls went direct from one whose calls quietly relayed, which is the
difference between working and working-until-the-direct-path-changes.

The existing credential machinery — 24-hour HMAC credentials with `RevokeUser`
(`turn.go:113,135`) — is sound and stays.

### 7.1 Two settings this ADR required and the code did not have

**A vendor STUN server was hardcoded.** Every call was offered
`stun:stun.l.google.com:19302`. In a product whose promise is that it needs no vendor
account, that sends a call's address discovery to a third party, and on an
air-gapped or offline installation it points at a name that cannot resolve — so the
call has no path and the failure presents as a network fault.

`stun_server` is now an operator setting and **empty by default**. A server with no
STUN server configured offers an empty list rather than reaching for one. The setting
is validated where it is saved, because ICE reports an unusable server as a network
fault: a typo saved today would otherwise present as a call that sometimes fails
tomorrow, which is the worst shape for a setting documented as optional.

The validation has one subtlety worth recording, because the obvious version is
wrong in both directions. `stun:host:port` is an *opaque* URL — everything after the
scheme is the address, so `url.Parse` leaves `Host` empty. Checking for a host
rejects every correct value, while `stun.example.com:3478` parses with the hostname
as its *scheme* and would pass a scheme-only check. What distinguishes a real one is
the scheme, a non-empty remainder, and a port.

**There was no way to force the relay.** A direct path and a relayed path are
different products, and without a switch a passing call never says which of the two
carried it. "Tailscale and HTTPS both worked" is not evidence that media
established — which is exactly the point §7 makes above, and it applies to the relay
too. `force_relay` sets the client's ICE transport policy to `relay`, forbidding
direct candidates so every packet goes through the server.

It is off by default: relaying everything costs the server bandwidth and adds
latency to a call that had a direct path. It is server-wide rather than per-call
because it is a statement about the network, not about one conversation. It requires
a configured relay — forcing a path with nothing to force onto would leave the client
with *no* path at all, which is worse than the direct path it was giving up — and
that requirement is enforced where the operator changes it rather than discovered at
the next call.

### 7.2 A removed member was still published in the room

`leavePeer` removed the peer from the map and closed its connection, and never
touched the room's publication registry. So a member whose authority was withdrawn —
removed from a group, banned, deactivated, erased — kept a publication in the room.

That is not a cosmetic leftover. The room's bound is counted from that registry, so
the removed member's screen kept consuming the one-screen allowance and refused
**everybody after them**: the room stayed full for a share that ended the moment its
owner was removed, which is the same failure as the one-stop case above, reached by a
different route.

Withdrawal now happens under the same lock that removed the peer, so nothing can
observe a member who is gone and still published. The room is told only when
something was actually withdrawn — a member who left with nothing sharing does not
make the room re-render a screen nobody was watching.

Withdrawal is **scoped to the departing peer**. Clearing the room's registry would
pass the obvious test and break every call in it, so the retained-member case is
asserted directly: after one member is removed, the other's publication survives and
their connection is untouched.

Every lifecycle path already called `leaveVoice` — session close, sign-out,
deactivation, ban, erasure, group removal — so this sits on the live revocation path
rather than beside it.

### 7.3 Reading a room created one

`s.room(id)` returned a registry for any id and stored it. Two call sites were
*reads* — "what is in this room" and "does this room still have a screen" — so every
lookup for a room that did not exist left a permanent empty map behind, and nothing
ever removed one.

The per-packet read is the one that matters. "A stopped screen has nothing to forward
to" is checked on **every packet**, so that one is bounded by the number of rooms
rather than by nothing at all — but it is the same mistake, and the read-and-create
version is the kind of thing that gets copied into the next accessor.

The two questions are now separate functions. `room` makes a room; `existingRoom`
reads one. The distinction is one word at every call site, which is the right size
for a mistake this easy to repeat.

### 7.4 Forced relay was half an instruction

`force_relay` was implemented on the client and on nothing else. Telling a browser to
use only the relay does not make the call use only the relay: the server was still
offering direct candidates, and ICE is free to pair a relayed client with a direct
server. The call then runs peer to peer — which is fine, and is what the operator was
trying to *avoid*.

The failure this produces is the exact one the setting exists to prevent, and it is
invisible from the client: calls work for the operator and fail for everybody else,
on a network the operator switched the setting on *because* direct paths did not work
there.

Proven with a real call. Two peers configured `ICETransportPolicyRelay`, a real TURN
relay in the same process, real allocations and real RTP. Before the fix the
connecting call was carried by `host`/`prflx` — a direct path — and the test failed
naming it. After, it is `relay`/`relay`. This is the first evidence in the project that
the relay path carries a call at all; everything before it established that the relay
*starts* and issues credentials, which is not the same claim.

**And then the fix itself was half an instruction.** Removing the hardcoded public
STUN server left the *server* with no ICE servers at all, while forced relay forbids
direct candidates on both sides. The engine had nothing to allocate from, so a
forced-relay deployment would have refused every call it was switched on to enable.

The engine is the other end of the call. It needs the relay for the same reason the
clients do, and it needs its own credentials — issued for a fixed internal account
rather than for a member, so revoking one person's voice does not take the server's
own path out. Both halves need the relay: the client's to reach it, the server's to be
reachable through it. The operator's STUN server reaches both, and turning the relay
off removes it from both, because a stopped relay that is still offered is a URL the
engine cannot connect to and ICE reports that as a network fault.

Two more things the same work found:

- **The setting never reached the media engine.** Saving it reconfigured the relay but
  not the SFU, so it would display as active while the server kept offering direct
  candidates. It is applied on save now, in both directions — an operator who turned
  it on to test a network needs to turn it off without a restart.
- **Turning it off was never possible.** There was no read path and no live update.

`SetForceRelay` does **not** tear down existing calls. A transport policy is how a new
connection is offered; changing it mid-call would drop a working call to move it onto
a path not yet shown to work, which is the opposite of the request. Calls already up
keep the path they negotiated; the next call gets the new policy, and the
candidate-pair log line is what confirms it happened.

### 7.5 One more defect this work found in passing

`ice_servers` was a nil slice, which marshals to JSON `null`, not `[]`. The client
iterates that list, so `ice_servers.map(...)` threw a `TypeError` and the call failed
with a client-side error naming nothing an operator could act on. It is now an empty
list, which is the honest encoding of "no servers to offer".

## 8. What this ADR does not establish

- That any of it works. It records a decision and its basis; the evidence comes after
  the implementation.
- That the support profile is achievable. The egress arithmetic in §6 has to be measured,
  not assumed.
- That screen sharing is safe on any platform. Browser capture permission, the
  indicator, and what happens when a track ends without a clean stop are browser-lifecycle
  questions, and they are answered on real devices.
- That calls work on the two required deployments. That is §3 of
  [host qualification](../host-qualification.md), and it needs hosts this environment
  does not have.

## 8a A call is a live relationship, and history is not the question

A private conversation's read admission is deliberately **retained** history: a
blocked member can still read what was said before the block, because erasing the past
on request is not what a block is. That decision was being reused as the admission rule
for a *call*.

Which produced two defects, both reproduced (completion review 14, V14-07):

**A blocked pair could open a call.** The message POST returned 403 and the call was
admitted. A block is the user saying "do not contact me", and a call is the most
immediate contact there is; the two disagreeing about the same block is the defect, not
a subtlety. A call now asks the block question in both directions — the caller's own
block list, and the block lists of the other accepted members, because a block is
recorded against the blocker and a conversation has two sides.

**Revalidation evicted every authorized private participant.** The enforcement pass
that runs after any authority change looked the room up as a *community channel*
unconditionally. A direct conversation has no channel row, the lookup failed, and every
member of every private call was dropped — by a moderator renaming a channel, or a role
being granted three rooms away. Reproduced: two participants in a live direct call, one
unrelated change, both gone.

Both were the same mistake: the call path and the message path asked different
questions about the same relationship, and the enforcement pass asked a third. The
enforcement pass now resolves the room through the *same function that admits a join*,
so the two cannot disagree — and because that function asks about blocks, a block
applied while two people are already talking ends the call too. Admission and
revalidation are the same question asked at two moments; answering it at only the first
means "the block works" holds only for people who have not already joined.

## 9. Screen audio is a separate publication, and it is best-effort by construction

The server has carried `screen_audio` as a distinct kind since §4. Nothing produced
it: `requestScreen` could ask for it and the voice controller never did, so a person
sharing their screen had no way to include the sound their computer was playing.

It is a **separate publication** rather than a second track inside the video's offer,
because the room's bound is counted per publication. Audio inside the video's offer
would be counted twice against the screen's allowance, and a share that is within
bounds would be refused — reported to the user as "the room is full", in a room that
is not full. The stop path withdraws it separately too, for the same reason: stopping
the video does not remove an entry the video never owned.

**Best-effort, and visibly so.** Windows and Linux commonly cannot capture system
audio, and a browser that cannot is expected to return video-only rather than fail. So
audio is requested and never required, a failure to add the audio transceiver leaves
the video publishing rather than failing the share, and the person sharing is told
which of the two they got. Silence is the right answer for the ordinary case — a
notice on every share would train people to dismiss notices, which costs the ones that
matter.

The promise stays bounded: system audio is not available everywhere, and nothing here
claims it is. A share of a muted screen is a working share, and the message says
exactly that rather than promising sound the platform cannot deliver.

**What this does not establish.** No real system-audio capture has been observed. The
capture path, the separate publication, the bounds and the messaging are all covered;
whether a given browser and operating system actually hand over the audio is the
device qualification in §8, and it is not something a synthetic stream can answer.

## 10. A microphone that did not start now says which one of six things happened

WP-19 asks for accurate device diagnostics. There were none. Every failure from
`getUserMedia` produced one sentence — "check browser and device microphone
permissions" — regardless of cause, and the settings list reported a failed
enumeration as "your browser could not list microphones".

Those are the wrong words for almost everyone who reads them. Somebody whose headset
button latched muted, or whose stored device was unplugged last week, has *already*
checked the permission. Reading it again teaches them that this message is not worth
reading, and the next time it matters they will skip it too.

Six causes, six sentences, and they are told apart by the platform's own error name
rather than its message — messages differ between browsers and between versions, so
matching on them produces a diagnosis that changes with an unrelated update:

| Cause | What the reader is told |
| --- | --- |
| Insecure page | Needs HTTPS, or localhost. Diagnosed *first*, because a refusal here would otherwise read as a permission problem they cannot change. |
| No media API | The browser has no microphone access at all. Not a permission — a capability. |
| Permission denied | Where to allow it, in the browser's own site settings. |
| No input device | Connect one, or choose a different input. |
| Device unavailable | The chosen device is gone, and — because the choice is remembered — it stayed selected after being unplugged. |
| Device in use | Another program has it; a meeting app left running is the usual cause. |

Two properties are asserted rather than assumed. **No two diagnoses share a sentence**,
because two causes with one message is a message that is wrong for one of them and the
reader cannot tell which. And **every diagnosis produces words**, so a new browser
error degrades to a generic sentence rather than to a blank panel.

The settings list now uses the same enumeration, and distinguishes *"your browser
withholds names until you allow the microphone"* from *"this machine has no
microphone"*. An empty device list shown silently reads as a product that cannot see
the hardware; those are different problems with different fixes.

**What this does not establish.** None of this observes a real device. It reads the
name the browser attaches to a failure and produces a sentence. Whether the
microphone then works, and what it sounds like, is §8's device qualification and cannot
be answered from a test runner.

## 11. The hundred share start/stop cycles — the regression half

The master plan asks for a hundred share start/stop cycles. On hardware that is also
an observation of media quality and of the browser's own capture lifecycle, and it is
not repeatable in CI. What *is* repeatable is the part that actually breaks: whether
a stop releases the room.

So the hundred cycles run here, through the production `renegotiate` and
`PublicationStopped` paths, with a real client peer applying each answer. Each cycle
asserts the room's count, the stop, the count again, and — the property that matters —
**that the room is shareable again immediately afterwards**. A stop that leaks an
entry leaves the room full for the rest of the call, so the hundredth person is
refused by the first person's share. The last assertion is that the connection is
still usable: a hundred cycles that leave a connection which cannot carry a call have
not passed.

Confirmed to fail on cycle 1 when the stop is made a no-op, which is the point of
checking the count after *every* cycle rather than at the end.

**What this does not establish.** Nothing about capture, picture quality, latency, or
the browser's own behaviour across a hundred real prompts. A stop that leaks a
transceiver on the *client* — rather than a publication on the server — would not be
seen here, because the client half is a test peer. That is the device observation,
and it is still open.

## 12. Authority withdrawal now reaches the browser, and takes the capture with it

WP-21 asks for authority withdrawal to "clear capture and indicators for cooperative
clients". The server cleared its half and the client had no idea.

`SetSpeakAllowed` marked the peer muted and stopped forwarding — which is the half it
can enforce alone — and said nothing. So a moderator withdrew a member's permission
and the browser kept its sharing indicator lit, kept a rectangle of that person's
desktop in memory, and kept publishing a screen the server was discarding. From where
they sat, nothing had happened: the app looked exactly as it did while it worked.
The browser's own capture prompt was the only place the truth was visible, and it
shows *"you are sharing"*.

The server now withdraws the peer's publications from the room's registry and emits
`voice.authority_changed` with a reason. The client releases the capture before it
shows anything, so the browser's indicator goes off even if the rest of the handler
fails.

Withdrawn means **withdrawn, not removed**. A member who may no longer share is still
in the call and still hears it; a fix that dropped them from the room would satisfy the
obvious test and break every moderated channel. Both halves are asserted: the share
goes and the reason arrives, and the person stays.

Whether the publication is withdrawn is read from the room's registry rather than from
a flag on the peer. A flag that can disagree with the room's own record is how the
removed-member defect in §7.2 happened, and the same shape would reproduce it here.

**The server half is not enough, and this is why.** Everything above is the server
telling a cooperative client to stop. A non-cooperative client keeps capturing and
keeps publishing, and the server discards it — the media never reaches the room. That
is the intended shape: the guarantee is that nothing is *delivered*, not that nothing
is *captured*. Only the browser can end a capture it started, and only a cooperative
client will.


## A packet is forwarded only for the connection that owns the publication

The forwarding boundary asked "does this room have a publication of this kind?"
rather than "does *this connection* own one here". Those are different questions, and
the difference is the whole boundary.

A second member of a room — one who was refused a share, or never asked for one —
could therefore advance every viewer's screen output under another member's
publication. The room's allowance was legitimate, the publication was legitimate, and
the packet was not: forwarding is where a non-cooperative client is stopped, so a
check that another peer's grant can satisfy is not a check.

The registry answers the narrower question directly. `ownedBy` is deliberately not
`countOfKind`: the first asks whether *this* account holds a live publication of *this*
kind in *this* room, which is the only question a forwarding decision can be justified
by. Verified by running the packet path both ways — the owner's packet reaches the
viewer, and a member who owns nothing here does not.

This is a bound, not a whole authorization model. It does not stop a member who
*does* hold a publication from forwarding it, and it is not a substitute for the
server refusing the publication in the first place. It closes the case where the
publication was correctly refused and the packets were not.


## The allowance is taken by the transaction that registers the publication

The room's screen bound was read under the lock, the lock released for the SDP
exchange, and the publication registered afterwards. A second peer arriving in that
window read the same count the first did and passed too, so the room could end up
carrying two screens against a bound of one — the second consuming bandwidth and a
viewer's output for an offer the server should have refused.

The comment above the check claimed one critical section for the whole decision. The
lock said otherwise, and the lock is what the code does.

The reservation is now re-asked **at registration, under the same lock that inserts**.
That is the only point close enough to the write to be about it: an earlier check is a
statement about the room before the exchange, and the exchange is exactly the window in
which another peer can take the allowance.

**What this does not prove.** There is no test here that interleaves two real
negotiations, because there cannot be one in this harness: Pion's answer blocks on ICE
gathering, and two overlapping offers on one connection are refused outright. The test
asserts the behaviour through the real signal path — the first member's screen is
published, the second's is refused, and the owner is unaffected — which is the
observable contract. It does not demonstrate the interleaving, and the ADR says so
rather than implying a race test that was never run.


## A mute is a voice state, not a permission, and a screen is not a microphone

The packet gate asked the microphone's question of every publication: a screen frame
was dropped if its sender was muted, deafened, or silenced. So a member who pressed
their own mute button stopped broadcasting their screen to everyone in the room.

That is wrong for a reason that is visible in the product rather than in the code.
**The commonest reason to mute is somebody talking over you** — an interruption, a
cough, a colleague in the next room. It is not a withdrawal of the right to be seen,
and it is certainly not something the person presenting intends to do to their
audience. The audience sees a shared screen freeze mid-sentence because the presenter
hit spacebar.

The gate now asks the question that matches the publication:

| Publication | Client-side voice state | Withdrawn permission | Admin mute |
| --- | --- | --- | --- |
| Microphone | gates it | gates it | gates it |
| Screen / screen audio | does not gate it | gates it | gates it |

`TestSpeakPermissionGatesAScreenToo` already pinned the third column and continues to
pass. The first column is new, and it is what the mute test asserts.

**The distinction is client state versus server authority, not audio versus video.**
`requestedMuted` and `state.Muted` are something the member chose; `speakDenied` and
`state.AdminMuted` are something the server decided. The screen is gated on the
second and not the first, because only the second is a statement about whether this
member is allowed to transmit at all. The microphone is gated on both, because there
the client's choice *is* the request.

A fix that simply dropped `Muted` from the shared condition would also have dropped
`Deafened` and `AdminMuted` with it — those are three flags in one expression, and only
one of them is the client's own business. `TestAWithdrawnPermissionStopsALiveScreen`
exists to catch that: it withdraws a permission *while a share is live* and requires
the frames to stop. Without it, "the mute no longer freezes the screen" and "a
moderator can no longer stop a screen" are the same test, and only one of them is
correct.


## One connection, one microphone — and the second microphone is a replacement

The server refuses a renegotiated microphone with *"this connection already has a
microphone"*, and `OnTrack` returns without reading any audio track that arrives once
one is claimed. Both are correct as statements about **slots** and wrong as statements
about **people**.

A connection has one audio output because it has one peer. It does not follow that the
peer has one microphone for the duration of the call. People plug in a headset, switch
to a USB microphone, hand the device to a colleague, or have a browser reopen the input
after a device change. On the old code every one of those produced a connected track, a
moving level meter in the caller's own window, and **no audio at all for anybody else**
— the exact failure where the interface says everything is fine.

So the second microphone takes over the connection's audio output rather than being
discarded, and the renegotiation that carries it is accepted and releases the claim. The
refusal that remains is about the *permission*: `SetSpeakAllowed(false)` still gates the
offer, so a member who may not transmit cannot talk their way back in by renegotiating.

Two things this deliberately does not do. It does not treat a second microphone as a
second *input* — that is the bound, and it is enforced by the single output, not by
refusing. And it does not make the grant permanent: a replacement can be replaced, which
is the whole point, and the alternative is the server needing a restart to hear somebody
again.

**What the shipped client actually does.** It replaces the device in place, through
`replaceTrack` on the existing transceiver, so a device change never produces a second
track and never reaches the takeover. The takeover is therefore a statement about the
*server's* handling of a second microphone track, not a client feature — which is why
it emits no event. An event with no client handler is a claim about the interface that
nobody made.

**The classification is not by codec, and that is the trap.** Screen audio is an audio
track, exactly like the microphone. Classifying a track as "audio therefore microphone"
made a member sharing a screen *with system audio* hand their connection's single audio
output to the screen's audio, and their real microphone stopped being forwarded. A track
belongs to the publication it was negotiated under, so the classifier asks what the
peer has published rather than what the codec is.


## A capture belongs to the audience that was there when the picker opened

`getDisplayMedia` opens a modal the app cannot observe or close. While it is open the
person can do anything the app allows: sign in to another account, leave the call, move
to another voice channel, or have a moderator withdraw their permission. The promise
resolves with a stream that belongs to *that* moment, and the caller is somewhere else.

The share path read the channel id before the picker opened and never re-read it, so a
returned capture was published into whatever room the person was in *now*. That is a
disclosure rather than a glitch: the previous account's screen appears in a room nobody
agreed to see it, the previous room's occupants watch a share that has nothing to do with
them, and the browser's own capture indicator is lit throughout — so the person sharing
sees every sign of a working capture and never learns it went somewhere else.

`captureStillWanted` compares everything that identifies the audience — account, call
channel, connection identity and the call's generation counter — and a mismatch stops
the returned tracks and releases the indicator rather than renegotiating them. The
generation is what covers leave, join, reconnect and authority change without each
being enumerated, and it is the same counter the join and reconnect paths already use.

**The order of the reasons is part of the contract.** The account is checked first
because it is the one a person can act on, and because signing in as somebody else
replaces the session and usually moves the generation too: reporting "the conversation
changed" for that would send them looking at the wrong thing. The conversation did not
change — they did.


## A screen and the element that shows it arrive independently

The model attached a received screen by looking for the workspace's video element at the
moment the stream arrived. The element is created by `{#if voice.screenShared}`, and
`screenShared` was set *after* the attach was attempted — so the very first share of a
call had no element to attach to, every time. The stream was stored, the screen was
announced as shared, and a blank rectangle appeared: no error, no retry, and nothing a
viewer could tell from a share that failed to send at all. On a slow join it is the
ordinary case rather than a rare one.

The fix is that either event triggers the render. A share arriving before the element is
held and rendered the moment one binds; a share arriving after one is rendered
immediately. Neither order is an error and neither is privileged.

The workspace hands the element over through an **action** rather than `bind:this`.
`bind:this` records the element and nothing else — it does not tell the model that
something is now waiting for it, which is the entire missing half. The action also
clears the field on destroy, so the model never points at a detached node, and it
re-renders into whatever element exists next rather than writing into one nobody owns.

Redundant binds are idempotent. Svelte re-runs an action when its argument changes and a
workspace re-render can bind again; re-assigning the same `srcObject` restarts the stream,
so a share would visibly jump whenever something else on the page changed.


## Screen audio is a separate output, and it was delivered to nobody

A screen's video and its audio are separate publications with separate bounds, but the
forwarding had one output per viewer and ignored which publication it was carrying.
`forwardScreen` took a `kind` and never used it.

That produced two failures at once, and only one of them was visible:

- **A viewer's audio output was never created.** The output was made for the video
  publication and the audio publication skipped output creation entirely, so a client
  that asked to receive the system audio negotiated a track the server had nothing to
  write to.
- **What did travel was written into the video output.** An Opus stream on a 90 kHz H264
  track is not audio; it is a track that decodes as noise. So the failure presented as "the
  shared screen's sound is broken" and pointed at the speaker rather than at routing.

A registered screen-audio publication with no delivered audio is the shape of a feature
that looks present and is not: the sharer's toggle works, the publication exists, the
audit records it, the queue shows it, and nobody hears anything.

Each kind now gets its own output with its own clock and codec, and `forwardScreen`
writes only to the outputs of the kind it is carrying. The forwarder carries the kind
because that is what makes a mismatched packet refusable rather than merely improbable —
and refused is the right outcome, because a translated packet is not a delayed one.

**The viewer's subscription is the output.** Two tests state it: a peer in the room that
asked for nothing receives nothing, and each publication reaches the output that is for
it and leaves the other alone. Both fail if the kind check is removed from `forwardScreen`.

The room's bound of one screen hid this. With only one screen video published at a time
the forwarded set is the room's set whichever question is asked, so the property could not
be observed from outside — it is an accident of the bound, not a property of the
forwarding, and it would have become a disclosure the moment the bound moved.


## A capacity client has to speak the deployment's transport

The capacity client connected with `net.createConnection` regardless of the URL's
scheme, and read frames without ever answering a **ping**. Both were found by reading
the script, and both are the shape of failure that does not announce itself:

- Against a deployment behind HTTPS the handshake is sent in the clear, the server
  closes the connection, and every participant fails to connect. **A run that connects to
  nothing still writes a report**, and a report with no participants reads like a very
  fast server.
- The server heartbeats every 30 seconds and ends a connection that has not answered, so
  a client that reads frames but never replies loses its participants part-way through a
  long run — which is why a validation run has to last longer than the heartbeat before a
  long measurement means anything.

The client now opens a TLS socket for `https:`/`wss:` and verifies the certificate unless
the caller opts out. The exemption exists because a measurement number taken through an
unverified connection is a number about somebody else's server, so it is not the default.

**The handshake has a deadline.** A plaintext client aimed at a TLS port gets no reply at
all — the server waits for a ClientHello that never arrives — so the connect promise
never settled and the run hung rather than reporting that its participants could not
connect. Ten seconds, reported as a failure with the timeout in the message.

`RawSocket` is exported and the module's executable half is reachable only through its
entry-point guard, because a measurement client that cannot be imported cannot be tested.
The seven tests drive it against real sockets: a TLS server it completes, a TLS server it
*cannot* complete from plaintext, a ping answered with a pong carrying the same payload,
and a text frame that still arrives — the permitted control for the ping change.


## The reservation is taken last, and that is the point

The room's screen allowance is one, so a registration that outlives its negotiation is
not a small leak: it spends the room's only screen on a track nobody will ever send, and
the next member to share is refused with "only one screen can be shared in this
conversation at a time" **while nobody is sharing**.

The ordering prevents it. `announcePublication` runs *after* applying the offer, after
creating the answer, after applying the answer, and after re-asking the allowance under
the lock — which is the opposite of how the room's capacity is taken everywhere else in
this codebase, where a check precedes a write and the two are separated.

That inversion is forced. The output track has to exist before the description is
created, or the answer does not satisfy the offer; and the allowance has to be asked at
the same lock that registers, or two peers negotiating at once pass on the same count.
So the reservation is last, and everything that can fail happens before it.

`TestAPublicationThatCannotBeNegotiatedIsNotLeftRegistered` pins it, and the negative
control is the ordering itself: moving the announce to before the SDP exchange fails with
"the room holds 1 publications after a failed negotiation, want at most 0". The test uses
a structurally invalid offer rather than an empty one, because an empty SDP is refused by
a guard that runs before either ordering — a test using it would pass against both.


## A viewer's line carries the screen, and a publisher never views its own share

Alpha.8 recorded the first half as an unfinished boundary: a browser viewer negotiated a
line for a screen nobody was sending on, the server answered it with a trackless
transceiver while the real screen output appeared as an m-line the client had never
offered, and the connection reported `connected` with `framesDecoded` at zero. Both sides
were wrong, and each was only half the defect.

The client said it wanted to send. `addScreenReceive` asked with `sendonly` and no track,
which is a statement that the client intends to publish — the answer to it can never
deliver a picture. A client that means to receive says `recvonly`, and now it does.

The server bound the wrong section. Pion associates an arriving m-line with a same-kind
transceiver that is not yet associated, so the screen output has to exist **before**
`SetRemoteDescription` for the answer's section to be the one the screen travels on.
Created afterwards, it became an extra m-line the client never offered: negotiation
succeeded and no frame could arrive. `renegotiate` now reads the offer's own view lines
(`viewRequestKinds`: `a=recvonly`, or the view stream id) and creates the outputs first.

Fixing only those two produced the second defect, and it is the reason this section
exists. A publication is announced to the room, and the publisher is *in* the room: told
that a screen existed, the sharing client asked to view it, the server answered with a
video line for it, and `ontrack` treated the arriving track as a picture to display —
`attachScreen` stopped the previous stream, which was the capture. The sharer's own share
died the instant it started; the browser journey's publisher encoded zero frames while
the connection stayed `connected`. So the boundary is now stated on both sides, each as a
rule:

- **A publisher is never told about its own publication.** It already has the publication
  id from the answer that registered the share, and the announcement exists so that
  *another* client can negotiate a line for it. The owner is filtered out of the
  recipients, and the client also ignores an announcement addressed to a publication it
  owns, so a replayed or cross-tab event cannot restart the cycle.
- **A local capture is not the stream the panel renders.** The two were one field, and
  the consequence was that a stream arriving from the server stopped the capture. They
  are separate now, and while a capture is live nothing arriving on the connection can
  replace it. Leaving the call releases the capture, which also protects the next call
  from a stale one.
- **An offer that names no publication may ask to receive and may not ask to send.** A
  `recvonly` line — or a line carrying the view stream id — is a view request and is
  answered with the output that carries the screen. A `sendonly`/`sendrecv` video section
  that names no publication is refused with "a second offer must name the publication it
  is adding": the receive path must not become a way to publish by omitting a field. A
  section whose mid this connection has already negotiated is a re-offer of media the
  connection already has, not an addition.
- **A view line can never become a publication.** Forwarding is gated on ownership of a
  live publication of the kind, so media arriving on a viewer's line is dropped rather
  than registered. `TestAViewLineIsAnsweredAndCannotPublish` is the permitted control
  standing beside `TestAnUnmarkedSendingVideoLineIsStillRefused`, so the fix is not
  "refuse everything".

The evidence is layered, and the layers prove different things.
`TestAViewerReceivesScreenFramesOnTheOfferItMade` drives real peer connections: it counts
the answer's video sections, requires exactly one answered `sendonly`, and reads RTP off
the receiver — synthetic frames written by the server, which is not evidence about a
browser. The two-browser journey proves the browser half: the viewer's inbound
`framesDecoded` advances, its `frameWidth` is the 320-pixel canvas the sharer draws,
decoding stops when the share stops, and decoding resumes on a restart while the sharer
stays in the call. Neither replaces the other, and neither says anything about a physical
capture device or a real network path.


## A voice place belongs to a live connection, not to a closed tab

`Detach` exists so a short signaling interruption does not cost a call: when a voice
client's WebSocket goes away and its session is still valid, its peer keeps its media
connection and its resume token for a thirty-second drain. That part is right. What was
wrong is what the *next* join was told in the meantime.

`Join` refused any second connection for the same account — "you are already in voice,
possibly in another tab" — which was written when the only thing that could hold a place
was a live connection. A detached peer is not that: it has no sink, so nothing can be
delivered to it; it is unreachable except through a token its own client never persisted;
and it held the account's place for the whole drain. The reachable case is ordinary:
reload the page during a call, or close the tab and come straight back, and the rejoin was
refused for up to thirty seconds with a message naming a tab that no longer exists.

A detached peer is now released when a new join arrives for the same account, and the new
connection takes the place: `Join` tears the old peer down — closing its media connection,
withdrawing its publications and telling the room — and admits the newcomer. An *attached*
peer is still refused, which is the case the refusal was written for: two live tabs
sharing one account's voice place. The detached peer's resume token dies with it, so a
client that reconnects its socket after a new tab has joined falls back to a fresh join
and is refused by whoever arrived second. That ordering is stated rather than preferred,
and `TestADetachedPeerDoesNotHoldTheVoicePlace` pins both halves: the rejoin that must be
admitted and the second live tab that must not.

This was found while chasing an intermittent browser-journey failure — a join that never
became a call, once, in a full-suite run. The journeys have not reproduced it since, in
twenty-nine suite executions, so the fix is not claimed as its cause. What is claimed is
the defect the code showed, the test that fails without the fix, and the diagnostics the
next occurrence will now leave.


## A keyframe request is the viewer's only way to start a picture

H.264 starts with a keyframe, and a stream that begins mid-GOP decodes to nothing: the
viewer sees a black rectangle, not a degraded picture. A browser asks for one — PLI, or
FIR — and its request is addressed to whoever is sending the media. In this architecture
that is never the peer it is watching; it is the server. Until this change the server
read the feedback on each viewer's line and dropped it, so the request died one hop
short of the encoder that could answer it.

Three properties make the path real, and each is testable on its own:

- **The feedback is negotiated.** `nack pli` comes from Pion's NACK configuration; `ccm
  fir` is registered explicitly. A browser sends only feedback its answer offered, so a
  registration gap looks exactly like a client that never asks.
- **The request is forwarded, addressed at the publisher's stream.** A PLI names the
  media source it is about, and a browser demultiplexes feedback by that value: a request
  that names something unknown is dropped before the publisher's encoder hears about it.
  Both SSRC fields therefore carry the publisher's sending SSRC, which the server learns
  from the track it receives — a request cannot be sent before the first frame arrives,
  and the first frame of a publication is a keyframe anyway.
- **It is rate-limited per room and kind.** A viewer that is waiting asks several times a
  second, and forwarding each one turns one browser's impatience into an encoder
  producing keyframes for the whole room. A request with nothing behind it — no
  publication, a publisher that has left, a peer asking about its own stream — is dropped
  without consuming the window, so a viewer with nothing to watch cannot delay the viewer
  who has something.

**A viewer that joins late has to be told what is already running.** The announcement was
emitted only when a publication was registered, so a client that arrived afterwards
negotiated nothing at all: no line of its own, no output for the server to write to, and
a share invisible to exactly the person who joined to watch it. A joining peer is now
told about the room's live publications through the same event the registration path
uses, and the line it binds triggers the same keyframe request.

The evidence is layered. `internal/rtc` drives real peer connections and real RTCP: a
viewer's PLI arrives at the publisher addressed at the publisher's own SSRC; a second
request inside the window does not; an ordinary receiver report never becomes one; a
request after the share stops is dropped; binding a line to a live share produces a
request with no client RTCP at all; and the answers offer `nack pli` and `ccm fir` while
not offering feedback this build does not implement. The browser journey proves the
outcome for a real capture: a third viewer that joins while the share is running decodes
the 320-pixel canvas within ten seconds. Neither layer says anything about a physical
display, a lossy network, or a browser other than Chromium.


## The codec a screen is carried in is the client's own choice

A screen share travels end to end as one codec: the server rewrites sequence numbers,
timestamps and payload types on the way out, but it does not transcode, so whatever the
publisher's encoder produced is what every viewer's decoder receives. Two rules follow,
and the second only became visible on a second engine.

**A publication is carried in the first video codec the publisher's own offer listed.**
The output track is created from the offer *before* it is applied — that ordering is what
makes the answer's section the one the screen travels on — so the choice has to be made
from the description's own text. H264 and VP8 are both registered, and the answer names
the first of those the offer listed. The rule used to be "H264 whenever the offer mentions
it", on the reasoning that H264 is what a capture produces most reliably. On Firefox that
produced a share which negotiated, encrypted, arrived in full — the byte counts at both
ends matched exactly — and decoded to nothing, because the publisher encoded H264 and the
viewer's decoder produced no frame from any of it. Firefox lists VP8 first for exactly
that reason; the server was overruling it.

**A viewer's output carries what the publication carries**, when the viewer offers it: an
output negotiated for a codec other than the payloads it will carry delivers bytes the
receiver cannot decode, which is the same failure from the other side. A viewer that does
not offer the publication's codec is answered with its own first preference instead. The
negotiation then succeeds and the picture may not — but the alternative is a failed
negotiation, which is a worse answer for a member who cannot change what the publisher
chose.

The evidence is a Go test per half (a VP8-only viewer is answered VP8 and receives frames;
a viewer that lists both gets the one it listed first, with the publication carried the
same way) and Firefox's project, where the two-browser journey decodes the shared picture,
stops when the share stops and resumes on restart under the same assertions Chromium runs.
It does not establish anything about a codec neither engine offers, a viewer whose codec
the publisher cannot produce, or the quality of either codec: the presets bound the
bitrate, and legibility at the text preset is still a qualification item that needs the
reference hardware.
