import { expect, test } from '@playwright/test';
import {
  assignOnly,
  captureGeneratedAudio,
  createAccount,
  createRole,
  OWNER,
  setRolePermissions,
  signIn,
  signInOverApi
} from './support.mjs';

// R2-04's acceptance is about media, not about peer state: withdrawing voice.speak must stop
// audio being forwarded, and no client cooperation may be needed for that. Signaling and SFU
// flags are covered by Go tests; this journey measures the negotiated transport instead, in a
// real Chromium with a deterministic synthetic audio track, by reading the WebRTC byte counters of the
// listener's peer connection before, during and after the withdrawal.
//
// The sending peer is deliberately made non-cooperative, because a client that politely stops
// capturing when it is told it may not speak proves nothing about the server: capture is forced
// back on and its outbound counter is asserted to keep growing while the listener's inbound
// counter stops. That combination is what makes the denial server-side.
//
// It also covers the permitted side of the contract: the same speaker gets audible again when
// the capability is returned.
//
// This is synthetic Chromium/loopback evidence with a Web Audio oscillator. It is not audible
// quality, not a real microphone, not a forced relay and not a second network.
const SPEAKER = { username: 'speaker', password: 'a speaker passphrase' };
const LISTENER = { username: 'listener', password: 'a listener passphrase' };
const DEFAULT_ROLE_WITHOUT_SPEAK = ['channel.read', 'message.send', 'voice.connect'];
const SPEAKERS_WITHOUT_SPEAK = ['channel.read', 'message.send', 'voice.connect'];
const SPEAKERS_WITH_SPEAK = ['channel.read', 'message.send', 'voice.connect', 'voice.speak'];

// capturePeers keeps a handle on every peer connection the page creates, so the journey
// can read the transport counters the application itself never exposes.
async function capturePeers(page) {
  await page.addInitScript(() => {
    // Chromium's fake-device capture can hang in the host audio service before a track
    // exists. Generate a real MediaStream with Web Audio for this transport-only journey.
    // The application's negotiation, actual RTP, SFU forwarding, withdrawal and regrant
    // remain unchanged; this does not validate OS microphone capture or permission UI.
    navigator.mediaDevices.getUserMedia = async (constraints) => {
      if (!constraints?.audio || constraints.video)
        throw new Error('This fixture supplies audio only.');
      const audio = new AudioContext();
      await audio.resume();
      const oscillator = audio.createOscillator();
      const gain = audio.createGain();
      const destination = audio.createMediaStreamDestination();
      oscillator.frequency.value = 440;
      gain.gain.value = 0.1;
      oscillator.connect(gain).connect(destination);
      oscillator.start();
      const track = destination.stream.getAudioTracks()[0];
      const stop = track.stop.bind(track);
      let stopped = false;
      track.stop = () => {
        if (stopped) return;
        stopped = true;
        oscillator.stop();
        void audio.close();
        stop();
      };
      return destination.stream;
    };
    const Original = window.RTCPeerConnection;
    if (!Original) return;
    window.__brrpPeers = [];
    window.RTCPeerConnection = class extends Original {
      constructor(...args) {
        super(...args);
        window.__brrpPeers.push(this);
      }
    };
  });
}

// decodedVideo reads what a page's inbound video has actually done: frames decoded,
// and the width of the widest decoded frame. The frame count alone would accept a
// still picture and a picture from the wrong share alike; the width is what makes
// "this is the 320-wide canvas the journey shares" checkable rather than assumed.
async function decodedVideo(target) {
  return target.evaluate(async () => {
    let frames = 0;
    let width = 0;
    for (const peer of window.__brrpPeers ?? []) {
      if (peer.connectionState !== 'connected') continue;
      const report = await peer.getStats();
      report.forEach((entry) => {
        if (entry.type !== 'inbound-rtp' || entry.kind !== 'video') return;
        frames += entry.framesDecoded ?? 0;
        width = Math.max(width, entry.frameWidth ?? 0);
      });
    }
    return { frames, width };
  });
}

