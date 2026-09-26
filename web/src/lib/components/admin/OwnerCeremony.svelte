<script lang="ts">
  import { api, errorMessage, requiresFreshAuthentication } from '../../api';
  import { workspace } from '../../workspace.svelte';

  // The ownership ceremony had an API and no interface, so an owner had to use the
  // command line to hand the installation to someone else (completion review F-09).
  // It is a fresh-authentication ceremony: the server refuses with an actionable 403
  // until this account has confirmed a recent strong sign-in, and the refusal is
  // explained here instead of being shown as a bare error.
  let username = $state('');
  let confirmText = $state('');
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let needsConfirmation = $state(false);

  const owner = $derived(workspace.state?.users.find((user) => user.is_owner));
  const designated = $derived(workspace.state?.authorization.owner_designated ?? false);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = '';
    notice = '';
    needsConfirmation = false;
    try {
      await api('/owner', 'POST', {
        username: username.trim().toLowerCase(),
        confirm: confirmText
      });
      const message = `@${username.trim().toLowerCase()} is now the installation owner.`;
      notice = message;
      // Transferring ownership away from this account takes the operator tier with it,
      // so this ceremony is unmounted by the refresh below. The confirmation has to
      // outlive it, or the person who just moved the installation would see the form
      // disappear and never learn whether it worked.
      workspace.showToast(message);
      username = '';
      confirmText = '';
      await workspace.refreshState();
    } catch (failure) {
      if (requiresFreshAuthentication(failure)) needsConfirmation = true;
      error = errorMessage(failure);
    } finally {
      busy = false;
    }
  }
</script>

<details class="owner-ceremony">
  <summary>Installation owner</summary>
  <p class="muted">
    {#if designated}
      The owner is @{owner?.username ?? 'unknown'}. The owner holds the capabilities no role can
      delegate — a full archive and account credential recovery — so transferring it is a separate
      ceremony that needs a recent sign-in. The new owner must be an existing, active account;
      ownership is never inferred from administrator status.
    {:else}
      No owner is designated for this installation. Until one is, an administrator can complete the
      ceremony; afterwards only the owner can move it. The owner must be an existing, active
      account.
    {/if}
  </p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if needsConfirmation}<p class="notice" role="status">
      This ceremony needs an account that has enrolled a second factor and signed in recently.
      <button
        class="secondary"
        onclick={() => {
          workspace.preferencesSection = 'Account';
          workspace.settingsOpen = true;
        }}>Open account security</button
      >
    </p>{/if}
  {#if notice}<p class="success-note" role="status">{notice}</p>{/if}
  <form class="admin-form" onsubmit={submit}>
    <div class="form-grid">
      <label>New owner username<input bind:value={username} required autocomplete="off" /></label
      ><label
        >Type the username to confirm<input
          bind:value={confirmText}
          required
          autocomplete="off"
        /></label
      >
    </div>
    <button
      class="primary"
      disabled={busy || !username.trim() || confirmText !== username.trim().toLowerCase()}
      >{busy ? 'Saving…' : designated ? 'Transfer ownership' : 'Designate owner'}</button
    >
  </form>
</details>
