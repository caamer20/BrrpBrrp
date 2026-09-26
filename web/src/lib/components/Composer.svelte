<script lang="ts">
  import { tick, onDestroy } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { errorMessage, uploadFile } from '../api';
  import type { Attachment, User, Message } from '../types';
  import Icon from './Icon.svelte';

  let { thread, unavailable = false }: { thread?: Message; unavailable?: boolean } = $props();
  const componentId = $props.id();
  const helpId = `${componentId}-help`;
  const mentionsId = `${componentId}-mentions`;
  let scope = $derived(thread ? `thread:${thread.id}` : workspace.activeId);
  let readOnly = $derived(
    unavailable ||
      workspace.authorizationUnverified ||
      !workspace.canWriteConversation(
        thread ? workspace.conversationId(thread) : workspace.activeId
      )
  );

  let text = $state('');
  let attachments = $state<Attachment[]>([]);
  let progress = $state<number | null>(null);
  let uploadName = $state('');
  let uploadChannelName = $state('');
  let dragging = $state(false);
  let dragDepth = 0;
  let mounted = true;
  let fileInput: HTMLInputElement;
  let textarea: HTMLTextAreaElement;
  let draftChannel = $state('');
  let mention = $state<{ start: number; end: number; query: string } | null>(null);
  let mentionIndex = $state(0);
  let matches = $derived(
    mention
      ? (workspace.direct ? [workspace.user!, workspace.direct.peer] : workspace.state?.users || [])
          .filter(
            (user) =>
              !user.is_banned &&
              !user.is_deactivated &&
              user.username.toLowerCase().startsWith(mention!.query.toLowerCase())
          )
          .slice(0, 8)
      : []
  );
  const drafts = workspace.composerDrafts;
  const captureDrafts = () => {
    if (!draftChannel) return;
    if (text.trim() || attachments.length) drafts.set(draftChannel, { text, attachments });
    else drafts.delete(draftChannel);
  };
  workspace.composerCaptures.add(captureDrafts);
  onDestroy(() => {
    captureDrafts();
    mounted = false;
    workspace.composerCaptures.delete(captureDrafts);
  });
  $effect(() => {
    if (draftChannel !== scope) {
      captureDrafts();
      draftChannel = scope;
      const draft = drafts.get(draftChannel);
      text = draft?.text ?? workspace.drafts[draftChannel] ?? '';
      attachments = draft?.attachments || [];
      mention = null;
    }
  });
  $effect(() => {
    text;
    void tick().then(() => {
      if (!textarea) return;
      textarea.style.height = 'auto';
      textarea.style.height = `${Math.min(textarea.scrollHeight, 144)}px`;
    });
  });
  function changed() {
    workspace.saveDraft(draftChannel, text);
    workspace.typingStarted();
    const end = textarea.selectionStart;
    const match = text.slice(0, end).match(/(?:^|\s)@([A-Za-z0-9_]*)$/);
    mention = match ? { start: end - match[1].length - 1, end, query: match[1] } : null;
    mentionIndex = 0;
  }
  function complete(user: User) {
    if (!mention) return;
    const start = mention.start;
    text = text.slice(0, start) + '@' + user.username + ' ' + text.slice(mention.end);
    mention = null;
    workspace.saveDraft(draftChannel, text);
    textarea.focus();
    requestAnimationFrame(() =>
      textarea.setSelectionRange(start + user.username.length + 2, start + user.username.length + 2)
    );
  }

  async function attach(files: FileList | File[] | null) {
    if (!files || progress !== null || readOnly) return;
    const channel = scope;
    const destination = thread ? workspace.conversationId(thread) : workspace.activeId;
    const generation = workspace.authorizationGeneration;
    const channelName = workspace.conversationName;
    uploadChannelName = channelName;
    let savedElsewhere = false;
    for (const file of Array.from(files)) {
      const existing =
        channel === draftChannel ? attachments : drafts.get(channel)?.attachments || [];
      if (existing.length >= 5) {
        workspace.showToast('Attach up to five files per message.');
        break;
      }
      if (file.size > (workspace.state?.settings.max_upload_bytes || 12582912)) {
        const limit = (workspace.state?.settings.max_upload_bytes || 12582912) / 1048576;
        workspace.showToast(
          `${file.name} is too large. Choose a file under ${Number(limit.toFixed(1))} MB.`
        );
        continue;
      }
      progress = 0;
      workspace.uploadCount++;
      uploadName = file.name;
      try {
        const attachment = await uploadFile<Attachment>(
          file,
          (value) => (progress = value),
          workspace.isDirect(destination) ? destination : undefined
        );
        if (
          generation !== workspace.authorizationGeneration ||
          !workspace.canWriteConversation(destination)
        )
          return;
        if (workspace.composerDrafts !== drafts) return;
        if (mounted && channel === draftChannel) attachments = [...attachments, attachment];
        else {
          const draft = drafts.get(channel);
          drafts.set(channel, {
            text: draft?.text ?? workspace.drafts[channel] ?? '',
            attachments: [...(draft?.attachments || []), attachment]
          });
          savedElsewhere = true;
        }
      } catch (error) {
        if (mounted) workspace.showToast(errorMessage(error));
      } finally {
        progress = null;
        workspace.uploadCount--;
      }
    }
    if (savedElsewhere) workspace.showToast(`Files added to your draft in #${channelName}.`);
    if (fileInput) fileInput.value = '';
  }

  function send() {
    if ((!text.trim() && !attachments.length) || progress !== null || readOnly) return;
    const content = text.trim();
    const uploaded = attachments;
    text = '';
    attachments = [];
    const reply = thread?.id || workspace.replyTo?.id;
    if (!thread) workspace.replyTo = null;
    mention = null;
    workspace.saveDraft(draftChannel, '');
    workspace.send(content, uploaded, reply, !!thread);
    textarea?.focus();
  }
