// The scaffolding every rehearsal shares.
//
// Three rehearsals now run the same path — install, write content through the API, stop
// the server, act on the data directory, start a second server on the result — and a
// helper that exists once is a helper that is fixed once. The teardown in particular:
// ten orphaned servers from aborted runs were still listening because a failed check
// threw straight past the stop calls, and that bug exists in one place now.
import { spawn, spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { createServer } from "node:net";

// Every rehearsal server is tracked so the process tears its children down on every
// exit path — a failed check throwing out of the run, a readiness loop throwing after
// spawn, a signal — not only on the path that reaches stopServer. Ten servers from
// aborted runs on 30 September and 1 October were still listening the next day
// because a failed check threw straight past the stop calls.
export const liveChildren = new Set();

export function trackChild(child) {
  liveChildren.add(child);
}

export function untrackChild(child) {
  liveChildren.delete(child);
}

export function killLiveChildren() {
  for (const child of liveChildren) {
    try {
      child.kill("SIGKILL");
    } catch {
      /* already exited */
    }
  }
  liveChildren.clear();
}

process.on("exit", killLiveChildren);
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, async () => {
    console.error(
      `Rehearsal interrupted (${signal}); stopping rehearsal servers.`,
    );
    // SIGTERM first so a server can close its database cleanly; the exit-handler
    // backstop and killLiveChildren cover one that refuses.
    for (const child of liveChildren) child.kill("SIGTERM");
    await new Promise((done) => setTimeout(done, 1500));
    killLiveChildren();
    process.exit(1);
  });
}

// The transcript sink. One rehearsal runs per process, and createRehearsal installs the
// one its checks write to, so the helpers below do not each need a logger argument.
let say = (line) => console.log(line);

export function useSay(write) {
  say = write;
}

export function run(binary, args) {
  say(`$ brrpbrrp ${args.join(" ")}`);
  const result = spawnSync(binary, args, { encoding: "utf8" });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}`.trim();
  if (output) say(output.replace(/^/gm, "  "));
  return result;
}

export function goToolchain() {
  const candidates = [process.env.BRRP_GO, "./.tools/go/bin/go", "go"].filter(
    Boolean,
  );
  for (const candidate of candidates) {
    if (candidate.includes("/") || candidate.includes("\\")) {
      if (existsSync(candidate)) return candidate;
      continue;
    }
    const probe = spawnSync(candidate, ["version"], { encoding: "utf8" });
    if (probe.status === 0) return candidate;
  }
  return null;
}

export function buildBinary(destination) {
  const go = goToolchain();
  if (!go)
    throw new Error("no Go toolchain found; set BRRP_GO or pass --binary");
  say(`$ ${go} build -o ${destination} ./cmd/brrpbrrp`);
  const result = spawnSync(go, ["build", "-o", destination, "./cmd/brrpbrrp"], {
    encoding: "utf8",
    env: { ...process.env, GOTOOLCHAIN: "local", CGO_ENABLED: "0" },
  });
  if (result.status !== 0) {
    process.stderr.write(result.stdout ?? "");
    process.stderr.write(result.stderr ?? "");
    if (result.error) process.stderr.write(`${result.error.message}\n`);
    throw new Error("build failed");
  }
}

export async function freePort() {
  return await new Promise((resolve, reject) => {
    const probe = createServer();
    probe.unref();
    probe.on("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(() => resolve(port));
    });
  });
}

export function serverEnv(dataDir, port, extra = {}) {
  return {
    BRRP_DATA_DIR: dataDir,
    BRRP_TLS_MODE: "http-local",
    BRRP_BASE_URL: `http://127.0.0.1:${port}`,
    BRRP_ADDR: `127.0.0.1:${port}`,
    ...extra,
  };
}

