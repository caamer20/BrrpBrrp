<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, requiresFreshAuthentication } from '../../api';
  import type { Role, User } from '../../types';
  import { workspace } from '../../workspace.svelte';
  import Avatar from '../Avatar.svelte';
  import Invitations from './Invitations.svelte';
  import OwnerCeremony from './OwnerCeremony.svelte';
  let users = $state<User[]>([]);
  let error = $state('');
  let notice = $state('');
  let busy = $state(false);
  let loading = $state(true);
  let creating = $state(false);
  let username = $state('');
  let displayName = $state('');
  let password = $state('');
  let admin = $state(false);
  let target = $state<User | null>(null);
  let action = $state('');
  let confirm = $state('');
  let reason = $state('');
  let timeoutMinutes = $state('60');
  // Role assignment is its own panel: it replaces a set rather than confirming a
  // single destructive action, so it does not fit the confirm-by-username flow.
  let roleTarget = $state<User | null>(null);
  let roleCatalog = $state<Role[]>([]);
  let assigned = $state<string[]>([]);
  let savingRoles = $state(false);
  // Recovering another account's password is operator authority, so the server
  // may refuse until this account confirms a recent sign-in.
  let needsConfirmation = $state(false);
  async function load() {
    users = await api<User[]>('/admin/users');
  }
  onMount(() => {
    load()
      .catch((e) => (error = errorMessage(e)))
      .finally(() => (loading = false));
  });
  function choose(user: User, next: string) {
    target = user;
    action = next;
    confirm = '';
    password = '';
    reason = '';
    error = '';
    notice = '';
    creating = false;
    roleTarget = null;
  }
  async function chooseRoles(user: User) {
    target = null;
    creating = false;
    confirm = '';
    password = '';
    error = '';
    notice = '';
    roleTarget = user;
    try {
      if (!roleCatalog.length) roleCatalog = await api<Role[]>('/roles');
      const current = await api<{ user: User; roles: Role[] }>(`/admin/users/${user.id}/roles`);
      assigned = current.roles.map((role) => role.id);
    } catch (e) {
      error = errorMessage(e);
    }
  }
  async function saveRoles(event: SubmitEvent) {
    event.preventDefault();
    if (!roleTarget) return;
    savingRoles = true;
    error = '';
    notice = '';
    try {
      await api(`/admin/users/${roleTarget.id}/roles`, 'PUT', { role_ids: assigned });
      notice = `Roles updated for @${roleTarget.username}.`;
      roleTarget = null;
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      savingRoles = false;
    }
  }
  // Each action names the capability the server checks for it, so the row menu offers
  // exactly the operations this account may perform instead of discovering refusals.
  const canRecoverCredentials = $derived(workspace.can('operator.credential_recovery'));
  const canManageRoles = $derived(workspace.can('role.manage'));
  const canManageMembers = $derived(workspace.can('member.manage'));

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    notice = '';
    needsConfirmation = false;
    try {
      if (creating) {
        await api('/admin/users', 'POST', {
          username,
          display_name: displayName,
          password,
          is_admin: admin
        });
        username = '';
        displayName = '';
        admin = false;
        creating = false;
        notice = 'Account created. Share its credentials privately.';
      } else if (target) {
        const path = `/admin/users/${target.id}`;
        if (action === 'Reset password')
          await api(`${path}/reset-password`, 'POST', { confirm, new_password: password });
        else if (action === 'Remove second factor') await api(`${path}/mfa`, 'DELETE', { confirm });
        else if (action === 'Sign out') await api(`${path}/kick`, 'POST', { confirm });
        else if (action === 'Delete account') await api(path, 'DELETE', { confirm });
        else if (action === 'Remove account, keep messages')
          await api(`${path}/anonymize`, 'POST', { confirm });
        else if (action === 'Suspend for a while') {
          const minutes = Number(timeoutMinutes);
          if (!Number.isFinite(minutes) || minutes < 1)
            throw new Error('Choose how many minutes the timeout lasts.');
          await api(`${path}/timeout`, 'POST', { minutes, reason });
        } else if (action === 'End timeout early')
          await api(`${path}/timeout`, 'DELETE', { confirm });
        else if (action === 'Ban' || action === 'Unban')
          await api(path, 'PATCH', { confirm, is_banned: action === 'Ban', ban_reason: reason });
        else if (action === 'Deactivate' || action === 'Reactivate')
          await api(path, 'PATCH', { confirm, is_deactivated: action === 'Deactivate' });
        else await api(path, 'PATCH', { confirm, is_admin: !target.is_admin });
        notice =
          action === 'Remove second factor'
            ? 'Second factor removed. Tell them to enroll a new one as soon as they sign in.'
            : action === 'Suspend for a while'
              ? `Account suspended. It regains exactly the access it had when the timeout started, in ${timeoutMinutes} minutes.`
              : action === 'End timeout early'
                ? 'Timeout ended. The account can act again.'
                : 'Account updated.';
        target = null;
      }
      password = '';
      await load();
    } catch (e) {
      if (requiresFreshAuthentication(e)) needsConfirmation = true;
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="section-heading">
  <div>
    <h3>A place for your people</h3>
    <p>Accounts are invitation-only. You decide who joins.</p>
  </div>
  {#if canManageMembers}<button
      class="primary"
      onclick={() => {
        creating = true;
        target = null;
        password = '';
        error = '';
      }}>Create account</button
    >{/if}
</div>
{#if workspace.can('invite.manage')}<Invitations />
{/if}
{#if workspace.can('operator.ownership_transfer')}<OwnerCeremony />{/if}
{#if error}<p class="error-banner" role="alert">{error}</p>{/if}
{#if needsConfirmation}<p class="notice" role="status">
    This action needs an account that has enrolled a second factor and signed in recently.
    <button
      class="secondary"
      onclick={() => {
        workspace.preferencesSection = 'Account';
        workspace.settingsOpen = true;
      }}>Open account security</button
    >
  </p>{/if}
{#if notice}<p class="success-note" role="status">{notice}</p>{/if}
{#if creating || target}
  <form class="admin-form" onsubmit={submit}>
    <h3>{creating ? 'Create an account' : `${action}: @${target?.username}`}</h3>
    {#if creating}
      <div class="form-grid">
        <label
          >Username<input
            bind:value={username}
            required
            pattern={'[A-Za-z0-9_]{3,32}'}
            autocomplete="off"
          /></label
        ><label>Display name<input bind:value={displayName} maxlength={80} /></label>
      </div>
      {#if canManageRoles}<label class="check-label"
          ><input type="checkbox" bind:checked={admin} />Administrator access</label
        >{/if}
    {:else}
      <p>
        {action === 'Delete account'
          ? 'This permanently removes the account and its messages from live conversations. Selected message text already included in reports may remain until the report’s 90-day expiry. Server backups follow the operator’s retention policy. This cannot be undone.'
          : action === 'Remove account, keep messages'
            ? 'This permanently removes sign-in access and profile details. Existing messages and files remain as Deleted member, and their content may still identify the author. Report explanations and selected message evidence remain available until each report’s 90-day expiry; backups follow the operator’s retention policy. This cannot be undone. Choose Deactivate instead if you may need to restore this account.'
            : action === 'Deactivate'
              ? 'This prevents sign-in and ends current sessions. Their messages and account are kept so you can reactivate them later.'
              : action === 'Ban'
                ? 'This removes the person from chat and voice, and prevents sign-in.'
                : action === 'Remove second factor'
                  ? 'Use this only when someone has lost both the authenticator app and every recovery code. Their sessions end and they can sign in with their password alone until they enroll a new factor. This is recorded in the audit log.'
                  : action === 'Reset password' || action === 'Sign out'
                    ? 'All current sessions will end, including voice connections.'
                    : 'This changes the permissions available to this account.'}
      </p>
      {#if action === 'Ban'}<label
          >Reason (optional)<input bind:value={reason} maxlength={300} /></label
        >{/if}
      <label
        >Type {target?.username} to confirm<input
          bind:value={confirm}
          required
          autocomplete="off"
        /></label
      >
    {/if}
    {#if creating || action === 'Reset password'}<label
        >{creating ? 'Initial password' : 'New password'}<input
          type="password"
          bind:value={password}
          required
          minlength={12}
          maxlength={72}
          autocomplete="new-password"
        /><small>12–72 bytes. Use a unique passphrase.</small></label
      >{/if}
    <div class="button-row">
      <button
        class="secondary"
        type="button"
        disabled={busy}
        onclick={() => {
          target = null;
          creating = false;
          password = '';
        }}>Cancel</button
      ><button class="primary" disabled={busy || (!creating && confirm !== target?.username)}
        >{busy ? 'Saving…' : creating ? 'Create account' : action}</button
      >
    </div>
  </form>
{/if}
{#if roleTarget}
  <form class="admin-form" onsubmit={saveRoles}>
    <h3>Roles for @{roleTarget.username}</h3>
    <p>
      Roles add authority. Removing one takes it away on the account's next request, and the account
      is told to refresh what it has open. You can only hand out what you already hold.
    </p>
    <div class="permission-grid">
      {#each roleCatalog as role (role.id)}
        <label class="check-label">
          <input
            type="checkbox"
            checked={role.is_default || assigned.includes(role.id)}
            disabled={role.is_default ||
              !role.editable ||
              (!assigned.includes(role.id) && !role.grantable)}
            title={role.is_default
              ? 'Every account holds this role.'
              : !role.editable
                ? `The ${role.name} role sits at or above your own authority.`
                : !role.grantable
                  ? `You cannot grant ${role.name} because you do not hold its capabilities.`
                  : `Position ${role.position}`}
            onchange={(event) =>
              (assigned = event.currentTarget.checked
                ? [...assigned, role.id]
                : assigned.filter((id) => id !== role.id))}
          />{role.name}<small>{role.permissions.length} cap.</small></label
        >
      {/each}
    </div>
    {#if !roleCatalog.length}<p class="muted">Loading roles…</p>{/if}
    <div class="button-row">
      <button
        class="secondary"
        type="button"
        disabled={savingRoles}
        onclick={() => (roleTarget = null)}>Cancel</button
      ><button class="primary" disabled={savingRoles}
        >{savingRoles ? 'Saving…' : 'Save roles'}</button
      >
    </div>
  </form>
{/if}
{#if loading}<p class="muted">Loading accounts…</p>{/if}
<div class="admin-list">
  {#each users as user (user.id)}
    <article class="admin-row">
      <Avatar {user} />
      <div class="admin-row-label">
        <strong>{user.display_name || user.username}</strong><small
          >{user.is_deleted ? 'Account removed · Messages kept' : `@${user.username}`}
          {user.is_deleted ? '' : '·'}
          {user.is_deleted
            ? ''
            : user.is_banned
              ? 'Banned'
              : user.is_deactivated
                ? 'Deactivated'
                : user.is_admin
                  ? 'Administrator'
                  : 'Member'}</small
        >
      </div>
      <details class="row-menu">
        <summary>Manage</summary>
        <div class="row-menu-content">
          {#if !user.is_deleted}
            {#if canRecoverCredentials}<button onclick={() => choose(user, 'Reset password')}
                >Reset password</button
              >{#if user.is_owner}<button
                  disabled
                  title="An owner's second factor is recovered on the server host with brrpbrrp recover-owner."
                  >Remove lost second factor</button
                >{:else}<button onclick={() => choose(user, 'Remove second factor')}
                  >Remove lost second factor</button
                >{/if}{/if}
            {#if canManageRoles}<button onclick={() => chooseRoles(user)}
                >Roles and authority</button
              >{/if}
            {#if canManageMembers}<button onclick={() => choose(user, 'Sign out')}
                >Sign out everywhere</button
              >{/if}
            {#if canManageMembers && user.id !== workspace.user?.id}<button
                onclick={() => choose(user, user.is_deactivated ? 'Reactivate' : 'Deactivate')}
                >{user.is_deactivated ? 'Reactivate' : 'Deactivate'}</button
              ><button onclick={() => choose(user, user.is_banned ? 'Unban' : 'Ban')}
                >{user.is_banned ? 'Unban' : 'Ban'}</button
              ><button onclick={() => choose(user, 'Suspend for a while')}>Timeout</button
              >{#if user.in_timeout}<button onclick={() => choose(user, 'End timeout early')}
                  >End timeout</button
                >{/if}
              >{#if canManageRoles}<button
                  onclick={() => choose(user, user.is_admin ? 'Remove admin' : 'Make admin')}
                  >{user.is_admin ? 'Remove admin' : 'Make admin'}</button
                >{/if}
              <button onclick={() => choose(user, 'Remove account, keep messages')}
                >Remove account, keep messages</button
              >
            {/if}
          {/if}
          {#if canManageMembers && user.id !== workspace.user?.id}<button
              class="danger-text"
              onclick={() => choose(user, 'Delete account')}>Erase account and messages</button
            >{/if}
        </div>
      </details>
    </article>
  {/each}
</div>
