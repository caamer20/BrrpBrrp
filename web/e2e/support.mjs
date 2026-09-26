import { createHmac } from 'node:crypto';
import { expect } from '@playwright/test';

// Shared setup for the journeys that run against the installation the first journey
// created. Anything here is arrangement: every journey still reads the rendered page
// for what it asserts, so a capability or rendering gap cannot pass unnoticed.
export const OWNER = { username: 'release_admin', password: 'release check passphrase' };

// signIn drives the real form, which is what an account does.
export async function signIn(page, account) {
  await page.goto('/');
  if (
    await page
      .getByRole('heading', { name: 'Welcome back.' })
      .isVisible()
      .catch(() => false)
  ) {
    await page.getByLabel('Username').fill(account.username);
    await page.locator('input[autocomplete="current-password"]').fill(account.password);
    await page.getByRole('button', { name: 'Sign in' }).click();
  }
  await expect(page.getByLabel('Your preferences')).toBeVisible();
}

// signInOverApi uses its own cookie jar, so a journey can act as the owner while a
// delegated account stays signed in in the page.
export async function signInOverApi(request, account = OWNER) {
  const response = await request.post('/api/login', { data: account });
  expect(response.ok(), `sign-in as ${account.username}: ${response.status()}`).toBe(true);
  return request;
}

export async function signOut(page) {
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back.' })).toBeVisible();
}

export async function createAccount(request, account) {
  const response = await request.post('/api/admin/users', {
    data: {
      username: account.username,
      password: account.password,
      display_name: account.username
    }
  });
  expect(response.status(), `create ${account.username}`).toBe(201);
  return (await response.json()).id;
}

export async function createRole(request, name, permissions, position = 25) {
  const response = await request.post('/api/roles', { data: { name, position, permissions } });
  expect(response.ok(), `create role ${name}: ${response.status()}`).toBe(true);
  return (await response.json()).id;
}

export async function assignOnly(request, userID, roleIDs) {
  const response = await request.put(`/api/admin/users/${userID}/roles`, {
    data: { role_ids: roleIDs }
  });
  expect(response.ok(), `assign roles: ${response.status()}`).toBe(true);
}

// setRolePermissions keeps a role's name and position, because an edit that replaced
// them would change more than the journey is testing.
export async function setRolePermissions(request, roleID, permissions) {
  const roles = await (await request.get('/api/roles')).json();
  const role = roles.find((entry) => entry.id === roleID);
  expect(role, `role ${roleID} is listed`).toBeTruthy();
  const response = await request.patch(`/api/roles/${roleID}`, {
    data: { name: role.name, position: role.position, permissions }
  });
  expect(response.ok(), `update role ${role.name}: ${response.status()}`).toBe(true);
  return role;
}

export async function permissionsOf(request, roleID) {
  const roles = await (await request.get('/api/roles')).json();
  return roles.find((entry) => entry.id === roleID).permissions;
}

const BASE32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function decodeBase32(secret) {
  let bits = 0;
  let value = 0;
  const bytes = [];
  for (const character of secret.replace(/=+$/, '').toUpperCase()) {
    const index = BASE32.indexOf(character);
    if (index === -1) throw new Error(`setup key contains ${character}`);
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

// totpCode is the same RFC 6238 code an authenticator app would show for a setup key
// the server generated: SHA-1, six digits, thirty-second steps.
export function totpCode(secret, at = Date.now()) {
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const digest = createHmac('sha1', decodeBase32(secret)).update(message).digest();
  const offset = digest[digest.length - 1] & 15;
  const number = digest.readUInt32BE(offset) & 0x7fffffff;
  return String(number % 1000000).padStart(6, '0');
}
