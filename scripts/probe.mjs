// An external probe for a deployment this repository does not control.
//
// The rehearsals and the browser suite run against a server the harness started on
// localhost, which is the one deployment shape where nothing can be wrong at the edges. A
// real installation sits behind a reverse proxy, on a hostname with TLS, possibly in a
// container with a port mapping — and the failures that live there are invisible to every
// check in the repository: a proxy that answers the frontend but not the websocket
// upgrade, an asset that 404s because a cache is stale, a certificate that expired, a
// websocket that upgrades and never speaks.
//
// So this probe asks the deployment from outside, over the network, the way a member's
// browser would: the setup status, the shell, one of the assets the shell names, the
// security headers, an authenticated endpoint and the metrics counters when a session
// cookie is supplied, and the websocket handshake. It prints what it found and exits
// non-zero on any failure, so it can run from a cron job or an operator's laptop.
//
//   node scripts/probe.mjs --base https://chat.example.com [--cookie 'brrp_session=…']
//
// The websocket check without a cookie is not skipped: an unauthenticated upgrade must be
// *refused cleanly* — an HTTP response, not a hang — because a proxy that swallows the
// upgrade is the ordinary deployment failure, and from the outside it looks like nothing
// at all.
import { connect as netConnect } from "node:net";
import { connect as tlsConnect } from "node:tls";
import { performance } from "node:perf_hooks";
import { pathToFileURL } from "node:url";

const DEFAULT_TIMEOUT = 8000;

