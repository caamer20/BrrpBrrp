<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../../api';
  import type { Invitation, Role } from '../../types';
  let invitations = $state<Invitation[]>([]);
  let roles = $state<Role[]>([]);
  let hours = $state(24);
  let selected = $state<string[]>([]);
  let link = $state('');
  let busy = $state(false);
  let error = $state('');
  const refresh = async () => (invitations = await api<Invitation[]>('/admin/invitations'));
  onMount(() => {
    refresh().catch((e) => (error = errorMessage(e)));
    api<Role[]>('/roles')
      .then((list) => (roles = list))
      .catch(() => {
        /* Invitations without roles stay available. */
      });
  });
  async function create() {
    busy = true;
    error = '';
    link = '';
    try {
      const result = await api<{ url: string }>('/admin/invitations', 'POST', {
        hours,
        role_ids: selected
      });
      link = result.url;
      selected = [];
      await refresh();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function revoke(id: string) {
    busy = true;
    error = '';
    try {
      await api(`/admin/invitations/${id}`, 'DELETE');
      link = '';
      await refresh();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<section class="admin-form">
  <h3>Invite someone</h3>
  <p>
    Send a private, single-use link. They choose their own password. Links expire automatically.
  </p>
  <div class="button-row">
    <label
      >Expires in<select bind:value={hours}
        ><option value={1}>1 hour</option><option value={24}>1 day</option><option value={168}
          >7 days</option
        ></select
      ></label
    ><button class="primary" disabled={busy} onclick={create}>Create invitation</button>
  </div>
  <fieldset class="capability-group">
    <legend>Roles for the new account</legend>
    <p class="muted">
      They receive these grants when they accept. You can only promise authority you hold, and the
      server checks that again at acceptance, so a link never outlives the authority that made it.
      Leave this empty for an ordinary member.
    </p>
    <div class="permission-grid">
      {#each roles.filter((role) => !role.is_default && role.grantable) as role (role.id)}
        <label class="check-label">
          <input
            type="checkbox"
            checked={selected.includes(role.id)}
            onchange={(event) =>
              (selected = event.currentTarget.checked
                ? [...selected, role.id]
                : selected.filter((id) => id !== role.id))}
          />{role.name}<small>Position {role.position}</small></label
        >
      {/each}
    </div>
    {#if !roles.some((role) => !role.is_default && role.grantable)}<p class="muted">
        No role you hold outranks another, so there is nothing to promise here yet.
      </p>{/if}
  </fieldset>
  {#if link}<label
      >Copy this invitation link<input
        value={link}
        readonly
        onclick={(event) => event.currentTarget.select()}
      /><small>This link is shown only now. Share it privately with one person.</small></label
    >{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#each invitations as invitation (invitation.id)}<div class="admin-row">
      <span
        >Expires {new Date(invitation.expires_at).toLocaleString()}{invitation.roles.length
          ? ` · ${invitation.roles.join(', ')}`
          : ''}</span
      ><button class="secondary" disabled={busy} onclick={() => revoke(invitation.id)}
        >Revoke</button
      >
    </div>{/each}
</section>
