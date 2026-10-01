import { api, APIError, errorMessage } from './api';
import { workspace } from './workspace.svelte';
import type { ThreadSendOutcome } from './workspace.svelte';
import type { Message, RealtimeEvent } from './types';

// A thread is a channel conversation with a subject (ADR 0017). This store holds one
// open thread as a bounded window rather than the whole history: a thread with a
// thousand replies is read by asking for older pages, so the client never holds more
// than it shows. The generation guard is what stops a slow page from landing after the
// person has moved on, and `missing` is what stops a deleted thread from being
// re-requested on every render.

// ThreadSendResult is what one reply attempt left behind, as the composer needs it to
// decide what to do with the author's text: only `sent` may clear it.
export type ThreadSendResult = 'sent' | 'refused' | 'uncertain' | 'dropped';

export interface Thread {
  id: string;
  conversation_id: string;
  root_message_id: string;
  title: string;
  state: 'active' | 'archived' | 'locked';
  created_by: string;
  created_at: string;
  updated_at: string;
  reply_count: number;
  last_reply_at: string;
  followers: number;
  following: boolean;
}

export class ThreadView {
  thread = $state<Thread | null>(null);
  messages = $state<Message[]>([]);
  loading = $state(true);
  sending = $state(false);
  error = $state('');
  // The outcome of the last send, named rather than inferred: a definite refusal and
  // an unknown outcome both keep the author's text and key, and only the second says
  // "this may have been sent".
  sendError = $state<{
    status?: number;
    outcome?: 'refused' | 'uncertain';
    message: string;
  } | null>(null);
  missing = $state(false);
  hasOlder = $state(false);
  // Whether the window currently includes the newest reply of the thread. A deep link
  // to the newest reply lands there without any scrolling, and reading it must clear
  // the thread's unread count exactly as scrolling to the bottom does; a link to an
  // older reply must not.
  atNewest = $state(true);
  private generation = 0;
  private disposed = false;
  // The replies already held locally, matched by the account's own send key. A send
  // whose response is lost is shown from here rather than re-sent.
  private pending = new Map<string, Message>();

  constructor(readonly id: string) {}

  // A thread is read as a bounded window, not as an ever-growing list: the point of
  // a window is that a thousand-reply thread costs the same as a fifty-reply one, in
  // memory, in DOM nodes and in requests. Loading older replies fetches the page
  // before the oldest held one and drops the newest page once the window is full;
  // the same bound applies to replies arriving live.
  private static readonly pageLimit = 50;
  private static readonly windowLimit = 200;

  async load() {
    if (this.disposed || this.missing) return;
    const generation = ++this.generation;
    this.loading = true;
    this.error = '';
    try {
      this.thread = await api<Thread>(`/threads/${encodeURIComponent(this.id)}`);
      if (this.disposed || generation !== this.generation) return;
      const page = await api<Message[]>(`/threads/${encodeURIComponent(this.id)}/messages`);
      if (this.disposed || generation !== this.generation) return;
      this.messages = page;
      this.hasOlder = page.length >= ThreadView.pageLimit;
      this.atNewest = true;
    } catch (error) {
      if (this.disposed || generation !== this.generation) return;
      // A thread that is not found is not found, whether because it was archived
      // away, its channel closed, or the reader lost access. Re-requesting it on
      // every render would be a way to probe for its existence.
      if (error instanceof APIError && error.status === 404) this.missing = true;
      else this.error = errorMessage(error);
    } finally {
      if (!this.disposed && generation === this.generation) this.loading = false;
    }
  }

  // loadAround opens the window positioned at one reply, for a deep link: the pages
  // immediately before and after it, so the linked reply is present and reachable
  // whether it is the newest reply or the thousandth.
  async loadAround(messageID: string) {
    if (this.disposed || this.missing || !messageID) return;
    const generation = ++this.generation;
    this.loading = true;
    this.error = '';
    try {
      this.thread = await api<Thread>(`/threads/${encodeURIComponent(this.id)}`);
      if (this.disposed || generation !== this.generation) return;
      const [older, newer, target] = await Promise.all([
        api<Message[]>(
          `/threads/${encodeURIComponent(this.id)}/messages?before=${encodeURIComponent(messageID)}`
        ),
        api<Message[]>(
          `/threads/${encodeURIComponent(this.id)}/messages?after=${encodeURIComponent(messageID)}`
        ),
        api<Message>(`/messages/${encodeURIComponent(messageID)}`)
      ]);
      if (this.disposed || generation !== this.generation) return;
      this.messages = [...older, target, ...newer].slice(-ThreadView.windowLimit);
      this.hasOlder = older.length >= ThreadView.pageLimit;
      this.atNewest = newer.length === 0;
    } catch (error) {
      if (this.disposed || generation !== this.generation) return;
      if (error instanceof APIError && error.status === 404) this.missing = true;
      else this.error = errorMessage(error);
    } finally {
      if (!this.disposed && generation === this.generation) this.loading = false;
    }
  }

  // older prepends exactly one page and keeps the window bounded: the overflow is
  // dropped from the newest end, which is where the reader is not looking. The
  // earlier design incremented a page count and re-read every accumulated page, so
  // each "load earlier" cost more than the last and the window never stopped
  // growing.
  async older() {
    if (this.disposed || !this.hasOlder || !this.messages.length) return;
    const generation = ++this.generation;
    this.loading = true;
    try {
      const oldest = this.messages[0].id;
      const page = await api<Message[]>(
        `/threads/${encodeURIComponent(this.id)}/messages?before=${encodeURIComponent(oldest)}`
      );
      if (this.disposed || generation !== this.generation) return;
      // The server returns the page oldest-first, so the next older page starts
      // before its first row. Taking the last would re-read the page just fetched.
      const combined = [...page, ...this.messages];
      this.messages =
        combined.length > ThreadView.windowLimit
          ? combined.slice(0, ThreadView.windowLimit)
          : combined;
      this.hasOlder = page.length >= ThreadView.pageLimit;
      this.atNewest = false;
    } catch (error) {
      if (this.disposed || generation !== this.generation) return;
      this.error = errorMessage(error);
    } finally {
      if (!this.disposed && generation === this.generation) this.loading = false;
    }
  }

