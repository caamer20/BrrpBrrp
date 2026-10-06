// The first-run checklist is the only place a new owner is told what a fresh
// installation is still missing, so the two things worth pinning are the *wording that
// follows from state* — an interval longer than the objective is called out, a fresh
// install does not tell an account without invite authority to invite somebody — and
// that the copy line says what it must: a backup kept on this machine alone is not
// recovery.
import assert from 'node:assert/strict';
import test from 'node:test';
import { firstRunLines } from '../src/lib/first-run.ts';

/** The state a brand-new installation reports: nothing configured, nobody else here. */
function fresh(overrides = {}) {
  return {
    mfa: { enabled: false, required: false },
    ownerDesignated: false,
    others: 0,
    canInvite: true,
    canDesignate: true,
    diagnostics: {
      https_configured: false,
      local_only: true,
      base_url: 'https://localhost:8082',
      voice_ip: '',
      voice_port_min: 50000,
      voice_port_max: 50100,
      turn_enabled: false,
      copy_configured: false,
      backup: {}
    },
    settings: {
      backup_enabled: false,
      backup_interval_hours: 12,
      backup_keep: 7
    },
    alerts: { configured: false, delivered: '', destination: '' },
    ...overrides
  };
}

const line = (lines, id) => lines.find((entry) => entry.id === id);

test('a fresh installation is told what is missing, and where each thing is fixed', () => {
  const lines = firstRunLines(fresh());
  assert.deepEqual(
    lines.map((entry) => entry.id),
    ['mfa', 'owner', 'members', 'https', 'voice', 'schedule', 'copy', 'alerts']
  );
  assert.equal(line(lines, 'owner').done, false);
  assert.equal(line(lines, 'owner').title, 'No owner is designated');
  assert.equal(line(lines, 'members').title, 'Nobody else has joined yet');
  assert.equal(line(lines, 'https').title, 'This server answers on its own machine only');
  assert.equal(line(lines, 'schedule').title, 'Scheduled backups are off');
  assert.equal(line(lines, 'copy').done, false);
  assert.equal(line(lines, 'alerts').title, 'No alert destination is configured');
  for (const entry of lines) {
    assert.ok(entry.detail.length > 30, `${entry.id} has no explanation`);
    assert.ok(entry.action, `${entry.id} does not say where it is fixed`);
  }
});

test('the copy line says a backup on this machine alone is not recovery', () => {
  const lines = firstRunLines(fresh());
  const copy = line(lines, 'copy');
  assert.equal(copy.title, 'No backup copy destination is configured');
  assert.match(copy.detail, /not recovery/);
  assert.match(copy.detail, /BRRP_BACKUP_COPY_DIR/);
  assert.equal(copy.fix, undefined, 'the copy destination is host configuration, not a button');
});

test('an interval longer than the objective is called out, twelve hours is not', () => {
  const long = line(
    firstRunLines(
      fresh({ settings: { backup_enabled: true, backup_interval_hours: 24, backup_keep: 7 } })
    ),
    'schedule'
  );
  assert.equal(long.done, true);
  assert.match(long.detail, /every 24 hours/);
  assert.match(long.detail, /longer than the twelve-hour cadence/);
  const objective = line(
    firstRunLines(
      fresh({ settings: { backup_enabled: true, backup_interval_hours: 12, backup_keep: 7 } })
    ),
    'schedule'
  );
  assert.doesNotMatch(objective.detail, /longer than/);
  // Seven copies at twelve hours is three and a half days, written as the guides write
  // it rather than rounded up to four.
  assert.match(objective.detail, /about 3\.5 days of history/);
});

test('an account that cannot invite is not told to invite anybody', () => {
  const lines = firstRunLines(fresh({ canInvite: false }));
  const members = line(lines, 'members');
  assert.match(members.detail, /manage invitations/);
  assert.equal(members.fix, undefined, 'a button would open a tab this account cannot use');
});

test('the settled installation reads as done, and the alert line names its destination', () => {
  const lines = firstRunLines(
    fresh({
      mfa: { enabled: true, required: true },
      ownerDesignated: true,
      others: 12,
      diagnostics: {
        https_configured: true,
        local_only: false,
        base_url: 'https://community.example',
        voice_ip: '203.0.113.9',
        voice_port_min: 50000,
        voice_port_max: 50100,
        turn_enabled: true,
        copy_configured: true,
        backup: { recoverable_id: 'archive-7' }
      },
      settings: { backup_enabled: true, backup_interval_hours: 12, backup_keep: 7 },
      alerts: { configured: true, delivered: '2026-10-06T09:00:00Z', destination: 'alerts.example' }
    })
  );
  for (const entry of lines) {
    assert.equal(entry.done, true, `${entry.id} is still unfinished on a settled server`);
  }
  assert.equal(line(lines, 'members').title, '12 other accounts are here');
  assert.match(line(lines, 'copy').detail, /recovery point/);
  assert.match(line(lines, 'alerts').detail, /alerts\.example/);
});

test('a configured destination that has never delivered is not reported as working', () => {
  const lines = firstRunLines(
    fresh({ alerts: { configured: true, delivered: '', destination: 'alerts.example' } })
  );
  const alerts = line(lines, 'alerts');
  assert.equal(alerts.done, false);
  assert.equal(alerts.title, 'An alert destination is configured but nothing has been delivered');
  assert.match(alerts.detail, /Nothing has reached alerts\.example/);
});
