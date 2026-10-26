// A thin wrapper around shikii serve's HTTP entry points (v1/...). Paths are relative (so it works under a reverse proxy's prefix, the same convention as index.html's base './'). Errors are thrown as an Error built from the body's error (or the status code if there is none)
async function j(url, opts) {
  const r = await fetch(url, opts);
  let b;
  try { b = await r.json(); } catch (e) { throw new Error(r.ok ? e.message : String(r.status)); } // a non-JSON response such as a reverse proxy's HTML is reported by its status code
  if (!r.ok) throw new Error(b.error || r.status);
  return b;
}

const post = (url, body) => j(url, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });

export const api = {
  status: () => j('v1/status'),
  queue: id => j('v1/queue?spec=' + encodeURIComponent(id) + '&limit=200'),
  answered: id => j('v1/answered?spec=' + encodeURIComponent(id) + '&limit=200'),
  tune: id => j('v1/tune?spec=' + encodeURIComponent(id)),
  wiki: async () => {
    const result = await j('v1/wiki');
    let cursor = result.next_cursor;
    const seen = new Set();
    while (cursor) {
      if (seen.has(cursor)) throw new Error('the next page of knowledge does not move forward');
      seen.add(cursor);
      const page = await j('v1/wiki?cursor=' + encodeURIComponent(cursor));
      result.lessons = [...(result.lessons || []), ...(page.lessons || [])];
      result.notebook_issues = [...(result.notebook_issues || []), ...(page.notebook_issues || [])];
      cursor = page.next_cursor;
    }
    delete result.next_cursor;
    return result;
  },
  // Sources (0.9.2): a long captured source comes in parts (observation_part) with next_cursor; join the parts by id so a part is never shown as the whole source
  sources: async ids => {
    const result = await post('v1/wiki/notebook', { operation: 'query', sources: ids });
    const observations = new Map((result.observations || []).map(o => [o.id, { ...o }]));
    // A long conversation's originals (0.9.3) come as pieces of their JSON (original_part); join them in order and parse once the last piece is in
    const pieces = new Map();
    const joinOriginal = page => {
      const part = page.original_part;
      if (!part) return;
      const text = (pieces.get(part.id) || '') + part.json;
      if (part.to < part.bytes) { pieces.set(part.id, text); return; }
      pieces.delete(part.id);
      result.originals = { ...(result.originals || {}), [part.id]: JSON.parse(text) };
    };
    joinOriginal(result);
    let cursor = result.next_cursor;
    const seen = new Set();
    while (cursor) {
      if (seen.has(cursor)) throw new Error('the next part of the source does not move forward');
      seen.add(cursor);
      const page = await post('v1/wiki/notebook', { operation: 'query', sources: ids, cursor });
      for (const o of page.observations || []) {
        const had = observations.get(o.id);
        if (had) had.content += o.content;
        else observations.set(o.id, { ...o });
      }
      result.source_material = [...(result.source_material || []), ...(page.source_material || [])];
      result.originals = { ...(result.originals || {}), ...(page.originals || {}) };
      joinOriginal(page);
      cursor = page.next_cursor;
    }
    result.observations = [...observations.values()];
    delete result.next_cursor;
    delete result.observation_part;
    delete result.original_part;
    if (pieces.size) throw new Error('the long original of a conversation ended before its last part');
    return result;
  },
  label: body => post('v1/label', body),
  redo: body => post('v1/redo', body),
  tuneRun: body => post('v1/tune/run', body),
  tuneAdopt: body => post('v1/tune/adopt', body),
  tuneReview: body => post('v1/tune/review', body),
  tuneVariant: body => post('v1/tune/variant', body),
  wikiClaim: body => post('v1/wiki/claim', body),
  notebook: body => post('v1/wiki/notebook', body),
  // Work records (0.8.1): the entry point for one item. Images are read directly by <img>, and text is fetched as text
  handoffFile: p => 'v1/handoff/file?path=' + encodeURIComponent(p),
  handoffText: async p => {
    const r = await fetch('v1/handoff/file?path=' + encodeURIComponent(p));
    if (!r.ok) { let m = String(r.status); try { m = (await r.json()).error || m; } catch (e) {} throw new Error(m); }
    return r.text();
  },
};
