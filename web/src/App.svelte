<script lang="ts">
  import { onMount } from 'svelte';
  import { trackViewport } from './lib/viewport';
  import { workspace } from './lib/workspace.svelte';
  import AuthPage from './lib/components/AuthPage.svelte';
  import Workspace from './lib/components/Workspace.svelte';
  import Icon from './lib/components/Icon.svelte';
  import LazyPanel from './lib/components/LazyPanel.svelte';
  import UpdateNotice from './lib/components/UpdateNotice.svelte';
  import { updates } from './lib/updates.svelte';
  const installing = location.pathname === '/install';
  let systemDark = $state(matchMedia('(prefers-color-scheme: dark)').matches);

  $effect(() => {
    document.documentElement.dataset.theme =
      workspace.theme === 'system' ? (systemDark ? 'dark' : 'light') : workspace.theme;
    document.documentElement.dataset.density = workspace.density;
    document.documentElement.dataset.lowResource = String(workspace.lowResource);
    document.title = workspace.state
      ? `${workspace.channel ? '#' + workspace.channel.name + ' · ' : ''}${workspace.state.settings.server_name} · BrrpBrrp`
      : 'BrrpBrrp';
  });
  onMount(() => {
    const stopViewport = trackViewport();
    const media = matchMedia('(prefers-color-scheme: dark)');
    const themeChanged = () => (systemDark = media.matches);
    media.addEventListener('change', themeChanged);
    if (installing)
      return () => {
        stopViewport();
        media.removeEventListener('change', themeChanged);
      };
    workspace.start();
    const stopUpdates = updates.start();
    let activity = 0;
    const active = () => {
      if (Date.now() - activity > 10000) {
        activity = Date.now();
        workspace.sendEvent('activity', {});
      }
    };
    const focus = () => workspace.markRead();
    const shortcut = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === 'k' &&
        workspace.user &&
        !document.querySelector('dialog[open]')
      ) {
        event.preventDefault();
        if (event.shiftKey) workspace.searchOpen = true;
        else workspace.switcherOpen = true;
      }
      active();
    };
    window.addEventListener('keydown', shortcut);
    window.addEventListener('pointerdown', active);
    window.addEventListener('focus', focus);
    document.addEventListener('visibilitychange', focus);
    return () => {
      stopViewport();
      media.removeEventListener('change', themeChanged);
      stopUpdates();
      workspace.stop();
      window.removeEventListener('keydown', shortcut);
      window.removeEventListener('pointerdown', active);
      window.removeEventListener('focus', focus);
      document.removeEventListener('visibilitychange', focus);
    };
  });
</script>

{#if installing}<LazyPanel
    title="Server setup"
    load={() => import('./lib/components/InstallWizard.svelte')}
    componentProps={{
      dark: workspace.theme === 'system' ? systemDark : workspace.theme === 'dark'
    }}
  />
{:else if workspace.loading}<div class="app-loading">
    <span class="brand-mark">b.</span>
    <p>Opening your space…</p>
  </div>
{:else if workspace.error}<div class="app-loading">
    <span class="brand-mark">b.</span>
    <h1>We couldn't reach your server.</h1>
    <p class="error">{workspace.error}</p>
    <button class="primary" onclick={() => workspace.start()}>Try again</button>
  </div>
{:else if workspace.state && workspace.inviteToken}<div class="app-loading">
    <h1>You have an invitation.</h1>
    <p>
      You’re already signed in as {workspace.user?.username}. Sign out to create a new account with
      this invitation.
    </p>
    <div class="button-row">
      <button class="primary" onclick={() => workspace.logout()}>Sign out to use invitation</button
      ><button class="secondary" onclick={() => (workspace.inviteToken = '')}
        >Keep my current account</button
      >
    </div>
  </div>
{:else if workspace.state}<Workspace />
{:else}<AuthPage
    dark={workspace.theme === 'system' ? systemDark : workspace.theme === 'dark'}
  />{/if}
{#if workspace.toast && !installing}<div class="toast" role="status">
    <span>{workspace.toast}</span><button
      class="icon-button"
      aria-label="Dismiss notification"
      onclick={() => (workspace.toast = '')}><Icon name="close" size={16} /></button
    >
  </div>{/if}

{#if !installing}<UpdateNotice />{/if}
