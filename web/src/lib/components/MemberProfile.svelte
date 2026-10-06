<script lang="ts">
  import Modal from './Modal.svelte';
  import Avatar from './Avatar.svelte';
  import { workspace } from '../workspace.svelte';
  import type { User } from '../types';

  // A member's identity as the community shows it: name, handle, role standing and when
  // the account was created. Nothing here is private — no conversations, no unread state,
  // no moderation history — because a profile is what anybody in the workspace can
  // already see in the member list, laid out legibly and reachable by keyboard.
  let { user }: { user: User } = $props();

  const presence = $derived(
    workspace.state?.settings.enable_presence
      ? (workspace.state.presence[user.id] ?? 'offline')
      : ''
  );
  const standing = $derived(user.is_owner ? 'Owner' : user.is_admin ? 'Administrator' : 'Member');
  const joined = $derived(new Date(user.created_at).toLocaleDateString());
</script>

<Modal
  title={`${user.display_name || user.username} — profile`}
  close={() => (workspace.profileUser = null)}
>
  <div class="profile">
    <div class="profile-head">
      <Avatar {user} size={56} />
      <div>
        <h3>{user.display_name || user.username}</h3>
        <p class="handle">@{user.username}</p>
      </div>
    </div>
    <dl class="profile-facts">
      <dt>Role</dt>
      <dd>{standing}{user.id === workspace.user?.id ? ' (that’s you)' : ''}</dd>
      {#if presence}
        <dt>Availability</dt>
        <dd>{presence === 'online' ? 'Online' : presence === 'idle' ? 'Away' : 'Offline'}</dd>
      {/if}
      <dt>Member since</dt>
      <dd>{joined}</dd>
    </dl>
    {#if user.is_deactivated}
      <p class="profile-note">This account is deactivated; its history is still here.</p>
    {/if}
    {#if user.id === workspace.user?.id}
      <!--
        Your own profile is where you notice your own name is wrong, so it is where the
        control to fix it belongs. It opens Preferences on the Profile section rather than
        editing in place, so there is one form that writes these two fields.
      -->
      <div class="profile-actions">
        <button
          class="secondary"
          onclick={() => {
            workspace.profileUser = null;
            workspace.preferencesSection = 'Profile';
            workspace.settingsOpen = true;
          }}>Edit profile</button
        >
      </div>
    {/if}
  </div>
</Modal>

<style>
  .profile-head {
    display: flex;
    align-items: center;
    gap: 0.85rem;
    margin-bottom: 1rem;
  }
  .profile-head h3 {
    margin: 0;
    font-size: 1.05rem;
  }
  .handle {
    margin: 0.15rem 0 0;
    color: var(--muted);
    font-size: 0.85rem;
  }
  .profile-facts {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.35rem 1rem;
    margin: 0;
  }
  .profile-facts dt {
    color: var(--muted);
    font-size: 0.82rem;
  }
  .profile-facts dd {
    margin: 0;
    font-size: 0.9rem;
  }
  .profile-note {
    margin: 1rem 0 0;
    color: var(--muted);
    font-size: 0.85rem;
  }
  .profile-actions {
    margin-top: 1rem;
  }
</style>
