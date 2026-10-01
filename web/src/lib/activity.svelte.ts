/**
 * The account's own activity list (ADR 0015).
 *
 * Durable activity exists so an account can catch up after being offline, which is
 * why this is a list of the account's own records rather than a notification count
 * derived from history. Two things follow and are why the loading is here rather
 * than in the component:
 *
 * - Every result is fenced on the account, the authorization generation and a local
 *   version. Activity is private metadata, so a response that arrives after an
 *   account change must be discarded rather than shown — the same rule the rest of
 *   the workspace follows.
 * - Acknowledgment is a separate call, not a side effect of reading. A read that
 *   fails halfway must not have consumed the acknowledgment, and acknowledgment has
 *   to survive the page it was made from.
 */
import { api, errorMessage } from './api';
import { workspace } from './workspace.svelte';

export interface ActivityEntry {
  id: string;
  kind: 'message' | 'request' | 'group_message';
  source_id: string;
  actor: string;
  created_at: string;
  acknowledged_at: string;
  acknowledged: boolean;
}

export interface ActivityPage {
  activity: ActivityEntry[];
  next_cursor: string;
  outstanding: number;
  unacknowledged_count: number;
}

export interface NotificationPreference {
  kind: 'direct_message' | 'incoming_request' | 'group_message';
  muted: boolean;
  show_sender: boolean;
}

/**
 * The list's own state. `version` is what fences a late response: every fetch
 * captures the version it started with, and a result for a superseded one is
 * dropped rather than applied.
 */
import { admitted as isAdmitted, acknowledgedRows, markAcknowledged } from './admission';

/**
 * The activity controller.
 *
 * Its admission question lives in `./admission` rather than inline, because the
 * controller is a Svelte module that node cannot import (its imports are
 * extensionless), so a test could only ever read this file's source. Extracting the one
 * decision that matters made it directly testable, which is what found that teardown
 * was permanent.
 */
class ActivityStore {
  entries = $state<ActivityEntry[]>([]);
  cursor = $state('');
  outstanding = $state(0);
  unacknowledged = $state(0);
  loading = $state(false);
  error = $state('');
  /** True once a fetch has completed, so "empty" is distinguishable from "not yet". */
  loaded = $state(false);
  preferences = $state<NotificationPreference[]>([]);

  private version = 0;
  private alive = true;

  /** The clock acknowledgment stamps rows with. Injected so a test can state the value. */
  now = (): string => new Date().toISOString();
  /** Advances on every mount, so a reopened panel fetches again. Reactive by design. */
  revision = $state(0);

  identity() {
    return {
      account: workspace.user?.id ?? '',
      generation: workspace.authorizationGeneration,
      version: this.version
    };
  }

  admitted(request: { account: string; generation: number; version: number }) {
    return isAdmitted(request, {
      account: workspace.user?.id ?? '',
      generation: workspace.authorizationGeneration,
      version: this.version,
      alive: this.alive,
      authorizationUnverified: workspace.authorizationUnverified
    });
  }

  /** Replaces the list. `append` is the "load older" case, which must not reset it. */
  async load(append = false) {
    const request = this.identity();
    if (!this.admitted(request)) return;
    this.loading = true;
    this.error = '';
    try {
      const query = append && this.cursor ? `?before=${encodeURIComponent(this.cursor)}` : '';
      const page = await api<ActivityPage>(`/me/activity${query}`);
      if (!this.admitted(request)) return;
      this.entries = append ? [...this.entries, ...page.activity] : page.activity;
      this.cursor = page.next_cursor;
      this.outstanding = page.outstanding;
      this.unacknowledged = page.unacknowledged_count;
      this.loaded = true;
    } catch (failure) {
      if (this.admitted(request)) this.error = errorMessage(failure);
    } finally {
      if (this.admitted(request)) this.loading = false;
    }
  }