async function audioBytes(page) {
  return page.evaluate(async () => {
    let inbound = 0;
    let outbound = 0;
    let level = 0;
    const tracks = {};
    for (const peer of window.__brrpPeers ?? []) {
      if (peer.connectionState !== 'connected') continue;
      const report = await peer.getStats();
      report.forEach((entry) => {
        if (entry.kind !== 'audio') return;
        if (entry.type === 'inbound-rtp') {
          inbound += entry.bytesReceived ?? 0;
          level = Math.max(level, entry.audioLevel ?? 0);
          const id = entry.trackIdentifier || entry.id;
          tracks[id] = (tracks[id] ?? 0) + (entry.bytesReceived ?? 0);
        }
        if (entry.type === 'outbound-rtp') outbound += entry.bytesSent ?? 0;
      });
    }
    return { inbound, outbound, level, tracks };
  });
}

// forceTransmission turns the page's capture back on and re-attaches it wherever the
// application detached it, so the sender keeps sending RTP regardless of what it was told.
async function forceTransmission(page) {
  return page.evaluate(async () => {
    let attached = 0;
    for (const peer of window.__brrpPeers ?? []) {
      if (peer.connectionState !== 'connected') continue;
      for (const sender of peer.getSenders()) {
        if (sender.track) {
          if (sender.track.kind !== 'audio') continue;
          sender.track.enabled = true;
          attached++;
          continue;
        }
        // Voice is audio-only, so a detached sender is an audio sender: give it a fresh
        // capture back and keep publishing.
        if (!window.__brrpForced) {
          const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
          window.__brrpForced = stream.getAudioTracks()[0];
        }
        await sender.replaceTrack(window.__brrpForced);
        attached++;
      }
    }
    return attached;
  });
}

// The sidebar control is the one that outlives a join: clicking "Join voice" closes the room
// dialog, so the joined state is read from the sidebar and the dialog is reopened only when
// the journey needs the participant list (the checked-in test used to wait inside the dialog
// that had just closed, which is review R3-04).
function sidebarVoice(page) {
  return page.locator('aside.channel-sidebar .voice-controls');
}

async function joinVoice(page, account) {
  await signIn(page, account);
  const channel = page
    .locator('nav.channel-navigation button.voice-link')
    .filter({ hasText: 'Lounge' });
  await expect(channel).toBeVisible();
  await channel.click();
  const panel = page.getByRole('dialog', { name: 'Lounge', exact: true });
  await expect(panel).toBeVisible();
  await panel.getByRole('button', { name: 'Join voice' }).click();
  // Joining closes the dialog; the sidebar reports the connected state it is left in.
  await expect(sidebarVoice(page).getByRole('button', { name: 'Leave voice' })).toBeVisible({
    timeout: 30000
  });
  await expect(sidebarVoice(page)).toContainText('Voice connected');
  return panel;
}

// openRoom reopens the room dialog, which is where the participant list and per-participant
// state live.
async function openRoom(page) {
  await sidebarVoice(page)
    .getByRole('button', { name: /Voice connected/ })
    .click();
  const panel = page.getByRole('dialog', { name: 'Lounge', exact: true });
  await expect(panel).toBeVisible();
  return panel;
}

// A failed voice journey is diagnosed from the panel, and the panel used to be thrown
// away: the store's error text, the screen share's message and the control state are all
// rendered there, and the failure screenshot only shows the last moment of a long wait.
// Printing it turns the next intermittent into something with a named cause in the log.
test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status === 'passed' || testInfo.status === 'skipped') return;
  const parts = [];
  for (const [name, locator] of [
    ['sidebar', page.locator('aside.channel-sidebar .voice-controls')],
    ['dialog', page.getByRole('dialog')]
  ]) {
    const text = await locator
      .first()
      .innerText({ timeout: 2000 })
      .catch(() => '');
    if (text.trim()) parts.push(`[${name}] ${text.replace(/\s+/g, ' ').trim()}`);
  }
  console.log(
    `VOICE-STATE ${testInfo.title}: ${parts.join(' | ') || '(no voice surface rendered)'}`
  );
});

