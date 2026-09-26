import { readFile } from 'node:fs/promises';

// Disposable local UI fixture. Never point this helper at a deployed server.
const base = 'http://127.0.0.1:8082';
const status = await fetch(`${base}/api/setup/status`).then(response => response.json());
if (status.setup_required) {
  const token = await readFile('/tmp/brrp-preview/setup-token', 'utf8');
  const response = await fetch(`${base}/api/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Origin: base },
    body: JSON.stringify({ token, username: 'preview_user', password: 'Local preview password 2026!', server_name: 'BrrpBrrp' })
  });
  if (!response.ok) throw new Error(`Preview setup failed: ${response.status}`);
}
console.log('Disposable preview account is ready.');
