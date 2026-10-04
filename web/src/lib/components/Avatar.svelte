<script lang="ts">
  import type { User } from '../types';
  import { avatarStyle } from '../avatar-ink';
  let { user, size = 36 }: { user?: User; size?: number } = $props();
  // The initials' ink comes from the background rather than from the stylesheet's constant:
  // a deleted member's avatar is grey, and the dark ink measured 4.0:1 on it. A saturated
  // mid-tone has no ink that clears the minimum at all, so `avatarStyle` also moves the
  // background far enough for its ink to pass — the hue survives and the letters are
  // legible (see avatar-ink.ts).
  const style = $derived(avatarStyle(user?.avatar_color || '#89a8f4'));
</script>

<span
  class="avatar"
  style:width={`${size}px`}
  style:height={`${size}px`}
  style:background={style.background}
  style:color={style.ink}
  aria-hidden="true">{(user?.display_name || user?.username || '?').slice(0, 2).toUpperCase()}</span
>