test('withdrawing voice.speak stops the forwarded audio, and returning it resumes', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(420000);
  await signInOverApi(request);

  // The default role grants voice.speak to everyone, so the speaker's own role is the
  // only thing this journey can take it away from: narrowing the default role first keeps
  // the withdrawal meaningful.
  const roles = await (await request.get('/api/roles')).json();
  const defaultRole = roles.find((role) => role.is_default);
  const originalDefault = defaultRole.permissions;
  let speakersRole = '';
  let listenerContext;
  try {
    await setRolePermissions(request, defaultRole.id, DEFAULT_ROLE_WITHOUT_SPEAK);

    speakersRole = await createRole(request, 'speakers', SPEAKERS_WITH_SPEAK);
    const speakerID = await createAccount(request, SPEAKER);
    await createAccount(request, LISTENER);
    await assignOnly(request, speakerID, [speakersRole]);

    await capturePeers(page);
    listenerContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
    const listenerPage = await listenerContext.newPage();
    await capturePeers(listenerPage);

    await joinVoice(page, SPEAKER);
    await joinVoice(listenerPage, LISTENER);

    // Both peers are in the call, and each sees the other. The dialog is reopened through
    // the sidebar control rather than assumed to be open.
    const speakerPanel = await openRoom(page);
    const listenerPanel = await openRoom(listenerPage);
    await expect(speakerPanel.getByText('listener')).toBeVisible({ timeout: 20000 });
    await expect(listenerPanel.getByText('speaker')).toBeVisible({ timeout: 20000 });

    // Audio is flowing: the listener's transport is receiving bytes that keep arriving.
    const flowing = async (target) => {
      let previous = await audioBytes(target);
      for (let attempt = 0; attempt < 12; attempt++) {
        await target.waitForTimeout(1000);
        const current = await audioBytes(target);
        if (current.inbound > previous.inbound + 2000) return current;
        previous = current;
      }
      return previous;
    };
    const before = await flowing(listenerPage);
    await expect
      .poll(async () => (await audioBytes(listenerPage)).inbound, { timeout: 20000 })
      .toBeGreaterThan(before.inbound);

    // The capability is withdrawn. The speaker keeps its client as it is: the denial must
    // not depend on the sender volunteering a mute.
    await setRolePermissions(request, speakersRole, SPEAKERS_WITHOUT_SPEAK);
    await listenerPage.waitForTimeout(3000);
    const afterWithdrawal = await audioBytes(listenerPage);

    // Now make the sender non-cooperative: whatever the application did with capture, put it
    // back and keep it on. The sender's own outbound counter is asserted to keep growing,
    // which is what turns the following inbound measurement into server evidence.
    await forceTransmission(page);
    const transmitting = await audioBytes(page);
    await listenerPage.waitForTimeout(4000);
    await forceTransmission(page);
    const stillTransmitting = await audioBytes(page);
    const settled = await audioBytes(listenerPage);
    expect(
      stillTransmitting.outbound,
      'the sending peer stopped transmitting, so the measurement below cannot show server enforcement'
    ).toBeGreaterThan(transmitting.outbound + 2000);
    expect(
      settled.inbound - afterWithdrawal.inbound,
      'audio kept being forwarded after voice.speak was withdrawn'
    ).toBeLessThan(2000);
    // The call itself is intact: the peer was muted, not evicted.
    await expect(listenerPanel.getByText('speaker')).toBeVisible();

    // Returning the capability is the permitted half of the same contract.
    await setRolePermissions(request, speakersRole, SPEAKERS_WITH_SPEAK);
    const resumed = await flowing(listenerPage);
    expect(resumed.inbound, 'audio did not resume after voice.speak was returned').toBeGreaterThan(
      settled.inbound + 2000
    );
  } finally {
    // Whatever happened, the installation is left as it was found: the shared default role
    // restored and the journey's own browser closed, so a failure here does not narrow
    // another journey's coverage.
    if (listenerContext) await listenerContext.close().catch(() => {});
    await setRolePermissions(request, defaultRole.id, originalDefault);
  }
});

