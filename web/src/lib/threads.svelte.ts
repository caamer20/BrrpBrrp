import { api, APIError, errorMessage } from './api';
import { workspace } from './workspace.svelte';
import type { Message, RealtimeEvent } from './types';

// A thread is a channel conversation with a subject (ADR 0017). This store holds one
// open thread as a bounded window rather than the whole history: a thread with a
// thousand replies is read by asking for older pages, so the client never holds more
// than it shows. The generation guard is what stops a slow page from landing after the
// person has moved on, and `missing` is what stops a deleted thread from being
// re-requested on every render.

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
  sendError = $state<{ status?: number; message: string } | null>(null);
  missing = $state(false);
  hasOlder = $state(false);
  pages = $state(1);
  private generation = 0;
  private disposed = false;
  // The replies already held locally, matched by the account's own send key. A send
  // whose response is lost is shown from here rather than re-sent.
  private pending = new Map<string, Message>();

  constructor(readonly id: string) {}

  async load() {
    if (this.disposed || this.missing) return;
    const generation = ++this.generation;
    this.loading = true;
    this.error = '';
    try {
      this.thread = await api<Thread>(`/threads/${encodeURIComponent(this.id)}`);
      if (this.disposed || generation !== this.generation) return;
      await this.readPage(generation);
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

  private async readPage(generation: number) {
    let before = '';
    let messages: Message[] = [];
    let page: Message[] = [];
    for (let i = 0; i < this.pages; i++) {
      const path = `/threads/${encodeURIComponent(this.id)}/messages${
        before ? `?before=${encodeURIComponent(before)}` : ''
      }`;
      page = await api<Message[]>(path);
      if (this.disposed || generation !== this.generation) return;
      messages = [...page, ...messages];
      if (page.length < 50) break;
      // The server returns the page oldest-first, so the next older page starts
      // before its first row. Taking the last would re-read the page just fetched.
      before = page[0].id;
    }
    this.messages = messages;
    this.hasOlder = page.length >= 50;
  }

  async older() {
    const generation = ++this.generation;
    this.pages += 1;
    this.loading = true;
    try {
      await this.readPage(generation);
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
    const generation = this.generation;
    try {
      await this.readPage(generation);
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
  async send(content: string, idempotencyKey: string) {
    if (this.sending || this.missing) return;
    const trimmed = content.trim();
    if (!trimmed || !this.thread) return;
    this.sending = true;
    this.sendError = null;
    const key = idempotencyKey;
    try {
      const message = await workspace.sendIntoThread(this.id, trimmed, key);
      if (!message) return;
      this.pending.delete(key);
      if (!this.messages.some((existing) => existing.id === message.id))
        this.messages = [...this.messages, message];
    } catch (error) {
      // A definite refusal wrote nothing, so the same key is still free. An unknown
      // outcome may well have committed, and the receipt is what says so — so the
      // error is kept rather than cleared, and the person is not invited to send it
      // again under a new key.
      this.sendError = {
        status: error instanceof APIError ? error.status : undefined,
        message: errorMessage(error)
      };
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
