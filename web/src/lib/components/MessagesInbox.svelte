<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { api, errorMessage } from '../api';
  import { activity } from '../activity.svelte';
  import { workspace } from '../workspace.svelte';
  import type { DirectConversation, Group, GroupInvitation, GroupMember, User } from '../types';
  import ActivityPanel from './ActivityPanel.svelte';
  import Avatar from './Avatar.svelte';
  import ReportRequest from './ReportRequest.svelte';
  import Modal from './Modal.svelte';
  let directs = $state<DirectConversation[]>([]);
  let blocked = $state<User[]>([]);
  let allowRequests = $state(true);
  // Groups the account has been accepted into, and the invitations addressed to it.
  // Both are the caller's own state and both are gated by the same admission as the
  // rest of this panel: a fetched list is not shown to an account that has changed
  // since it was fetched (ADR 0016).
  let groups = $state<Group[]>([]);
  let groupInvitations = $state<GroupInvitation[]>([]);
  let groupMembers = $state<GroupMember[]>([]);
  // The open group's id and the member list belong together: a removal changes the
  // member list, and showing a stale one next to a fresh group would offer an action
  // against somebody who is no longer there.
  let openGroup = $state('');
  let newGroupName = $state('');
  // Who the owner is inviting, and who they are inviting from. The picker's list is
  // the people who are not already in the group, so the interface cannot offer an
  // invite to somebody who is already there.
  let invitingGroup = $state('');
  let inviteUsername = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  // A committed mutation's outcome survives the invalidation it causes. A group
  // change bumps the authorization generation twice — once for the mutation and once
  // for the refresh that follows it — so clearing on the first wiped a notice about
  // work that had committed, and a person who had just handed over a group was told
  // nothing had happened (completion review 13, V13-01).
  let noticePending = $state(false);
  let query = $state('');
  let target = $state<User | null>(null);
  let introduction = $state('');
  // The account the unsent request draft belongs to. Draft ownership is separate
  // from fetched contact data: an unrelated contact or authority refresh must not
  // destroy text this account just typed (review 8, R8-03).
  let draftAccount = '';
  // The tab the dialog opens on is chosen by whoever opened it, so the sidebar's
  // request count can lead straight to Requests (ADR 0013). The dialog is mounted
  // fresh each time it opens, so reading the opening choice here is enough;
  // changing the tab writes it back, so the entry point stays in step with what
  // the member is looking at rather than resetting on the next open.
  let tab = $state(workspace.messagesTab);
  // The count is read from the account snapshot, so the tab shows the same number
  // the sidebar does and neither can be stale in a way the other is not.
  const outstanding = $derived(workspace.outstandingIncomingRequests);
  function selectTab(next: typeof tab) {
    tab = next;
    workspace.messagesTab = next;
  }
  let scope = $state<{ account: string; generation: number; version: number } | null>(null);
  // Which incoming request is open in the report dialog, if any, and the name it
  // had when it was opened. The name is captured rather than looked up later: the
  // operation blocks the sender and invalidates this list, so by the time the
  // dialog needs it the card is gone and the dialog would lose the person it is
  // about.
  let reporting = $state('');
  let reportingName = $state('');
  let version = 0;
  let mutationVersion = 0;
  let alive = true;
  const identity = () => ({
    account: workspace.user?.id || '',
    generation: workspace.authorizationGeneration,
    version
  });
  const admitted = (request: ReturnType<typeof identity>) =>
    alive &&
    request.account === workspace.user?.id &&
    request.generation === workspace.authorizationGeneration &&
    request.version === version &&
    workspace.privateConversationsEnabled &&
    !workspace.authorizationUnverified;
  // The group badge counts invitations this account can still answer, which is a
  // different question from the outstanding request badge and is not derived from it.
  const visibleActivityUnacknowledged = $derived(
    scope && admitted(scope) ? activity.unacknowledged : 0
  );
  const activityCount = $derived(visibleActivityUnacknowledged);
  const pendingGroupInvitations = $derived(
    scope && admitted(scope)
      ? groupInvitations.filter((row) => row.my_state === 'invited' && row.state === 'pending')
          .length
      : 0
  );
  const visible = $derived(scope && admitted(scope) ? directs : []);
  // Groups, invitations and the open member list are private metadata and are
  // rendered without an equivalent projection to `visible`, which is how a group
  // kept rendering after the authority change that should have cleared it
  // (completion review 13, V13-03). They are gated the same way, so a panel that has
  // failed admission shows nothing at all rather than whatever arrived last.
  const admittedNow = $derived(!!scope && admitted(scope));
  const visibleGroups = $derived(admittedNow ? groups : []);
  const visibleInvitations = $derived(admittedNow ? groupInvitations : []);
  const visibleMembers = $derived(admittedNow ? groupMembers : []);
  const visibleBlocks = $derived(scope && admitted(scope) ? blocked : []);
  const candidates = $derived(
    (workspace.state?.users || [])
      .filter(
        (user) =>
          user.id !== workspace.user?.id &&
          !user.is_deactivated &&
          !user.is_banned &&
          !user.is_deleted &&
          `${user.display_name} ${user.username}`.toLowerCase().includes(query.toLowerCase())
      )
      .slice(0, 30)
  );
  // The draft's destination is revalidated from the freshly admitted lists after
  // every refresh. A target that became forbidden keeps its text — the person can
  // copy or explicitly discard it — and is never swapped for another destination.
  // While authority is unverified, fetched data proves nothing: no problem is
  // claimed from it.
  const targetProblem = $derived.by(() => {
    const draft = target;
    if (!draft || !scope || !admitted(scope)) return '';
    const name = draft.display_name || draft.username;
    const account = (workspace.state?.users || []).find((user) => user.id === draft.id);
    if (!account || account.is_deactivated || account.is_banned || account.is_deleted)
      return `${name} is no longer available, so this request cannot be sent. Your introduction stays below for you to copy; it will not be sent to anyone else.`;
    if (
      blocked.some((user) => user.id === draft.id) ||
      directs.some(
        (direct) => direct.peer.id === draft.id && (direct.blocked || direct.blocked_by_me)
      )
    )
      return `Contact with ${name} is blocked, so this request cannot be sent. Your introduction stays below for you to copy; it will not be sent to anyone else.`;
    return '';
  });
  onDestroy(() => {
    alive = false;
    version++;
  });
  $effect(() => {
    const account = workspace.user?.id || '';
    workspace.authorizationGeneration;
    workspace.contactsVersion;
    const ready = workspace.privateConversationsEnabled && !workspace.authorizationUnverified;
    untrack(() => {
      version++;
      directs = [];
      blocked = [];
      // Group metadata is private and every one of these arrays was rendered without
      // an admitted projection, so leaving them populated across an authority
      // change showed the previous account's group names, members and invitations
      // until the next fetch replaced them — and indefinitely if the fetch failed
      // (completion review 13, V13-03). Fetched private data is cleared here for the
      // same reason the direct lists are.
      groups = [];
      groupInvitations = [];
      groupMembers = [];
      invitingGroup = '';
      inviteUsername = '';
      openGroup = '';
      scope = null;
      error = '';
      if (noticePending) {
        // Keep it: the invalidation is describing the change this notice reports.
        noticePending = false;
      } else {
        notice = '';
      }
      loading = false;
      busy = false;
      // Fetched data is refreshed, but the same account's unsent request text is
      // not fetched data: it survives. Only an account change, an admitted send or
      // an explicit discard clears it. The target is then revalidated from the
      // freshly admitted lists (targetProblem), never replaced silently.
      if (account !== draftAccount) {
        target = null;
        introduction = '';
        draftAccount = '';
      }
      // One effect clears the stale private state and then fetches, in that order.
      // As two effects they raced: the fetch could land before `scope` was cleared,
      // so its result was discarded as unadmitted and the panel showed nothing —
      // a member who had just created a group was told they were in none
      // (completion review 13, V13-01).
      if (ready) load();
    });
  });
  async function load() {
    const request = identity();
    if (!admitted(request)) return;
    loading = true;
    error = '';
    try {
      const [items, blocks, preference, groupList, invitations] = await Promise.all([
        api<DirectConversation[]>('/directs'),
        api<User[]>('/me/blocks'),
        api<{ allow_requests: boolean }>('/me/contact-preferences'),
        api<{ groups: Group[] }>('/groups'),
        api<{ invitations: GroupInvitation[] }>('/group-invitations')
      ]);
      if (!admitted(request)) return;
      directs = items;
      blocked = blocks;
      allowRequests = preference.allow_requests;
      groups = groupList.groups;
      groupInvitations = invitations.invitations;
      scope = request;
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) loading = false;
    }
  }
  // The member list is fetched for one group at a time rather than for all of them,
  // because it is only needed while somebody is acting on one, and a private group's
  // membership is not something to hold for every group in the list at once.
  async function openGroupMembers(groupId: string) {
    const request = identity();
    if (!admitted(request)) return;
    if (openGroup === groupId) {
      openGroup = '';
      groupMembers = [];
      return;
    }
    try {
      const result = await api<{ members: GroupMember[] }>(`/groups/${groupId}/members`);
      // Every axis, not just the account: a membership change advances the
      // generation, and a response that resolved after it describes a group this
      // account may no longer be in.
      if (!admitted(request) || openGroup === groupId || !groups.some((g) => g.id === groupId))
        return;
      groupMembers = result.members;
      openGroup = groupId;
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    }
  }

  // Group mutations go through act, so they carry the same admission, busy and
  // supersession rules as everything else in this panel. A refused lifecycle action
  // says why, and the list is re-read rather than patched locally, so the interface
  // cannot claim a membership the server did not record.
  // Who may be invited to the open group: anybody on this server who is usable and
  // not already a member. Restricting this to existing members was wrong — the
  // point of an invitation is to add somebody who is not in the group yet, and the
  // owner is precisely the person who knows whom to ask.
  const inviteCandidates = $derived(
    (workspace.state?.users || []).filter(
      (person) =>
        person.id !== workspace.user?.id &&
        !person.is_deactivated &&
        !person.is_banned &&
        !person.is_deleted &&
        !groupMembers.some((member) => member.id === person.id)
    )
  );

  async function sendGroupInvite(person: User) {
    if (!invitingGroup) return;
    const groupId = invitingGroup;
    if (
      await act(`/groups/${groupId}/invite`, 'POST', { user_id: person.id }, 'Invitation sent.')
    ) {
      invitingGroup = '';
      inviteUsername = '';
    }
  }

  async function createGroup() {
    const name = newGroupName.trim();
    if (!name) return;
    if (await act('/groups', 'POST', { name }, 'Group created.')) newGroupName = '';
  }
  // A lifecycle change is captured before it is sent, so the member list refreshed
  // afterwards is the one for the account and group that asked for it. Without the
  // capture, a response that resolved after an account change or a membership
  // change would populate the new account's panel with the old account's members
  // (completion review 13, V13-03).
  async function groupLifecycle(path: string, body: unknown, success: string) {
    const groupId = openGroup;
    const request = identity();
    if (!(await act(path, 'POST', body, success))) return;
    if (!groupId) return;
    try {
      const result = await api<{ members: GroupMember[] }>(`/groups/${groupId}/members`);
      if (!admitted(request) || openGroup !== groupId) return;
      groupMembers = result.members;
    } catch {
      // The lifecycle succeeded; a member list that cannot be re-read is left as it
      // is rather than shown as empty, which would read as "nobody is here".
    }
  }

  // act reports whether the mutation was admitted and completed for this mounted
  // account: a no-op on stale state or a superseded result answers false, never
  // "no error", so callers never treat work nobody confirmed as a success (R8-03).
  async function act(path: string, method: string, body: unknown, success: string) {
    if (busy || !scope || !admitted(scope)) return false;
    const request = identity();
    const mutation = ++mutationVersion;
    const current = () =>
      alive && request.account === workspace.user?.id && mutation === mutationVersion;
    busy = true;
    error = '';
    notice = '';
    try {
      await api(path, method, body);
      if (!current()) return false;
      // Consent and blocking advance authority. Refresh through the workspace protocol;
      // its generation checks own all histories and pending sends.
      await workspace.refreshState();
      // An authority change clears the notice, and inviting, accepting, transferring
      // and removing all advance the generation — so a notice set before the refresh
      // was wiped a moment later and a person who had just handed over a group saw
      // nothing happen. It is set after the reload, once the panel has settled, and
      // only while this is still the mounted account's panel (completion review 13,
      // V13-01).
      await load();
      // Set after the reload, which itself advances the generation for a group
      // change — so a guard keyed on the pre-change generation refuses it, and a
      // person who had just handed over a group was told nothing had happened
      // (completion review 13, V13-01). The mutation committed and this panel
      // issued it, so the notice follows the panel's own lifetime rather than a
      // continuation the invalidation has just superseded.
      if (alive && request.account === workspace.user?.id) {
        await tick();
        noticePending = true;
        notice = success;
      }
      return true;
    } catch (failure) {
      if (current()) error = errorMessage(failure);
      return false;
    } finally {
      if (current()) busy = false;
    }
  }
  function discardTarget() {
    target = null;
    introduction = '';
    draftAccount = '';
  }
  async function start(event: SubmitEvent) {
    event.preventDefault();
    if (!target || targetProblem) return;
    // The captured account, target and text are the intent. The draft is cleared
    // only after the send was admitted as successful for this account; a refused,
    // stale or merely silent attempt keeps the text (R8-03).
    const account = workspace.user?.id || '';
    const destination = target;
    const text = introduction;
    if (
      await act(
        '/directs',
        'POST',
        { user_id: destination.id, introduction: text.trim() },
        'Request sent. Messaging begins only after they accept.'
      )
    ) {
      if (target === destination && introduction === text && draftAccount === account)
        discardTarget();
      selectTab('requests');
    }
  }
  async function open(direct: DirectConversation) {
    if (!direct.can_read || !scope || !admitted(scope)) return;
    if (direct.hidden)
      await act(
        `/directs/${direct.id}/visibility`,
        'PUT',
        { hidden: false },
        'Conversation restored to your sidebar.'
      );
    await workspace.refreshState();
    if (!workspace.canReadConversation(direct.id)) return;
    workspace.messagesOpen = false;
    await workspace.selectChannel(direct.id, true);
  }
