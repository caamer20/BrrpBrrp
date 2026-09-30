<script lang="ts">
  import { onDestroy } from 'svelte';
  import { api, errorMessage } from '../../api';
  import Icon from '../Icon.svelte';

  // The answer this panel exists to give is "are my alerts working", not "are my alerts
  // configured". A webhook that has never delivered looks exactly like coverage, and the
  // operator finds that out during a restore — so the first thing on screen is when
  // something last arrived, not whether a URL is set (ADR: WP-28).
  interface AlertRecord {
    at: string;
    kind: string;
    severity: string;
    summary: string;
    delivered: string;
    attempts: number;
    last_error: string;
  }
  interface AlertCondition {
    kind: string;
    severity: string;
    active: boolean;
    summary: string;
  }
  interface AlertStatus {
    configured: boolean;
    delivered: string;
    destination: string;
    history: AlertRecord[];
    conditions: AlertCondition[];
  }

  let status = $state<AlertStatus | null>(null);
  let loading = $state(true);
  let error = $state('');
  let alive = true;
  onDestroy(() => {
    alive = false;
  });

  $effect(() => {
    void load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      const result = await api<AlertStatus>('/admin/alerts');
      if (alive) status = result;
    } catch (failure) {
      if (alive) error = errorMessage(failure);
    } finally {
      if (alive) loading = false;
    }
  }

  const label: Record<string, string> = {
    backup_stale: 'Backups are behind',
    backup_failed: 'A backup attempt failed',
    copy_stale: 'The independent copy is behind',
    storage_low: 'Low disk space'
  };

  function when(value: string) {
    return value ? new Date(value).toLocaleString() : 'never';
  }

  const neverDelivered = $derived(!!status?.configured && status.delivered === '' && !loading);
</script>

<div class="section-heading">
  <div>
    <h3>Alerts</h3>
    <p>
      Problems with this server’s recovery that an operator has to act on. Set
      <code>BRRP_ALERT_WEBHOOK</code> and restart to have them delivered.
    </p>
  </div>
  <button class="secondary" onclick={() => void load()} disabled={loading}>Refresh</button>
</div>

{#if error}<p class="error-banner" role="alert">{error}</p>{/if}

{#if loading}
  <p role="status">Checking alert health…</p>
{:else if status}
  {#if neverDelivered}
    <!-- The failure this panel is for. Silently showing a configured URL as if it were
         working is how an operator discovers the gap during a restore. -->
    <p class="alert-warning" role="alert">
      No alert has reached {status.destination} yet. The destination is configured but nothing has been
      delivered — a webhook that has never worked looks exactly like one that is working until the moment
      you need it.
    </p>
  {:else if status.configured}
    <p class="muted">Last delivered to {status.destination}: {when(status.delivered)}.</p>
  {:else}
    <p class="muted">
      No destination is configured, so nothing is being delivered. The conditions below are still
      readable — a server that only reports a problem when a webhook is set up cannot be used to
      find out that the webhook is what is broken.
    </p>
  {/if}

  <h4>Right now</h4>
  {#if !status.conditions.length}
    <p class="muted">Nothing needs attention.</p>
  {:else}
    <ul class="condition-list">
      {#each status.conditions as condition (condition.kind)}
        <li class:critical={condition.severity === 'critical'}>
          <strong>{label[condition.kind] ?? condition.kind}</strong>
          <span>{condition.summary}</span>
        </li>
      {/each}
    </ul>
  {/if}

  <h4>Recent alerts</h4>
  {#if !status.history.length}
    <p class="muted">No alerts have been raised.</p>
  {:else}
    <ul class="alert-list">
      {#each status.history as alert (alert.at + alert.kind)}
        <li>
          <div class="alert-row">
            <strong>{label[alert.kind] ?? alert.kind}</strong>
            <span class="muted">{when(alert.at)}</span>
          </div>
          <p>{alert.summary}</p>
          {#if alert.last_error}
            <!-- The delivery result is stated, not inferred from silence. -->
            <p class="alert-delivery-failed">
              Not delivered{alert.attempts > 1 ? ` after ${alert.attempts} attempts` : ''}:
              {alert.last_error}
            </p>
          {:else if alert.delivered}
            <p class="muted">Delivered {when(alert.delivered)}.</p>
          {:else if !status.configured}
            <p class="muted">No destination is configured, so this was not sent anywhere.</p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
{/if}

<style>
  .alert-warning {
    color: var(--danger);
    border: 1px solid var(--danger);
    border-radius: 6px;
    padding: 0.5rem 0.6rem;
  }
  .condition-list,
  .alert-list {
    list-style: none;
    margin: 0.4rem 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .condition-list li {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    border-left: 3px solid var(--border);
    padding-left: 0.5rem;
  }
  /* A critical condition is the recovery path being unavailable. It is marked as
     critical even when the alert about it could not be delivered, because downgrading
     it would hide the problem behind the alerting failure. */
  .condition-list li.critical {
    border-left-color: var(--danger);
  }
  .alert-list li {
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.45rem 0.6rem;
  }
  .alert-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .alert-list p {
    margin: 0.2rem 0 0;
    font-size: 0.9rem;
  }
  .alert-delivery-failed {
    color: var(--danger);
  }
</style>
