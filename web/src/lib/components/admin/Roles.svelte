<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../../api';
  import { capabilityGroups, allCapabilities } from '../../permissions';
  import type { Role } from '../../types';
  import { workspace } from '../../workspace.svelte';
  import RoleAssignments from './RoleAssignments.svelte';

  let roles = $state<Role[]>([]);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let editing = $state<Role | null>(null);
  let creating = $state(false);
  let name = $state('');
  let position = $state(20);
  let permissions = $state<string[]>([]);

  async function load() {
    roles = await api<Role[]>('/roles');
  }
  onMount(() => {
    load()
      .catch((e) => (error = errorMessage(e)))
      .finally(() => (loading = false));
  });

  function create() {
    editing = null;
    creating = true;
    name = '';
    position = 20;
    permissions = [
      'channel.read',
      'message.send',
      'message.react',
      'message.upload',
      'voice.connect',
      'voice.speak'
    ];
    error = '';
    notice = '';
  }
  function edit(role: Role) {
    creating = false;
    editing = role;
    name = role.name;
    position = role.position;
    permissions = [...role.permissions];
    error = '';
    notice = '';
  }
  function toggle(capability: string, checked: boolean) {
    permissions = checked
      ? [...permissions, capability]
      : permissions.filter((granted) => granted !== capability);
  }
  function close() {
    editing = null;
    creating = false;
    error = '';
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!editing && !creating) return;
    busy = true;
    error = '';
    notice = '';
    try {
      const body = { name, position, permissions };
      if (editing) await api(`/roles/${editing.id}`, 'PATCH', body);
      else await api('/roles', 'POST', body);
      notice = editing
        ? 'Role updated. Everyone holding it now has the new grants.'
        : 'Role created.';
      close();
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  // The server refuses a deletion that would strand the installation, so the
  // panel explains the reason before offering the button.
  function deletableReason(role: Role) {
    if (!role.editable) return `The ${role.name} role sits at or above your own authority.`;
    if (role.is_default) return 'The default role cannot be deleted.';
    if (role.is_system) return 'A built-in role cannot be deleted.';
    return 'Move its members to another role first.';
  }
  async function remove(role: Role) {
    if (!confirm(`Delete the ${role.name} role? No account holds it.`)) return;
    busy = true;
    error = '';
    notice = '';
    try {
      await api(`/roles/${role.id}`, 'DELETE');
      notice = 'Role deleted.';
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  // Operator capabilities are the non-delegable tier: only the installation
  // owner may grant them, so the checkbox says who can rather than failing on
  // submit.
  const operatorGrantable = (capability: string) =>
    !!workspace.user?.is_owner ||
    !allCapabilities.find((entry) => entry.name === capability)?.operator;
</script>

<div class="section-heading">
  <div>
    <h3>Shape what each role can do</h3>
    <p>
      Roles add authority; they never remove it. A role you create must sit below your own position,
      and you can only grant capabilities you already hold.
    </p>
  </div>
  <button class="primary" onclick={create}>Create role</button>
</div>
{#if error}<p class="error-banner" role="alert">{error}</p>{/if}{#if notice}<p
    class="success-note"
    role="status"
  >
    {notice}
  </p>{/if}
{#if creating || editing}
  <form class="admin-form" onsubmit={submit}>
    <h3>{creating ? 'Create a role' : `Edit ${editing?.name}`}</h3>
    <div class="form-grid">
      <label
        >Name<input
          bind:value={name}
          maxlength={60}
          required
          disabled={!!editing?.is_system}
        /><small
          >{editing?.is_system
            ? 'A built-in role keeps its name so an upgrade cannot silently move authority.'
            : '1–60 characters.'}</small
        ></label
      ><label
        >Position<input
          type="number"
          min={1}
          max={99}
          bind:value={position}
          required
          disabled={!!editing?.is_system}
        /><small
          >{editing?.is_system
            ? 'A built-in role keeps its position.'
            : 'Higher positions outrank lower ones. This role must sit below yours.'}</small
        ></label
      >
    </div>
    {#each capabilityGroups as group (group.title)}
      <fieldset class="capability-group">
        <legend>{group.title}</legend>
        <p class="muted">{group.detail}</p>
        <div class="permission-grid">
          {#each group.capabilities as capability (capability.name)}
            <label class="check-label">
              <input
                type="checkbox"
                checked={permissions.includes(capability.name)}
                disabled={!operatorGrantable(capability.name)}
                title={!operatorGrantable(capability.name)
                  ? 'Only the installation owner can grant this capability.'
                  : capability.detail}
                onchange={(event) => toggle(capability.name, event.currentTarget.checked)}
              />{capability.label}</label
            >
          {/each}
        </div>
      </fieldset>
    {/each}
    <div class="button-row">
      <button class="secondary" type="button" disabled={busy} onclick={close}>Cancel</button><button
        class="primary"
        disabled={busy}>{busy ? 'Saving…' : editing ? 'Save role' : 'Create role'}</button
      >
    </div>
  </form>
{/if}
{#if loading}<p class="muted">Loading roles…</p>{/if}
<div class="admin-list">
  {#each roles as role (role.id)}
    <article class="admin-row">
      <div class="admin-row-label">
        <strong>{role.name}</strong><small
          >Position {role.position} · {role.members}
          {role.members === 1 ? 'account' : 'accounts'} · {role.permissions.length}
          {role.permissions.length === 1 ? 'capability' : 'capabilities'}</small
        >
        <span class="chip-row">
          {#if role.is_default}<span class="chip">Default for new accounts</span>{/if}
          {#if role.is_system}<span class="chip">Built in</span>{/if}
          {#if role.permissions.includes('channel.manage')}<span class="chip"
              >Community manager</span
            >{/if}
        </span>
      </div>
      <details class="row-menu">
        <summary>Manage</summary>
        <div class="row-menu-content">
          {#if role.editable}
            <button onclick={() => edit(role)}>Edit capabilities</button>
          {:else}
            <button disabled title={`The ${role.name} role sits at or above your own authority.`}
              >Edit capabilities</button
            >
          {/if}
          {#if role.deletable}
            <button class="danger-text" disabled={busy} onclick={() => remove(role)}
              >Delete role</button
            >
          {:else}
            <button disabled title={deletableReason(role)}>Delete role</button>
          {/if}
        </div>
      </details>
    </article>
  {/each}
</div>

<RoleAssignments />
