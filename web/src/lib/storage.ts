// Storage can be unavailable in private browsing or full on a shared device.
// Keep preferences usable for this session even when they cannot be persisted.
export const storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> = {
  getItem(key) {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  setItem(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch {
      // The in-memory preference remains active.
    }
  },
  removeItem(key) {
    try {
      localStorage.removeItem(key);
    } catch {
      // The browser may deny all access to storage.
    }
  }
};
