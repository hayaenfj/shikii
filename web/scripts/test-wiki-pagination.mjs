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

test('reads a long source in parts and joins them, so the page never shows a part as the whole source', async () => {
  const original = globalThis.fetch;
  try {
    const bodies = [];
    const replies = [
      { observations: [{ id: 'o1', content: 'abc' }], observation_part: { id: 'o1', from: 0, to: 3, bytes: 6 }, next_cursor: 'c1' },
      { observations: [{ id: 'o1', content: 'def' }, { id: 'o2', content: 'short' }], observation_part: { id: 'o1', from: 3, to: 6, bytes: 6 }, source_material: [{ id: 's1' }], originals: { s1: [{ text: 'x' }] } },
    ];
    globalThis.fetch = async (url, opts) => {
      bodies.push(JSON.parse(opts.body));
      return { ok: true, json: async () => replies[bodies.length - 1] };
    };
    const result = await api.sources(['o1', 'o2', 's1']);
    assert.deepEqual(result.observations.map(o => [o.id, o.content]), [['o1', 'abcdef'], ['o2', 'short']]);
    assert.deepEqual(result.source_material.map(x => x.id), ['s1']);
    assert.equal(result.originals.s1[0].text, 'x');
    assert.equal(bodies[1].cursor, 'c1');
    assert.deepEqual(bodies[1].sources, ['o1', 'o2', 's1']);

    let asked = 0; // a reader without the guard would ask forever; stop it here so the test fails instead of hanging
    globalThis.fetch = async () => { if (++asked > 20) throw new Error('kept asking for the same part'); return { ok: true, json: async () => ({ observations: [], next_cursor: 'same' }) }; };
    await assert.rejects(api.sources(['o1']), /does not move forward/);
  } finally {
    globalThis.fetch = original;
  }
});

test('joins a long conversation original that comes in original_part pieces, so the page does not drop it (0.9.3 audit)', async () => {
  const original = globalThis.fetch;
  try {
    const json = JSON.stringify([{ trace_id: 't1', inputs: { request: 'long café text' } }]);
    const cut = 9;
    const replies = [
      { source_material: [{ id: 's1' }], originals: {}, original_part: { id: 's1', from: 0, to: cut, bytes: json.length, json: json.slice(0, cut) }, next_cursor: 'c1' },
      { source_material: [], originals: {}, original_part: { id: 's1', from: cut, to: json.length, bytes: json.length, json: json.slice(cut) } },
    ];
    let n = 0;
    globalThis.fetch = async () => ({ ok: true, json: async () => replies[n++] });
    const result = await api.sources(['s1']);
    assert.deepEqual(result.originals.s1, JSON.parse(json));
    assert.equal(result.original_part, undefined);
  } finally {
    globalThis.fetch = original;
  }
});
