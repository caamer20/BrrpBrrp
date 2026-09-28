<script lang="ts">
  import Modal from './Modal.svelte';
  import AdminUsers from './admin/Users.svelte';
  import AdminChannels from './admin/Channels.svelte';
  import AdminRoles from './admin/Roles.svelte';
  import Reports from './admin/Reports.svelte';
  import PrivateEvidence from './admin/PrivateEvidence.svelte';
  import AdminSettings from './admin/Settings.svelte';
  import Maintenance from './admin/Maintenance.svelte';
  import AdminInvitations from './admin/Invitations.svelte';
  import { workspace } from '../workspace.svelte';
  // Each tab names the capabilities that make it useful. A delegated manager sees the
  // tabs their authority covers and nothing else, so the panel never offers a section
  // whose every action the server would refuse (completion review F-09).
  const tabs = [
    {
      name: 'People',
      capabilities: ['member.manage', 'operator.credential_recovery']
    },
    { name: 'Invitations', capabilities: ['invite.manage'] },
    { name: 'Channels', capabilities: ['channel.manage', 'category.manage'] },
    { name: 'Roles', capabilities: ['role.manage'] },
    { name: 'Reports', capabilities: ['message.delete_any'] },
    // Private evidence is a separate queue behind its own capability, not another
    // section of Reports: a reviewer of one is not automatically a community
    // moderator, and an account with only message.delete_any must not learn that a
    // private queue exists (ADR 0013).
    { name: 'Private evidence', capabilities: ['report.review_private'] },
    { name: 'Settings', capabilities: ['settings.manage'] },
    { name: 'Maintenance', capabilities: ['operator.backup', 'settings.manage', 'audit.read'] }
  ];
  const allowed = $derived(tabs.filter((entry) => workspace.canAny(...entry.capabilities)));
  // The requested tab may not be one this account may open (a privileged refusal sends
  // an account to Settings, which an owner-only capability does not reach), so fall
  // back to the first tab the account can use.
  const tab = $derived(
    allowed.some((entry) => entry.name === workspace.adminTab)
      ? workspace.adminTab
      : allowed[0]?.name
  );
</script>

<Modal title="Manage your server" close={() => (workspace.adminOpen = false)} wide>
  <nav class="admin-tabs" aria-label="Server administration">
    {#each allowed as entry (entry.name)}
      <button
        class:active={tab === entry.name}
        aria-current={tab === entry.name ? 'page' : undefined}
        onclick={() => (workspace.adminTab = entry.name)}>{entry.name}</button
      >
    {/each}
  </nav>
  {#if !allowed.length}
    <p class="muted" role="status">Your account does not hold any server management capability.</p>
  {:else if tab === 'People'}<AdminUsers />{:else if tab === 'Invitations'}<AdminInvitations
    />{:else if tab === 'Channels'}<AdminChannels />{:else if tab === 'Roles'}<AdminRoles
    />{:else if tab === 'Private evidence'}<PrivateEvidence />{:else if tab === 'Reports'}<Reports
    />{:else if tab === 'Settings'}<AdminSettings />{:else}<Maintenance />{/if}
</Modal>
