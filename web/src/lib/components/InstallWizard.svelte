<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Icon from './Icon.svelte';
  import { workspace } from '../workspace.svelte';

  let { dark }: { dark: boolean } = $props();

  type Settings = {
    mode: string;
    port: number;
    data_dir: string;
    network_host: string;
    public_url: string;
    trusted_proxies: string;
    voice_ip: string;
    allow_firewall: boolean;
  };
  type Paths = { install_dir: string; binary: string; data_dir: string; config: string };
  type Plan = {
    settings: Settings;
    paths: Paths;
    url: string;
    address: string;
    free_bytes: number;
    warnings: string[];
  };
  type Status = { phase: string; message: string; error: string; plan?: Plan; registered: boolean };
  type RemovalPlan = {
    id: string;
    found: boolean;
    binaries: string[];
    config: string;
    data_dir: string;
    next_data_dir: string;
    service: boolean;
    blocked: string;
  };
  type Info = {
    os: string;
    can_update: boolean;
    version: string;
    paths: Paths;
    addresses: string[];
    preview: boolean;
    privilege_error: string;
    state: Status;
  };

  const steps = ['Connection', 'Storage', 'Review', 'Administrator', 'Ready'];
  const token = location.hash.slice(1);
  type CertificateInfo = { fingerprint: string; is_ca: boolean; name: string; expires_at: string };
  let certificateInfo = $state<CertificateInfo>();
  let certificateKey = '';
  let trustConfirmed = $state(false);
  let updateConfirmed = $state(false);
  let updateFinished = $state(false);
  let updateRequested = false;
  let repairConfirmed = $state(false);
  let certificateNotice = $state('');
  let info = $state<Info>();
  let settings = $state<Settings>({
    mode: 'computer',
    port: 8082,
    data_dir: '',
    network_host: '',
    public_url: '',
    trusted_proxies: '127.0.0.1/32,::1/128',
    voice_ip: '',
    allow_firewall: true
  });
  let status = $state<Status>({ phase: 'welcome', message: '', error: '', registered: false });
  let plan = $state<Plan>();
  let step = $state(0);
  let busy = $state(false);
  let error = $state('');
  let serverName = $state('BrrpBrrp');
  let username = $state('');
  let password = $state('');
  let confirmation = $state('');
  let showPassword = $state(false);
  let closed = $state(false);
  let managing = $state(false);
  let removal = $state<RemovalPlan>();
  let removalConfirmed = $state(false);
  let heading = $state<HTMLHeadingElement>();
  const installing = $derived(
    ['installing', 'creating', 'removing', 'certifying'].includes(status.phase)
  );
  const platform = $derived(
    info?.os === 'windows' ? 'Windows' : info?.os === 'darwin' ? 'macOS' : 'Linux'
  );

  async function request<T>(path: string, body?: unknown): Promise<T> {
    const response = await fetch(`/api/install/${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' })
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store'
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'Setup could not complete that step.');
    return result;
  }
  const message = (failure: unknown) =>
    failure instanceof Error ? failure.message : 'Something went wrong. Please try again.';
  async function go(next: number) {
    step = next;
    error = '';
    await tick();
    heading?.focus();
  }
  function sync(next: Status & { removal?: RemovalPlan }) {
    status = next;
    if (updateRequested && ['complete', 'account'].includes(next.phase)) {
      updateFinished = true;
      updateRequested = false;
    } else if (next.phase === 'error') updateRequested = false;
    if (
      next.plan?.settings.mode === 'network' &&
      next.registered &&
      !['installing', 'certifying'].includes(next.phase)
    ) {
      const key = next.plan.url + next.phase;
      if (certificateKey !== key) {
        certificateKey = key;
        request<CertificateInfo>('certificate-info')
          .then((result) => (certificateInfo = result))
          .catch(() => {
            certificateKey = '';
          });
      }
    }
    if (next.removal) removal = next.removal;
    if (['removing', 'removed', 'remove_error'].includes(next.phase)) managing = true;
    if (next.plan) plan = next.plan;
    if (next.plan && ['resume', 'installing', 'error'].includes(next.phase) && step !== 2)
      void go(2);
    if (next.phase === 'account' && step !== 3) void go(3);
    if (next.phase === 'complete' && step !== 4) void go(4);
  }
  onMount(() => {
    let active = true;
    let polling = false;
    request<Info>('info')
      .then((result) => {
        if (!active) return;
        info = result;
        settings.data_dir = result.paths.data_dir;
        settings.network_host = result.addresses[0] || '';
        sync(result.state);
        if (result.state.plan) {
          settings = { ...result.state.plan!.settings };
        }
      })
      .catch((failure) => {
        if (active) error = message(failure);
      });
    const timer = setInterval(async () => {
      if (!active || closed || polling || !installing) return;
      polling = true;
      try {
        const next = await request<Status>('status');
        if (active) sync(next);
      } catch (failure) {
        if (active) error = message(failure);
      } finally {
        polling = false;
      }
    }, 750);
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (installing) {
        event.preventDefault();
        event.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', beforeUnload);
    return () => {
      active = false;
      clearInterval(timer);
      window.removeEventListener('beforeunload', beforeUnload);
    };
  });
  async function next(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    if (step === 0) {
      await go(1);
      return;
    }
    busy = true;
    try {
      plan = await request<Plan>('check', settings);
      await go(2);
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function start() {
    busy = true;
    error = '';
    try {
      await request(status.registered ? 'retry' : 'apply', status.registered ? {} : settings);
      status = { ...status, phase: 'installing', error: '' };
      sync(await request<Status>('status'));
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function createAccount(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    if (password !== confirmation) {
      error = 'The passwords do not match.';
      return;
    }
    const length = new TextEncoder().encode(password).length;
    if (length < 12 || length > 72) {
      error =
        'Use a password between 12 and 72 bytes. Accented characters may use more than one byte.';
      return;
    }
    busy = true;
    try {
      await request('account', { server_name: serverName, username, password });
      password = '';
      confirmation = '';
      sync({ ...status, phase: 'complete', error: '' });
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function certificate() {
    try {
      const response = await fetch('/api/install/certificate', {
        headers: { Authorization: `Bearer ${token}` }
      });
      if (!response.ok) throw new Error((await response.json()).error);
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement('a');
      link.href = url;
      link.download = 'BrrpBrrp-local-ca.cer';
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (failure) {
      error = message(failure);
    }
  }
  async function certificateAction(action: 'trust' | 'repair') {
    if (!certificateInfo) return;
    busy = true;
    error = '';
    certificateNotice = '';
    try {
      await request(`${action}-certificate`, {
        confirm: action === 'trust' ? trustConfirmed : repairConfirmed,
        fingerprint: certificateInfo.fingerprint
      });
      if (action === 'trust') {
        certificateNotice =
          'Trusted on this Windows computer. Restart your browser, then open the server address. Other devices still need the certificate.';
        trustConfirmed = false;
      } else {
        certificateKey = '';
        certificateInfo = undefined;
        repairConfirmed = false;
        sync({
          ...status,
          phase: 'certifying',
          message: 'Repairing the certificate and restarting the server',
          error: ''
        });
      }
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function updateApplication() {
    busy = true;
    error = '';
    try {
      await request('update', { confirm: updateConfirmed });
      updateRequested = true;
      sync({
        ...status,
        phase: 'installing',
        message: 'Updating the application and restarting the server',
        error: ''
      });
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function openTray() {
    try {
      await request('open-tray', {});
      certificateNotice =
        'Open the BrrpBrrp icon in the Windows notification area. It may be under Show hidden icons.';
    } catch (failure) {
      error = message(failure);
    }
  }
  async function copyAddress() {
    try {
      await navigator.clipboard.writeText(plan?.url || '');
      certificateNotice = 'Server address copied.';
    } catch {
      error = 'Select and copy the server address shown above.';
    }
  }
  async function finish() {
    try {
      await request('close', {});
      closed = true;
    } catch (failure) {
      error = message(failure);
    }
  }
  async function manageInstallation() {
    managing = true;
    busy = true;
    error = '';
    removal = undefined;
    removalConfirmed = false;
    try {
      removal = await request<RemovalPlan>('uninstall-plan');
      await tick();
      heading?.focus();
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function uninstall() {
    if (!removal || !removalConfirmed) return;
    busy = true;
    error = '';
    try {
      await request('uninstall', { id: removal.id, confirmed: removalConfirmed });
      status = {
        phase: 'removing',
        message: 'Preparing to uninstall',
        error: '',
        registered: false
      };
      sync(await request<Status>('status'));
    } catch (failure) {
      error = message(failure);
    } finally {
      busy = false;
    }
  }
  async function freshInstallation() {
    if (!removal) return;
    settings.data_dir = removal.next_data_dir;
    status = { phase: 'welcome', message: '', error: '', registered: false };
    plan = undefined;
    managing = false;
    await go(0);
  }
</script>

<svelte:head><title>Set up BrrpBrrp</title></svelte:head>

<div class="installer">
  <header class="installer-top">
    <div class="installer-brand">
      <span class="brand-mark">b.</span><span
        >BrrpBrrp <span class="installer-label">/ Setup</span></span
      >
    </div>
    <button
      class="icon-button"
      aria-label="Switch light or dark theme"
      onclick={() => {
        workspace.theme = dark ? 'light' : 'dark';
        workspace.savePreferences();
      }}><Icon name={dark ? 'sun' : 'moon'} /></button
    >
  </header>
  <main class="installer-shell">
    <aside class="installer-steps" aria-label="Setup progress">
      <p class="eyebrow">YOUR SERVER, YOUR SPACE</p>
      <ol>
        {#each steps as label, index}<li
            class:current={!managing && step === index}
            class:done={!managing && step > index}
            aria-current={!managing && step === index ? 'step' : undefined}
          >
            <span class="step-number"
              >{#if step > index}<Icon name="check" size={16} />{:else}{index + 1}{/if}</span
            ><span>{label}</span>
          </li>{/each}
      </ol>
      <div class="installer-footnote">
        <Icon name="shield" size={18} />
        <p>Hosted on your computer.<br />Your data stays with you.</p>
      </div>
    </aside>
    <section class="installer-content" aria-busy={busy || installing}>
      {#if info?.preview}<div class="installer-notice">
          Preview · Installation and account changes are disabled.
        </div>{/if}
      {#if info?.privilege_error && !info.preview}<div class="installer-notice warning">
          {info.privilege_error}
        </div>{/if}
      {#if !info && !error}<h1 bind:this={heading} tabindex="-1">Getting things ready…</h1>
        <p class="muted">Checking this computer.</p>
      {:else if closed}<div class="installer-success"><Icon name="check" size={28} /></div>
        <h1 bind:this={heading} tabindex="-1">Setup is finished.</h1>
        <p class="muted">Your server keeps running. You can close this tab and the setup window.</p>
        {#if plan}<a class="primary installer-link" href={plan.url} target="_blank" rel="noreferrer"
            >Open BrrpBrrp <Icon name="arrow" size={18} /></a
          >{/if}
      {:else if managing}
        <p class="eyebrow">EXISTING INSTALLATION</p>
        <h1 bind:this={heading} tabindex="-1">
          {status.phase === 'removed'
            ? 'Ready for a fresh start.'
            : status.phase === 'removing'
              ? 'Removing the old version.'
              : 'Uninstall BrrpBrrp.'}
        </h1>
        {#if !removal}
          <p class="muted">
            {busy
              ? 'Looking for the installed application and service…'
              : 'Setup could not inspect the existing installation.'}
          </p>
        {:else if status.phase === 'removed'}
          <p class="installer-intro">
            The old application and background service have been removed. Your previous accounts,
            messages, uploads and settings are still on this computer.
          </p>
          <div class="installer-summary">
            <div><span>Previous data kept at</span><code>{removal.data_dir}</code></div>
            {#if removal.config}<div>
                <span>Previous settings kept at</span><code>{removal.config}</code>
              </div>{/if}
            <div>
              <span>Folder for your new installation</span><code>{removal.next_data_dir}</code>
            </div>
          </div>
          <p class="muted">
            A fresh installation starts with a new administrator account. Keep the previous folder
            if you want to recover the old server later.
          </p>
          <div class="installer-actions">
            <span></span><button class="primary" onclick={freshInstallation}
              >Continue with fresh setup <Icon name="arrow" size={18} /></button
            >
          </div>
        {:else if !removal.found}
          <p class="installer-intro">
            No previous installation was found in the supported application locations.
          </p>
          <p class="muted">
            If you chose a folder containing an unrelated database or other files, choose an empty
            data folder in the Storage step.
          </p>
        {:else}
          <p class="installer-intro">
            Setup will stop BrrpBrrp and remove its application and background service. Your data
            and settings stay in their current locations.
          </p>
          <div class="installer-summary">
            {#each removal.binaries as binary}<div>
                <span>Application to remove</span><code>{binary}</code>
              </div>{/each}
            <div>
              <span>Background service</span><strong
                >{removal.service
                  ? 'Will be stopped and removed'
                  : 'No registered service found'}</strong
              >
            </div>
            <div><span>Data to keep</span><code>{removal.data_dir}</code></div>
            {#if removal.config}<div>
                <span>Settings to keep</span><code>{removal.config}</code>
              </div>{/if}
            <div><span>Folder for fresh setup</span><code>{removal.next_data_dir}</code></div>
          </div>
          <p class="installer-notice">
            Anyone connected to this server will be disconnected. Fresh setup uses an empty folder,
            so your old data will not cause another installation conflict.
          </p>
          {#if removal.blocked}<p class="installer-error" role="alert">{removal.blocked}</p>{/if}
          {#if status.phase === 'removing'}
            <div class="installer-progress" role="status">
              <span class="progress-dot"></span><span>{status.message}</span>
            </div>
          {:else}
            {#if status.phase === 'remove_error'}<p class="installer-error" role="alert">
                {status.error}
              </p>{/if}
            <label class="installer-check"
              ><input
                type="checkbox"
                bind:checked={removalConfirmed}
                disabled={!!removal.blocked || info?.preview}
              /><span>I’m ready to uninstall this server and keep its data.</span></label
            >
            <div class="installer-actions">
              <button class="secondary" disabled={busy} onclick={manageInstallation}
                >Refresh review</button
              >
              <button
                class="primary"
                disabled={busy ||
                  !removalConfirmed ||
                  !!removal.blocked ||
                  info?.preview ||
                  !!info?.privilege_error}
                onclick={uninstall}>Uninstall and keep data</button
              >
            </div>
          {/if}
        {/if}
        {#if status.phase !== 'removing' && status.phase !== 'removed'}<button
            class="secondary removal-back"
            disabled={busy}
            onclick={() => {
              managing = false;
              error = '';
            }}>Back to setup</button
          >{/if}
      {:else if step === 0}
        <p class="eyebrow">LET’S GET YOU SETTLED</p>
        <h1 bind:this={heading} tabindex="-1">Where will you connect?</h1>
        <p class="installer-intro">
          Start with a local server. You can make it available online later, using the hosting setup
          you prefer.
        </p>
        <form onsubmit={next}>
          <fieldset class="connection-options">
            <legend class="sr-only">Connection type</legend>
            {#each [{ value: 'computer', icon: 'shield', title: 'This computer', text: 'Open BrrpBrrp here. A simple place to start.' }, { value: 'network', icon: 'users', title: 'My local network', text: 'Let people on your home or office network join.' }, { value: 'proxy', icon: 'settings', title: 'My own HTTPS proxy', text: 'I already manage a domain and a reverse proxy.' }] as option}
              <label class="connection-option" class:chosen={settings.mode === option.value}
                ><input
                  type="radio"
                  name="connection"
                  value={option.value}
                  bind:group={settings.mode}
                /><span class="option-icon"><Icon name={option.icon} size={21} /></span><span
                  ><strong>{option.title}</strong><small>{option.text}</small></span
                ></label
              >
            {/each}
          </fieldset>
          <div class="installer-grid">
            <label
              >Server port<input
                type="number"
                min="1024"
                max="65535"
                required
                bind:value={settings.port}
              /><small>8082 is the default. Choose another if it is in use.</small></label
            >
            {#if settings.mode === 'network'}<label
                >Server address<select required bind:value={settings.network_host}
                  ><option value="" disabled>Choose a network address</option
                  >{#each info?.addresses || [] as address}<option value={address}>{address}</option
                    >{/each}</select
                ><small>Use an address on the network your members will join.</small></label
              >{/if}
          </div>
          {#if settings.mode === 'network'}<div class="installer-notice">
              Local-network voice needs a trusted HTTPS certificate. Setup creates a private local
              certificate authority and shows you how to trust it on your devices.
            </div>{/if}
          {#if settings.mode === 'proxy'}<label
              >Public HTTPS address<input
                type="url"
                required
                placeholder="https://chat.example.com"
                bind:value={settings.public_url}
              /></label
            ><label
              >Trusted proxy IP ranges<input
                required
                bind:value={settings.trusted_proxies}
                spellcheck="false"
              /><small>The proxy connects to 127.0.0.1 on your chosen port.</small></label
            ><label
              >Advertised voice IP <span class="muted">(optional)</span><input
                bind:value={settings.voice_ip}
                placeholder="Your server’s reachable IP address"
                spellcheck="false"
              /><small>Set this when your network needs an explicit address for voice.</small
              ></label
            >{/if}
          {#if info?.os === 'windows' && settings.mode !== 'computer'}<label class="installer-check"
              ><input type="checkbox" bind:checked={settings.allow_firewall} /><span
                >Allow local-network connections in Windows Firewall<small
                  >Private and domain networks only. Public internet access stays under your
                  control.</small
                ></span
              ></label
            >{/if}
          <div class="installer-actions">
            <span class="muted">Step 1 of 5</span><button class="primary" disabled={!info}
              >Continue <Icon name="arrow" size={18} /></button
            >
          </div>
        </form>
      {:else if step === 1}
        <p class="eyebrow">A HOME FOR YOUR COMMUNITY</p>
        <h1 bind:this={heading} tabindex="-1">Keep your files together.</h1>
        <p class="installer-intro">
          Messages, attachments, settings and backups live in one private folder on this computer.
        </p>
        <form onsubmit={next}>
          <label
            >Data folder<input
              required
              bind:value={settings.data_dir}
              spellcheck="false"
              autocomplete="off"
            /><small
              >Choose a new, empty folder on a local disk. Existing data is never replaced.</small
            ></label
          >
          <div class="installer-summary">
            <div><span>Application</span><code>{info?.paths.install_dir}</code></div>
            <div>
              <span>Background service</span><strong
                >{info?.os === 'darwin'
                  ? 'Starts when you sign in to macOS'
                  : `Starts automatically with ${platform}`}</strong
              >
            </div>
            <div><span>Runtime</span><strong>Everything is included in BrrpBrrp</strong></div>
          </div>
          <p class="muted">
            Allow extra disk space for attachments and backup copies. Setup will check available
            space and your chosen port before installing.
          </p>
          <div class="installer-actions">
            <button type="button" class="secondary" onclick={() => go(0)}>Back</button><button
              class="primary"
              disabled={busy}
              >{busy ? 'Checking…' : 'Review setup'} <Icon name="arrow" size={18} /></button
            >
          </div>
        </form>
      {:else if step === 2}
        <p class="eyebrow">{status.registered ? 'PICK UP WHERE YOU LEFT OFF' : 'ONE LAST LOOK'}</p>
        <h1 bind:this={heading} tabindex="-1">
          {installing
            ? 'Making room for your people.'
            : status.registered
              ? 'Your installation is saved.'
              : 'Ready when you are.'}
        </h1>
        <p class="installer-intro">
          {status.registered
            ? 'We’ll check your existing server and continue account setup.'
            : 'Setup will save these settings, install the background service and check that your server starts.'}
        </p>
        {#if plan}<div class="installer-summary">
            <div><span>Server address</span><code>{plan.url}</code></div>
            <div><span>Data folder</span><code>{plan.paths.data_dir}</code></div>
            <div><span>Settings file</span><code>{plan.paths.config}</code></div>
            {#if plan.free_bytes}<div>
                <span>Free storage</span><strong
                  >{(plan.free_bytes / 1024 ** 3).toFixed(1)} GB</strong
                >
              </div>{/if}
            <div><span>Voice ports</span><strong>UDP 50000–50100</strong></div>
          </div>
          {#each plan.warnings as warning}<p class="installer-notice">{warning}</p>{/each}{/if}
        {#if installing}<div class="installer-progress" role="status">
            <span class="progress-dot"></span><span>{status.message}</span>
          </div>{/if}
        {#if status.error}<div class="installer-error" role="alert">{status.error}</div>{/if}
        <div class="installer-actions">
          {#if !status.registered}<button
              class="secondary"
              disabled={busy || installing}
              onclick={() => go(1)}>Back</button
            >{:else}<span></span>{/if}<button
            class="primary"
            disabled={busy || installing || info?.preview || !!info?.privilege_error}
            onclick={start}
            >{installing
              ? 'Installing…'
              : status.registered
                ? 'Check and continue'
                : 'Install and start'}
            <Icon name="arrow" size={18} /></button
          >
        </div>
      {:else if step === 3}
        <p class="eyebrow">YOUR SERVER IS RUNNING</p>
        <h1 bind:this={heading} tabindex="-1">Make yourself at home.</h1>
        <p class="installer-intro">
          Create your administrator account. You’ll use it to invite people, manage channels and
          look after your server.
        </p>
        <form onsubmit={createAccount}>
          <label>Server name<input required maxlength="80" bind:value={serverName} /></label><label
            >Administrator username<input
              required
              minlength="3"
              maxlength="32"
              pattern={'[a-zA-Z0-9_]{3,32}'}
              autocomplete="username"
              bind:value={username}
            /><small>3–32 letters, numbers or underscores.</small></label
          ><label
            >Password
            <div class="installer-password">
              <input
                required
                minlength="12"
                maxlength="72"
                type={showPassword ? 'text' : 'password'}
                autocomplete="new-password"
                bind:value={password}
              /><button
                type="button"
                class="secondary"
                aria-pressed={showPassword}
                onclick={() => (showPassword = !showPassword)}
                >{showPassword ? 'Hide' : 'Show'}</button
              >
            </div>
            <small>At least 12 characters. Password managers and pasting are welcome.</small></label
          ><label
            >Confirm password<input
              required
              type="password"
              autocomplete="new-password"
              bind:value={confirmation}
            /></label
          >
          <div class="installer-actions">
            <span class="muted">Your password stays private.</span><button
              class="primary"
              disabled={busy}
              >{busy ? 'Creating account…' : 'Create administrator'}
              <Icon name="arrow" size={18} /></button
            >
          </div>
        </form>
      {:else if step === 4 && plan}
        <div class="installer-success"><Icon name="check" size={28} /></div>
        <p class="eyebrow">ALL SET</p>
        <h1 bind:this={heading} tabindex="-1">
          {plan.settings.mode === 'network' ? 'Your server is running.' : 'Your space is ready.'}
        </h1>
        <p class="installer-intro">
          Open BrrpBrrp and sign in with your administrator account. The server keeps running after
          you close setup.
        </p>
        <div class="installer-address">
          <code>{plan.url}</code><a
            class="primary installer-link"
            href={plan.url}
            target="_blank"
            rel="noreferrer">Open BrrpBrrp <Icon name="arrow" size={18} /></a
          >
        </div>
        <div class="button-row">
          <button class="secondary" onclick={copyAddress}>Copy server address</button>
          {#if info?.os === 'windows'}<button class="secondary" onclick={openTray}
              >Open Windows tray controls</button
            >{/if}
        </div>
        {#if plan.settings.mode === 'network'}<p class="installer-notice">
            Complete certificate trust below before opening the app on each device.
          </p>{/if}
        {#if info?.os !== 'windows' && plan.settings.mode !== 'computer'}<p
            class="installer-notice"
          >
            If this computer has a firewall, allow your selected TCP port and UDP 50000–50100 from
            your local network.
          </p>{/if}
        <details>
          <summary>Make it available online later</summary>
          <p>
            Use your preferred HTTPS reverse proxy, certificate or network setup. Update <code
              >{plan.paths.config}</code
            > with the public HTTPS address and restart the service. The package’s hosting guide explains
            the settings.
          </p>
          <p>
            A web proxy carries chat and connection signaling. Voice also needs reachable UDP ports
            or a TURN relay. Test from another network before inviting people.
          </p>
        </details>
        <p class="muted">
          Next: enable two-factor authentication, invite a friend and set up automatic backups in
          Manage server.
        </p>
      {/if}
      {#if info?.can_update && status.registered && !installing && !managing && !closed && !updateFinished}
        <details open>
          <summary>Update this installation</summary>
          <p>
            Install BrrpBrrp {info.version} from this release. Accounts, messages, attachments, and settings
            stay in place. The previous executable is saved in the application folder for rollback. Export
            a backup before updating.
          </p>
          <label class="installer-checkbox"
            ><input type="checkbox" bind:checked={updateConfirmed} /> Update the app and restart the server,
            disconnecting active calls.</label
          >
          <button
            class="primary"
            disabled={busy || !updateConfirmed || info.preview}
            onclick={updateApplication}>Update and restart</button
          >
        </details>
      {/if}
      {#if certificateInfo && plan?.settings.mode === 'network' && status.registered && !installing && !managing && !closed}
        <details class="certificate-guide" open>
          <summary>Connect your devices securely</summary>
          <p>
            Use <strong>{plan.url}</strong> on every device. Download this server’s public {certificateInfo.is_ca
              ? 'root certificate'
              : 'certificate'} and transfer it to each person joining your network.
          </p>
          <button class="secondary" onclick={certificate}
            ><Icon name="download" size={18} /> Download trust certificate</button
          >
          <p class="muted">
            {certificateInfo.name} · Expires {new Date(
              certificateInfo.expires_at
            ).toLocaleDateString()}
          </p>
          <p>Compare this SHA-256 fingerprint with the certificate before trusting it:</p>
          <code class="certificate-fingerprint">{certificateInfo.fingerprint}</code>
          {#if !certificateInfo.is_ca}<p class="installer-notice">
              This installation uses an older self-signed server certificate. Repair below creates a
              root certificate that works with device trust settings. Everyone will need to trust
              the new root once.
            </p>{/if}
          {#if info?.os === 'windows'}
            <label class="installer-checkbox"
              ><input type="checkbox" bind:checked={trustConfirmed} /> I trust this server’s certificate
              on this Windows computer for all users.</label
            >
            <button
              class="primary"
              disabled={busy || !trustConfirmed || info.preview}
              onclick={() => certificateAction('trust')}>Trust on this Windows computer</button
            >
          {/if}
          <details>
            <summary>Windows devices</summary>
            <p>
              Open the downloaded .cer file, choose Install Certificate → Current User → Place all
              certificates in the following store → Trusted Root Certification Authorities. Verify
              the fingerprint and finish. Restart your browser.
            </p>
          </details>
          <details>
            <summary>iPhone and iPad</summary>
            <p>
              Open the .cer file on your device and install the downloaded profile in Settings →
              General → VPN &amp; Device Management. Then go to Settings → General → About →
              Certificate Trust Settings and enable full trust for BrrpBrrp Local CA. Reopen Safari
              at the server address. After signing in, use Share → Add to Home Screen.
            </p>
          </details>
          <details>
            <summary>Android devices</summary>
            <p>
              In Settings, search for “Install a certificate” and choose CA certificate (usually
              under Security → Encryption &amp; credentials). Select the .cer file and confirm, then
              reopen Chrome. Menu names vary by device. If your managed device blocks CA
              installation, ask its administrator or use a publicly trusted HTTPS address.
            </p>
          </details>
          <details>
            <summary>macOS and Linux devices</summary>
            <p>
              On macOS, import the .cer into Keychain Access, open its Trust settings and trust it
              for SSL. On Linux, import the certificate through your browser’s certificate authority
              settings or your distribution’s trust store. Restart the browser. Firefox may need the
              root imported in its own Authorities settings.
            </p>
          </details>
          <p>
            Open the exact address above and confirm the browser shows a secure connection before
            testing the microphone. Continuing past a warning does not complete certificate trust.
          </p>
          <details open={!certificateInfo.is_ca}>
            <summary>Repair a certificate problem</summary>
            <p>
              Repair stops the server briefly and saves the old certificate and key under
              certs/previous-* for recovery. It updates the certificate for the configured address.
              An existing BrrpBrrp root is reused; an older self-signed installation gets a new root
              to trust on each device.
            </p>
            <label class="installer-checkbox"
              ><input type="checkbox" bind:checked={repairConfirmed} /> Repair this certificate and restart
              the server, disconnecting active calls.</label
            >
            <button
              class="secondary"
              disabled={busy || !repairConfirmed || info?.preview}
              onclick={() => certificateAction('repair')}>Repair certificate and restart</button
            >
          </details>
        </details>
      {/if}
      {#if step === 4 && plan && !managing && !closed && !installing}
        <div class="installer-actions">
          <span class="muted">BrrpBrrp {info?.version}</span><button
            class="secondary"
            onclick={finish}>Finish setup <Icon name="check" size={18} /></button
          >
        </div>
      {/if}
      {#if certificateNotice}<p class="installer-notice" role="status">{certificateNotice}</p>{/if}
      {#if error}<div class="installer-error" role="alert">{error}</div>{/if}
      {#if info && !managing && !closed && !installing}
        <div class="installer-maintenance">
          <button class="secondary" disabled={busy} onclick={manageInstallation}
            >Uninstall an existing version</button
          ><small>Keep your old data and prepare for a fresh installation.</small>
        </div>
      {/if}
    </section>
  </main>
  <footer class="installer-bottom">BrrpBrrp · A little space for your people.</footer>
</div>

<style>
  .installer-maintenance {
    margin-top: 28px;
    padding-top: 22px;
    border-top: 1px solid var(--border);
    display: grid;
    gap: 10px;
    justify-items: start;
  }
  .installer-maintenance small {
    color: var(--muted);
  }
  .removal-back {
    margin-top: 22px;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  .installer {
    min-height: 100dvh;
    background: var(--sidebar-deep);
    padding: 28px 36px 18px;
  }
  .installer-top {
    max-width: 1080px;
    margin: 0 auto 36px;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .installer-brand {
    display: flex;
    align-items: center;
    gap: 12px;
    font-weight: 650;
  }
  .installer-brand .brand-mark {
    width: 36px;
    height: 36px;
    font-size: 25px;
  }
  .installer-label {
    color: var(--muted);
    font-weight: 400;
    margin-left: 8px;
  }
  .installer-shell {
    max-width: 980px;
    margin: auto;
    display: grid;
    grid-template-columns: 238px minmax(0, 1fr);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 18px;
    overflow: hidden;
    box-shadow: 0 16px 64px #0002;
  }
  .installer-steps {
    background: var(--sidebar);
    padding: 36px 25px;
    display: flex;
    flex-direction: column;
  }
  .installer-steps .eyebrow {
    font-size: 10px;
    letter-spacing: 0.09em;
  }
  .installer-steps ol {
    padding: 0;
    margin: 30px 0;
    list-style: none;
    display: grid;
    gap: 27px;
  }
  .installer-steps li {
    display: flex;
    align-items: center;
    gap: 13px;
    color: var(--muted);
    font-size: 14px;
    font-weight: 550;
  }
  .step-number {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border: 1px solid var(--border);
    border-radius: 50%;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .installer-steps .current {
    color: var(--text);
  }
  .current .step-number {
    background: var(--accent);
    color: var(--accent-text);
    border-color: var(--accent);
  }
  .done .step-number {
    color: var(--accent);
    border-color: var(--accent);
  }
  .installer-footnote {
    display: flex;
    gap: 9px;
    margin-top: auto;
    padding-top: 42px;
    color: var(--muted);
    font-size: 11px;
    line-height: 1.7;
  }
  .installer-content {
    min-height: 620px;
    padding: 38px 42px 28px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  h1 {
    font-size: clamp(25px, 3vw, 32px);
    letter-spacing: -0.045em;
    line-height: 1.17;
  }
  h1:focus {
    outline: none;
  }
  .installer-intro {
    color: var(--muted);
    font-size: 14px;
    line-height: 1.65;
    max-width: 520px;
    margin-top: -5px;
  }
  form {
    display: flex;
    flex-direction: column;
    gap: 19px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-size: 13px;
    font-weight: 550;
  }
  input,
  select {
    width: 100%;
    min-height: 43px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--sidebar);
    color: var(--text);
    padding: 10px 12px;
    font-size: 14px;
  }
  small {
    display: block;
    color: var(--muted);
    font-size: 11px;
    font-weight: 400;
    line-height: 1.55;
  }
  .connection-options {
    padding: 0;
    border: 0;
    margin: 4px 0 0;
    display: grid;
    gap: 9px;
  }
  .connection-option {
    position: relative;
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 13px;
    padding: 15px;
    border: 1px solid var(--border);
    border-radius: 11px;
    background: var(--sidebar);
    cursor: pointer;
  }
  .connection-option input {
    width: 16px;
    min-height: 16px;
    height: 16px;
    margin: 0;
    accent-color: var(--accent);
  }
  .connection-option strong {
    display: block;
    margin-bottom: 3px;
    font-size: 13px;
  }
  .connection-option.chosen {
    border-color: var(--accent);
    background: var(--selected);
  }
  .option-icon {
    width: 32px;
    display: grid;
    place-items: center;
    color: var(--muted);
  }
  .chosen .option-icon {
    color: var(--accent);
  }
  .installer-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 18px;
  }
  .installer-grid > label:only-child {
    max-width: 260px;
  }
  .installer-notice {
    padding: 12px 14px;
    border-radius: 8px;
    background: var(--surface);
    color: var(--muted);
    border: 1px solid var(--border);
    font-size: 12px;
    line-height: 1.65;
  }
  .installer-notice.warning,
  .installer-error {
    color: var(--danger);
  }
  .installer-check {
    display: flex;
    flex-direction: row;
    align-items: flex-start;
    gap: 10px;
    font-size: 12px;
    line-height: 1.5;
  }
  .installer-check input {
    width: 16px;
    height: 16px;
    min-height: 16px;
    accent-color: var(--accent);
    margin: 2px 0 0;
  }
  .installer-check small {
    margin-top: 4px;
  }
  .installer-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    border-top: 1px solid var(--line);
    padding-top: 22px;
    margin-top: 8px;
    font-size: 11px;
  }
  .installer-actions button,
  .installer-link {
    min-height: 42px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    font-size: 13px;
    padding: 10px 16px;
  }
  .installer-summary {
    display: grid;
    gap: 0;
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 0 16px;
  }
  .installer-summary > div {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 14px 0;
    font-size: 12px;
  }
  .installer-summary > div + div {
    border-top: 1px solid var(--line);
  }
  .installer-summary span {
    color: var(--muted);
    font-size: 11px;
  }
  .installer-summary strong {
    font-weight: 500;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .installer-progress {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 0;
    color: var(--accent);
    font-size: 13px;
  }
  .progress-dot {
    width: 8px;
    height: 8px;
    background: currentColor;
    border-radius: 50%;
    animation: pulse 1s ease-in-out infinite alternate;
  }
  @keyframes pulse {
    to {
      opacity: 0.35;
    }
  }
  .installer-error {
    padding: 12px 14px;
    border: 1px solid var(--danger);
    border-radius: 8px;
    font-size: 13px;
    line-height: 1.6;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  .installer-password {
    display: flex;
    gap: 8px;
  }
  .installer-password input {
    min-width: 0;
    flex: 1;
  }
  .installer-password button {
    min-width: 62px;
    font-size: 12px;
  }
  .installer-success {
    background: var(--selected);
    color: var(--accent);
    width: 56px;
    height: 56px;
    display: grid;
    place-items: center;
    border-radius: 18px;
  }
  .installer-address {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 14px;
    padding: 16px;
    background: var(--sidebar);
    border: 1px solid var(--border);
    border-radius: 10px;
  }
  .installer-address code {
    flex: 1;
  }
  details {
    border: 1px solid var(--border);
    border-radius: 9px;
    padding: 14px;
    font-size: 12px;
  }
  summary {
    font-weight: 550;
    cursor: pointer;
  }
  details p {
    color: var(--muted);
    line-height: 1.7;
    margin-top: 12px;
  }
  details button {
    display: inline-flex;
    gap: 8px;
    align-items: center;
    font-size: 12px;
    padding: 10px 12px;
    margin-top: 12px;
  }
  .installer-content > .muted {
    font-size: 12px;
    line-height: 1.7;
  }
  .installer-bottom {
    text-align: center;
    color: var(--faint);
    font-size: 11px;
    padding-top: 23px;
  }
  @media (hover: hover) and (pointer: fine) {
    .connection-option:hover {
      background: var(--hover);
    }
    .connection-option.chosen:hover {
      background: var(--selected);
    }
  }
  @media (max-width: 760px) {
    .installer {
      padding: 18px 14px;
    }
    .installer-top {
      margin-bottom: 20px;
    }
    .installer-shell {
      grid-template-columns: 1fr;
    }
    .installer-steps {
      padding: 16px 20px;
    }
    .installer-steps > .eyebrow,
    .installer-footnote,
    .installer-steps li > span:last-child {
      display: none;
    }
    .installer-steps ol {
      display: flex;
      justify-content: space-between;
      margin: 0;
      gap: 10px;
    }
    .installer-content {
      padding: 28px 22px 22px;
      min-height: 520px;
    }
    .installer-grid {
      grid-template-columns: 1fr;
    }
    .installer-grid > label:only-child {
      max-width: none;
    }
    .installer-actions {
      flex-wrap: wrap;
    }
    .option-icon {
      display: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .progress-dot {
      animation: none;
    }
  }
  .installer-checkbox {
    display: flex;
    flex-direction: row;
    align-items: flex-start;
    gap: 10px;
    margin: 16px 0;
    font-size: 14px;
    line-height: 1.6;
  }
  .installer-checkbox input {
    flex: 0 0 18px;
    width: 18px;
    min-height: 18px;
    height: 18px;
    padding: 0;
    margin-top: 3px;
  }
  @media (max-width: 640px) {
    input,
    select {
      font-size: 16px;
    }
  }
  .certificate-guide {
    margin-top: 24px;
  }
  .certificate-fingerprint {
    display: block;
    overflow-wrap: anywhere;
    user-select: all;
    padding: 12px;
    background: var(--raised);
    border-radius: 8px;
    font-size: 12px;
  }
</style>