</script>

<div class="composer-area" class:thread-composer={!!thread}>
  {#if workspace.direct && readOnly}<p class="upload-status" role="status">
      This conversation is read-only. You can read earlier messages and copy your drafts; sending
      resumes only when both participants can write.
    </p>{/if}
  {#if unavailable}<p class="upload-status" role="status">
      This discussion was removed. Copy your unsent text before closing.
    </p>{/if}
  {#if !thread && workspace.replyTo}<div class="reply-composer">
      <span
        >Replying to <strong
          >{workspace.state?.users.find((user) => user.id === workspace.replyTo?.user_id)
            ?.display_name || 'Member'}</strong
        >
        · {workspace.replyTo.content.slice(0, 100) || 'Attachment'}</span
      ><button
        class="icon-button"
        aria-label="Cancel reply"
        onclick={() => (workspace.replyTo = null)}><Icon name="close" size={16} /></button
      >
    </div>{/if}
  {#if matches.length}<div
      class="mention-suggestions"
      id={mentionsId}
      role="listbox"
      aria-label="Mention a member"
    >
      {#each matches as user, index (user.id)}<button
          role="option"
          id={`${componentId}-mention-${index}`}
          aria-selected={index === mentionIndex}
          class:selected={index === mentionIndex}
          onpointerdown={(event) => event.preventDefault()}
          onclick={() => complete(user)}
          ><strong>{user.display_name || user.username}</strong><span>@{user.username}</span
          ></button
        >{/each}
    </div>{/if}
  <div
    class="composer"
    class:dragging
    ondragenter={(event) => {
      if (event.dataTransfer?.types.includes('Files')) {
        dragDepth++;
        dragging = true;
      }
    }}
    ondragleave={() => {
      if (--dragDepth <= 0) {
        dragDepth = 0;
        dragging = false;
      }
    }}
    ondragover={(event) => event.preventDefault()}
    ondrop={(event) => {
      event.preventDefault();
      dragging = false;
      dragDepth = 0;
      attach(event.dataTransfer?.files || null);
    }}
    role="group"
    aria-label="Message composer"
  >
    {#if attachments.length}<div class="draft-attachments">
        {#each attachments as attachment (attachment.id)}<span
            ><Icon name="file" size={15} />{attachment.file_name}<button
              class="icon-button"
              aria-label={`Remove ${attachment.file_name}`}
              onclick={() =>
                (attachments = attachments.filter((item) => item.id !== attachment.id))}
              ><Icon name="close" size={14} /></button
            ></span
          >{/each}
      </div>{/if}
    <div class="composer-input">
      <input
        bind:this={fileInput}
        type="file"
        multiple
        class="visually-hidden"
        tabindex="-1"
        aria-label="Choose attachments"
        onchange={() => attach(fileInput.files)}
      />
      <button
        class="icon-button attach-button"
        aria-label="Attach files"
        title="Attach up to 5 files"
        disabled={progress !== null || readOnly}
        onclick={() => fileInput.click()}><Icon name="plus" /></button
      >
      <textarea
        bind:this={textarea}
        bind:value={text}
        rows="1"
        maxlength="4000"
        aria-label={thread ? 'Reply in thread' : `Message ${workspace.conversationName}`}
        aria-describedby={helpId}
        placeholder={dragging
          ? 'Drop files to attach'
          : thread
            ? 'Continue this conversation…'
            : workspace.direct
              ? `Message ${workspace.conversationName}`
              : `Message #${workspace.conversationName}`}
        aria-controls={matches.length ? mentionsId : undefined}
        oninput={changed}
        onclick={() => (mention = null)}
        onkeydown={(event) => {
          if (matches.length && !event.isComposing) {
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault();
              mentionIndex =
                (mentionIndex + (event.key === 'ArrowDown' ? 1 : -1) + matches.length) %
                matches.length;
              return;
            }
            if (event.key === 'Enter' || event.key === 'Tab') {
              event.preventDefault();
              complete(matches[mentionIndex]);
              return;
            }
            if (event.key === 'Escape') {
              mention = null;
              event.preventDefault();
              return;
            }
          }
          if (
            event.key === 'Enter' &&
            !event.shiftKey &&
            !event.isComposing &&
            (!matchMedia('(pointer: coarse)').matches || event.ctrlKey || event.metaKey)
          ) {
            event.preventDefault();
            send();
          }
        }}
        onpaste={(event) => {
          const files = event.clipboardData?.files;
          if (files?.length) {
            event.preventDefault();
            attach(files);
          }
        }}></textarea>
      <button
        class="send-button"
        aria-label={thread ? 'Send reply' : 'Send message'}
        title="Send message"
        disabled={(!text.trim() && !attachments.length) || progress !== null || readOnly}
        onclick={send}><Icon name="send" size={19} /></button
      >
    </div>
    {#if progress !== null}<div
        class="upload-progress"
        role="progressbar"
        aria-label="Uploading file"
        aria-valuenow={progress}
        aria-valuemin="0"
        aria-valuemax="100"
      >
        <span style:width={`${progress}%`}></span>
      </div>{/if}
  </div>
  {#if progress !== null}<p class="upload-status" role="status">
      Uploading {uploadName} to #{uploadChannelName} · {progress}%
    </p>{/if}
  <div class="composer-hint" id={helpId}>
    <span
      >{#if progress !== null}Uploading · {progress}%{:else}Shift + Enter for a new line{/if}</span
    ><span
      >{text.length > 3500
        ? `${text.length}/4000`
        : workspace.channel?.slow_mode_seconds
          ? `Slow mode: ${workspace.channel.slow_mode_seconds}s between messages`
          : 'Keep it friendly.'}</span
    >
  </div>
  {#if text.length > 3500}<span class="mobile-character-count">{text.length}/4000</span>{/if}
</div>
