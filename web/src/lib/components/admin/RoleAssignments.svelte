<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../../api';
  import type { Role } from '../../types';

  // The account list a role manager may see. It carries identity and role
  // assignments only, because assigning a role needs neither credential state nor
  // moderation state, and the wider account list stays behind member.manage
  // (review R2-06).
  interface Summary {
    id: string;
    username: string;
    display_name: string;
    is_admin: boolean;
    is_owner: boolean;
    role_ids: string[];
    assignable: boolean;
  }

  let accounts = $state<Summary[]>([]);
  let roles = $state<Role[]>([]);
  let selected = $state('');
  let chosen = $state<string[]>([]);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');

  async function refresh() {
    accounts = await api<Summary[]>('/admin/accounts/summary');
    roles = await api<Role[]>('/roles');
  }
  onMount(() => {
    refresh()
      .catch((e) => (error = errorMessage(e)))
      .finally(() => (loading = false));
  });

  function pick(id: string) {
    selected = id;
    notice = '';
    const account = accounts.find((entry) => entry.id === id);
    chosen = account ? [...account.role_ids] : [];
  }

  async function save() {
    const account = accounts.find((entry) => entry.id === selected);
    if (!account) return;
    busy = true;
    error = '';
    notice = '';
    try {
      await api(`/admin/users/${account.id}/roles`, 'PUT', { role_ids: chosen });
      notice = `Roles saved for ${account.username}.`;
      await refresh();
      pick(account.id);
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  const current = $derived(accounts.find((entry) => entry.id === selected));
</script>

<section class="admin-form">
  <h3>Assign roles</h3>
  <p>
    Pick an account, then choose the roles it holds. Authority is delegated downward only: roles
    above your own authority are refused by the server, and ownership moves through the ownership
    ceremony instead.
  </p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}{#if notice}<p
      class="success-note"
      role="status"
    >
      {notice}
    </p>{/if}
  {#if loading}<p class="muted" role="status">Loading accounts…</p>{:else}
    <label>
      Account
      <select value={selected} onchange={(event) => pick(event.currentTarget.value)}>
        <option value="">Choose an account…</option>
        {#each accounts as account (account.id)}<option value={account.id}
            >{account.username}{account.is_owner ? ' · owner' : ''}{account.is_admin
              ? ' · administrator'
              : ''}</option
          >{/each}
      </select>
    </label>
    {#if current}
      <fieldset disabled={!current.assignable || busy}>
        <legend>Roles for {current.username}</legend>
        {#if !current.assignable}
          <p class="muted" role="status">
            This account holds the same or higher authority than you do, so its roles cannot be
            changed here.
          </p>
        {/if}
        {#each roles as role (role.id)}<label class="check-label"
            ><input
              type="checkbox"
              checked={chosen.includes(role.id)}
              onchange={(event) =>
                (chosen = event.currentTarget.checked
                  ? [...chosen, role.id]
                  : chosen.filter((id) => id !== role.id))}
            />{role.name}</label
          >{/each}
        <button class="primary" onclick={save} disabled={busy}
          >{busy ? 'Saving…' : 'Save roles'}</button
        >
      </fieldset>
    {/if}
  {/if}
</section>
