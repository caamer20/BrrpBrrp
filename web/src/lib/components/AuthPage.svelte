<script lang="ts">
  import { tick } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import Icon from './Icon.svelte';
  let { dark }: { dark: boolean } = $props();
  let showPassword = $state(false);
  let codeInput = $state<HTMLInputElement>();

  let username = $state('');
  let password = $state('');
  let code = $state('');
  let needsCode = $state(false);
  let displayName = $state('');
  let serverName = $state('BrrpBrrp');
  const initialToken = new URLSearchParams(location.search).get('token') || '';
  let token = $state(initialToken);
  let busy = $state(false);
  let error = $state('');
  if (initialToken) history.replaceState(null, '', '/setup');

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = '';
    const settingUp = workspace.setupRequired;
    try {
      if (workspace.setupRequired)
        await api('/setup', 'POST', { username, password, server_name: serverName, token });
      else if (workspace.inviteToken) {
        await api('/invitations/accept', 'POST', {
          token: workspace.inviteToken,
          username,
          password,
          display_name: displayName
        });
        workspace.inviteToken = '';
      } else {
        const result = await api<{ second_factor_required?: boolean }>('/login', 'POST', {
          username,
          password,
          code
        });
        if (result.second_factor_required) {
          needsCode = true;
          await tick();
          codeInput?.focus();
          return;
        }
      }
      password = '';
      await workspace.enter();
      if (settingUp) {
        workspace.adminTab = 'Maintenance';
        workspace.adminOpen = true;
      }
    } catch (failure) {
      error = errorMessage(failure);
    } finally {
      busy = false;
    }
  }
</script>

<div class="auth-page">
  <button
    class="theme-switch icon-button"
    aria-label={`Use ${dark ? 'light' : 'dark'} theme`}
    onclick={() => {
      workspace.theme = dark ? 'light' : 'dark';
      workspace.savePreferences();
    }}><Icon name={dark ? 'sun' : 'moon'} /></button
  >
  <div class="auth-brand"><span class="brand-mark">b.</span><span>BrrpBrrp</span></div>
  <section class="auth-card">
    <div class="eyebrow">
      {workspace.setupRequired
        ? 'MAKE YOURSELF AT HOME'
        : workspace.inviteToken
          ? 'YOUR INVITATION'
          : 'GOOD TO SEE YOU'}
    </div>
    <h1>
      {workspace.setupRequired
        ? 'Your space starts here.'
        : workspace.inviteToken
          ? 'You’re invited.'
          : 'Welcome back.'}
    </h1>
    <p class="muted">
      {workspace.setupRequired
        ? 'Create the first administrator account for your community.'
        : workspace.inviteToken
          ? 'Choose your own username and password to join.'
          : 'Sign in to catch up with your people.'}
    </p>
    <form onsubmit={submit}>
      {#if workspace.inviteToken}<label
          >Display name<input
            bind:value={displayName}
            maxlength="80"
            autocomplete="nickname"
          /></label
        >{/if}
      {#if workspace.setupRequired}
        <label
          >Server name<input
            bind:value={serverName}
            maxlength="80"
            required
            autocomplete="organization"
          /></label
        >
        <label
          >Setup token<input bind:value={token} type="password" required autocomplete="off" /><small
            >Found in the setup-token file on your server.</small
          ></label
        >
      {/if}
      <label
        >Username<input
          bind:value={username}
          minlength="3"
          maxlength="32"
          pattern="[a-zA-Z0-9_]+"
          required
          autocomplete="username"
          autocapitalize="none"
          spellcheck="false"
          placeholder="Your username"
        />{#if workspace.setupRequired || workspace.inviteToken}<small
            >3–32 letters, numbers, or underscores.</small
          >{/if}</label
      >
      <label
        >Password<span class="password-field"
          ><input
            bind:value={password}
            type={showPassword ? 'text' : 'password'}
            minlength="12"
            maxlength="72"
            required
            autocomplete={workspace.setupRequired || workspace.inviteToken
              ? 'new-password'
              : 'current-password'}
            placeholder={workspace.setupRequired || workspace.inviteToken
              ? 'At least 12 characters'
              : 'Your password'}
          /><button
            type="button"
            aria-label={showPassword ? 'Hide password' : 'Show password'}
            aria-pressed={showPassword}
            onclick={() => (showPassword = !showPassword)}>{showPassword ? 'Hide' : 'Show'}</button
          ></span
        ></label
      >
      {#if needsCode}<label
          >Authenticator or recovery code<input
            bind:value={code}
            bind:this={codeInput}
            required
            autocomplete="one-time-code"
            maxlength="24"
          /><small>Each code can be used once.</small></label
        >{/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="primary full" disabled={busy} type="submit"
        >{busy
          ? 'One moment…'
          : workspace.setupRequired
            ? 'Create your space'
            : workspace.inviteToken
              ? 'Join the community'
              : 'Sign in'}<Icon name="arrow" size={18} /></button
      >
    </form>
    {#if !workspace.setupRequired}<p class="auth-note">
        Need an account or a password reset?<br />Ask your server administrator.
      </p>{/if}
  </section>
  <p class="auth-footer">A little place to hang out.</p>
</div>
