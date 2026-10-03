<script lang="ts">
  import MessageTokens from './MessageTokens.svelte';
  import { type Token } from '../message-tokens';
  import { workspace } from '../workspace.svelte';

  // Renders a tokenized message body, recursively: emphasis and quotes carry their own
  // tokens, so `**bold with a `link` in it**` is one bold element containing a link
  // element rather than a string anybody has to parse again.
  //
  // Nothing here ever writes markup from the message. Every piece of text is text — the
  // element an emphasis token becomes is chosen here, from a closed set of styles the
  // tokenizer can produce — which is the property the whole tokenizer exists for.
  let { tokens }: { tokens: Token[] } = $props();

  // A mention of somebody who is not here reads as plain text rather than as a link to
  // nowhere, which is also what a name inside a code block is.
  const memberExists = (name: string) =>
    workspace.state?.users.some((member) => member.username.toLowerCase() === name.toLowerCase())
      ? ''
      : 'Nobody by this name is on this server';
</script>

{#each tokens as token, index (index)}{#if token.kind === 'link'}<a
      href={token.value}
      target="_blank"
      rel="noopener noreferrer">{token.value}</a
    >{:else if token.kind === 'mention'}<span class="mention" title={memberExists(token.name)}
      >{token.value}</span
    >{:else if token.kind === 'code'}<code class="message-code">{token.value}</code
    >{:else if token.kind === 'emphasis'}{#if token.style === 'bold'}<strong
        ><MessageTokens tokens={token.tokens} /></strong
      >{:else if token.style === 'italic'}<em><MessageTokens tokens={token.tokens} /></em>{:else}<s
        ><MessageTokens tokens={token.tokens} /></s
      >{/if}
  {:else if token.kind === 'quote'}<blockquote>
      <MessageTokens tokens={token.tokens} />
    </blockquote>{:else}{token.value}{/if}{/each}
