export class APIError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
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
      throw new APIError(result?.error || 'The request failed. Please try again.', response.status);
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

export function uploadFile<T>(file: File, progress: (percent: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', '/api/uploads');
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
    const data = new FormData();
    data.append('file', file);
    request.send(data);
  });
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