  /**
   * Acknowledges everything currently listed, from the position rather than from a
   * set of ids, so a row that arrived between the list and this call is not
   * acknowledged by accident — the cursor is the last row the person actually saw.
   */
  async acknowledge() {
    const request = this.identity();
    if (!this.admitted(request)) return false;
    if (this.entries.length === 0) return true;
    // The cursor is the *oldest loaded* row, because the list is newest-first and the
    // server acknowledges everything strictly older than this. That is right for the
    // rows the person has actually seen.
    const before = this.entries[this.entries.length - 1].id;
    try {
      const result = await api<{ acknowledged: number; unacknowledged_count: number }>(
        '/me/activity/acknowledge',
        'POST',
        { before }
      );
      if (!this.admitted(request)) return false;
      this.unacknowledged = result.unacknowledged_count;
      // Only the rows the server actually acknowledged are marked. The count is
      // carried for this reason: an acknowledgment that reports fewer rows than are on
      // screen means the server applied a bound the client cannot see, and painting the
      // whole page "seen" would claim the person looked at rows that stayed unacknowledged
      // on the server — and would stay unacknowledged after a reload, which is a visible
      // flicker of the badge coming back.
      //
      // The rows are still shown either way: acknowledgment says "seen", not "gone", and
      // the person may want to look back.
      this.entries = markAcknowledged(
        this.entries,
        acknowledgedRows(result.acknowledged),
        this.now()
      );
      return true;
    } catch (failure) {
      if (this.admitted(request)) this.error = errorMessage(failure);
      return false;
    }
  }

  async loadPreferences() {
    const request = this.identity();
    if (!this.admitted(request)) return;
    try {
      const result = await api<{ preferences: NotificationPreference[] }>(
        '/me/notification-preferences'
      );
      if (this.admitted(request)) this.preferences = result.preferences;
    } catch (failure) {
      if (this.admitted(request)) this.error = errorMessage(failure);
    }
  }

  async setPreference(kind: string, change: { muted?: boolean; show_sender?: boolean }) {
    const request = this.identity();
    if (!this.admitted(request)) return false;
    try {
      const result = await api<NotificationPreference>('/me/notification-preferences', 'PUT', {
        kind,
        ...change
      });
      if (!this.admitted(request)) return false;
      this.preferences = this.preferences.map((entry) =>
        entry.kind === result.kind ? result : entry
      );
      return true;
    } catch (failure) {
      if (this.admitted(request)) this.error = errorMessage(failure);
      return false;
    }
  }

  /**
   * The destination an entry opens, resolved under current authority when it is
   * followed rather than carried from when it was written. A row that survives after
   * the person lost access leads somewhere that refuses, which is the honest answer.
   */
  destination(entry: ActivityEntry): string {
    // A hash the application acts on, not a path it ignores. The previous value was a
    // bare `/messages`, which no route matched, so following an entry did nothing a
    // reader could see. Authority is still checked when the destination is opened, not
    // carried from when the entry was written.
    return '#messages';
  }

  /**
   * Drops everything on an account or authority change. Activity is private
   * metadata, so it is cleared with the rest rather than left for the next fetch.
   */
  reset() {
    this.version++;
    this.entries = [];
    this.cursor = '';
    this.outstanding = 0;
    this.unacknowledged = 0;
    this.loaded = false;
    this.error = '';
    this.preferences = [];
  }

  /**
   * Revives the controller when a panel mounts.
   *
   * This store is a module-level singleton, so a member who closes the Activity panel
   * and opens it again mounted a new panel over a dead controller: `destroy` had set
   * `alive` false for good, `alive` gates `admitted`, and `admitted` gates every load —
   * so the second open rendered an empty panel with no error and no way to retry. The
   * browser test could not see it because the panel is mounted once per journey.
   *
   * `alive` exists to discard a response that arrives after the panel is gone, which is
   * a statement about *in-flight work* rather than about the controller's existence.
   * So it is per-mount: unmount invalidates what was in flight, and mount says there is
   * a listener again. The account and generation fences are unaffected — they are what
   * keep one account's private metadata out of the next account's panel, and they do
   * not depend on the panel being open.
   */
  mount() {
    this.alive = true;
    // A render-level value, so a panel effect that reads it re-runs on reopen. `alive`
    // is not reactive and must not be: it gates responses, and making it reactive would
    // re-run every panel effect that admits work, on every teardown.
    this.revision++;
  }

  destroy() {
    this.alive = false;
    this.version++;
    this.reset();
  }
}

export const activity = new ActivityStore();
