import type { Attachment } from './types';
export type ComposerDrafts = Record<string, { text: string; attachments: Attachment[] }>;
const key = 'brrp.update-drafts';
const uuid = /^[\da-f-]{36}$/;
type SessionStore = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

export function saveUpdateDrafts(
  store: SessionStore,
  user: string,
  drafts: ComposerDrafts,
  reply: string | undefined,
  now = Date.now()
) {
  const populated = Object.fromEntries(
    Object.entries(drafts)
      .filter(([, draft]) => draft.text.trim() || draft.attachments.length)
      .slice(-100)
  );
  if (!Object.keys(populated).length) {
    store.removeItem(key);
    return;
  }
  // This temporary handoff is created only by the user's explicit Update action.
  store.setItem(key, JSON.stringify({ user, drafts: populated, reply, expires: now + 86400000 }));
}

export function consumeUpdateDrafts(
  store: SessionStore,
  user: string,
  now = Date.now()
): { drafts: ComposerDrafts; reply?: string } | null {
  try {
    const raw = store.getItem(key);
    if (!raw) return null;
    store.removeItem(key);
    const saved = JSON.parse(raw);
    if (
      saved.user !== user ||
      !Number.isFinite(saved.expires) ||
      saved.expires < now ||
      saved.expires > now + 86400000 ||
      !saved.drafts ||
      typeof saved.drafts !== 'object'
    )
      return null;
    const drafts: ComposerDrafts = {};
    for (const [channel, value] of Object.entries(saved.drafts).slice(-100)) {
      const draft = value as { text?: unknown; attachments?: unknown };
      if (!/^(thread:)?[\da-f-]{36}$/.test(channel) || !draft || typeof draft.text !== 'string')
        continue;
      const attachments = Array.isArray(draft.attachments)
        ? draft.attachments
            .filter(
              (item): item is Attachment =>
                !!item &&
                uuid.test(item.id) &&
                typeof item.file_name === 'string' &&
                item.file_name.length <= 255 &&
                typeof item.mime_type === 'string' &&
                item.mime_type.length <= 100 &&
                Number.isFinite(item.file_size) &&
                item.file_size >= 0 &&
                item.file_size <= 12582912
            )
            .slice(0, 5)
        : [];
      drafts[channel] = { text: draft.text.slice(0, 4000), attachments };
    }
    return {
      drafts,
      reply: typeof saved.reply === 'string' && uuid.test(saved.reply) ? saved.reply : undefined
    };
  } catch {
    return null;
  }
}

export function clearUpdateDrafts(store: SessionStore) {
  try {
    store.removeItem(key);
  } catch {
    /* Storage may be blocked. */
  }
}
