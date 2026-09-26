<script lang="ts">
  import type { User } from '../types';
  let { user, size = 36 }: { user?: User; size?: number } = $props();
</script>

<span
  class="avatar"
  style:width={`${size}px`}
  style:height={`${size}px`}
  style:background={user?.avatar_color || '#89a8f4'}
  aria-hidden="true">{(user?.display_name || user?.username || '?').slice(0, 2).toUpperCase()}</span
>