// A screen share is offered as a real control, and every way it can not happen leaves
// the person with an explanation rather than a silent no-op. The capture itself is
// stubbed — a browser's picker is not something a test can drive, and pretending
// otherwise would test Playwright rather than this product — but the *decisions* the
// application makes about capture are exactly what is under test here.
test('a screen share is offered, and every refusal is explained', async ({ page }) => {
  test.setTimeout(180000);
  // Capture is decided by this stub so the journey can drive each browser outcome.
  await page.addInitScript(() => {
    let mode = 'cancelled';
    window.__screenMode = (next) => {
      mode = next;
    };
    navigator.mediaDevices.getDisplayMedia = async () => {
      if (mode === 'cancelled') {
        const error = new Error('Permission denied');
        error.name = 'NotAllowedError';
        throw error;
      }
      if (mode === 'insecure') {
        const error = new Error('needs a secure context');
        error.name = 'SecurityError';
        throw error;
      }
      const canvas = document.createElement('canvas');
      canvas.width = 320;
      canvas.height = 240;
      const context = canvas.getContext('2d');
      context.fillStyle = '#2f6f4f';
      context.fillRect(0, 0, 320, 240);
      return canvas.captureStream(15);
    };
  });

  // The share control belongs to a joined call, so the journey joins one first.
  await capturePeers(page);
  await joinVoice(page, OWNER);
  const share = sidebarVoice(page).getByRole('button', { name: 'Share screen' });
  await expect(share).toBeVisible();

  // Declining the picker is an ordinary answer, and the app says so rather than
  // reporting a failure the person did not cause.
  await page.evaluate(() => window.__screenMode('cancelled'));
  await share.click();
  await expect(sidebarVoice(page).getByText(/Nothing was shared/)).toBeVisible();
  // And the control is not left in a sharing state.
  await expect(sidebarVoice(page).getByRole('button', { name: 'Share screen' })).toBeVisible();

  // An insecure page is named as such, because "sharing failed" sends somebody
  // looking for a bug instead of for a missing https.
  await page.evaluate(() => window.__screenMode('insecure'));
  await sidebarVoice(page).getByRole('button', { name: 'Share screen' }).click();
  await expect(sidebarVoice(page).getByText(/secure \(https\) connection/)).toBeVisible();

  // A successful capture says that people can see it, and stopping it is offered.
  await page.evaluate(() => window.__screenMode('share'));
  await sidebarVoice(page).getByRole('button', { name: 'Share screen' }).click();
  await expect(
    sidebarVoice(page).getByText(/Everyone in the conversation can see it/)
  ).toBeVisible();

  // The quality preset is applied to the live sender, not merely remembered: the
  // ceiling and the degradation preference are what make "text" a different stream on
  // the wire, and a choice that never left the interface would look identical here.
  const senderCeiling = () =>
    page.evaluate(() => {
      for (const peer of window.__brrpPeers ?? []) {
        for (const sender of peer.getSenders()) {
          if (sender.track?.kind !== 'video') continue;
          return sender.getParameters().encodings?.[0]?.maxBitrate ?? 0;
        }
      }
      return 0;
    });
  await expect.poll(senderCeiling).toBe(2_000_000);
  // And switching presets reaches that same sender while the share is running.
  await sidebarVoice(page).getByLabel('Screen share quality').selectOption('text');
  await expect
    .poll(() =>
      page.evaluate(() => {
        for (const peer of window.__brrpPeers ?? []) {
          for (const sender of peer.getSenders()) {
            if (sender.track?.kind !== 'video') continue;
            return sender.getParameters().encodings?.[0]?.maxFramerate ?? 0;
          }
        }
        return 0;
      })
    )
    .toBe(5);

  const stop = sidebarVoice(page).getByRole('button', { name: 'Stop sharing screen' });
  await expect(stop).toBeVisible();
  await stop.click();
  await expect(sidebarVoice(page).getByRole('button', { name: 'Share screen' })).toBeVisible();

  // The voice session is left rather than resumed. A clean close detaches and keeps a
  // resume token, which is right for a person who closed a tab and is coming back — and
  // wrong here, because the next journey in this file joins the same account and would
  // be handed a session it did not ask for. Leaving is what a person changing their
  // mind does, so it is what the journey does.
  const leave = sidebarVoice(page).getByRole('button', { name: /Leave (voice|call)/ });
  if (await leave.isVisible().catch(() => false)) {
    await leave.click();
    await expect(leave).toHaveCount(0, { timeout: 15000 });
  }
});

