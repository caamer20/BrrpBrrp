# Daily beta ledger — template

**Copy this file once per community per day** (for example
`planning/beta/<community>/day-07.md`) and fill it in at the end of the day. It is the
record the beta gate is judged from, so it is written for somebody who was not there: a
reader six weeks later, or the person deciding whether the fourteen clean days stand.

The plan's requirement is in [docs/v1-implementation-plan.md](../docs/v1-implementation-plan.md)
§14.2: record daily build and deployment identity, activity, incidents, delivered alerts
and recovery status. The same fields appear in the evidence record at §6.3 of
[v1-release-execution-plan-2026-10-03.md](v1-release-execution-plan-2026-10-03.md).

**Two rules for filling it in.**

1. **Record what was observed, not what was expected.** "Voice worked" is not an
   observation. "Two operators, different networks, relay-transported, 42 minutes, no
   dropout reported" is.
2. **No private content.** Nicknames and message text do not belong in this file. Where
   something specific must be identified, record the account, channel or timestamp, never
   what was said.

---

## Day header

| Field | Value |
| --- | --- |
| Community | `<name>` |
| Beta day | `<N of 14>` |
| Date (with timezone) | `<YYYY-MM-DD, TZ>` |
| Operator on duty | `<name>` |
| Alternate available | `<name>` |

## Build and configuration identity

| Field | Value |
| --- | --- |
| Application version | `<e.g. 0.4.0-alpha.10>` |
| Source commit | `<40 hex or "unknown">` |
| Artifact file and SHA-256 | `<name, hash>` |
| Schema / backup format / protocol | `<n / n / n>` |
| Deployment | `<Windows native over Tailscale | Linux Docker | other>` |
| Host and OS build | `<host, OS build>` |
| Feature modes | `restricted_channels=<on/off>, private_conversations=<on/off>` |
| Configuration changes today | `<keys changed and why, or "none">` |

If the build or the configuration changed today, say so here **and** in §7, because a
change may reset a clean window.

## 1. Activity

```text
Members active (approx)        <n>
Messages sent                  <n>
Uploads                        <n>
Calls held                     <n>
Longest single call            <mm>
Screen shares                  <n>
```

## 2. Prescribed journeys attempted

Record one line per journey attempt. "Blocked" is a valid result and must not be
converted into a pass.

| Journey | Result (pass / fail / blocked) | Counts p/f/s/dnr | Where it stopped |
| --- | --- | --- | --- |
| Invitation and first sign-in | `<...>` | `<0/0/0/0>` | `<...>` |
| Restricted-channel membership and the outsider's view | `<...>` | `<...>` | `<...>` |
| Direct conversation: request, accept, message | `<...>` | `<...>` | `<...>` |
| Group conversation | `<...>` | `<...>` | `<...>` |
| Thread: start, reply, follow, lock | `<...>` | `<...>` | `<...>` |
| Reporting and moderation | `<...>` | `<...>` | `<...>` |
| Voice call on two devices, different networks | `<...>` | `<...>` | `<...>` |
| Screen share: view, audio-only, stop | `<...>` | `<...>` | `<...>` |
| Install / update / restore by the operator, unaided | `<...>` | `<...>` | `<...>` |

Add a row rather than omitting a journey. A journey nobody attempted is the most
interesting line in this file.

## 3. Cross-device calls

| Call | Route (host / srflx / relay) | Browsers and OS | Minutes | Dropped or degraded? |
| --- | --- | --- | --- | --- |
| `<n>` | `<...>` | `<...>` | `<mm>` | `<...>` |

The beta requires at least 30 complete cross-device calls, including six longer than 30
minutes, with the route and browser outcome logged.

## 4. Incidents

One block per incident. If there were none, write "none" — do not leave the section
empty, because an empty section is indistinguishable from an unfilled one.

```text
What happened            <one sentence a member would recognise>
When first seen          <timestamp>
Who noticed              <operator name or member>
Severity (0/1/2/3)       <n>   # policy: master plan 14.3
Reproduction             <exact steps, or "not reproduced; once, at <time>">
Affected scope           <whom and what; no message content>
Disposition              <fixed | workaround <x> | open, owner <name> | blocked>
Evidence                 <log path, screenshot path, or "none captured">
Reset required?          <no | yes: which clean window and why>
```

## 5. Alerts and backup status

| Field | Value |
| --- | --- |
| Alert destination configured | `<yes/no>` |
| Alerts delivered today | `<n, kinds>` |
| Alerts that failed to deliver | `<n, and what the record said>` |
| Scheduled backups enabled | `<yes/no>` |
| Configured interval | `<hours>` |
| Last successful scheduled backup | `<timestamp>` |
| Its age against the objective | `<h; objective is to lose at most 24 h>` |
| Last failure recorded | `<timestamp and message, or "none">` |
| Last verified restore | `<date and where it was restored to>` |
| Independent copy location | `<path, device or service>` |
| Independent copy age | `<h>` |
| Recovery verdict the server reports | `<ok / warning / critical — quote it>` |

**A silent scheduled-backup failure fails the beta gate even while chat is usable.** If
the age above is unknown, that is the finding.

## 6. Unexplained observations

The most valuable section, and the one most easily left blank because nothing is "wrong"
yet. Anything a person did that the documentation did not predict, any question asked more
than once, any place somebody hesitated.

```text
<n>
```

## 7. Change and reset check

```text
Did anything executable / dependency / schema / protocol change today?   <yes/no>
Did any default or configuration key change?                             <yes/no>
Does any of the above invalidate earlier days' evidence?                 <yes/no>
  If yes: which window restarts, from when, and why                       <...>
```

## 8. Questions for the author

```text
<n, or "none">
```

## 9. Sign-off

```text
Filled in by          <name>
Time                  <timestamp with timezone>
Tomorrow's duty       <name>
```
