/**
 * When to try again, and when to decide that a connection which looks open is not.
 *
 * Two facts shape this policy. The server sends a frame every thirty seconds on an open
 * connection — a `presence.update` the client can see — so silence for more than two of
 * those intervals means the socket is a *half-open* one: the path died, TCP has not
 * noticed, and no `close` event is coming. Waiting for one leaves a member staring at a
 * workspace that stopped updating with no sign of why.
 *
 * And the browser tells the client when connectivity comes *back* (`online`, and the page
 * becoming visible again), which is a far better moment to retry than whichever backoff
 * timer happens to be pending: the plan's budget is ten seconds at p95 after connectivity
 * returns, and a schedule that has already climbed to thirty cannot meet it. The backoff
 * stays for the case where nothing tells us anything — a server that is down while the
 * network is fine — because retrying that every second is a denial of service with one
 * participant.
 */

/** The longest a retry is ever delayed, for a server that keeps refusing connections. */
export const MAX_RECONNECT_DELAY_MS = 30_000;

/**
 * How long a connected socket may deliver nothing before it is treated as half-open.
 * Two and a half of the server's thirty-second frames, so one lost frame is not a
 * reconnection and a dead path is noticed within about a minute.
 */
export const SILENT_CONNECTION_MS = 75_000;

/** How often the silence is checked. Cheap: one comparison and no I/O. */
export const HEARTBEAT_WATCH_MS = 10_000;

/**
 * The backoff for a given attempt, before jitter: one second doubling to a thirty-second
 * ceiling. Jitter is added by the caller, because two tabs reconnecting in lockstep is a
 * thundering herd against a server that just came back.
 */
export function reconnectDelayMs(attempt: number): number {
  return Math.min(MAX_RECONNECT_DELAY_MS, 1000 * 2 ** attempt);
}
