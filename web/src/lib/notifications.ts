import { api } from './api';

export async function enableNotifications(publicKey: string) {
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window))
    throw new Error(
      'This browser does not support push notifications. On iOS, install the app on your Home Screen first.'
    );
  if ((await Notification.requestPermission()) !== 'granted')
    throw new Error('Notifications are blocked. You can change this in your browser settings.');
  const registration = await navigator.serviceWorker.getRegistration();
  if (!registration)
    throw new Error('The app is not ready for notifications. Reload using trusted HTTPS.');
  const normalized = publicKey.replace(/-/g, '+').replace(/_/g, '/');
  const bytes = Uint8Array.from(atob(normalized), (character) => character.charCodeAt(0));
  const subscription =
    (await registration.pushManager.getSubscription()) ||
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: bytes
    }));
  await api('/push/subscribe', 'POST', {
    endpoint: subscription.endpoint,
    keys: subscription.toJSON().keys
  });
}

export async function disableNotifications() {
  if (!('serviceWorker' in navigator)) return;
  const registration = await navigator.serviceWorker.getRegistration();
  if (!registration || !('pushManager' in registration)) return;
  const subscription = await registration.pushManager.getSubscription();
  if (subscription) {
    try {
      await api('/push/subscribe', 'DELETE', { endpoint: subscription.endpoint });
    } finally {
      await subscription.unsubscribe();
    }
  }
}

export interface NotificationStatus {
  state: 'enabled' | 'off' | 'blocked' | 'unsupported' | 'server-off' | 'unlinked';
  title: string;
  detail: string;
}

export async function notificationStatus(serverEnabled: boolean): Promise<NotificationStatus> {
  if (!serverEnabled)
    return {
      state: 'server-off',
      title: 'Disabled by your server',
      detail: 'Your administrator can enable mention notifications in server settings.'
    };
  if (
    !isSecureContext ||
    !('serviceWorker' in navigator) ||
    !('PushManager' in window) ||
    !('Notification' in window)
  )
    return {
      state: 'unsupported',
      title: 'Unavailable in this browser',
      detail:
        'Use trusted HTTPS and a browser that supports push. On iPhone or iPad, add the app to your Home Screen first.'
    };
  if (Notification.permission === 'denied')
    return {
      state: 'blocked',
      title: 'Blocked by your browser',
      detail: 'Allow notifications in this site’s browser settings, then check again.'
    };
  const registration = await navigator.serviceWorker.getRegistration();
  const subscription = await registration?.pushManager.getSubscription();
  if (Notification.permission === 'granted' && subscription) {
    const hash = Array.from(
      new Uint8Array(
        await crypto.subtle.digest('SHA-256', new TextEncoder().encode(subscription.endpoint))
      ),
      (byte) => byte.toString(16).padStart(2, '0')
    ).join('');
    const devices = await api<{ endpoint_hashes: string[] }>('/push/devices');
    if (devices.endpoint_hashes.includes(hash))
      return {
        state: 'enabled',
        title: 'Enabled on this device',
        detail: 'You’ll receive a notification when someone mentions you.'
      };
    return {
      state: 'unlinked',
      title: 'This device needs to be connected',
      detail: 'Reconnect notifications to use them with this account.'
    };
  }
  return {
    state: 'off',
    title: 'Off on this device',
    detail: 'Enable notifications to hear about mentions while the app is closed.'
  };
}