// A screen share is captured, *published*, and negotiated — and the whole thing has to
// survive a stop. The capture is stubbed because a browser's picker cannot be driven by
// a test, but the publication is the product's own code: if it is not sent, the server
// never sees a screen, and if the server refuses, the browser's own indicator must not
// be left on claiming otherwise.
test('a screen share is published to the room, and stopping it releases the track', async ({
  page
}) => {
  test.setTimeout(180000);
  await captureGeneratedAudio(page);
  await page.addInitScript(() => {
    window.__shareState = { grabbed: 0, stopped: 0, offers: 0 };
    navigator.mediaDevices.getDisplayMedia = async () => {
      window.__shareState.grabbed += 1;
      const canvas = document.createElement('canvas');
      canvas.width = 320;
      canvas.height = 240;
      const context = canvas.getContext('2d');
      context.fillStyle = '#2f6f4f';
      context.fillRect(0, 0, 320, 240);
      context.fillStyle = '#fff';
      context.font = '24px sans-serif';
      context.fillText('shared', 100, 130);
      // Repainted on a timer so a viewer receives a moving picture rather than one
      // frame, which is what distinguishes a forwarded stream from a stale one.
      let frame = 0;
      const timer = setInterval(() => {
        frame += 1;
        context.fillStyle = frame % 2 ? '#2f6f4f' : '#1d4a35';
        context.fillRect(0, 0, 320, 240);
        context.fillStyle = '#fff';
        context.fillText('shared ' + frame, 60, 130);
      }, 250);
      const stream = canvas.captureStream(4);
      const track = stream.getVideoTracks()[0];
      const originalStop = track.stop.bind(track);
      track.stop = () => {
        clearInterval(timer);
        window.__shareState.stopped += 1;
        originalStop();
      };
      Object.defineProperty(track, 'label', { value: 'Shared test screen', configurable: true });
      return stream;
    };
  });

  await joinVoice(page, OWNER);

  // The control is offered because the owner holds voice.share_screen.
  const share = sidebarVoice(page).getByRole('button', { name: 'Share screen' });
  await expect(share).toBeVisible();
  await share.click();

  // The browser granted a capture, and the app reports that it is sharing.
  await expect.poll(() => page.evaluate(() => window.__shareState.grabbed)).toBe(1);
  // The state the app reached is reported on failure, because "the message is not
  // there" and "the share was refused and the message says so" are different failures
  // and a reader cannot tell them apart from a timeout.
  const shareState = await page.evaluate(() => ({
    controls: document.querySelector('.voice-controls')?.textContent?.trim() ?? ''
  }));
  await expect(
    sidebarVoice(page).getByText(/Everyone in the conversation can see it/),
    `share state: ${JSON.stringify(shareState)}`
  ).toBeVisible();

  // Stopping it releases the track, so the browser's own indicator goes away. An
  // indicator left on is a person believing they are visible when they are not.
  await sidebarVoice(page).getByRole('button', { name: 'Stop sharing screen' }).click();
  await expect.poll(() => page.evaluate(() => window.__shareState.stopped)).toBe(1);
  await expect(sidebarVoice(page).getByRole('button', { name: 'Share screen' })).toBeVisible();

  // The voice session is left behind. The server holds it until somebody leaves, and
  // a later journey in this file joins the same account: a test that leaves the shared
  // server holding a session makes the next one fail for a reason that has nothing to
  // do with the thing under test.
  const leave = sidebarVoice(page).getByRole('button', { name: /Leave (voice|call)/ });
  if (await leave.isVisible().catch(() => false)) {
    await leave.click();
    await expect(leave).toHaveCount(0, { timeout: 15000 });
  }
});

// The two-browser screen journey the registry tests cannot stand in for: a real
// publisher, a real viewer and the embedded SFU between them. What is asserted is what
// the *viewer's* transport decoded — frames the browser only counts once the picture
// arrived — and that the screen's own audio arrives as a separate inbound stream and
// stops with the share. The capture is a stub because a browser's picker cannot be
// driven; everything after the capture is the product's code.
const SHARER = { username: 'screen_sharer', password: 'a screen sharer passphrase' };
const VIEWER = { username: 'screen_viewer', password: 'a screen viewer passphrase' };
// A third account for the late-joining viewer: two tabs of one account cannot be in the
// same call (each account holds one voice place), and this journey needs somebody who
// arrives after the share is already running.
const WATCHER = { username: 'screen_watcher', password: 'a screen watcher passphrase' };

