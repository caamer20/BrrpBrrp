type DraftStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function readDrafts(storage: DraftStorage, userId: string): Record<string, string> {
  try {
    const saved: unknown = JSON.parse(storage.getItem(`brrp.drafts.${userId}`) || '{}');
    if (!saved || typeof saved !== 'object' || Array.isArray(saved)) return {};
    return Object.fromEntries(
      Object.entries(saved)
        .filter(([id, text]) => /^(thread:)?[\da-f-]{36}$/.test(id) && typeof text === 'string')
        .slice(0, 100)
        .map(([id, text]) => [id, (text as string).slice(0, 4000)])
    );
  } catch {
    return {};
  }
}

export function writeDrafts(storage: DraftStorage, userId: string, drafts: Record<string, string>) {
  try {
    storage.setItem(
      `brrp.drafts.${userId}`,
      JSON.stringify(
        Object.fromEntries(
          Object.entries(drafts)
            .filter(([, text]) => text.trim())
            .slice(-100)
        )
      )
    );
  } catch {
    /* A full or blocked browser store must not interrupt typing. */
  }
}
