import test from 'node:test';
import assert from 'node:assert/strict';
import { api } from '../src/lib/api.js';

test('fetches the rest of the knowledge list, and does not treat a partial list as complete on failure', async () => {
  const original = globalThis.fetch;
  try {
    const calls = [];
    globalThis.fetch = async url => {
      calls.push(url);
      return { ok: true, json: async () => calls.length === 1
        ? { lessons: [{ id: 'one' }], next_cursor: 'next token' }
        : { lessons: [{ id: 'two' }] } };
    };
    const result = await api.wiki();
    assert.deepEqual(result.lessons.map(x => x.id), ['one', 'two']);
    assert.equal(calls[1], 'v1/wiki?cursor=next%20token');

    let n = 0;
    globalThis.fetch = async () => ({ ok: true, json: async () => {
      if (++n === 1) return { lessons: [{ id: 'partial' }], next_cursor: 'next' };
      throw new Error('page unavailable');
    } });
    await assert.rejects(api.wiki(), /page unavailable/);

    globalThis.fetch = async () => ({ ok: true, json: async () => ({ lessons: [], next_cursor: 'repeated' }) });
    await assert.rejects(api.wiki(), /does not move forward/);
  } finally {
    globalThis.fetch = original;
  }
});
