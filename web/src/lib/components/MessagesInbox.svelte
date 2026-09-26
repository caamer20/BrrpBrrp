<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import type { DirectConversation, User } from '../types';
  import Avatar from './Avatar.svelte';
  import Modal from './Modal.svelte';

  let directs = $state<DirectConversation[]>([]);
  let blocked = $state<User[]>([]);
  let allowRequests = $state(true);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let query = $state('');
  let target = $state<User | null>(null);
  let introduction = $state('');
  let tab = $state<'conversations' | 'requests' | 'people' | 'blocked'>('conversations');
  let scope = $state<{ account: string; generation: number; version: number } | null>(null);
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
  const visible = $derived(scope && admitted(scope) ? directs : []);
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
  onDestroy(() => {
    alive = false;
    version++;
  });
  $effect(() => {
    workspace.user?.id;
    workspace.authorizationGeneration;
    workspace.contactsVersion;
    const ready = workspace.privateConversationsEnabled && !workspace.authorizationUnverified;
    untrack(() => {
      version++;
      directs = [];
      blocked = [];
      scope = null;
      error = notice = '';
      loading = false;
      busy = false;
      target = null;
      introduction = '';
      if (ready) void load();
    });
  });
  async function load() {
    const request = identity();
    if (!admitted(request)) return;
    loading = true;
    error = '';
    try {
      const [items, blocks, preference] = await Promise.all([
        api<DirectConversation[]>('/directs'),
        api<User[]>('/me/blocks'),
        api<{ allow_requests: boolean }>('/me/contact-preferences')
      ]);
      if (!admitted(request)) return;
      directs = items;
      blocked = blocks;
      allowRequests = preference.allow_requests;
      scope = request;
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) loading = false;
    }
  }
  async function act(path: string, method: string, body: unknown, success: string) {
    if (busy || !scope || !admitted(scope)) return;
    const request = identity();
    const mutation = ++mutationVersion;
    const current = () =>
      alive && request.account === workspace.user?.id && mutation === mutationVersion;
    busy = true;
    error = '';
    notice = '';
    try {
      await api(path, method, body);
      if (!current()) return;
      // Consent and blocking advance authority. Refresh through the workspace protocol;
      // its generation checks own all histories and pending sends.
      await workspace.refreshState();
      if (!current()) return;
      await load();
      if (current()) notice = success;
    } catch (failure) {
      if (current()) error = errorMessage(failure);
    } finally {
      if (current()) busy = false;
    }
  }
  async function start(event: SubmitEvent) {
    event.preventDefault();
    if (!target) return;
    await act(
      '/directs',
      'POST',
      { user_id: target.id, introduction: introduction.trim() },
      'Request sent. Messaging begins only after they accept.'
    );
    if (!error) {
      target = null;
      introduction = '';
      tab = 'requests';
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
    await workspace.selectChannel(direct.id);
  }
</script>

<Modal title="Messages" close={() => (workspace.messagesOpen = false)} wide>
  <p class="muted">
    Direct messages open only after a request is accepted. Only participants can read them in the
    app. The server operator manages storage; messages are not end-to-end encrypted.
  </p>
  <nav class="admin-tabs" aria-label="Messages sections">
    {#each [{ id: 'conversations', label: 'Conversations' }, { id: 'requests', label: 'Requests' }, { id: 'people', label: 'New conversation' }, { id: 'blocked', label: 'Blocked people' }] as item}
      <button
        aria-current={tab === item.id ? 'page' : undefined}
        class:active={tab === item.id}
        onclick={() => (tab = item.id as typeof tab)}>{item.label}</button
      >
    {/each}
  </nav>
  {#if workspace.authorizationUnverified}<p role="status">Checking your access…</p>
  {:else if !workspace.privateConversationsEnabled}<p role="status">
      Direct conversations are not enabled on this server.
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
          <button class="secondary" onclick={() => open(direct)} disabled={busy}
            >Open conversation</button
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
    {:else if tab === 'people'}
      {#if target}<form class="admin-form" onsubmit={start}>
          <h3>Request a conversation with {target.display_name || target.username}</h3>
          <p>Your introduction is shared with this person before they choose whether to accept.</p>
          <label
            >Introduction (optional)<textarea bind:value={introduction} maxlength="500" rows="3"
            ></textarea></label
          >
          <div class="button-row">
            <button type="button" class="secondary" onclick={() => (target = null)}>Cancel</button
            ><button class="primary" disabled={busy}
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
                tab = 'requests';
                return;
              }
              target = person;
              introduction = '';
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
