import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Optional first argument is the repository root; the retained report layout is the default.
const projectRoot = path.resolve(process.argv[2] || fileURLToPath(new URL('../../../', import.meta.url)));
const require = createRequire(path.join(projectRoot, 'web', 'package.json'));
const ts = require('typescript');

// Exercise request ordering with deferred responses and an inert browser shell.
const source = await readFile(path.join(projectRoot, 'web', 'src', 'lib', 'workspace.svelte.ts'), 'utf8');
// The workspace imports the outbox store at runtime, and the shell below strips imports, so the
// store's real source is compiled into the same module: the tests exercise the shipped code
// rather than a stand-in for it.
const outboxSource = await readFile(path.join(projectRoot, 'web', 'src', 'lib', 'outbox.ts'), 'utf8');
const draftsSource = await readFile(path.join(projectRoot, 'web', 'src', 'lib', 'drafts.ts'), 'utf8');
const shellModules = [outboxSource, draftsSource]
  .map((file) =>
    ts
      .transpileModule(file, {
        compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
      })
      .outputText.replace(/^export /gm, '')
  )
  .join('\n');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '')
  .replace('export const workspace = new Workspace();', 'export { Workspace };');
const prelude = `
const $state = value => value;
const memory = new Map();
export const store = {
  getItem: key => (memory.has(key) ? memory.get(key) : null),
  setItem: (key, value) => memory.set(key, String(value)),
  removeItem: key => memory.delete(key),
  clear: () => memory.clear(),
  keys: () => [...memory.keys()]
};
const storage = store;
const localStorage = storage;
const window = { innerWidth: 1200 };
const location = { hash: '', search: '', protocol: 'http:', host: 'localhost' };
const history = { replaceState() {} };
const document = { hidden: true, hasFocus: () => false };
export const setDocumentFocus = (hidden, focused) => {
  document.hidden = hidden;
  document.hasFocus = () => focused;
};
const WebSocket = { OPEN: 1 };
const errorMessage = error => error.message;
class APIError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}
export const apiError = (message, status) => new APIError(message, status);
let handler;
export const setHandler = value => handler = value;
const api = (...args) => handler(...args);
`;
const { Workspace, setHandler, store, setDocumentFocus, apiError } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + shellModules + compiled).toString('base64')
);
const state = () => ({
  user: { id: 'me' },
  channels: [
    { id: 'a', type: 'text' },
    { id: 'b', type: 'text' }
  ],
  settings: {},
  unread: [],
  voice_states: [],
  categories: [],
  authorization: {
    revision: 1,
    is_owner: false,
    capabilities: ['channel.read', 'message.send'],
    owner_designated: true,
    ownership_established: true
  }
});
const message = (id, extra = {}) => ({
  id,
  idempotency_key: id,
  channel_id: 'a',
  user_id: 'me',
  content: 'hello',
  attachments: [],
  reactions: [],
  ...extra
});
function workspace() {
  const value = new Workspace();
  value.state = state();
  value.activeId = 'a';
  value.showToast = (text) => {
    value.toast = text;
  };
  return value;
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}


const uid='11111111-1111-4111-8111-111111111111';
const cid='22222222-2222-4222-8222-222222222222';
const entry={key:'33333333-3333-4333-8333-333333333333',conversation:cid,content:'lost text',attachments:[],reply_to:'',created:new Date().toISOString()};
const box=(await import('data:text/javascript;base64,'+Buffer.from(ts.transpileModule(outboxSource,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022}}).outputText).toString('base64')));
const rawKey='brrp.outbox.'+uid;
store.setItem(rawKey,JSON.stringify([entry,{...entry,key:crypto.randomUUID(),content:'',attachments:undefined}]));
console.log('malformed_entry_suppresses_valid_sibling',box.readOutbox(store,uid).length);
box.writeOutbox(store,uid,[{...entry,content:'x'.repeat(80*1024)}]);
console.log('raw_persisted_text_characters',JSON.parse(store.getItem(rawKey))[0].content.length);
console.log('restored_text_characters',box.readOutbox(store,uid)[0].content.length);
store.clear();
for(const testCase of ['full-composer','no-text-channel']) {
 const value=workspace(); value.state.user.id=uid; value.saveDrafts=true;
 value.state.channels=testCase==='no-text-channel'?[]:[{id:cid,type:'text'}]; value.activeId=cid;
 value.drafts[cid]=testCase==='full-composer'?'z'.repeat(4000):'';
 value.outbox=[entry]; value.persistOutbox();
 value.releaseOutbox(entry.key,'Recovery promise: Your text is in the composer.');
 console.log(testCase,JSON.stringify({outbox:store.getItem(rawKey),draftHasRecoveredText:(value.drafts[cid]||'').includes(entry.content),toast:value.toast}));
}
store.clear();
const value=workspace(); value.state.user.id=uid; value.saveDrafts=true; value.state.channels=[{id:cid,type:'text'}]; value.activeId=cid;
store.setItem(rawKey,JSON.stringify([{...entry,created:new Date(Date.now()-25*3600*1000).toISOString()}]));
await value.restoreOutbox();
console.log('expired_entry_after_restore',JSON.stringify({outbox:store.getItem(rawKey),draft:value.drafts[cid]||'',toast:value.toast||''}));
