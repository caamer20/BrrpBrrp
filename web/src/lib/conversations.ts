import type { Message } from './types';

export function canGroup(message: Message, previous?: Message): boolean {
  return (
    !!previous &&
    !message.reply_to_id &&
    !message.pinned &&
    message.user_id === previous.user_id &&
    new Date(message.created_at).toDateString() === new Date(previous.created_at).toDateString() &&
    new Date(message.created_at).getTime() - new Date(previous.created_at).getTime() < 5 * 60 * 1000
  );
}

export function firstUnread(messages: Message[], after: string | null, userId?: string): string {
  if (after === null) return '';
  return (
    messages.find((message) => !message.pending && message.id > after && message.user_id !== userId)
      ?.id || ''
  );
}
