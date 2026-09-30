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
