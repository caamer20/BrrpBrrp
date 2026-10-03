/**
 * The conversation this bundle speaks, and whether the server's hello agrees.
 *
 * A tab can be open across a server update, and the code in it is then older than the
 * server it is talking to. Most of the time that is harmless — the frames carry additive
 * fields it ignores — but a change in what a frame *means* is not something an old bundle
 * can survive quietly: it would send text the server stores differently, or hold text in
 * a composer while every send is refused and nothing says why.
 *
 * So the server's hello carries the protocol it speaks and the oldest one it still
 * accepts, and this module answers the two questions that follow: is this bundle inside
 * that range, and if not, who is stale. Both answers are about what to *say* — the text
 * itself is never at risk, because the composer refuses to hand anything over while this
 * is unresolved and the draft handler keeps the draft (see `UpdateNotice` and
 * `protocolVerdict`'s callers).
 *
 * Keep in step with internal/ws/protocol.go, which is the server's half of the same
 * number.
 */
export const PROTOCOL = 1;

export type ProtocolVerdict = { ok: true; message: '' } | { ok: false; message: string };

/**
 * Reads a `hello` frame's protocol fields.
 *
 * An absent field means a server that predates the handshake. That is treated as
 * compatible rather than as an update-required state: the field arrives with the first
 * protocol-aware release, so an older server cannot be *ahead* of this bundle, and
 * refusing to talk to one would turn a missing field into a broken installation.
 */
export function protocolVerdict(hello: unknown): ProtocolVerdict {
  const frame = (hello ?? {}) as { protocol?: unknown; protocol_min?: unknown };
  const server = typeof frame.protocol === 'number' ? frame.protocol : PROTOCOL;
  const minimum = typeof frame.protocol_min === 'number' ? frame.protocol_min : PROTOCOL;
  if (PROTOCOL < minimum) {
    return {
      ok: false,
      message:
        'This app is older than the server. Update it before sending — your drafts stay here until you do.'
    };
  }
  if (PROTOCOL > server) {
    return {
      ok: false,
      message:
        'This server is older than this app. Update the server, then reload this page — your drafts stay here.'
    };
  }
  return { ok: true, message: '' };
}
