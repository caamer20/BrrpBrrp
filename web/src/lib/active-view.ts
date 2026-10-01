// When is the member actually looking at this conversation?
//
// Marking read and raising a desktop notification are the same question asked twice: is
// this person *watching this thing happen*? They were answered twice, and the two
// answers disagreed — the read cursor asked about four conditions and the notification
// asked about one.
//
// `document.hidden` alone means the tab is in the background. It does not mean the
// member is looking elsewhere *inside* the app, so a member with the tab open on
// another channel — or reading back through history in this one — got an operating
// system notification for a message they could see arrive. On a laptop that is a banner
// over the very conversation being read, and it is the sort of thing that teaches people
// to dismiss notifications without reading them, which is the opposite of the point.
//
// The rule is therefore stated once, here, and both callers read it. It is deliberately
// the same four conditions rather than a tidier subset: a member who is scrolled away
// from the newest message is not watching this message arrive, and their read cursor must
// not move for it either.

/** What the app knows about whether the member is watching the conversation in front of them. */
export interface ActiveView {
  /** The tab is not in the background. */
  visible: boolean;
  /** The tab has focus. A visible tab in another window is still not being read. */
  focused: boolean;
  /** The view is scrolled to the newest message. */
  atLatest: boolean;
  /** The member is reading back through history rather than the live tail. */
  viewingHistory: boolean;
}

/**
 * Whether the member is watching this conversation as it happens.
 *
 * Strictly boolean, and the comparisons are explicit rather than a `&&` chain. A chain
 * returns its first falsy operand, so a missing field would answer `undefined` — and a
 * caller writing `!watchingNow(...)` and a caller writing `=== false` would then
 * disagree about a state the types say cannot happen.
 */
export function watchingNow(view: ActiveView): boolean {
  if (view.visible !== true) return false;
  if (view.focused !== true) return false;
  if (view.atLatest !== true) return false;
  if (view.viewingHistory === true) return false;
  return true;
}
