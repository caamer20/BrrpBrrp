// What an operator reads under "Server details" in Manage server → Maintenance. The server's
// metrics route already reports every figure here; until this existed the only way to see
// them was `curl` with a session cookie. This module turns that report into rows of words
// and has no browser in it, so the wording that matters — a figure that was never measured
// is not shown as zero, a platform that reports no process usage says so — is checked by a
// test rather than by reading a screen.
//
// Nothing in the report identifies a member or a conversation: the route labels are the
// patterns the server declares, never a path with an identifier in it.

export interface RouteSnapshot {
  route: string;
  total: number;
  in_flight: number;
  errors: number;
  sum_ms: number;
  max_ms: number;
  p95_bucket_ms: number;
}

export interface MetricsReport {
  version: string;
  uptime_seconds: number;
  // Absent from a server older than the client, which a cached client can briefly meet
  // during an update.
  about?: {
    schema: number;
    backup_format: number;
    protocol: number;
    protocol_min: number;
    platform: string;
    go: string;
    tls_mode: string;
  };
  routes: RouteSnapshot[];
  process: {
    reported: boolean;
    resident_bytes: number;
    cpu_seconds: number;
    goroutines: number;
    go_heap_bytes: number;
  };
  media: {
    voice_users: number;
    forwarded_packets: number;
    forwarded_bytes: number;
    dropped_packets: number;
  };
  connections: number;
  storage: {
    measured: boolean;
    age_seconds: number;
    database_bytes: number;
    uploads_bytes?: number;
    backups_bytes?: number;
    free_bytes?: number;
    free_disk_error?: string;
    measurement_error?: string;
  };
}

export interface Fact {
  label: string;
  value: string;
  /** A second line that says what the figure is, or why it is missing. */
  note?: string;
}

const NOT_MEASURED = 'Not measured yet';

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return NOT_MEASURED;
  if (value >= 1073741824) return `${(value / 1073741824).toFixed(1)} GB`;
  if (value >= 1048576) return `${(value / 1048576).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return NOT_MEASURED;
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days) return `${days}d ${hours}h`;
  if (hours) return `${hours}h ${minutes}m`;
  if (minutes) return `${minutes}m`;
  return `${Math.floor(seconds)}s`;
}

const TLS_MODES: Record<string, string> = {
  auto: 'HTTPS, with this installation’s own certificate authority',
  https: 'HTTPS, with a supplied or automatically issued certificate',
  'http-behind-proxy': 'Behind a proxy that terminates TLS',
  'http-local': 'Plain HTTP, this machine only'
};

export function aboutFacts(report: MetricsReport): Fact[] {
  const facts: Fact[] = [
    { label: 'Version', value: report.version },
    { label: 'Running for', value: formatDuration(report.uptime_seconds) }
  ];
  const about = report.about;
  if (!about) return facts;
  facts.push(
    {
      label: 'Database schema',
      value: String(about.schema),
      note: 'A backup restores onto a build with this schema or a newer one.'
    },
    { label: 'Backup format', value: String(about.backup_format) },
    {
      label: 'Realtime protocol',
      value:
        about.protocol_min === about.protocol
          ? String(about.protocol)
          : `${about.protocol_min}–${about.protocol}`,
      note: 'An open tab older than this range is asked to reload.'
    },
    { label: 'Address mode', value: TLS_MODES[about.tls_mode] ?? about.tls_mode },
    { label: 'Platform', value: `${about.platform}, ${about.go}` }
  );
  return facts;
}

export function processFacts(report: MetricsReport): Fact[] {
  const process = report.process;
  const unreported = 'Not reported on this platform';
  return [
    {
      label: 'Memory in use',
      value: process.reported ? formatBytes(process.resident_bytes) : unreported,
      note: 'The whole process, as the operating system counts it.'
    },
    { label: 'Go heap', value: formatBytes(process.go_heap_bytes) },
    {
      label: 'Processor time',
      value: process.reported ? formatDuration(process.cpu_seconds) : unreported,
      note: 'Since the server started.'
    },
    { label: 'Connections', value: String(report.connections) },
    { label: 'Background tasks', value: String(process.goroutines) }
  ];
}

export function mediaFacts(report: MetricsReport): Fact[] {
  const media = report.media;
  return [
    { label: 'In voice now', value: String(media.voice_users) },
    {
      label: 'Media forwarded',
      value: media.forwarded_packets
        ? `${formatBytes(media.forwarded_bytes)} in ${media.forwarded_packets.toLocaleString('en')} packets`
        : 'Nothing yet',
      note: 'Voice and screen sharing, since the server started.'
    },
    {
      // The server counts a packet here when it refuses to pass it on because its sender is
      // muted, deafened or listen-only. It is the rule being enforced, not a network fault,
      // and it is worded that way so a large number is not read as loss.
      label: 'Media withheld',
      value: media.dropped_packets
        ? `${media.dropped_packets.toLocaleString('en')} packets`
        : 'None',
      note: 'Not passed on because the sender was muted, deafened or listen-only. This is not packet loss.'
    }
  ];
}

export function storageFacts(report: MetricsReport): Fact[] {
  const storage = report.storage;
  const age = storage.measured ? `Measured ${formatDuration(storage.age_seconds)} ago.` : undefined;
  const facts: Fact[] = [
    { label: 'Database', value: formatBytes(storage.database_bytes) },
    {
      label: 'Uploads',
      value: storage.measured ? formatBytes(storage.uploads_bytes ?? -1) : NOT_MEASURED,
      note: age
    },
    {
      label: 'Backups on this machine',
      value: storage.measured ? formatBytes(storage.backups_bytes ?? -1) : NOT_MEASURED,
      note: age
    },
    {
      label: 'Free disk',
      value:
        storage.free_bytes === undefined ? 'Could not be read' : formatBytes(storage.free_bytes),
      note: storage.free_disk_error
    }
  ];
  if (storage.measurement_error)
    facts.push({ label: 'Measurement problem', value: storage.measurement_error });
  return facts;
}

/** The realtime socket stays open for a whole session, so its "latency" is a visit's length. */
function isSocket(route: RouteSnapshot): boolean {
  return /(^|\s)\/ws$/.test(route.route);
}

/**
 * The routes that have answered slowest, by the bucket their 95th percentile fell in.
 * Routes that have not been asked are left out: a zero is not a measurement.
 */
export function slowestRoutes(routes: RouteSnapshot[], limit = 5): RouteSnapshot[] {
  return routes
    .filter((route) => route.total > 0 && !isSocket(route))
    .sort(
      (a, b) =>
        b.p95_bucket_ms - a.p95_bucket_ms ||
        b.max_ms - a.max_ms ||
        a.route.localeCompare(b.route, 'en')
    )
    .slice(0, limit);
}

export function routeLine(route: RouteSnapshot): string {
  const requests = `${route.total.toLocaleString('en')} request${route.total === 1 ? '' : 's'}`;
  // The server counts every 4xx and 5xx answer together: a refusal — signed out, not
  // permitted, throttled — as well as a failure of its own. The wording says both.
  const errors = route.errors ? `, ${route.errors.toLocaleString('en')} refused or failed` : '';
  return `95% within ${route.p95_bucket_ms.toLocaleString('en')} ms, slowest ${route.max_ms.toLocaleString('en')} ms — ${requests}${errors}`;
}

/** Requests answered with a refusal or a failure, across every route. */
export function refusedOrFailed(routes: RouteSnapshot[]): number {
  return routes.reduce((sum, route) => sum + route.errors, 0);
}
