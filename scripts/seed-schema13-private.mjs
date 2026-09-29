// Seeding a real schema-13 installation with private state, using an actual
// archived P9 executable.
//
// The P10 checkpoint rehearsed alpha.6 (schema 12) to schema 14, which skips
// migration 13 and therefore never exercised the schema-13 data that migration 14
// has to be careful with: a `direct_requests` table with no `attempt_id` column, a
// `private_message_reports` table whose rows must survive untouched, and a pending
// attempt that the backfill must give a distinct identity.
//
// Nothing here is a row inserted into a database. Every record is created by the
// old binary through the same API a person would use, so the upgrade that follows
// is exercised against data the old code actually wrote. The identities printed at
// the end are what `upgrade-schema13-private.mjs` compares against after migrating.
//
//   node scripts/seed-schema13-private.mjs --base URL --dir PATH
//
// Exits nonzero if any step did not produce the state it claims.

const args = process.argv.slice(2);
const option = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index < 0 ? fallback : args[index + 1];
};
const base = option('--base', process.env.SEED_BASE);
const dir = option('--dir', process.env.SEED_DIR);
if (!base || !dir) {
  console.error('usage: --base URL --dir PATH (the archived binary must already be serving --dir)');
  process.exit(2);
}

const { readFileSync } = await import('node:fs');
const token = readFileSync(`${dir}/setup-token`, 'utf8').trim();

let failures = 0;
const call = async (method, path, body, cookie) => {
  const response = await fetch(base + path, {
    method,
    headers: { 'content-type': 'application/json', ...(cookie ? { cookie } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const jar = response.headers
    .getSetCookie()
    .map((entry) => entry.split(';')[0])
    .join('; ');
  return { status: response.status, jar, body: await response.json().catch(() => null) };
};
const post = (path, body, cookie) => call('POST', path, body, cookie);

const ok = (label, result, ...allowed) => {
  if (!allowed.includes(result.status)) {
    console.log(`  FAIL ${label} — HTTP ${result.status} ${JSON.stringify(result.body ?? {})}`);
    failures += 1;
  } else {
    console.log(`  ok   ${label} — HTTP ${result.status}`);
  }
  return result;
};

console.log('1. Install and seed private state with the archived schema-13 executable');
const setup = await post('/api/setup', {
  token,
  server_name: 'Schema 13 seed',
  username: 'owner_s13',
  password: 'a schema thirteen passphrase'
});
ok('first-time setup', setup, 201);
const owner = setup.jar;

const ann = ok('member ann', await post('/api/admin/users', { username: 'ann_s13', password: 'an ann passphrase 12', display_name: 'Ann' }, owner), 201);
const ben = ok('member ben', await post('/api/admin/users', { username: 'ben_s13', password: 'a ben passphrase 12x', display_name: 'Ben' }, owner), 201);
const carol = ok('member carol', await post('/api/admin/users', { username: 'carol_s13', password: 'a carol passphrase 1', display_name: 'Carol' }, owner), 201);
const dave = ok('member dave', await post('/api/admin/users', { username: 'dave_s13', password: 'a dave passphrase 12', display_name: 'Dave' }, owner), 201);

const annJar = ok('ann signs in', await post('/api/login', { username: 'ann_s13', password: 'an ann passphrase 12' }), 200).jar;
const benJar = ok('ben signs in', await post('/api/login', { username: 'ben_s13', password: 'a ben passphrase 12x' }), 200).jar;
const daveJar = ok('dave signs in', await post('/api/login', { username: 'dave_s13', password: 'a dave passphrase 12' }), 200).jar;

// An accepted conversation with a message, so there is private history and
// evidence that has to survive the migration byte for byte.
const asked = ok('ann asks ben', await post('/api/directs', { user_id: ben.body.id, introduction: 'schema thirteen hello' }, annJar), 201);
const conversation = asked.body.id;
ok('ben accepts', await post(`/api/directs/${conversation}/accept`, {}, benJar), 200, 409);
const sent = ok('a private message is sent', await post(`/api/conversations/${conversation}/messages`, { content: 'a schema thirteen private message', idempotency_key: 's13-seed-message' }, annJar), 201);
const messageID = sent.body.id;

const preview = ok('the message is previewable for evidence', await call('GET', `/api/messages/${messageID}/private-report-preview`, undefined, annJar), 200);
const reported = ok('private evidence is submitted', await post(`/api/messages/${messageID}/private-report`, { reason: 'schema thirteen rehearsal evidence', version: preview.body.version }, annJar), 201, 200);
const evidenceID = reported.body?.id ?? '';
console.log(`  evidence: ${evidenceID}`);

// A pending request with an introduction. This is the row migration 14 has to
// give an attempt identity, and it must be distinct from the accepted pair's.
const pending = ok('dave sends carol a pending request', await post('/api/directs', { user_id: carol.body.id, introduction: 'a pending schema thirteen introduction' }, daveJar), 201);
console.log(`  accepted conversation: ${conversation}`);
console.log(`  pending conversation: ${pending.body.id}`);
console.log(`  message: ${messageID}`);

if (failures > 0) {
  console.log(`\nSeed FAILED: ${failures} step(s) did not produce the claimed state.`);
  process.exit(1);
}
console.log('\nSeed complete. The schema-13 executables under dist/ must report 13 applied migrations before this is used.');