export async function startServer(
  binary,
  dataDir,
  port,
  logFile,
  extraEnv = {},
) {
  const env = serverEnv(dataDir, port, extraEnv);
  const log = [];
  const child = spawn(binary, ["serve", "--data-dir", dataDir], {
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  trackChild(child);
  // A spawn *error* (binary missing, fork failed) arrives asynchronously on 'error', long
  // after the process.on('exit') wiring above would have fired. Without a listener it's an
  // uncaught exception; with one it lands in the same log the rehearsal keeps next to the
  // ready wait, and the exit handlers still kill any tracked child that never scheduled.
  child.on("error", (error) => log.push(`spawn failed: ${error.message}\n`));
  child.stdout.on("data", (chunk) => log.push(chunk.toString()));
  child.stderr.on("data", (chunk) => log.push(chunk.toString()));
  const base = `http://127.0.0.1:${port}`;
  const server = { child, base, log };
  await ready(server, logFile);
  return server;
}

/**
 * Waits until a rehearsal server answers.
 *
 * It is exported because a rehearsal that has to restart a server mid-run — an upgrade, a
 * rollback, a second instance on a restored directory — waits for it explicitly, and the
 * wait has to be the same wait.
 */
export async function ready(server, logFile) {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    if (server.child.exitCode !== null) break;
    try {
      const response = await fetch(`${server.base}/api/setup/status`, {
        signal: AbortSignal.timeout(1000),
      });
      if (response.ok) return;
    } catch {
      /* not listening yet */
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(
    `server on ${server.base} did not become ready:\n${(server.log ?? []).join("") || logFile}`,
  );
}

export async function stopServer(server) {
  server.child.kill("SIGTERM");
  const exited = await Promise.race([
    new Promise((resolve) => server.child.once("exit", () => resolve(true))),
    new Promise((resolve) => setTimeout(() => resolve(false), 20000)),
  ]);
  if (!exited) server.child.kill("SIGKILL");
  untrackChild(server.child);
}

export async function api(
  base,
  method,
  route,
  { body, cookie, raw, contentType } = {},
) {
  const headers = { Origin: base };
  let payload;
  if (raw !== undefined) {
    payload = raw;
    headers["Content-Type"] = contentType;
  } else if (body !== undefined) {
    payload = JSON.stringify(body);
    headers["Content-Type"] = "application/json";
  }
  if (cookie) headers.Cookie = cookie;
  const response = await fetch(base + route, {
    method,
    headers,
    body: payload,
    redirect: "manual",
    signal: AbortSignal.timeout(30000),
  });
  const setCookie = response.headers.getSetCookie?.() ?? [];
  const bytes = Buffer.from(await response.arrayBuffer());
  let parsed = {};
  try {
    parsed = JSON.parse(bytes.toString() || "{}");
  } catch {
    parsed = { bytes: bytes.length };
  }
  return {
    status: response.status,
    body: parsed,
    bytes,
    cookie: setCookie[0]?.split(";")[0] ?? null,
  };
}

export function multipart(filename, content) {
  const boundary = `----brrp${Math.random().toString(16).slice(2)}${Date.now().toString(16)}`;
  const head = Buffer.from(
    `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${filename}"\r\n` +
      `Content-Type: text/plain\r\n\r\n`,
  );
  const tail = Buffer.from(`\r\n--${boundary}--\r\n`);
  return {
    body: Buffer.concat([head, Buffer.from(content), tail]),
    contentType: `multipart/form-data; boundary=${boundary}`,
  };
}

// Teardown is installed by importing this module: the process wiring above. A rehearsal
// that also called an installer would run the grace period twice per signal.
/**
 * Creates the run's transcript and its check function.
 *
 * `check` throws on failure, deliberately: a rehearsal that continues past a failed step
 * reports a cascade of consequences instead of the one cause, and the workspace is kept
 * for inspection either way.
 */
export function createRehearsal() {
  const checks = [];
  const transcript = [];
  const say = (line) => {
    transcript.push(line);
    console.log(line);
  };
  const check = (name, ok, detail) => {
    checks.push({ name, ok, detail });
    say(
      `  ${ok ? "ok  " : "FAIL"}  ${name}${detail ? ` \u2014 ${detail}` : ""}`,
    );
    if (!ok) throw new Error(`check failed: ${name}`);
  };
  useSay(say);
  return { checks, transcript, say, check };
}
