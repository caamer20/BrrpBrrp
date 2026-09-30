export class APIError extends Error {
  // detail carries whatever structured fields the server sent alongside the
  // message. A conflict that has to be shown differently — a refreshed preview the
  // person must re-read, for instance — cannot be acted on from a message alone,
  // and throwing the fields away is what made that flow impossible (completion
  // review 10, R10-03). It is the server's own body, never anything assembled here.
  detail: unknown;
  constructor(
    message: string,
    public status: number,
    detail?: unknown
  ) {
    super(message);
    this.detail = detail;
  }
}

export async function api<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  { timeoutMs = 30000 }: { timeoutMs?: number } = {}
): Promise<T> {
  const controller = new AbortController();
  const timeout = timeoutMs > 0 ? setTimeout(() => controller.abort(), timeoutMs) : undefined;
  try {
    const response = await fetch(`/api${path}`, {
      method,
      credentials: 'same-origin',
      signal: controller.signal,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body)
    });
    const result = await response.json().catch(() => null);
    if (!response.ok)
      throw new APIError(
        result?.error || 'The request failed. Please try again.',
        response.status,
        result
      );
    if (result === null)
      throw new Error('The server returned an unreadable response. Please try again.');
    return result as T;
  } catch (error) {
    if (controller.signal.aborted)
      throw new Error('The server took too long to respond. Please try again.');
    if (error instanceof TypeError)
      throw new Error('Couldn’t reach your server. Check your connection and try again.');
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

/**
 * Uploads a file, reporting progress and handing back a way to stop.
 *
 * The request handle is returned because an upload that cannot be cancelled is an
 * upload somebody is trapped in: the only way out is to close the tab, which loses the
 * draft along with it. Returning `{ promise, abort }` is what makes a cancel button
 * possible at all, and a cancel that abandons the promise alone is not a cancel.
 */
export function uploadFile<T>(
  file: File,
  progress: (percent: number) => void,
  conversationId?: string
): { promise: Promise<T>; abort: () => void } {
  const request = new XMLHttpRequest();
  const promise = new Promise<T>((resolve, reject) => {
    request.open(
      'POST',
      `/api/uploads${conversationId ? '?conversation_id=' + encodeURIComponent(conversationId) : ''}`
    );
    request.responseType = 'json';
    request.timeout = 60000;
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) progress(Math.round((event.loaded / event.total) * 100));
    };
    request.onload = () => {
      if (request.status >= 200 && request.status < 300) resolve(request.response);
      else reject(new Error(request.response?.error || 'Upload failed.'));
    };
    request.onerror = () => reject(new Error('Upload interrupted. Check your connection.'));
    request.ontimeout = () => reject(new Error('The upload timed out. Please try again.'));
    // A deliberate cancel is not a failure. Rejecting with a distinguishable error is
    // what stops the composer showing "Upload failed" for something the person just
    // asked to stop.
    request.onabort = () => reject(new UploadCancelled());
    const data = new FormData();
    data.append('file', file);
    request.send(data);
  });
  return { promise, abort: () => request.abort() };
}

/** Raised when an upload was stopped on purpose. Never shown as a failure. */
export class UploadCancelled extends Error {
  constructor() {
    super('Upload cancelled.');
    this.name = 'UploadCancelled';
  }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}

// requiresFreshAuthentication reports whether the server refused an action because
// the account must confirm a password or enrol a second factor first. Those
// actions can defeat a privacy boundary, so the client explains the next step
// instead of presenting the refusal as a generic failure.
export function requiresFreshAuthentication(error: unknown): boolean {
  return (
    error instanceof APIError &&
    error.status === 403 &&
    (error.message.includes('Confirm your password') ||
      error.message.includes('Enroll two-factor authentication'))
  );
}
