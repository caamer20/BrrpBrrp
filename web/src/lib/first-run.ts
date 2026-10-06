// The first-run checklist a new installation is walked through.
//
// It lives here rather than inside the Maintenance panel because the part worth
// checking is the *wording that follows from state*: whether an interval longer than
// twelve hours is called out, whether a fresh installation says "nobody else has
// joined" rather than "invite somebody" to an account that cannot invite, and whether
// the copy line says the one thing a new owner must read — a backup that never leaves
// this machine recovers a mistake, not a lost host.
//
// Nothing here is stored state of its own: every line comes from something the server
// already reports, and the panel is what fetches those reports. `action` names the
// place the line is fixed; the panel maps it to a control, so this module stays a value
// function with no browser in it.
import type { Settings } from './types';

export interface FirstRunMfa {
  enabled: boolean;
  required: boolean;
}

export interface FirstRunAlerts {
  configured: boolean;
  delivered: string;
  destination: string;
}

export interface FirstRunDiagnostics {
  https_configured: boolean;
  local_only: boolean;
  base_url: string;
  voice_ip: string;
  voice_port_min: number;
  voice_port_max: number;
  turn_enabled: boolean;
  copy_configured: boolean;
  backup: { recoverable_id?: string };
}

export interface FirstRunInputs {
  mfa: FirstRunMfa | null;
  ownerDesignated: boolean;
  /** Active accounts other than this one, so "nobody else has joined" is a count. */
  others: number;
  canInvite: boolean;
  canDesignate: boolean;
  diagnostics: FirstRunDiagnostics | null;
  settings: Settings | null;
  alerts: FirstRunAlerts | null;
}

export interface FirstRunLine {
  /** Stable key for the list, so a line that appears or disappears animates once. */
  id: string;
  done: boolean;
  title: string;
  detail: string;
  /** Where the line is fixed. The panel owns the actual destination. */
  action: 'account-security' | 'people' | 'invitations' | 'settings' | 'backups';
  /** The button's words, when the place can be reached from the panel. */
  fix?: string;
}

/** How many hours the recovery objective assumes between backups (W-36). */
const OBJECTIVE_HOURS = 12;