  // receive folds a live reply into the window. A reply for another thread, or one
  // older than what is already held, is ignored rather than re-ordered: the view is
  // assembled by the same cursor walk as the initial read, and a live event that
  // jumped the queue would break that.
  receive(event: RealtimeEvent) {
    const data = event as unknown as { thread_id?: string; id?: string };
    if (data?.thread_id !== this.id) return;
    void this.reloadOnNewReply();
  }

  private async reloadOnNewReply() {
    if (this.disposed) return;
    if (!this.messages.length) {
      await this.load();
      return;
    }
    const generation = ++this.generation;
    try {
      // Only what is newer than the window's newest row is fetched, so a live reply
      // costs one page rather than the whole history — and the window stays bounded
      // when it arrives.
      const newest = this.messages[this.messages.length - 1].id;
      const page = await api<Message[]>(
        `/threads/${encodeURIComponent(this.id)}/messages?after=${encodeURIComponent(newest)}`
      );
      if (this.disposed || generation !== this.generation) return;
      if (!page.length) return;
      const combined = [...this.messages, ...page];
      this.messages = combined.slice(-ThreadView.windowLimit);
      if (combined.length > ThreadView.windowLimit) this.hasOlder = true;
    } catch {
      // A live refresh that fails leaves the window as it was; the person can still
      // ask for older replies, and the next live event will try again.
    }
  }

  // A thread reply is sent through the workspace's own send, not through a private
  // HTTP call. The private one had its own outbox (none), its own draft handling
  // (none) and its own account fence (none), so a lost response, a refused send and
  // an account switch all behaved differently in a thread than in the channel it
  // lives in — three cases that ought to be one case.
  //
  // The caller's key is passed in rather than generated, because a thread composer
  // freezes its key for the life of an unresolved send: rotating it after a caught
  // failure is what turns a send that may have committed into a second message.
  //
  // The resolved outcome is returned as well as recorded, because the composer's next
  // step differs for every one of them: only `sent` may clear a draft or a saved draft,
  // `dropped` belongs to a view that no longer exists, and the other two keep the
  // author's text. Inferring that from the absence of an error is what made a dropped
  // send look successful.
  async send(content: string, idempotencyKey: string): Promise<ThreadSendResult> {
    if (this.sending || this.missing) return 'refused';
    const trimmed = content.trim();
    if (!trimmed || !this.thread) return 'refused';
    this.sending = true;
    this.sendError = null;
    const key = idempotencyKey;
    try {
      const result: ThreadSendOutcome = await workspace.sendIntoThread(this.id, trimmed, key);
      if (result.state === 'sent') {
        const message = result.message;
        this.pending.delete(key);
        if (!this.messages.some((existing) => existing.id === message.id))
          this.messages = [...this.messages, message];
        return 'sent';
      }
      // The view has been cleared; there is nothing left to show the answer in.
      if (result.state === 'dropped') return 'dropped';
      // A definite refusal and an unknown outcome are different things, and neither is
      // a send that happened: the composer keeps the text and the operation key, and
      // only the unknown one warns that it may have been sent.
      this.sendError = {
        status: result.error instanceof APIError ? result.error.status : undefined,
        outcome: result.state,
        message: (result.error ? errorMessage(result.error) : '') || 'That reply was not sent.'
      };
      return result.state;
    } catch (error) {
      // An unexpected throw still belongs to the person who pressed Reply, not to a
      // console they will never read. It is an unknown outcome: the request may have
      // committed before the throw.
      this.sendError = {
        status: error instanceof APIError ? error.status : undefined,
        outcome: 'uncertain',
        message: errorMessage(error)
      };
      return 'uncertain';
    } finally {
      this.sending = false;
    }
  }

  async setState(state: Thread['state']) {
    if (!this.thread) return;
    const updated = await api<Thread>(`/threads/${encodeURIComponent(this.id)}/state`, 'PUT', {
      state
    });
    this.thread = updated;
  }

  // markRead moves this reader's position in the thread, and only in this thread.
  //
  // The cursor is the server's to keep and monotonic there, so the same position is
  // never re-sent: the panel calls this whenever the newest replies are on screen, and
  // a refused position is simply offered again later rather than assumed to have
  // landed. It deliberately never touches the channel's cursor — the channel the
  // thread lives in has its own position, and reading forty replies here must not
  // mark the channel read up to the thread's newest reply.
  private lastReadMarker = '';
  async markRead(messageID: string) {
    if (!messageID || messageID <= this.lastReadMarker) return;
    try {
      await api(`/threads/${encodeURIComponent(this.id)}/read`, 'POST', {
        last_read_message_id: messageID
      });
      if (messageID > this.lastReadMarker) this.lastReadMarker = messageID;
    } catch {
      /* A position the server did not confirm is not progress. */
    }
  }

  async setFollow(follow: boolean) {
    if (!this.thread) return;
    const result = await api<{ following: boolean }>(
      `/threads/${encodeURIComponent(this.id)}/follow`,
      follow ? 'POST' : 'DELETE'
    );
    // The server is the authority on whether the follow landed. Reflecting the
    // request instead would leave a refused follow looking like it worked.
    this.thread = { ...this.thread, following: result.following };
  }

  dispose() {
    this.disposed = true;
    this.generation += 1;
    this.pending.clear();
  }
}
