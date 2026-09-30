<script lang="ts">
  import { tick } from 'svelte';
  import type { Message, Thread } from '../types';
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import ReactionPicker from './ReactionPicker.svelte';
  import { reactionName } from '../reactions';
  import { tokenizeMessage, linkHost } from '../message-tokens';
  import Avatar from './Avatar.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import ImagePreview from './ImagePreview.svelte';
  import ReportMessage from './ReportMessage.svelte';
  import ReportPrivateMessage from './ReportPrivateMessage.svelte';

  let {
    message,
    grouped = false,
    scope = 'message',
    onreply,
    showThread = true
  }: {
    message: Message;
    grouped?: boolean;
    scope?: string;
    onreply?: () => void;
    showThread?: boolean;
  } = $props();
  let reactionButton = $state<HTMLButtonElement>();
  async function closePicker() {
    pickerOpen = false;
    await tick();
    reactionButton?.focus();
  }
  let pickerOpen = $state(false);
  let reacting = $state(false);
  let editing = $state(false);
  let editText = $state('');
  let reveal = $state(false);
  let saving = $state(false);
  let moreOpen = $state(false);
  // A direct conversation has its own disclosure flow and its own dialog rather
  // than a shared one with different rules: the evidence is a disclosed snapshot
  // the reporter confirms, not a moderation note (ADR 0013).
  let privateReportOpen = $state(false);
  let reportOpen = $state(false);
  let editInput = $state<HTMLTextAreaElement>();
  let editButton = $state<HTMLButtonElement>();
  let moreButton = $state<HTMLButtonElement>();
  let author = $derived(workspace.userFor(message.user_id));
  let own = $derived(message.user_id === workspace.user?.id);
  // A group is a private conversation as well as a direct pair, so this asks
  // `isPrivate`: a group message that reported through the community path would put
  // submitted private text into a moderator's queue (completion review 13, V13-01).
  let direct = $derived(workspace.isPrivate(workspace.conversationId(message)));
  let canWrite = $derived(
    !workspace.authorizationUnverified &&
      workspace.canWriteConversation(workspace.conversationId(message))
  );
  let canDelete = $derived(own || (!direct && workspace.can('message.delete_any')));
  // Pinning is a moderation capability, not an administrator flag: a delegated
  // moderator sees the control and the server accepts it.
  let canPin = $derived(canWrite && workspace.can('message.pin'));
  let reply = $derived(workspace.messages.find((item) => item.id === message.reply_to_id));
  const tokens = (text: string) => tokenizeMessage(text);
  const size = (bytes: number) =>
    bytes > 1048576 ? `${(bytes / 1048576).toFixed(1)} MB` : `${Math.ceil(bytes / 1024)} KB`;

  async function react(emoji: string) {
    if (reacting || !canWrite) return;
    reacting = true;
    try {
      await api(`/messages/${message.id}/reactions`, 'POST', { emoji });
      await workspace.refreshMessage(message.id);
    } catch (error) {
      workspace.showToast(errorMessage(error));
    } finally {
      reacting = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (saving || !canWrite || (!editText.trim() && !message.attachments.length)) return;
    saving = true;
    try {
      await api(`/messages/${message.id}`, 'PATCH', { content: editText });
      await finishEditing();
      await workspace.refreshMessage(message.id);
    } catch (error) {
      workspace.showToast(errorMessage(error));
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!confirm('Delete this message and its attachments? This cannot be undone.')) return;
    try {
      await api(`/messages/${message.id}`, 'DELETE', { confirm: message.id });
      await workspace.refreshMessage(message.id, true);
    } catch (error) {
      workspace.showToast(errorMessage(error));
    }
  }
  async function pin() {
    try {
      await api(`/messages/${message.id}/pin`, 'PUT', { pinned: !message.pinned });
      await workspace.refreshMessage(message.id);
    } catch (error) {
      workspace.showToast(errorMessage(error));
    }
  }
  function replyToMessage() {
    moreOpen = false;
    if (onreply) {
      onreply();
      return;
    }
    workspace.replyTo = message;
    void tick().then(() =>
      document.querySelector<HTMLTextAreaElement>('.composer textarea')?.focus()
    );
  }
  async function copyLink() {
    moreOpen = false;
    // A permalink, not the message text. Somebody sharing a link is sharing a
    // reference, and pasting the message into a channel would leak a private
    // conversation to whoever receives the paste.
    const link = workspace.messagePermalink(message.id);
    try {
      await navigator.clipboard.writeText(link);
      workspace.showToast('Link copied.');
    } catch {
      // A clipboard the browser refused is a real outcome, and telling somebody the
      // link is copied when it is not would be the worst version of this feature.
      workspace.showToast(`Copy this link: ${link}`);
    }
  }

  function openThread() {
    moreOpen = false;
    workspace.threadId = message.id;
  }
  // A named thread belongs to a channel, so it is only offered where one is in view.
  // The server decides for real either way; this only avoids offering it where it
  // could not possibly work.
  const canStartNamedThread = $derived(showThread && !!workspace.channel);
  let threadPromptOpen = $state(false);
  let threadTitleInput = $state<HTMLInputElement>();
  let threadTitle = $state('');
  let threadError = $state('');
  // A mention of somebody who is not here reads as plain text rather than as a link
  // to nowhere, which is also what a name inside a code block is.
  const memberExists = (name: string) =>
    workspace.state?.users.some((member) => member.username.toLowerCase() === name.toLowerCase())
      ? ''
      : 'Nobody by this name is on this server';

  async function startNamedThread() {
    const title = threadTitle.trim();
    if (!title) return;
    threadError = '';
    try {
      const created = await api<Thread>(
        `/channels/${encodeURIComponent(workspace.activeId)}/threads`,
        'POST',
        { root_message_id: message.id, title }
      );
      threadPromptOpen = false;
      threadTitle = '';
      workspace.namedThreadId = created.id;
    } catch (error) {
      threadError = errorMessage(error);
    }
  }
  async function beginEdit() {
    moreOpen = false;
    editText = message.content;
    editing = true;
    await tick();
    editInput?.focus();
    editInput?.setSelectionRange(editText.length, editText.length);
  }
  async function finishEditing() {
    editing = false;
    await tick();
    if (moreButton?.getClientRects().length) moreButton.focus();
    else editButton?.focus();
  }
</script>

<article
  class="message"
  class:grouped
  class:highlighted={workspace.highlightId === message.id}
  class:pending={message.pending === 'sending'}
  class:failed={message.pending === 'failed'}
  id={`${scope}-${message.id}`}
>
  {#if grouped}<span class="grouped-time" title={new Date(message.created_at).toLocaleString()}
      >{new Date(message.created_at).toLocaleTimeString([], {
        hour: 'numeric',
        minute: '2-digit'
      })}</span
    >{:else}<Avatar user={author} />{/if}
  <div class="message-body">
    {#if message.reply_to_id}<button
        class="reply-reference"
        onclick={() => workspace.jumpToMessage(message.reply_to_id!)}
        ><Icon name="reply" size={15} /><span
          >{reply ? reply.content.slice(0, 140) || 'Attachment' : 'View original message'}</span
        ></button
      >{/if}
    {#if message.pinned}<span class="pin-label"><Icon name="pin" size={12} /> Pinned</span>{/if}
    <div class="message-meta" class:visually-hidden={grouped}>
      <strong>{author?.display_name || author?.username || 'Former member'}</strong
      >{#if author?.is_admin}<span class="admin-badge">ADMIN</span>{/if}<time
        datetime={message.created_at}
        title={new Date(message.created_at).toLocaleString()}
        >{new Date(message.created_at).toLocaleTimeString([], {
          hour: 'numeric',
          minute: '2-digit'
        })}</time
      >{#if message.edited_at}<span class="edited">edited</span>{/if}
    </div>
    {#if editing}
      <form class="edit-form" onsubmit={save}>
        <textarea
          bind:this={editInput}
          bind:value={editText}
          maxlength="4000"
          aria-label="Edit message"
          rows="3"
          onkeydown={(event) => {
            if (event.key === 'Escape' && !saving) {
              event.preventDefault();
              void finishEditing();
            }
            if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && !event.isComposing) {
              event.preventDefault();
              editInput?.form?.requestSubmit();
            }
          }}></textarea>
        <div class="button-row">
          <button type="button" class="secondary" disabled={saving} onclick={finishEditing}
            >Cancel</button
          ><button
            class="primary"
            disabled={saving || (!editText.trim() && !message.attachments.length)}
            >{saving ? 'Saving…' : 'Save changes'}</button
          >
        </div>
      </form>
    {:else}
      <p class="message-content">
        {#each tokens(message.content) as token, index (index)}{#if token.kind === 'link'}<a
              href={token.value}
              target="_blank"
              rel="noopener noreferrer">{token.value}</a
            >{:else if token.kind === 'mention'}<span
              class="mention"
              title={memberExists(token.name)}>{token.value}</span
            >{:else if token.kind === 'code'}<code class="message-code">{token.value}</code
            >{:else}{token.value}{/if}{/each}
      </p>
    {/if}
    {#each message.attachments as attachment (attachment.id)}
      {#if /^image\/(png|jpeg|webp|gif)$/.test(attachment.mime_type)}<ImagePreview
          {attachment}
        />{/if}
      <a class="attachment" href={`/api/uploads/${attachment.id}?download=1`}
        ><Icon name="file" size={25} /><span
          ><strong>{attachment.file_name}</strong><small>{size(attachment.file_size)}</small></span
        ><Icon name="download" size={18} /></a
      >
    {/each}
    {#if message.embeds?.length && workspace.state?.settings.enable_link_previews}
      {#if (workspace.privacy || workspace.lowResource) && !reveal}
        <button class="preview-reveal" onclick={() => (reveal = true)}
          >Load external previews <span class="muted">· contacts the linked sites</span></button
        >
      {:else}
        {#each message.embeds as embed}
          <div class="embed-card">
            <small>{embed.site || new URL(embed.url).hostname}</small><a
              class="embed-title"
              href={embed.url}
              target="_blank"
              rel="noreferrer noopener">{embed.title}</a
            >{#if embed.description}<p>{embed.description}</p>{/if}
            {#if embed.video && workspace.state?.settings.enable_gif_embeds}<video
                src={embed.video}
                muted
                loop
                playsinline
                controls
                preload="none"
                aria-label={embed.title}
              ></video>
            {:else if embed.image && (workspace.state?.settings.enable_gif_embeds || !/gif|tenor|giphy/i.test(embed.image))}<img
                src={embed.image}
                alt=""
                loading="lazy"
                decoding="async"
                width="400"
                height="240"
                referrerpolicy="no-referrer"
              />{/if}
          </div>
        {/each}
      {/if}
    {/if}
    {#if message.reactions.length}<div class="reactions">
        {#each message.reactions as reaction (reaction.emoji)}<button
            class:mine={reaction.mine}
            aria-pressed={reaction.mine}
            disabled={reacting || !canWrite}
            title={`${reactionName(reaction.emoji)} · ${reaction.count}${reaction.mine ? ' · Click to remove your reaction' : ''}`}
            aria-label={`${reactionName(reaction.emoji)}, ${reaction.count} reactions${reaction.mine ? ', including yours' : ''}`}
            onclick={() => react(reaction.emoji)}
            >{reaction.emoji} <span>{reaction.count}</span></button
          >{/each}
      </div>{/if}
    {#if showThread && (message.reply_count || 0) > 0 && !message.pending}<button
        class="thread-link"
        onclick={openThread}
        ><Icon name="reply" size={15} /><strong
          >{message.reply_count} {message.reply_count === 1 ? 'reply' : 'replies'}</strong
        ><span>View discussion</span><Icon name="chevronRight" size={14} /></button
      >{/if}
    {#if message.pending === 'sending'}<span class="message-delivery" role="status">Sending…</span>
    {:else if message.pending === 'failed'}<button
        class="retry-link"
        onclick={() => workspace.retryMessage(message)}>Message not sent · Retry</button
      ><button
        class="text-button"
        aria-label="Discard unsent message"
        onclick={() => {
          if (
            confirm(
              'Discard this unsent message? Its text and attachments will be removed from this draft.'
            )
          )
            workspace.discardPending(message);
        }}>Discard</button
      >{/if}
  </div>
  {#if !message.pending && !editing}<div class="message-actions">
      {#if !direct}<button
          class="icon-button"
          aria-label="Report message"
          title="Report message"
          onclick={() => (reportOpen = true)}><Icon name="shield" size={17} /></button
        >{/if}
      {#if direct}<button
          class="icon-button"
          aria-label="Report private message"
          title="Report private message"
          onclick={() => (privateReportOpen = true)}><Icon name="shield" size={17} /></button
        >{/if}
      <button
        class="icon-button"
        aria-label="Reply to message"
        title="Reply"
        onclick={replyToMessage}><Icon name="reply" size={17} /></button
      >
      {#if showThread}<button
          class="icon-button"
          aria-label="Open reply thread"
          title="Open reply thread"
          onclick={openThread}><Icon name="hash" size={17} /></button
        >{/if}
      {#if canPin}<button
          class="icon-button"
          aria-label={message.pinned ? 'Unpin message' : 'Pin message'}
          title={message.pinned ? 'Unpin' : 'Pin'}
          onclick={pin}><Icon name="pin" size={17} /></button
        >{/if}
      <button
        class="icon-button"
        bind:this={reactionButton}
        aria-label="Add reaction"
        title="Add reaction"
        disabled={reacting || !canWrite}
        onclick={() => (pickerOpen = true)}><Icon name="smile" size={18} /></button
      >
      {#if own && canWrite}<button
          class="icon-button"
          title="Edit message"
          aria-label="Edit message"
          bind:this={editButton}
          onclick={beginEdit}><Icon name="edit" size={17} /></button
        >{/if}
      {#if canDelete}<button
          class="icon-button danger-text"
          title="Delete message"
          aria-label="Delete message"
          onclick={remove}><Icon name="trash" size={17} /></button
        >{/if}
    </div>{/if}
  {#if !message.pending && !editing}<button
      class="icon-button message-more"
      bind:this={moreButton}
      aria-label={`Actions for message from ${author?.display_name || author?.username || 'former member'}`}
      aria-haspopup="dialog"
      onclick={() => (moreOpen = true)}><Icon name="more" size={20} /></button
    >{/if}
</article>

{#if pickerOpen}<ReactionPicker close={closePicker} select={react} />{/if}
{#if moreOpen}<Modal title="Message actions" close={() => (moreOpen = false)}>
    <div class="message-action-preview">
      <strong>{author?.display_name || author?.username || 'Former member'}</strong>
      <p>{message.content || 'Attachment'}</p>
    </div>
    <div class="message-action-menu">
      {#if !direct}<button
          onclick={() => {
            moreOpen = false;
            reportOpen = true;
          }}><Icon name="shield" />Report message</button
        >{/if}
      {#if direct}<button
          onclick={() => {
            moreOpen = false;
            privateReportOpen = true;
          }}><Icon name="shield" />Report private message</button
        >{/if}
      <button onclick={replyToMessage}><Icon name="reply" />Reply</button>
      <button onclick={copyLink}><Icon name="link" />Copy link</button>
      {#if showThread}<button onclick={openThread}><Icon name="hash" />Open reply thread</button
        >{/if}
      {#if canStartNamedThread && !message.pending}<button
          onclick={() => {
            moreOpen = false;
            threadPromptOpen = true;
            void tick().then(() => threadTitleInput?.focus());
          }}><Icon name="hash" />Start a named thread</button
        >{/if}
      <button
        onclick={() => {
          moreOpen = false;
          pickerOpen = true;
        }}
        disabled={!canWrite}><Icon name="smile" />Add reaction</button
      >
      {#if canPin}<button
          onclick={() => {
            moreOpen = false;
            void pin();
          }}><Icon name="pin" />{message.pinned ? 'Unpin message' : 'Pin message'}</button
        >{/if}
      {#if own && canWrite}<button onclick={beginEdit}><Icon name="edit" />Edit message</button
        >{/if}
      {#if canDelete}<button
          class="danger-text"
          onclick={() => {
            moreOpen = false;
            void remove();
          }}><Icon name="trash" />Delete message</button
        >{/if}
    </div>
  </Modal>{/if}

{#if threadPromptOpen}<form
    class="thread-prompt"
    onsubmit={(event) => {
      event.preventDefault();
      void startNamedThread();
    }}
  >
    <label for="thread-title-{message.id}">Name this thread</label>
    <input
      id="thread-title-{message.id}"
      bind:this={threadTitleInput}
      bind:value={threadTitle}
      maxlength="120"
      placeholder="What is this discussion about?"
    />
    {#if threadError}<p class="thread-prompt-error" role="alert">{threadError}</p>{/if}
    <div class="button-row">
      <button
        type="button"
        class="secondary"
        onclick={() => {
          threadPromptOpen = false;
          threadTitle = '';
          threadError = '';
        }}>Cancel</button
      >
      <button type="submit" class="primary" disabled={!threadTitle.trim()}>Start thread</button>
    </div>
  </form>{/if}

{#if reportOpen}<ReportMessage {message} close={() => (reportOpen = false)} />{/if}
{#if privateReportOpen}<ReportPrivateMessage
    {message}
    close={() => (privateReportOpen = false)}
  />{/if}
