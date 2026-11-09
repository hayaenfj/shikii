import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { api } from '../src/lib/api.js';

function fixture() {
  const source = readFileSync(new URL('../src/lib/state.svelte.js', import.meta.url), 'utf8');
  const start = source.indexOf('let loadVersion = 0;');
  const end = source.indexOf('// Whether there is something', start);
  assert.ok(start >= 0 && end > start);
  const calls = [];
  const fake = { status: signal => new Promise(resolve => calls.push({ signal, resolve })) };
  const data = { state: null, loadError: '' };
  const ui = { page: 'wiki', tab: '', done: false, pinned: '' };
  const document = { hidden: false };
  const functions = new Function('api', 'data', 'ui', 'personSpecs', 'selectedPage', 'answerSpecs', 'selectedTab', 'pages', 't', 'document',
    source.slice(start, end).replace(/^export /gm, '') + '\nreturn {load, loadMark, dropPendingLoads, refresh: typeof refresh === "function" ? refresh : null};'
  )(fake, data, ui, st => st.specs, (_, selection) => selection.page, () => [], () => '', ['answer', 'wiki', 'ai'], x => x, document);
  return { ...functions, calls, data, ui, document, reply: (index, sample) => calls[index].resolve({ projects: [], sample }) };
}

test('bundles background updates before fetching and keeps one trailing refresh', async () => {
  const f = fixture();
  const first = f.load();
  assert.ok(f.calls[0].signal instanceof AbortSignal);
  f.refresh(); f.refresh();
  assert.equal(f.calls.length, 1);
  f.reply(0, 1); await first;
  assert.equal(f.calls.length, 2);
  f.reply(1, 2); await new Promise(setImmediate);
  assert.equal(f.calls.length, 2);
  assert.equal(f.data.state.sample, 2);
});

test('a queued timer refresh stays paused after the page becomes hidden', async () => {
  const f = fixture();
  const first = f.load(); f.refresh();
  f.document.hidden = true;
  f.reply(0, 1); await first;
  assert.equal(f.calls.length, 1);
  f.document.hidden = false;
  const resumed = f.refresh();
  f.reply(1, 2); await resumed;
  assert.equal(f.data.state.sample, 2);
});

test('an explicit load after a save cancels the old request and cannot apply its result', async () => {
  const f = fixture();
  const old = f.load();
  const mark = f.loadMark();
  f.dropPendingLoads(mark);
  const fresh = f.load();
  assert.equal(f.calls[0].signal?.aborted, true);
  assert.equal(f.calls.length, 2);
  f.reply(1, 2); await fresh;
  f.reply(0, 1); await old;
  assert.equal(f.data.state.sample, 2);
  assert.equal(f.data.loadError, '');
});

test('cancelling knowledge stops before the next page even if fetch did not cancel its body', async () => {
  const original = globalThis.fetch;
  const controller = new AbortController();
  let calls = 0;
  try {
    globalThis.fetch = async () => {
      calls++;
      return { ok: true, json: async () => {
        if (calls === 1) { controller.abort(); return { lessons: [], next_cursor: 'next' }; }
        return { lessons: [] };
      } };
    };
    await assert.rejects(api.wiki(controller.signal), e => e.name === 'AbortError');
    assert.equal(calls, 1);
  } finally { globalThis.fetch = original; }
});
