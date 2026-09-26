import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/updates.svelte.ts', import.meta.url), 'utf8');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '')
  .replace('export const updates = new Updates();', 'export { Updates };');
const prelude = `
const $state = value => value;
export const workspace = { canUpdate: true, user: {id:'me'}, captureComposerDrafts: () => ({a:{text:'Draft',attachments:[]}}) };
export const events = [];
const sessionStorage = {};
let saveFails = false;
export const failStorage = value => saveFails = value;
const saveUpdateDrafts = () => { if(saveFails) throw new Error('blocked'); events.push('save'); };
const clearUpdateDrafts = () => events.push('clear');
const location = {reload: () => events.push('reload')};
let timer;
const setTimeout = fn => { timer=fn; return 1; };
const clearTimeout = () => {timer=undefined;};
export const expire = () => timer?.();
`;
const { Updates, workspace, events, failStorage, expire } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + compiled).toString('base64')
);
function fresh() {
  events.length = 0;
  workspace.canUpdate = true;
  failStorage(false);
  return new Updates();
}
test('updates wait for active voice, pending messages, and uploads', () => {
  const value = fresh();
  value.apply(true);
  assert.deepEqual(events, []);
  workspace.canUpdate = false;
  value.apply(false);
  assert.deepEqual(events, []);
  assert.equal(value.busy, false);
});
test('an explicit refresh saves drafts before reload and stops on storage failure', () => {
  const value = fresh();
  failStorage(true);
  value.apply(false);
  assert.equal(value.busy, false);
  assert.match(value.error, /drafts couldn’t be saved/);
  assert.deepEqual(events, []);
  failStorage(false);
  value.apply(false);
  assert.deepEqual(events, ['save', 'reload']);
});
test('a stalled worker activation leaves drafts in place and allows retry', () => {
  const value = fresh();
  value.registration = { waiting: { postMessage: () => events.push('activate') } };
  value.apply(false);
  assert.equal(value.busy, true);
  assert.deepEqual(events, ['save', 'activate']);
  expire();
  assert.equal(value.busy, false);
  assert.equal(value.requested, false);
  assert.match(value.error, /Try again/);
  assert.deepEqual(events, ['save', 'activate', 'clear']);
  value.apply(false);
  assert.deepEqual(events.slice(-2), ['save', 'activate']);
});
