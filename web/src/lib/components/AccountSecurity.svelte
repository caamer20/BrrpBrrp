<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';

  type Status = {
    enabled: boolean;
    recovery_codes_remaining: number;
    required: boolean;
    recently_authenticated: boolean;
  };
  type SessionRecord = {
    id: string;
    created_at: string;
    last_seen_at: string;
    user_agent: string;
    current: boolean;
    recently_authenticated: boolean;
  };

  let status = $state<Status | null>(null);
  let sessions = $state<SessionRecord[]>([]);
  let password = $state('');
  let code = $state('');
  let secret = $state('');
  let recovery = $state<string[]>([]);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');

  // Timestamps arrive with nanosecond precision, which not every engine parses, so
  // trim the fraction to milliseconds before showing it.
  function at(value: string): string {
    if (!value) return 'unknown';
    const parsed = new Date(value.replace(/(\.\d{3})\d+Z$/, '$1Z'));
    return Number.isNaN(parsed.getTime()) ? 'unknown' : parsed.toLocaleString();
  }

  async function refresh() {
    status = await api('/me/mfa');
    sessions = await api('/me/sessions');
  }

  onMount(() => {
    refresh().catch((e) => (error = errorMessage(e)));
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    notice = '';
    try {
      if (status?.enabled) {
        await api('/me/mfa/disable', 'POST', { password, code });
        recovery = [];
        code = '';
        password = '';
        notice = 'Two-factor authentication is off.';
        await refresh();
      } else if (secret) {
        const result = await api<{ recovery_codes: string[] }>('/me/mfa/enable', 'POST', { code });
        recovery = result.recovery_codes;
        secret = '';
        code = '';
        notice = 'Two-factor authentication is on.';
        await refresh();
      } else {
        const result = await api<{ secret: string }>('/me/mfa/setup', 'POST', { password });
        secret = result.secret;
        password = '';
      }
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  async function confirmSignIn(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    notice = '';
    try {
      await api('/me/reauth', 'POST', { password, code });
      password = '';
      code = '';
      notice = 'Confirmed. Actions that need a fresh sign-in are unlocked.';
      await refresh();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  async function revoke(session: SessionRecord) {
    busy = true;
    error = '';
    notice = '';
    try {
      await api(`/me/sessions/${session.id}`, 'DELETE');
      if (session.current) {
        // The server already cleared this browser's session, so the workspace
        // cannot keep running against it.
        workspace.settingsOpen = false;
        workspace.state = null;
        workspace.stop();
        return;
      }
      notice = 'Signed that device out.';
      await refresh();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  async function revokeOthers() {
    busy = true;
    error = '';
    notice = '';
    try {
      const result = await api<{ revoked: number }>('/me/sessions', 'DELETE');
      notice =
        result.revoked === 0
          ? 'No other devices were signed in.'
          : `Signed out ${result.revoked} other ${result.revoked === 1 ? 'device' : 'devices'}.`;
      await refresh();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<form class="settings-section" onsubmit={submit}>
  <h3>Two-factor authentication</h3>
  <p class="muted">
    {status?.enabled
      ? `Enabled · ${status.recovery_codes_remaining} recovery codes left.`
      : 'Add an authenticator app for a second step when you sign in.'}
  </p>
  {#if status?.required && !status.enabled}
    <p class="error" role="status">
      Your account owns or administers this server, so a second factor is required before actions
      that reach other people's private conversations. Your recovery codes and the offline server
      command still work if you lose your authenticator.
    </p>
  {/if}
  {#if recovery.length}
    <div class="recovery-codes">
      <strong>Save these recovery codes now.</strong>
      <p>Each code works once. They will not be shown again.</p>
      <pre>{recovery.join('\n')}</pre>
      <button
        class="secondary"
        type="button"
        onclick={() => {
          recovery = [];
        }}>I have saved my codes</button
      >
    </div>
  {:else if status}
    {#if secret}
      <p>
        Add a time-based account in your authenticator app with this setup key. Enter its six-digit
        code within ten minutes.
      </p>
      <code class="setup-key">{secret}</code>
    {:else}<label
        >Current password<input
          type="password"
          bind:value={password}
          required
          autocomplete="current-password"
        /></label
      >{/if}
    {#if secret || status.enabled}<label
        >Authenticator or recovery code<input
          bind:value={code}
          required
          autocomplete="one-time-code"
          maxlength="24"
        /></label
      >{/if}
    <button class="secondary" disabled={busy}
      >{busy
        ? 'Saving…'
        : status.enabled
          ? 'Turn off two-factor authentication'
          : secret
            ? 'Verify and enable'
            : 'Set up authenticator'}</button
    >
  {/if}
</form>

<section class="settings-section">
  <h3>Signed-in devices</h3>
  <p class="muted">Sessions are listed without their credentials. Revoking one ends it at once.</p>
  {#if sessions.length}
    <ul class="sessions">
      {#each sessions as session (session.id)}
        <li>
          <span
            ><strong>{session.user_agent || 'Unknown device'}</strong>{#if session.current}<em
                >This device</em
              >{/if}<small
              >Started {at(session.created_at)} · last active {at(session.last_seen_at)} · {session.recently_authenticated
                ? 'recently confirmed'
                : 'not recently confirmed'}</small
            ></span
          >
          <button class="secondary" disabled={busy} onclick={() => revoke(session)}
            >{session.current ? 'Sign out here' : 'Sign out'}</button
          >
        </li>
      {/each}
    </ul>
    <button class="secondary" disabled={busy} onclick={revokeOthers}>Sign out other devices</button>
  {:else}<p class="muted">No other devices are signed in.</p>{/if}
</section>

{#if status && !status.recently_authenticated}
  <form class="settings-section" onsubmit={confirmSignIn}>
    <h3>Confirm your sign-in</h3>
    <p class="muted">
      Archiving the server, recovering an account's password and transferring ownership need a
      sign-in from the last fifteen minutes. Confirm your password to unlock them.
    </p>
    <label
      >Current password<input
        type="password"
        bind:value={password}
        required
        autocomplete="current-password"
      /></label
    >
    {#if status.enabled}<label
        >Authenticator or recovery code<input
          bind:value={code}
          required
          autocomplete="one-time-code"
          maxlength="24"
        /></label
      >{/if}
    <button class="secondary" disabled={busy}>{busy ? 'Confirming…' : 'Confirm sign-in'}</button>
  </form>
{/if}

{#if notice}<p class="muted" role="status">{notice}</p>{/if}
{#if error}<p class="error" role="alert">{error}</p>{/if}
