/**
 * Describes the candidate pair a call actually settled on.
 *
 * A TURN-relayed pair has a `relay` candidate on one side; that is the only reliable
 * signal, because the transport used is not exposed and the addresses alone can look
 * like a direct path. The label is what a support conversation needs before it needs
 * the round-trip time.
 */
export function describePath(
  local: { candidateType?: string } | undefined,
  remote: { candidateType?: string } | undefined
): string {
  const kinds = [local?.candidateType, remote?.candidateType].filter(Boolean) as string[];
  if (!kinds.length) return '';
  // Both ends are needed. A pair whose far end the browser has not described yet is
  // not a known-direct pair, and calling it one would assert a path nobody measured.
  // The weakest link decides: a pair that is relayed on one side is a relayed call,
  // whatever the other end negotiated.
  if (kinds.includes('relay')) return 'relayed through a TURN server';
  if (kinds.includes('prflx')) return 'peer-reflexive (server-assisted)';
  if (kinds.includes('srflx')) return 'direct, via a STUN server';
  if (kinds.length < 2) return '';
  if (kinds.every((kind) => kind === 'host')) return 'direct, on the local network';
  return 'direct';
}