/** The checks, in the order a member's browser makes them. */
export async function runProbe({
  base,
  cookie = "",
  timeout = DEFAULT_TIMEOUT,
} = {}) {
  if (!base)
    throw new Error(
      "a base URL is required, for example https://chat.example.com",
    );
  const root = base.replace(/\/+$/, "");
  const results = [];
  const check = async (name, run) => {
    const started = performance.now();
    try {
      const detail = await run();
      results.push({
        name,
        ok: true,
        ms: Math.round(performance.now() - started),
        detail,
      });
    } catch (error) {
      results.push({
        name,
        ok: false,
        ms: Math.round(performance.now() - started),
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const get = (path, headers = {}) =>
    fetch(root + path, {
      headers,
      redirect: "manual",
      signal: AbortSignal.timeout(timeout),
    });

  let shellBody = "";
  await check("setup status", async () => {
    const response = await get("/api/setup/status");
    if (!response.ok)
      throw new Error(`GET /api/setup/status answered ${response.status}`);
    const body = await response.json().catch(() => null);
    if (!body || typeof body !== "object")
      throw new Error("GET /api/setup/status did not answer JSON");
    return `setup_required=${body.setup_required === true}`;
  });

  await check("frontend shell", async () => {
    const response = await get("/");
    if (!response.ok) throw new Error(`GET / answered ${response.status}`);
    shellBody = await response.text();
    if (!/<html/i.test(shellBody)) throw new Error("GET / did not answer HTML");
    return `${shellBody.length} bytes`;
  });

  await check("frontend asset", async () => {
    const match = /(?:src|href)="(\/assets\/[^"]+)"/.exec(shellBody);
    if (!match) throw new Error("the shell names no /assets/ bundle to fetch");
    const response = await get(match[1]);
    if (!response.ok)
      throw new Error(`GET ${match[1]} answered ${response.status}`);
    const type = response.headers.get("content-type") || "";
    if (!type.includes("javascript") && !type.includes("css"))
      throw new Error(`GET ${match[1]} answered ${type || "no content type"}`);
    return match[1];
  });

  await check("security headers", async () => {
    const response = await get("/");
    const csp = response.headers.get("content-security-policy") || "";
    if (!/default-src/.test(csp))
      throw new Error(
        csp
          ? `the CSP is not a policy: ${csp}`
          : "served without a Content-Security-Policy",
      );
    if (!response.headers.get("x-content-type-options"))
      throw new Error("no X-Content-Type-Options");
    return "CSP and nosniff present";
  });

  if (cookie) {
    await check("authenticated session", async () => {
      const response = await get("/api/me", { cookie });
      if (response.status === 401)
        throw new Error(
          "the session cookie was not accepted: sign in again and copy a fresh one",
        );
      if (!response.ok)
        throw new Error(`GET /api/me answered ${response.status}`);
      return "signed in";
    });
    await check("operator metrics", async () => {
      const response = await get("/api/admin/metrics", { cookie });
      if (response.status === 403)
        throw new Error(
          "this account is not an operator, so /api/admin/metrics is not readable",
        );
      if (!response.ok)
        throw new Error(`GET /api/admin/metrics answered ${response.status}`);
      const body = await response.json().catch(() => null);
      if (!body || !body.routes)
        throw new Error("/api/admin/metrics answered no route counters");
      return `${body.routes.length} routes measured, uptime ${body.uptime_seconds}s`;
    });
  }

  await check("websocket handshake", async () => {
    const url = root.replace(/^http/, "ws") + "/ws";
    const result = await handshake(url, cookie, timeout);
    // An upgrade that produced no frame is a *different* fault from an upgrade nobody
    // answered: the first is the application not speaking, the second is a proxy eating
    // the request, and an operator fixes them in different places.
    if (result.timedOut)
      throw new Error(
        result.status === 101
          ? `upgraded but silent: no frame within ${timeout} ms`
          : `no answer to the upgrade within ${timeout} ms`,
      );
    if (result.error) throw new Error(`the connection failed: ${result.error}`);
    if (cookie) {
      if (result.status !== 101)
        throw new Error(`the upgrade was answered ${result.status}, not 101`);
      if (result.frame && typeof result.frame.t === "string")
        return `upgraded, first frame ${result.frame.t}`;
      throw new Error(`upgraded but silent: no frame within ${timeout} ms`);
    }
    if (result.status === 101) return "upgraded without a session";
    return `refused cleanly with ${result.status}`;
  });

  return results;
}

/**
 * Opens a websocket and reports how the server answered, without depending on a client
 * library: what matters is the handshake status and whether a frame ever arrives. A server
 * that accepts the upgrade and never speaks is the failure this catches — a connection
 * that is open and useless looks identical to a healthy one from the outside.
 */
export function handshake(url, cookie, timeout) {
  return new Promise((resolve) => {
    let settled = false;
    let socket;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      try {
        socket?.destroy();
      } catch {
        /* Already closing. */
      }
      resolve(value);
    };
    // The status is remembered so a timeout can be reported as the fault it is: nothing
    // answered at all, or the server upgraded and then said nothing.
    let upgraded = false;
    const timer = setTimeout(
      () => finish({ status: upgraded ? 101 : 0, frame: null, timedOut: true }),
      timeout + 1000,
    );
    let target;
    try {
      target = new URL(url);
    } catch (error) {
      finish({ status: 0, frame: null, error: String(error) });
      return;
    }
    const key = Buffer.from(`${Date.now()}-${Math.random()}`)
      .toString("base64")
      .replace(/=+$/, "");
    const request =
      `GET ${target.pathname || "/ws"} HTTP/1.1\r\n` +
      `Host: ${target.host}\r\n` +
      "Upgrade: websocket\r\n" +
      "Connection: Upgrade\r\n" +
      `Sec-WebSocket-Key: ${key}\r\n` +
      "Sec-WebSocket-Version: 13\r\n" +
      (cookie ? `Cookie: ${cookie}\r\n` : "") +
      "\r\n";
    const onConnect = () => socket.write(request);
    const onData = (chunk) => {
      buffer += chunk.toString("binary");
      const status = /^HTTP\/1\.[01] (\d{3})/.exec(buffer);
      if (!status) return;
      if (Number(status[1]) !== 101) {
        finish({ status: Number(status[1]), frame: null });
        return;
      }
      upgraded = true;
      const emptyLine = buffer.indexOf("\r\n\r\n");
      if (emptyLine === -1) return;
      const frame = decodeTextFrame(buffer.slice(emptyLine + 4));
      if (frame !== undefined) finish({ status: 101, frame: parseJSON(frame) });
    };
    let buffer = "";
    try {
      if (target.protocol === "wss:") {
        socket = tlsConnect(
          {
            host: target.hostname,
            port: Number(target.port || 443),
            servername: target.hostname,
          },
          onConnect,
        );
      } else {
        socket = netConnect(
          { host: target.hostname, port: Number(target.port || 80) },
          onConnect,
        );
      }
      socket.on("data", onData);
      socket.on("error", (error) =>
        finish({ status: 0, frame: null, error: error.message }),
      );
      socket.on("close", () =>
        finish({ status: 0, frame: null, closed: true }),
      );
    } catch (error) {
      finish({ status: 0, frame: null, error: String(error) });
    }
  });
}

/** A server frame with no mask: the shape a first `hello` arrives in. */
function decodeTextFrame(buffer) {
  if (buffer.length < 2) return undefined;
  if ((buffer.charCodeAt(0) & 0x0f) !== 1) return undefined;
  const length = buffer.charCodeAt(1) & 0x7f;
  if (length > 125) return undefined; // Anything longer than a hello needs extended length.
  if (buffer.length < 2 + length) return undefined;
  return buffer.slice(2, 2 + length);
}

function parseJSON(text) {
  try {
    return JSON.parse(text);
  } catch {
    return { t: text };
  }
}

export function formatLine(result) {
  const status = result.ok ? "ok  " : "FAIL";
  return `${status} ${result.name.padEnd(22)} ${String(result.ms).padStart(5)} ms  ${result.detail}`;
}

export async function main(argv) {
  const args = { base: "", cookie: "", json: false, timeout: DEFAULT_TIMEOUT };
  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index];
    if (arg === "--base") args.base = argv[++index] || "";
    else if (arg === "--cookie") args.cookie = argv[++index] || "";
    else if (arg === "--timeout")
      args.timeout = Number(argv[++index]) || DEFAULT_TIMEOUT;
    else if (arg === "--json") args.json = true;
    else if (arg === "--help" || arg === "-h") {
      console.log(
        'usage: node scripts/probe.mjs --base https://host [--cookie "name=value"] [--timeout ms] [--json]',
      );
      return 0;
    } else if (arg) {
      console.error(`unknown argument: ${arg}`);
      return 2;
    }
  }
  if (!args.base) {
    console.error(
      'usage: node scripts/probe.mjs --base https://host [--cookie "name=value"]',
    );
    return 2;
  }
  const results = await runProbe(args);
  if (args.json) {
    console.log(JSON.stringify({ base: args.base, results }, null, 2));
  } else {
    console.log(`probing ${args.base}`);
    for (const result of results) console.log(formatLine(result));
  }
  return results.every((result) => result.ok) ? 0 : 1;
}

const invoked = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invoked) {
  main(process.argv.slice(2)).then((code) => process.exit(code));
}
