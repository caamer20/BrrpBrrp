<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import type { Category, Channel, ChannelAccess, Role } from '../../types';
  import Icon from '../Icon.svelte';

  let editing = $state<Channel | null>(null);
  let action = $state('');
  let confirm = $state('');
  let days = $state(30);
  let error = $state('');
  let notice = $state('');
  let busy = $state(false);

  let categories = $state<Category[]>([]);
  let roles = $state<Role[]>([]);
  let categoryEditing = $state<Category | null>(null);
  let categoryCreating = $state(false);
  let categoryName = $state('');
  let categoryPosition = $state(0);

  // The access panel and the inspector are one question asked twice: the panel
  // changes the answer, the inspector states it.
  let accessChannel = $state<Channel | null>(null);
  let access = $state<ChannelAccess | null>(null);
  let accessBusy = $state(false);
  let accessError = $state('');
  let accessNotice = $state('');

  async function loadCategories() {
    categories = await api<Category[]>('/categories');
  }
  onMount(() => {
    loadCategories().catch((e) => (error = errorMessage(e)));
    api<Role[]>('/roles')
      .then((list) => (roles = list))
      .catch(() => {
        /* The access editor is usable without the role catalogue. */
      });
  });

  function choose(channel: Channel, next: string) {
    editing = { ...channel, slow_mode_seconds: channel.slow_mode_seconds ?? 0 };
    action = next;
    confirm = '';
    error = '';
    notice = '';
    accessChannel = null;
  }
  function create() {
    editing = {
      id: '',
      name: '',
      type: 'text',
      category: '',
      position: workspace.state?.channels.length || 0,
      topic: '',
      max_voice_users: 8,
      slow_mode_seconds: 0,
      visibility: 'public',
      posting: 'members',
      authorization_revision: 1
    };
    action = 'Create';
    error = '';
    accessChannel = null;
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!editing) return;
    busy = true;
    error = '';
    try {
      if (action === 'Delete') await api(`/channels/${editing.id}`, 'DELETE', { confirm });
      else if (action === 'Purge')
        await api(`/admin/channels/${editing.id}/purge`, 'POST', {
          confirm,
          older_than_days: days
        });
      else
        await api(
          `/channels${editing.id ? '/' + editing.id : ''}`,
          editing.id ? 'PATCH' : 'POST',
          editing
        );
      editing = null;
      notice = 'Channel updated.';
      // Category counts move with the channel, so both lists are refreshed.
      await loadCategories();
      await workspace.refreshState();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  // openAccess answers "who can access this?" with the server's own rule, so the
  // panel cannot promise access the server would refuse.
  async function openAccess(channel: Channel) {
    accessChannel = channel;
    access = null;
    accessError = '';
    accessNotice = '';
    editing = null;
    try {
      access = await api<ChannelAccess>(`/channels/${channel.id}/access`);
    } catch (e) {
      accessError = errorMessage(e);
    }
  }
  async function saveAccess(event: SubmitEvent) {
    event.preventDefault();
    if (!accessChannel || !access) return;
    accessBusy = true;
    accessError = '';
    accessNotice = '';
    try {
      // Posting policy is a channel property; visibility and the explicit grants
      // are one authorization change and are written together, so a conversion
      // to private cannot land without the list that makes it readable.
      if (access.posting !== accessChannel.posting)
        await api(`/channels/${accessChannel.id}`, 'PATCH', { posting: access.posting });
      await api(`/channels/${accessChannel.id}/access`, 'PUT', {
        visibility: access.visibility,
        role_ids: access.roles,
        user_ids: access.users
      });
      await openAccess(accessChannel);
      await workspace.refreshState();
      if (!accessError) accessNotice = 'Access updated.';
    } catch (e) {
      accessError = errorMessage(e);
    } finally {
      accessBusy = false;
    }
  }
  function toggleRole(id: string, checked: boolean) {
    if (!access) return;
    access.roles = checked ? [...access.roles, id] : access.roles.filter((role) => role !== id);
  }
  function toggleUser(id: string, checked: boolean) {
    if (!access) return;
    access.users = checked ? [...access.users, id] : access.users.filter((user) => user !== id);
  }

  function createCategory() {
    categoryCreating = true;
    categoryEditing = null;
    categoryName = '';
    categoryPosition = categories.length;
    error = '';
  }
  function editCategory(category: Category) {
    categoryCreating = false;
    categoryEditing = category;
    categoryName = category.name;
    categoryPosition = category.position;
    error = '';
  }
  async function submitCategory(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    notice = '';
    try {
      if (categoryEditing)
        await api(`/categories/${categoryEditing.id}`, 'PATCH', {
          name: categoryName,
          position: categoryPosition
        });
      else await api('/categories', 'POST', { name: categoryName, position: categoryPosition });
      notice = 'Categories updated.';
      categoryEditing = null;
      categoryCreating = false;
      await loadCategories();
      await workspace.refreshState();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function removeCategory(category: Category) {
    // The local `confirm` state holds the typed channel name, so the browser
    // confirmation is named explicitly.
    if (
      !window.confirm(
        `Delete the ${category.name} category? Its ${category.channels} channel(s) must be moved first.`
      )
    )
      return;
    busy = true;
    error = '';
    notice = '';
    try {
      await api(`/categories/${category.id}`, 'DELETE');
      notice = 'Category deleted.';
      await loadCategories();
      await workspace.refreshState();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  const categoryNames = $derived([
    ...new Set([
      ...categories.map((category) => category.name),
      ...(editing?.category ? [editing.category] : [])
    ])
  ]);
  const activeAccounts = $derived(
    (workspace.state?.users || []).filter((user) => !user.is_banned && !user.is_deactivated)
  );
  const roleName = (id: string) => roles.find((role) => role.id === id)?.name || id;
  const accountName = (id: string) => {
    const user = workspace.state?.users.find((entry) => entry.id === id);
    return user ? `@${user.username}` : id;
  };
</script>

<div class="section-heading">
  <div>
    <h3>Give conversations a home</h3>
    <p>Organize text channels, make room for voice, and decide who can walk in.</p>
  </div>
  <button class="primary" onclick={create}>Create channel</button>
</div>
{#if error}<p class="error-banner" role="alert">{error}</p>{/if}{#if notice}<p
    class="success-note"
    role="status"
  >
    {notice}
  </p>{/if}

<section class="settings-section">
  <div class="section-heading">
    <div>
      <h3>Categories</h3>
      <p>Categories order the sidebar. Renaming one never changes a channel's visibility.</p>
    </div>
    <button class="secondary" onclick={createCategory}>Add category</button>
  </div>
  {#if categoryCreating || categoryEditing}
    <form class="admin-form" onsubmit={submitCategory}>
      <h3>{categoryCreating ? 'Add a category' : `Rename ${categoryEditing?.name}`}</h3>
      <div class="form-grid">
        <label>Name<input bind:value={categoryName} maxlength={60} required /></label><label
          >Position<input
            type="number"
            min={0}
            max={1000}
            bind:value={categoryPosition}
            required
          /><small>Lower positions appear first.</small></label
        >
      </div>
      <div class="button-row">
        <button
          class="secondary"
          type="button"
          disabled={busy}
          onclick={() => {
            categoryCreating = false;
            categoryEditing = null;
          }}>Cancel</button
        ><button class="primary" disabled={busy}>{busy ? 'Saving…' : 'Save category'}</button>
      </div>
    </form>
  {/if}
  <div class="admin-list">
    {#each categories as category (category.id)}
      <article class="admin-row">
        <div class="admin-row-label">
          <strong>{category.name}</strong><small
            >Position {category.position} · {category.channels}
            {category.channels === 1 ? 'channel' : 'channels'}</small
          >
        </div>
        <details class="row-menu">
          <summary>Manage</summary>
          <div class="row-menu-content">
            <button onclick={() => editCategory(category)}>Rename or move</button><button
              class="danger-text"
              disabled={busy}
              onclick={() => removeCategory(category)}>Delete category</button
            >
          </div>
        </details>
      </article>
    {/each}
    {#if !categories.length}<p class="muted">No categories yet. Add one to group channels.</p>{/if}
  </div>
</section>

{#if editing}
  <form class="admin-form" onsubmit={submit}>
    <h3>{action} {editing.name || 'channel'}</h3>
    {#if action === 'Delete' || action === 'Purge'}
      <p>
        {action === 'Delete'
          ? 'This permanently deletes the channel and all its messages. Voice participants will be disconnected.'
          : 'This permanently deletes messages and attachments older than the selected number of days.'}
      </p>
      {#if action === 'Purge'}<label
          >Delete messages older than (days)<input
            type="number"
            min={0}
            max={36500}
            bind:value={days}
            required
          /><small>0 deletes all messages in this channel.</small></label
        >{/if}
      <label
        >Type {editing.name} to confirm<input
          bind:value={confirm}
          required
          autocomplete="off"
        /></label
      >
    {:else}
      <div class="form-grid">
        <label>Name<input bind:value={editing.name} maxlength={60} required /></label><label
          >Type<select bind:value={editing.type} disabled={!!editing.id}
            ><option value="text">Text</option><option value="voice">Voice</option></select
          ></label
        ><label
          >Category<select bind:value={editing.category}
            ><option value="">Uncategorized</option>{#each categoryNames as name (name)}<option
                value={name}>{name}</option
              >{/each}</select
          ></label
        ><label
          >Order<input
            type="number"
            min={0}
            max={1000}
            bind:value={editing.position}
            required
          /></label
        >
      </div>
      <label>Topic<input bind:value={editing.topic} maxlength={500} /></label>
      {#if editing.type === 'text'}<label
          >Time between messages (seconds)<input
            type="number"
            min={0}
            max={21600}
            step={1}
            bind:value={editing.slow_mode_seconds}
            required
          />
          <small
            >Slow mode gives conversations room to breathe. Use 0 to turn it off, or up to 21600
            seconds (6 hours).</small
          >
        </label>{/if}
      {#if editing.type === 'voice'}<label
          >Voice capacity<input
            type="number"
            min={1}
            max={10}
            bind:value={editing.max_voice_users}
            required
          /><small
            >How many people may be in this channel at once, up to 10. The server carries 20
            participants across all of its rooms.</small
          ></label
        >{/if}
      <p class="muted">
        Visibility and the accounts that may read this channel are set in “Who can access this?”.
      </p>
    {/if}
    <div class="button-row">
      <button type="button" class="secondary" disabled={busy} onclick={() => (editing = null)}
        >Cancel</button
      ><button
        class="primary"
        disabled={busy || ((action === 'Delete' || action === 'Purge') && confirm !== editing.name)}
        >{busy ? 'Saving…' : action === 'Edit' ? 'Save channel' : `${action} channel`}</button
      >
    </div>
  </form>
{/if}

{#if accessChannel}
  <form class="admin-form" onsubmit={saveAccess}>
    <h3>Who can access #{accessChannel.name}?</h3>
    {#if accessError}<p class="error" role="alert">{accessError}</p>{/if}
    {#if accessNotice}<p class="success-note" role="status">{accessNotice}</p>{/if}
    {#if !access && !accessError}<p class="muted">Asking the server…</p>{/if}
    {#if access}
      {#if !access.restricted_enabled}
        <p class="notice" role="status">
          Restricted channels are switched off on this server. Public channels and member posting
          work as usual, and private channels and announcements are refused while the setting is
          off. An operator turns them back on by removing
          <code>BRRP_ENABLE_RESTRICTED_CHANNELS=false</code> from the server’s configuration and restarting
          it.
        </p>
      {/if}
      <dl class="access-facts">
        <div>
          <dt>Visibility</dt>
          <dd>
            {access.visibility === 'private'
              ? 'Private — only the roles and accounts listed below'
              : 'Public — every member can read it'}
          </dd>
        </div>
        <div>
          <dt>Posting</dt>
          <dd>
            {access.posting === 'announcement'
              ? 'Announcements — only accounts that can manage channels may post'
              : 'Everyone who can read it may post'}
          </dd>
        </div>
        <div>
          <dt>May read now</dt>
          <dd>{access.effective_count} accounts</dd>
        </div>
      </dl>
      <!-- What a private voice channel does, shown only while the setting that permits one is on:
           with the gate off the notice above says so instead, and the channel cannot carry a
           restricted audience at all. Both halves are read from the server's own answer. -->
      {#if access.restricted_enabled && accessChannel.type === 'voice' && access.visibility === 'private'}<p
          class="notice"
          role="status"
        >
          Who is in a private voice channel is visible only to accounts that can read it, joining is
          refused exactly like a missing channel, and an account that loses access is removed from
          the call immediately. Voice is carried over IPv4 and the embedded relay only, so a member
          whose network blocks UDP and cannot reach that relay cannot join.
        </p>{/if}
      {#if access.managers_can_read}<p class="muted">
          Community managers can read every community channel, including this one, so a restricted
          channel is not private from them.
        </p>{/if}
      <details class="access-detail">
        <summary>List the {access.effective_count} accounts that can read this channel</summary>
        <div class="chip-row">
          {#each access.effective_members as username (username)}<span class="chip"
              >@{username}</span
            >{/each}
        </div>
      </details>
      <fieldset class="capability-group" disabled={!access.restricted_enabled}>
        <legend>Access</legend>
        <div class="form-grid">
          <label
            >Visibility<select bind:value={access.visibility}
              ><option value="public">Public</option><option value="private">Private</option
              ></select
            ></label
          ><label
            >Posting<select bind:value={access.posting}
              ><option value="members">Everyone who can read it</option><option value="announcement"
                >Announcements only</option
              ></select
            ></label
          >
        </div>
      </fieldset>
      <fieldset class="capability-group" disabled={!access.restricted_enabled}>
        <legend>Roles that may read it</legend>
        <p class="muted">
          A role grant follows the role: everyone holding it gains access, and loses it when the
          role is removed.
        </p>
        <div class="permission-grid">
          {#each roles as role (role.id)}
            <label class="check-label">
              <input
                type="checkbox"
                checked={access.roles.includes(role.id)}
                onchange={(event) => toggleRole(role.id, event.currentTarget.checked)}
              />{role.name}</label
            >
          {/each}
        </div>
        {#if !roles.length}<p class="muted">No roles available to grant.</p>{/if}
      </fieldset>
      <fieldset class="capability-group" disabled={!access.restricted_enabled}>
        <legend>Accounts that may read it</legend>
        <p class="muted">Choose individual people when a role would be too broad.</p>
        <div class="permission-grid account-grid">
          {#each activeAccounts as account (account.id)}
            <label class="check-label">
              <input
                type="checkbox"
                checked={access.users.includes(account.id)}
                onchange={(event) => toggleUser(account.id, event.currentTarget.checked)}
              />{account.display_name || account.username}</label
            >
          {/each}
        </div>
      </fieldset>
      {#if access.roles.length || access.users.length}<p class="muted">
          Granted: {access.roles.map(roleName).join(', ') || 'no roles'}{access.users.length
            ? ` · ${access.users.map(accountName).join(', ')}`
            : ''}
        </p>{/if}
    {/if}
    <div class="button-row">
      <button
        class="secondary"
        type="button"
        disabled={accessBusy}
        onclick={() => (accessChannel = null)}>Close</button
      ><button class="primary" disabled={accessBusy || !access || !access.restricted_enabled}
        >{accessBusy ? 'Saving…' : 'Save access'}</button
      >
    </div>
  </form>
{/if}

<div class="admin-list">
  {#each workspace.state?.channels || [] as channel (channel.id)}<article class="admin-row">
      <Icon name={channel.type === 'voice' ? 'volume' : 'hash'} />
      <div class="admin-row-label">
        <strong>{channel.name}</strong><small
          >{channel.category || 'Uncategorized'} · {channel.type === 'voice'
            ? `${channel.max_voice_users} seats`
            : 'Text channel'} · {channel.visibility === 'private'
            ? 'Private'
            : 'Public'}{channel.posting === 'announcement' ? ' · announcements' : ''}</small
        >
      </div>
      <details class="row-menu">
        <summary>Manage</summary>
        <div class="row-menu-content">
          <button onclick={() => choose(channel, 'Edit')}>Edit channel</button><button
            onclick={() => openAccess(channel)}>Who can access this?</button
          >{#if channel.type === 'text'}<button onclick={() => choose(channel, 'Purge')}
              >Purge messages</button
            >{/if}<button class="danger-text" onclick={() => choose(channel, 'Delete')}
            >Delete channel</button
          >
        </div>
      </details>
    </article>{/each}
</div>
