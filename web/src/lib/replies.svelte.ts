import { api, APIError, errorMessage } from './api';
import type { Message, RealtimeEvent, ReplyPage } from './types';

// One bounded view, with a generation guard for replies arriving during a fetch.
// It never changes channel history or read markers.
export class Replies {
  parent = $state<Message | null>(null);
  messages = $state<Message[]>([]);
  loading = $state(true);
  error = $state('');
  missing = $state(false);
  hasOlder = $state(false);
  pages = $state(1);
  private generation = 0;
  private disposed = false;
  private timer?: ReturnType<typeof setTimeout>;

  constructor(
    readonly id: string,
    private loaded: (messages: Message[]) => void = () => {}
  ) {}

  async load() {
    if (this.disposed || this.missing) return;
    const generation = ++this.generation;
    this.loading = true;
    this.error = '';
    try {
      let before = '';
      let messages: Message[] = [];
      let page: ReplyPage | undefined;
      for (let i = 0; i < this.pages; i++) {
        page = await api<ReplyPage>(
          `/messages/${this.id}/replies${before ? `?before=${encodeURIComponent(before)}` : ''}`
        );
        if (this.disposed || generation !== this.generation) return;
        messages = [...page.replies, ...messages];
        if (!page.has_older || !page.replies.length) break;
        before = page.replies[0].id;
      }
      this.parent = page!.parent;
      this.messages = messages;
      this.hasOlder = page!.has_older;
      this.loaded(messages);
    } catch (error) {
      if (this.disposed || generation !== this.generation) return;
      if (error instanceof APIError && error.status === 404) this.removeParent();
      else this.error = errorMessage(error);
    } finally {
      if (!this.disposed && generation === this.generation) this.loading = false;
    }
  }

  async older() {
    if (this.loading || this.pages >= 5) return;
    this.pages++;
    await this.load();
  }

  receive(event: RealtimeEvent) {
    if (this.disposed || this.missing) return;
    if (event.t === 'message.deleted' && event.d.id === this.id) {
      this.removeParent();
      return;
    }
    const relevant =
      event.t === 'hello' ||
      event.t === 'reconnect.required' ||
      (event.t.startsWith('channel.') &&
        (!event.d?.id || event.d.id === this.parent?.channel_id)) ||
      ((event.t.startsWith('message.') || event.t === 'reaction.updated') &&
        (event.d.id === this.id ||
          event.d.reply_to_id === this.id ||
          this.messages.some((message) => message.id === event.d.id)));
    if (!relevant) return;
    if (event.t === 'message.deleted')
      this.messages = this.messages.filter((message) => message.id !== event.d.id);
    ++this.generation;
    if (!this.timer)
      this.timer = setTimeout(() => {
        this.timer = undefined;
        void this.load();
      }, 120);
  }

  private removeParent() {
    ++this.generation;
    clearTimeout(this.timer);
    this.missing = true;
    this.loading = false;
    this.parent = null;
    this.messages = [];
    this.error = 'This discussion is no longer available. Return to the channel to continue.';
  }

  dispose() {
    this.disposed = true;
    ++this.generation;
    clearTimeout(this.timer);
  }
}