export function firstRunLines(input: FirstRunInputs): FirstRunLine[] {
  const lines: FirstRunLine[] = [];
  if (input.mfa) {
    lines.push({
      id: 'mfa',
      done: input.mfa.enabled,
      title: input.mfa.enabled
        ? 'Your account has a second factor'
        : 'Your account has no second factor',
      detail: input.mfa.enabled
        ? 'A privileged action such as exporting a full archive asks for a code as well as your password.'
        : input.mfa.required
          ? 'This installation asks an administrator for a code before a privileged action, so those are refused until one is enrolled.'
          : 'It is not required here. Enrolling one is what makes the export, the backup and the ownership ceremony safe to use from a browser that has been left signed in.',
      action: 'account-security',
      fix: 'Open account security'
    });
  }
  lines.push({
    id: 'owner',
    done: input.ownerDesignated,
    title: input.ownerDesignated ? 'An owner is designated' : 'No owner is designated',
    detail: input.ownerDesignated
      ? 'The owner holds the capabilities no role can delegate, and is who recovers this installation if everything else is lost.'
      : 'Until one is designated, an administrator can complete the ceremony. The owner is who can read a full archive, recover an account or hand the installation on.',
    action: 'people',
    fix: input.canDesignate ? 'Designate an owner' : 'Open People'
  });
  lines.push({
    id: 'members',
    done: input.others > 0,
    title:
      input.others > 0
        ? `${input.others} other ${input.others === 1 ? 'account is' : 'accounts are'} here`
        : 'Nobody else has joined yet',
    detail:
      input.others > 0
        ? input.canInvite
          ? 'Create an invitation for the next person; it is the only way in, because sign-up is closed.'
          : 'Only an account that can manage invitations can add anybody else.'
        : input.canInvite
          ? 'Sign-up is closed, so somebody joins only through an invitation you create. A community of one cannot tell you whether the server is working.'
          : 'Only an account that can manage invitations can add anybody else, so this is one to ask about.',
    action: 'invitations',
    fix: input.canInvite ? 'Open invitations' : undefined
  });
  if (input.diagnostics) {
    const https = input.diagnostics.https_configured && !input.diagnostics.local_only;
    lines.push({
      id: 'https',
      done: https,
      title: https
        ? 'An HTTPS address is configured'
        : input.diagnostics.local_only
          ? 'This server answers on its own machine only'
          : 'HTTPS needs attention',
      detail: `${input.diagnostics.base_url} — ${
        https
          ? 'open it on a second device and confirm there is no certificate warning.'
          : 'configure a public HTTPS address before inviting anybody: plain HTTP is not something to sign in over, and a secure cookie is refused on it.'
      }`,
      action: 'settings',
      fix: 'Open settings'
    });
    // The server can report the ports it will use and whether the relay that carries a
    // network which blocks UDP is on. Whether audio arrives is not a fact it has: only
    // two devices on the real networks can answer that, so the detail says so rather
    // than the line claiming a check nobody ran.
    lines.push({
      id: 'voice',
      done: input.diagnostics.turn_enabled,
      title: input.diagnostics.turn_enabled
        ? 'Voice ports and the relay are configured'
        : 'The relay that carries a blocked UDP network is off',
      detail: `Media address ${input.diagnostics.voice_ip || 'automatic'}, UDP ${input.diagnostics.voice_port_min}–${input.diagnostics.voice_port_max}. Open the address on two devices, join a voice channel and confirm two-way audio — nothing here proves that.`,
      action: 'settings',
      fix: 'Open settings'
    });
  }
  if (input.settings) {
    const hours = input.settings.backup_interval_hours || OBJECTIVE_HOURS;
    const keep = input.settings.backup_keep || 7;
    // Seven copies at twelve hours is three and a half days, so a whole number of days
    // is not always the truth. It is written the way the guides write it.
    const days = (hours * keep) / 24;
    const daysText = Number.isInteger(days) ? `${days}` : days.toFixed(1);
    const schedule = input.settings.backup_enabled
      ? `Backups run every ${hours} hour${hours === 1 ? '' : 's'}, and ${keep} of them are kept — about ${daysText} day${days === 1 ? '' : 's'} of history.`
      : 'Nothing is backed up on its own, so there is no recovery point at all beyond the live database.';
    const interval =
      input.settings.backup_enabled && hours > OBJECTIVE_HOURS
        ? ' That is longer than the twelve-hour cadence the recovery objective assumes, so a lost host loses more than a day.'
        : '';
    lines.push({
      id: 'schedule',
      done: input.settings.backup_enabled,
      title: input.settings.backup_enabled
        ? 'Scheduled backups are on'
        : 'Scheduled backups are off',
      detail: `${schedule}${interval}`,
      action: 'backups',
      fix: 'Backup schedule'
    });
  }
  if (input.diagnostics) {
    const copied = Boolean(input.diagnostics.backup.recoverable_id);
    lines.push({
      id: 'copy',
      done: input.diagnostics.copy_configured && copied,
      title: copied
        ? 'A backup has been copied off this machine'
        : input.diagnostics.copy_configured
          ? 'The copy destination is set but no copy has succeeded yet'
          : 'No backup copy destination is configured',
      // The one line a new owner must read. A backup that never leaves this machine
      // recovers a deleted message, not a lost host.
      detail: copied
        ? 'This is now a recovery point: the newest copy was verified against its archive and recorded independently of this host.'
        : 'A backup kept only on this machine is not recovery — it recovers a mistake, not a lost host. Set BRRP_BACKUP_COPY_DIR in the server’s own configuration (or run the Docker backup service) and let one copy succeed; the recovery check stays critical until it does.',
      action: 'backups'
    });
  }
  if (input.alerts) {
    const working = input.alerts.configured && input.alerts.delivered !== '';
    lines.push({
      id: 'alerts',
      done: working,
      title: input.alerts.configured
        ? input.alerts.delivered
          ? 'Alerts are configured and have been delivered'
          : 'An alert destination is configured but nothing has been delivered'
        : 'No alert destination is configured',
      detail: input.alerts.configured
        ? input.alerts.delivered
          ? `The last alert reached ${input.alerts.destination}. A destination that never delivers is worse than none, because it looks like coverage.`
          : `Nothing has reached ${input.alerts.destination} yet. Send one test alert and confirm it arrives before relying on this.`
        : 'Set BRRP_ALERT_WEBHOOK in the server’s own configuration so a failed backup or a critical recovery condition reaches you where you will see it.',
      action: 'settings'
    });
  }
  return lines;
}