</script>

<Modal title="Messages" close={() => (workspace.messagesOpen = false)} wide>
  <p class="muted">
    Direct messages open only after a request is accepted. Only participants can read them in the
    app. The server operator manages storage; messages are not end-to-end encrypted.
  </p>
  {#if outstanding > 0}
    <p class="muted">
      {outstanding}
      {outstanding === 1 ? 'request is' : 'requests are'} waiting for you. Opening this does not dismiss
      {outstanding === 1 ? 'it' : 'them'} — a request stays here until you accept, decline, or the other
      person withdraws. A new direct message does not ring or pop up on this device; unread messages show
      here and in the conversation.
    </p>
  {/if}
  <nav class="admin-tabs" aria-label="Messages sections">
    {#each [{ id: 'conversations', label: 'Conversations' }, { id: 'requests', label: 'Requests', count: outstanding }, { id: 'activity', label: 'Activity', count: activityCount }, { id: 'groups', label: 'Groups', count: pendingGroupInvitations }, { id: 'people', label: 'New conversation' }, { id: 'blocked', label: 'Blocked people' }] as item}
      <button
        aria-current={tab === item.id ? 'page' : undefined}
        class:active={tab === item.id}
        onclick={() => selectTab(item.id as typeof tab)}
        >{item.label}{#if item.count}<b class="unread-badge" aria-hidden="true">{item.count}</b
          >{/if}</button
      >
    {/each}
  </nav>
  {#if workspace.authorizationUnverified}<p role="status">Checking your access…</p>
  {:else if !workspace.privateConversationsEnabled}<p role="status">
      Direct conversations are switched off on this server.
    </p>
  {:else}
    {#if loading}<p role="status">Loading your conversations…</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>
      <button class="secondary" onclick={load} disabled={loading}>Try again</button>{/if}
    {#if notice}<p class="success-note" role="status">{notice}</p>{/if}
    {#if tab === 'conversations'}
      {#each visible.filter((direct) => direct.can_read) as direct (direct.id)}
        <article class="admin-row">
          <Avatar user={direct.peer} />
          <div class="admin-row-label">
            <strong>{direct.peer.display_name || direct.peer.username}</strong><small
              >{direct.hidden ? 'Hidden from sidebar · ' : ''}{direct.can_write
                ? 'Direct conversation'
                : 'Read-only conversation'}</small
            >
          </div>
          <button
            class="secondary"
            disabled={busy}
            onclick={() => {
              workspace.messagesOpen = false;
              open(direct);
            }}>Open conversation</button
          >{#if !direct.hidden}<button
              class="secondary"
              disabled={busy}
              onclick={() =>
                act(
                  `/directs/${direct.id}/visibility`,
                  'PUT',
                  { hidden: true },
                  'Conversation hidden from your sidebar. History is kept here.'
                )}>Hide</button
            >{/if}
        </article>
      {:else}{#if !loading}<p role="status">
            No accepted conversations yet. Start with a request or review incoming requests.
          </p>{/if}{/each}
    {:else if tab === 'requests'}
      <label class="toggle-row"
        ><span
          ><strong>Allow message requests</strong><small
            >Turning this off stops new requests. Existing conversations stay available.</small
          ></span
        ><input
          type="checkbox"
          checked={allowRequests}
          disabled={busy || loading}
          onchange={(event) =>
            act(
              '/me/contact-preferences',
              'PUT',
              { allow_requests: event.currentTarget.checked },
              'Message request preference saved.'
            )}
        /></label
      >
      {#each visible.filter((direct) => direct.request_state !== 'accepted') as direct (direct.id)}
        <article
          class="request-card"
          aria-label={`Message request with ${direct.peer.display_name || direct.peer.username}`}
        >
          <h3>{direct.peer.display_name || direct.peer.username}</h3>
          <p class="muted">
            {direct.request_state === 'rejected'
              ? 'Request declined'
              : direct.request_direction === 'incoming'
                ? 'Wants to message you'
                : 'Waiting for them to accept'}
          </p>
          {#if direct.introduction}<blockquote>{direct.introduction}</blockquote>{/if}
          {#if direct.request_state === 'pending' && direct.request_direction === 'incoming'}<div
              class="button-row"
            >
              <button
                class="primary"
                disabled={busy}
                onclick={() =>
                  act(
                    `/directs/${direct.id}/accept`,
                    'POST',
                    {},
                    'Request accepted. Open the conversation to start messaging.'
                  )}>Accept request</button
              ><button
                class="secondary"
                disabled={busy}
                onclick={() => act(`/directs/${direct.id}/reject`, 'POST', {}, 'Request declined.')}
                >Decline request</button
              ><button
                class="secondary"
                disabled={busy}
                onclick={() =>
                  act(
                    `/me/blocks/${direct.peer.id}`,
                    'PUT',
                    {},
                    'Person blocked. Existing history stays readable; messages and new requests are stopped.'
                  )}>Block person</button
              ><button class="secondary" disabled={busy} onclick={() => (reporting = direct.id)}
                >Report and block</button
              >
            </div>{/if}
          {#if direct.request_state === 'pending' && direct.request_direction === 'outgoing'}<button
              class="secondary"
              disabled={busy}
              onclick={() => act(`/directs/${direct.id}/reject`, 'POST', {}, 'Request withdrawn.')}
              >Withdraw request</button
            >{/if}
        </article>
      {:else}{#if !loading}<p role="status">No message requests.</p>{/if}{/each}
    {:else if tab === 'activity'}
      <ActivityPanel />
    {:else if tab === 'groups'}
      <!-- The history rule is stated here, before an invitation is answered rather
           than after, because it is part of what accepting means (ADR 0016). -->
      <p class="muted">
        A group is shared with the people in it. Anyone you accept can read everything already said
        in it, including before they joined.
      </p>

      {#each visibleInvitations.filter((row) => row.my_state === 'invited') as invitation (invitation.id)}
        <article>
          <h3>{invitation.group_name || 'A group'}</h3>
          <p>
            {invitation.inviter_name} invited you. Accepting means you can read everything already said
            in the group, and they can read everything you say in it.
          </p>
          <small>Open until {new Date(invitation.expires_at).toLocaleString()}</small>
          <div class="button-row">
            <button
              onclick={() =>
                act(
                  `/group-invitations/${invitation.id}/accept`,
                  'POST',
                  {},
                  'You joined the group.'
                )}>Accept</button
            ><button
              class="secondary"
              onclick={() =>
                act(
                  `/group-invitations/${invitation.id}/reject`,
                  'POST',
                  {},
                  'Invitation declined.'
                )}>Decline</button
            >
          </div>
        </article>
      {/each}

      <form
        onsubmit={(event) => {
          event.preventDefault();
          void createGroup();
        }}
      >
        <label for="new-group-name">Start a group</label>
        <input
          id="new-group-name"
          maxlength="80"
          required
          bind:value={newGroupName}
          placeholder="A name your members will recognise"
        />
        <button disabled={busy || !newGroupName.trim()}>Create group</button>
      </form>

      {#if !loading && visibleGroups.length === 0}
        <p role="status">You are not in any groups yet. Start one, or wait for an invitation.</p>
      {/if}
      {#each visibleGroups as group (group.id)}
        <article>
          <h3>{group.name}</h3>
          <p>
            {group.members === 1 ? '1 member' : `${group.members} members`}, run by
            {group.owner_name}.{#if group.pending_invitations > 0}
              {group.pending_invitations === 1
                ? '1 invitation waiting.'
                : `${group.pending_invitations} invitations waiting.`}
            {/if}
          </p>
          <div class="button-row">
            <!-- A group card that cannot be opened is a management screen, not a
                 conversation, and a member who joined a group came here to use it. -->
            <button
              onclick={() => {
                openGroup = '';
                groupMembers = [];
                invitingGroup = '';
                workspace.messagesOpen = false;
                void workspace.selectChannel(group.id, true);
              }}>Open conversation</button
            >
            <button
              onclick={() => openGroupMembers(group.id)}
              aria-expanded={openGroup === group.id}
              >{openGroup === group.id ? 'Hide members' : 'Members'}</button
            >
            {#if group.can_invite}
              <button
                class="secondary"
                aria-expanded={invitingGroup === group.id}
                onclick={() => {
                  invitingGroup = invitingGroup === group.id ? '' : group.id;
                  inviteUsername = '';
                  error = '';
                }}>{invitingGroup === group.id ? 'Cancel invite' : 'Invite'}</button
              >
            {/if}
            <!-- Any member may leave. The owner of a group that still has people in it
                 cannot simply walk out of it, and the server says so with an
                 actionable reason; the notice here is only what happened on success. -->
            <button
              class="secondary"
              onclick={() =>
                groupLifecycle(
                  `/groups/${group.id}/leave`,
                  {},
                  group.can_transfer && group.members > 1
                    ? 'Name a new owner on this card before leaving, or transfer it first.'
                    : 'You left the group.'
                )}>Leave</button
            >
          </div>
          {#if invitingGroup === group.id}
            <label for="group-invite-search">Invite someone</label>
            <input
              id="group-invite-search"
              type="search"
              bind:value={inviteUsername}
              placeholder="Name or username"
            />
            {#each inviteCandidates.filter((person) => `${person.display_name} ${person.username}`
                .toLowerCase()
                .includes(inviteUsername.toLowerCase())) as person (person.id)}
              <div class="admin-row">
                <Avatar user={person} />
                <div class="admin-row-label">
                  <strong>{person.display_name || person.username}</strong><small
                    >@{person.username}</small
                  >
                </div>
                <button
                  class="secondary"
                  disabled={busy}
                  onclick={() => void sendGroupInvite(person)}>Send invitation</button
                >
              </div>
            {/each}
          {/if}
          {#if openGroup === group.id}
            <h4>Members</h4>
            {#if visibleMembers.length === 0}
              <p role="status">No members to show.</p>
            {/if}
            <ul>
              {#each visibleMembers as member (member.id)}
                <li>
                  {member.display_name || member.username}{member.is_owner ? ' (owner)' : ''}
                  {#if group.can_transfer && !member.is_owner && member.id !== workspace.user?.id}
                    <button
                      class="secondary"
                      onclick={() =>
                        groupLifecycle(
                          `/groups/${group.id}/members/${member.id}/remove`,
                          {},
                          `${member.display_name || member.username} was removed.`
                        )}>Remove</button
                    ><button
                      class="secondary"
                      onclick={() =>
                        groupLifecycle(
                          `/groups/${group.id}/transfer`,
                          { new_owner_id: member.id },
                          `${member.display_name || member.username} now runs this group.`
                        )}>Make owner</button
                    >
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        </article>
      {/each}
    {:else if tab === 'people'}
      {#if target}<form class="admin-form" onsubmit={start}>
          <h3>Request a conversation with {target.display_name || target.username}</h3>
          <p>Your introduction is shared with this person before they choose whether to accept.</p>
          {#if targetProblem}<p class="error" role="alert">{targetProblem}</p>{/if}
          <label
            >Introduction (optional)<textarea bind:value={introduction} maxlength="500" rows="3"
            ></textarea></label
          >
          <div class="button-row">
            <button type="button" class="secondary" onclick={discardTarget}>Cancel</button><button
              class="primary"
              disabled={busy || !!targetProblem}
              >{busy ? 'Sending request…' : 'Send request'}</button
            >
          </div>
        </form>{/if}
      <label
        >Find a person<input
          bind:value={query}
          placeholder="Name or username"
          type="search"
        /></label
      >
      {#each candidates as person (person.id)}{@const existing = visible.find(
          (direct) => direct.peer.id === person.id
        )}
        <article class="admin-row">
          <Avatar user={person} />
          <div class="admin-row-label">
            <strong>{person.display_name || person.username}</strong><small
              >@{person.username}</small
            >
          </div>
          <button
            class="secondary"
            disabled={busy || loading || visibleBlocks.some((user) => user.id === person.id)}
            onclick={() => {
              if (existing?.can_read) {
                void open(existing);
                return;
              }
              if (existing?.request_state === 'pending') {
                selectTab('requests');
                return;
              }
              // Choosing a different person starts a new draft; re-choosing the same
              // person keeps the text already typed for them.
              if (target?.id !== person.id) introduction = '';
              target = person;
              draftAccount = workspace.user?.id || '';
            }}
            >{existing?.can_read
              ? 'Open conversation'
              : existing?.request_state === 'pending'
                ? 'Review request'
                : 'Request conversation'}</button
          >
        </article>{:else}<p class="muted">No people match this search.</p>{/each}
    {:else}
      <p>
        Blocking stops new requests and sends in either direction. Existing accepted history stays
        readable. Unblocking never resends unsent messages.
      </p>
      {#each visibleBlocks as person (person.id)}<article class="admin-row">
          <Avatar user={person} />
          <div class="admin-row-label">
            <strong>{person.display_name || person.username}</strong><small
              >@{person.username}</small
            >
          </div>
          <button
            class="secondary"
            disabled={busy}
            onclick={() =>
              act(
                `/me/blocks/${person.id}`,
                'DELETE',
                {},
                'Person unblocked. Review any recovered text before choosing to send a new message.'
              )}>Unblock person</button
          >
        </article>{:else}{#if !loading}<p role="status">You have not blocked anyone.</p>{/if}{/each}
    {/if}
  {/if}
</Modal>

<!-- One dialog at a time. The report flow owns its own disclosure, and the request
     it belongs to is named so the introduction and reviewer count come from that
     request rather than from whatever the list happened to render last. -->
{#if reporting}
  <ReportRequest
    conversationId={reporting}
    requesterName={reportingName || 'this person'}
    close={() => {
      reporting = '';
      reportingName = '';
      // The list is re-read on close: the operation either changed the request
      // state or the account, and a stale card would misreport both.
      void load();
    }}
  />
{/if}

<style>
  .request-card {
    padding: 14px;
    margin: 14px 0;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  blockquote {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 10px 0;
  }
</style>
