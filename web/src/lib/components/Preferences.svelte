<script lang="ts">
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import NotificationSettings from './NotificationSettings.svelte';
  import { updates } from '../updates.svelte';
  import Modal from './Modal.svelte';
  import AudioSettings from './AudioSettings.svelte';
  import AccountSecurity from './AccountSecurity.svelte';
  let currentPassword = $state('');
  let newPassword = $state('');
  let busy = $state(false);
  const sections = ['Appearance', 'Audio', 'Notifications', 'Account'];
  let section = $state(workspace.preferencesSection);
  const select = (item: string) => {
    section = item;
    workspace.preferencesSection = item;
  };
  const tabId = $props.id();
  async function password(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    try {
      await api('/me/password', 'POST', {
        current_password: currentPassword,
        new_password: newPassword
      });
      workspace.settingsOpen = false;
      workspace.stop();
      workspace.state = null;
      workspace.showToast('Password changed. Sign in with your new password.');
    } catch (error) {
      workspace.showToast(errorMessage(error));
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Your preferences" close={() => (workspace.settingsOpen = false)}>
  <div class="preference-tabs" role="tablist" aria-label="Preferences sections">
    {#each sections as item, index}<button
        role="tab"
        id={`${tabId}-tab-${index}`}
        aria-controls={`${tabId}-panel`}
        aria-selected={section === item}
        tabindex={section === item ? 0 : -1}
        onclick={() => select(item)}
        onkeydown={(event) => {
          let next = index;
          if (event.key === 'ArrowRight') next = (index + 1) % sections.length;
          else if (event.key === 'ArrowLeft')
            next = (index + sections.length - 1) % sections.length;
          else if (event.key === 'Home') next = 0;
          else if (event.key === 'End') next = sections.length - 1;
          else return;
          event.preventDefault();
          select(sections[next]);
          document.getElementById(`${tabId}-tab-${next}`)?.focus();
        }}>{item}</button
      >{/each}
  </div>
  <div
    id={`${tabId}-panel`}
    role="tabpanel"
    aria-labelledby={`${tabId}-tab-${sections.indexOf(section)}`}
    tabindex="0"
  >
    {#if section === 'Appearance'}
      <div class="settings-section">
        <h3>Appearance</h3>
        <div class="theme-options">
          {#each ['dark', 'light', 'system'] as theme}<button
              class:selected={workspace.theme === theme}
              aria-pressed={workspace.theme === theme}
              onclick={() => {
                workspace.theme = theme;
                workspace.savePreferences();
              }}
              ><span class={`theme-preview ${theme}`}><i></i><i></i><i></i></span>{theme === 'dark'
                ? 'Dark'
                : theme === 'light'
                  ? 'Light'
                  : 'System'}</button
            >{/each}
        </div>
        <label
          >Message spacing<select
            bind:value={workspace.density}
            onchange={() => workspace.savePreferences()}
            ><option value="comfortable">Comfortable</option><option value="compact">Compact</option
            ></select
          ></label
        >
        <label class="toggle-row"
          ><span
            ><strong>Remember drafts and unsent messages on this device</strong><small
              >Save unsent text between visits, including a message whose delivery was never
              confirmed, so it can be reconciled the next time you open the app. Signing out clears
              both. This setting applies only to your account on this device. Delivery checks keep
              attachment references, but never save file contents. Unconfirmed sends stop retrying
              after one day; recovered text is then available for one more day.</small
            ></span
          ><input
            type="checkbox"
            bind:checked={workspace.saveDrafts}
            onchange={() => workspace.savePreferences()}
          /></label
        >
        <label class="toggle-row"
          ><span
            ><strong>Save data &amp; reduce motion</strong><small
              >Load image previews and external media only when you choose.</small
            ></span
          ><input
            type="checkbox"
            bind:checked={workspace.lowResource}
            onchange={() => workspace.savePreferences()}
          /></label
        >
        <label class="toggle-row"
          ><span
            ><strong>Private previews</strong><small
              >Ask before loading media from external sites.</small
            ></span
          ><input
            type="checkbox"
            bind:checked={workspace.privacy}
            onchange={() => workspace.savePreferences()}
          /></label
        >
      </div>
    {:else if section === 'Audio'}<AudioSettings />
    {:else if section === 'Notifications'}<NotificationSettings />
    {:else}<AccountSecurity />
      <form class="settings-section" onsubmit={password}>
        <h3>Change password</h3>
        <label
          >Current password<input
            type="password"
            bind:value={currentPassword}
            required
            autocomplete="current-password"
          /></label
        ><label
          >New password<input
            type="password"
            bind:value={newPassword}
            required
            minlength="12"
            maxlength="72"
            autocomplete="new-password"
          /></label
        >
        <p class="muted">Changing your password signs out all your sessions.</p>
        <button class="secondary" disabled={busy}>Change password</button>
      </form>
      <section class="settings-section">
        <h3>App updates</h3>
        <p class="muted">Updates become available when your administrator updates the server.</p>
        {#if updates.error}<p class="error" role="alert">{updates.error}</p>
        {:else if updates.checked}<p class="muted" role="status">
            {updates.ready
              ? 'An update is ready. Use the update notice when you’re ready to refresh.'
              : 'You’re up to date.'}
          </p>{/if}
        <button class="secondary" disabled={updates.checking} onclick={() => updates.checkNow()}
          >{updates.checking ? 'Checking…' : 'Check for updates'}</button
        >
      </section>
    {/if}
  </div>
</Modal>
