// Every variable this harness understands, and what the Playwright child receives when the
// caller did not set it. Each BRRP_ variable is stripped from both children so the server's
// own configuration cannot leak into a run; the ones named here are then handed to the
// Playwright child by name. The list is explicit because the alternative was a fixed set
// written out by hand, and the set drifted from the config: BRRP_E2E_BROWSER (the browser
// channel) and BRRP_E2E_GLOBAL_TIMEOUT were read by playwright.config.mjs and silently
// dropped here, so a run "with Chrome stable" ran the bundled Chromium and said nothing
// (review finding F-08, W-08).
const FORWARDED = {
  BRRP_E2E_PRIVATE_CONVERSATIONS: '0',
  // The engine projects exist only for their flag. The WebKit pass cannot share an
  // invocation with the Chromium projects, because one installation serves one pass.
  BRRP_E2E_FIREFOX: '0',
  BRRP_E2E_WEBKIT: '0',
  // The long-thread scale project seeds a thousand messages and takes about ten minutes.
  BRRP_E2E_SCALE: '0',
  BRRP_E2E_BROWSER: undefined,
  BRRP_E2E_GLOBAL_TIMEOUT: undefined
};
// Read by this file or written by it for the child; never a caller's switch for the child.
const OWN = new Set(['BRRP_E2E_BINARY', 'BRRP_E2E_BASE_URL', 'BRRP_E2E_DATA_DIR']);

export function harnessEnvironment(environment) {
  const unknown = Object.keys(environment).filter(
    (key) => key.startsWith('BRRP_E2E_') && !(key in FORWARDED) && !OWN.has(key)
  );
  // A misspelt switch must not read as a run in the mode it names.
  if (unknown.length)
    throw new Error(
      `Unknown browser-harness variable(s): ${unknown.join(', ')}. Known: ${[
        ...Object.keys(FORWARDED),
        ...OWN
      ]
        .sort()
        .join(', ')}.`
    );
  const forwarded = {};
  for (const [key, fallback] of Object.entries(FORWARDED)) {
    const value = environment[key] || fallback;
    if (value !== undefined) forwarded[key] = value;
  }
  return forwarded;
}

// One line that says which browser a run actually used, so a log proves the engine and
// channel rather than the variable somebody hoped was honoured.
export function describeEngines(forwarded) {
  const engines = [
    `chromium (${forwarded.BRRP_E2E_BROWSER ? `channel ${forwarded.BRRP_E2E_BROWSER}` : 'bundled'})`
  ];
  if (forwarded.BRRP_E2E_FIREFOX === '1') engines.push('firefox (voice project available)');
  if (forwarded.BRRP_E2E_WEBKIT === '1') engines.push('webkit (release subset available)');
  if (forwarded.BRRP_E2E_SCALE === '1') engines.push('thread-scale project available');
  return engines.join('; ');
}
