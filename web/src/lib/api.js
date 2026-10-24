// shikii serve の HTTP の口(v1/...)への薄い包み。パスは相対(逆 proxy の prefix の下でも動く。index.html の base './' と同じ流儀)。誤りは本文の error(無ければ状態コード)を Error にして投げる
async function j(url, opts) {
  const r = await fetch(url, opts);
  let b;
  try { b = await r.json(); } catch (e) { throw new Error(r.ok ? e.message : String(r.status)); } // 逆 proxy の HTML など JSON でない応答は状態コードで伝える
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
      if (seen.has(cursor)) throw new Error('知識の続きが前進しません');
      seen.add(cursor);
      const page = await j('v1/wiki?cursor=' + encodeURIComponent(cursor));
      result.lessons = [...(result.lessons || []), ...(page.lessons || [])];
      result.notebook_issues = [...(result.notebook_issues || []), ...(page.notebook_issues || [])];
      cursor = page.next_cursor;
    }
    delete result.next_cursor;
    return result;
  },
  label: body => post('v1/label', body),
  redo: body => post('v1/redo', body),
  tuneRun: body => post('v1/tune/run', body),
  tuneAdopt: body => post('v1/tune/adopt', body),
  tuneVariant: body => post('v1/tune/variant', body),
  wikiClaim: body => post('v1/wiki/claim', body),
  notebook: body => post('v1/wiki/notebook', body),
  // 作業記録(0.8.1): 1 件の口。画像は <img> がそのまま読み、文字は text で取る
  handoffFile: p => 'v1/handoff/file?path=' + encodeURIComponent(p),
  handoffText: async p => {
    const r = await fetch('v1/handoff/file?path=' + encodeURIComponent(p));
    if (!r.ok) { let m = String(r.status); try { m = (await r.json()).error || m; } catch (e) {} throw new Error(m); }
    return r.text();
  },
};
