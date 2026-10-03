<script lang="ts">
  let { name, size = 20 }: { name: string; size?: number } = $props();
  const paths: Record<string, string> = {
    hash: 'M5 9h14M4 15h14M11 3 7 21M17 3l-4 18',
    link: 'M9 13a4 4 0 0 0 5.7 0l2.6-2.6a4 4 0 1 0-5.7-5.7l-1.3 1.3',
    search: 'm21 21-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
    volume: 'm11 5-6 4H2v6h3l6 4V5Zm4 3a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14',
    mic: 'M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0V5Zm-3 6v1a6 6 0 0 0 12 0v-1m-6 7v4m-4 0h8',
    micOff:
      'm2 2 20 20M9 5a3 3 0 0 1 6 0v5M9 9v3a3 3 0 0 0 4 3m-7-4v1a6 6 0 0 0 10 4m-4 2v4m-4 0h8',
    headphones: 'M3 14v-3a9 9 0 0 1 18 0v3M3 13h4v8H3v-8Zm14 0h4v8h-4v-8Z',
    settings:
      'M9.5 3h5l.6 2.4 1.4.8 2.4-.7 2.5 4.3-1.8 1.7v1.6l1.8 1.7-2.5 4.3-2.4-.7-1.4.8-.6 2.4h-5l-.6-2.4-1.4-.8-2.4.7-2.5-4.3 1.8-1.7v-1.6L2.1 9.8l2.5-4.3 2.4.7 1.4-.8L9.5 3ZM15.5 12a3.5 3.5 0 1 1-7 0 3.5 3.5 0 0 1 7 0',
    users:
      'M8 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm-7 9v-2a5 5 0 0 1 5-5h4a5 5 0 0 1 5 5v2m1-16a4 4 0 0 1 0 8m3 2a5 5 0 0 1 4 5v1',
    plus: 'M12 5v14M5 12h14',
    reply: 'm9 5-7 7 7 7M2 12h11a8 8 0 0 1 8 8',
    pin: 'M8 3h8l-1 7 4 4v2H5v-2l4-4-1-7ZM12 16v6',
    close: 'm6 6 12 12M6 18 18 6',
    send: 'm22 2-7 20-4-9-9-4 20-7ZM22 2 11 13',
    file: 'M14 2H4v20h16V8l-6-6Zm0 0v6h6M8 13h8M8 17h5',
    edit: 'm16 3 5 5-12 12-6 1 1-6L16 3Zm-3 3 5 5',
    trash: 'M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7',
    smile: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM8 9h.01M16 9h.01M8 15s1 3 4 3 4-3 4-3',
    menu: 'M4 6h16M4 12h16M4 18h16',
    arrow: 'M5 12h14m-6-6 6 6-6 6',
    leave: 'M9 4H3v16h6M10 12h12m-5-5 5 5-5 5',
    sun: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1',
    moon: 'M21 13A9 9 0 1 1 11 3a7 7 0 0 0 10 10Z',
    shield: 'm12 2 9 4v6c0 6-9 10-9 10S3 18 3 12V6l9-4Zm-4 10 3 3 5-6',
    bell: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M9 21h6',
    download: 'M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5',
    chevron: 'm6 9 6 6 6-6',
    chevronLeft: 'm15 6-6 6 6 6',
    chevronRight: 'm9 6 6 6-6 6',
    check: 'm5 12 4 4L19 6',
    more: 'M5 12h.01M12 12h.01M19 12h.01',
    connection:
      'M3 3l18 18M8 5a17 17 0 0 1 13 4M3 9l2-1m6 2a12 12 0 0 1 7 3M6 13l2-1m1 4a5 5 0 0 1 6 0m-3 4h.01',
    clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Zm0-16v6l4 2',
    volumeOff: 'M11 5 5 9H2v6h3l6 4V5Zm9 4-6 6m0-6 6 6',
    expand: 'M4 9V4h5M20 15v5h-5M4 4l6 6m10 10-6-6M15 4h5v5M9 20H4v-5'
  };
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.7"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"><path d={paths[name] || paths.hash} /></svg
>
