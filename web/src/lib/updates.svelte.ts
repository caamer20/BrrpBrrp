import { saveUpdateDrafts, clearUpdateDrafts } from './reload-drafts';
import { workspace } from './workspace.svelte';

class Updates {
  ready = $state(false);
  busy = $state(false);
  error = $state('');
  dismissed = $state(false);
  checking = $state(false);
  checked = $state(false);
  private checkRequest?: () => Promise<void>;
  private registration: ServiceWorkerRegistration | undefined;
  private requested = false;
  private activationTimer?: ReturnType<typeof setTimeout>;

  start() {
    if (!('serviceWorker' in navigator)) return () => {};
    let active = true;
    let lastCheck = 0;
    const build = document.querySelector<HTMLMetaElement>('meta[name="brrp-build"]')?.content;
    if (!build) return () => {}; // Development servers do not install a production worker.
    const inspect = () => {
      if (active && this.registration?.waiting && navigator.serviceWorker.controller) {
        if (!this.ready) this.dismissed = false;
        this.ready = true;
      }
    };
    const check = async (force = false) => {
      if (
        !active ||
        this.checking ||
        (!force && (document.hidden || Date.now() - lastCheck < 60000))
      )
        return;
      this.checking = true;
      if (force) this.error = '';
      lastCheck = Date.now();
      try {
        await this.registration?.update();
        const response = await fetch('/app-version.json', { cache: 'no-store' });
        if (!response.ok) throw new Error('Update check failed.');
        const next = await response.json();
        if (active && typeof next.build === 'string') {
          const ready = next.build !== build || !!this.registration?.waiting;
          if (ready && !this.ready) this.dismissed = false;
          this.ready = ready;
        }
        inspect();
      } catch {
        if (force) this.error = 'Couldn’t check for updates. Check your connection and try again.';
      } finally {
        this.checking = false;
        this.checked = true;
      }
    };
    this.checkRequest = () => check(true);
    const focus = () => {
      void check();
    };
    const updated = () => {
      const worker = this.registration?.installing;
      worker?.addEventListener('statechange', inspect);
    };
    const controllerChanged = () => {
      if (!active) return;
      if (this.requested) {
        clearTimeout(this.activationTimer);
        location.reload();
      } else {
        lastCheck = 0;
        void check();
      }
    };
    navigator.serviceWorker.addEventListener('controllerchange', controllerChanged);
    navigator.serviceWorker
      .register('/service-worker.js', { updateViaCache: 'none' })
      .then((registration) => {
        if (!active) return;
        this.registration = registration;
        registration.addEventListener('updatefound', updated);
        inspect();
        void check();
      })
      .catch(() => {});
    const online = () => {
      lastCheck = 0;
      void check();
    };
    const timer = setInterval(focus, 300000);
    window.addEventListener('focus', focus);
    window.addEventListener('online', online);
    return () => {
      active = false;
      clearInterval(timer);
      clearTimeout(this.activationTimer);
      navigator.serviceWorker.removeEventListener('controllerchange', controllerChanged);
      this.registration?.removeEventListener('updatefound', updated);
      window.removeEventListener('focus', focus);
      this.checkRequest = undefined;
      window.removeEventListener('online', online);
    };
  }

  async checkNow() {
    await this.checkRequest?.();
    if (this.ready) this.dismissed = false;
  }

  apply(voiceActive: boolean) {
    if (this.busy || !workspace.canUpdate || voiceActive) return;
    this.error = '';
    try {
      const drafts = workspace.captureComposerDrafts?.() || {};
      if (
        workspace.user &&
        Object.values(drafts).some((draft) => draft.text.trim() || draft.attachments.length)
      )
        saveUpdateDrafts(sessionStorage, workspace.user.id, drafts, workspace.replyTo?.id);
    } catch {
      this.error =
        'Your drafts couldn’t be saved for this refresh. Send them or copy them before updating.';
      return;
    }
    this.busy = true;
    this.requested = true;
    const worker = this.registration?.waiting;
    if (worker) {
      const failed = () => {
        this.busy = false;
        this.requested = false;
        this.error = 'The update didn’t finish. Your drafts are still here. Try again.';
        try {
          clearUpdateDrafts(sessionStorage);
        } catch {
          /* Storage may be unavailable. */
        }
      };
      try {
        this.activationTimer = setTimeout(failed, 10000);
        worker.postMessage({ type: 'ACTIVATE_UPDATE' });
      } catch {
        clearTimeout(this.activationTimer);
        failed();
      }
    } else location.reload();
  }
}
export const updates = new Updates();