test('a shared screen reaches another browser and stops cleanly', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(420000);
  await signInOverApi(request);
  await createAccount(request, SHARER);
  await createAccount(request, VIEWER);
  await createAccount(request, WATCHER);

  await capturePeers(page);
  await page.addInitScript(() => {
    window.__shareState = { grabbed: 0, stopped: 0 };
    navigator.mediaDevices.getDisplayMedia = async () => {
      window.__shareState.grabbed += 1;
      const canvas = document.createElement('canvas');
      canvas.width = 320;
      canvas.height = 240;
      const context = canvas.getContext('2d');
      let frame = 0;
      const paint = () => {
        frame += 1;
        context.fillStyle = frame % 2 ? '#2f6f4f' : '#1d4a35';
        context.fillRect(0, 0, 320, 240);
        context.fillStyle = '#fff';
        context.font = '24px sans-serif';
        context.fillText('shared ' + frame, 70, 130);
      };
      paint();
      const timer = setInterval(paint, 250);
      const stream = canvas.captureStream(4);
      // Screen audio is part of the capture: the share path publishes it, and stopping
      // the share releases it. That release is the boundary that used to leak.
      const audio = new AudioContext();
      await audio.resume();
      const oscillator = audio.createOscillator();
      const destination = audio.createMediaStreamDestination();
      oscillator.frequency.value = 660;
      oscillator.connect(destination);
      oscillator.start();
      stream.addTrack(destination.stream.getAudioTracks()[0]);
      const video = stream.getVideoTracks()[0];
      const originalStop = video.stop.bind(video);
      video.stop = () => {
        clearInterval(timer);
        window.__shareState.stopped += 1;
        oscillator.stop();
        void audio.close().catch(() => {});
        originalStop();
      };
      Object.defineProperty(video, 'label', {
        value: 'Shared two-browser screen',
        configurable: true
      });
      return stream;
    };
  });

  const viewerContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const viewer = await viewerContext.newPage();
  await capturePeers(viewer);
  await viewer.addInitScript(() => {
    const Original = window.WebSocket;
    if (!Original) return;
    window.__brrpEvents = [];
    window.WebSocket = class extends Original {
      constructor(...args) {
        super(...args);
        this.addEventListener('message', (event) => {
          try {
            const data = JSON.parse(event.data);
            if (data && data.t) window.__brrpEvents.push(String(data.t));
          } catch {
            /* Not an event frame. */
          }
        });
      }
    };
  });
  try {
    await joinVoice(page, SHARER);
    await joinVoice(viewer, VIEWER);

    const share = sidebarVoice(page).getByRole('button', { name: 'Share screen' });
    await expect(share).toBeVisible();
    await share.click();
    await expect
      .poll(() => page.evaluate(() => window.__shareState.grabbed), { timeout: 30000 })
      .toBe(1);
    await expect(
      sidebarVoice(page).getByText(/Everyone in the conversation can see it/)
    ).toBeVisible({
      timeout: 30000
    });

    // The viewer learns a share exists: the announcement reaches the voice store,
    // which negotiates receive lines for the picture and for the share's audio. This
    // is the boundary the workspace router used to drop — `screen.published` does not
    // start with `voice.`, so the client's whole receive path was dead code. The lines
    // are `recvonly`, which is how a viewer says it wants media sent to it
    // (review 17, two-browser screen journey).
    await expect
      .poll(
        () =>
          viewer.evaluate(
            () => (window.__brrpEvents ?? []).filter((t) => t === 'screen.published').length
          ),
        { timeout: 30000 }
      )
      .toBeGreaterThan(1);
    await expect
      .poll(
        () =>
          viewer.evaluate(async () => {
            for (const peer of window.__brrpPeers ?? []) {
              const kinds = peer
                .getTransceivers()
                .map((t) => `${t.direction}:${t.receiver?.track?.kind ?? 'none'}`);
              if (
                kinds.some((entry) => entry.startsWith('recvonly:video')) &&
                kinds.some((entry) => entry.startsWith('recvonly:audio'))
              )
                return true;
            }
            return false;
          }),
        { timeout: 30000 }
      )
      .toBe(true);

    // The call survives the share. The publisher's own view request arrives while its
    // publish offer is still being answered; without the offer/answer queue the
    // connection ended with "Voice negotiation failed" for the sharer whenever anybody
    // was in the room.
    const connected = async (target) =>
      target.evaluate(() =>
        (window.__brrpPeers ?? []).every((peer) => peer.connectionState === 'connected')
      );
    expect(await connected(page), 'the sharer left the call when it started sharing').toBe(true);
    expect(await connected(viewer), 'the viewer left the call when the share started').toBe(true);

    // The publisher encodes frames: the screen really is being sent to the server.
    await expect
      .poll(
        () =>
          page.evaluate(async () => {
            let encoded = 0;
            for (const peer of window.__brrpPeers ?? []) {
              const report = await peer.getStats();
              report.forEach((entry) => {
                if (entry.type === 'outbound-rtp' && entry.kind === 'video')
                  encoded += entry.framesEncoded ?? 0;
              });
            }
            return encoded;
          }),
        { timeout: 30000 }
      )
      .toBeGreaterThan(3);

    // And the viewer decodes it: the picture arrives, it is the shared one (320 wide),
    // and new frames keep coming. Until the server answered the viewer's own receive
    // line with the transceiver carrying the screen, this connection reported
    // `connected` and decoded nothing (review 17, two-browser screen journey).
    await expect
      .poll(async () => (await decodedVideo(viewer)).frames, { timeout: 30000 })
      .toBeGreaterThan(1);
    const firstDecoded = await decodedVideo(viewer);
    expect(firstDecoded.width, 'the decoded picture is not the shared one').toBe(320);
    await expect
      .poll(async () => (await decodedVideo(viewer)).frames, { timeout: 30000 })
      .toBeGreaterThan(firstDecoded.frames + 3);

    // Stopping the share releases it: the room hears the stop, the browser's capture
    // is stopped, the picture stops arriving, and both clients are still in the call
    // afterwards.
    await sidebarVoice(page).getByRole('button', { name: 'Stop sharing screen' }).click();
    await expect
      .poll(() => page.evaluate(() => window.__shareState.stopped), { timeout: 30000 })
      .toBe(1);
    await expect
      .poll(
        () =>
          viewer.evaluate(
            () => (window.__brrpEvents ?? []).filter((t) => t === 'screen.stopped').length
          ),
        { timeout: 30000 }
      )
      .toBeGreaterThan(0);
    const stoppedDecoded = await decodedVideo(viewer);
    await viewer.waitForTimeout(4000);
    const afterStopDecoded = await decodedVideo(viewer);
    expect(
      afterStopDecoded.frames - stoppedDecoded.frames,
      'the viewer kept decoding pictures after the share stopped'
    ).toBeLessThanOrEqual(3);
    expect(await connected(page), 'the sharer left the call when the share stopped').toBe(true);
    expect(await connected(viewer), 'the viewer left the call when the share stopped').toBe(true);

    // Sharing again works, and the viewer's receive lines are reused rather than
    // stacked: a restart is a swap, not a second screen (the room's bound is one).
    await sidebarVoice(page).getByRole('button', { name: 'Share screen' }).click();
    await expect
      .poll(() => page.evaluate(() => window.__shareState.grabbed), { timeout: 30000 })
      .toBe(2);
    await expect(
      sidebarVoice(page).getByText(/Everyone in the conversation can see it/)
    ).toBeVisible({ timeout: 30000 });
    expect(await connected(viewer), 'the viewer left the call on the share restart').toBe(true);
    await expect
      .poll(async () => (await decodedVideo(viewer)).frames, { timeout: 30000 })
      .toBeGreaterThan(afterStopDecoded.frames + 3);

    // A viewer that joins *after* the share is running still sees the picture within a
    // few seconds. Its browser is told the share exists when it joins, negotiates a
    // line for it, asks for a keyframe as the line is bound, and the publisher answers
    // with one — without that loop, this viewer sits in front of a black rectangle
    // until the encoder's next natural keyframe (ENG-02).
    const watcherContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
    const watcher = await watcherContext.newPage();
    await capturePeers(watcher);
    try {
      await joinVoice(watcher, WATCHER);
      await expect
        .poll(async () => (await decodedVideo(watcher)).frames, { timeout: 10000 })
        .toBeGreaterThan(1);
      expect((await decodedVideo(watcher)).width, 'the late viewer decoded another picture').toBe(
        320
      );
    } finally {
      await watcherContext.close().catch(() => {});
    }
  } finally {
    await viewerContext.close().catch(() => {});
    const leave = sidebarVoice(page).getByRole('button', { name: /Leave (voice|call)/ });
    if (await leave.isVisible().catch(() => false)) {
      await leave.click();
      await expect(leave).toHaveCount(0, { timeout: 15000 });
    }
  }
});
