// Where an activity entry takes the person who follows it.
//
// `destination` on the controller used to return `#messages` for every entry, which is
// the inbox the entry is listed in. Pressing one sent the person back where they came
// from and left them to look — the same experience as a link that does nothing, and the
// reason an activity feed could be read but not followed.
//
// Each kind has somewhere specific to be: a request belongs in the requests tab, and a
// message belongs in the conversation it happened in. A group message and a thread reply
// both name their conversation, so one destination covers them.
//
// This lives in its own module rather than on the controller because the controller is a
// Svelte module whose imports are extensionless, which node cannot import — so an inline
// rule could only be checked by reading its source. That is how a controller returning a
// constant passed a suite.

/** The part of an activity entry a destination is decided from. */
export interface ActivityDestinationInput {
  kind: string;
  /** The conversation the entry is about. Absent for a conversation that is gone. */
  conversation_id?: string;
}

/**
 * The destination for an entry, or the empty string when there is nowhere to go.
 *
 * An entry with no conversation returns nothing rather than a fallback: falling back to
 * the inbox is what this replaces, and a dead link that goes somewhere real is worse
 * than one that is plainly not a link.
 */
export function activityDestination(entry: ActivityDestinationInput): string {
  if (entry.kind === 'request') return '#requests';
  const wanted = entry.conversation_id;
  if (!wanted) return '';
  // Escaped rather than concatenated. An id is server-generated today, but a destination
  // that is assembled from a value has to be safe against one that is not, and the cost
  // of escaping is nothing.
  return `#conversation=${encodeURIComponent(wanted)}`;
}
