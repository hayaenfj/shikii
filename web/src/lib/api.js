// shikii serve の HTTP の口(/v1/...)への薄い包み。誤りは本文の error(無ければ状態コード)を Error にして投げる
async function j(url, opts) {
  const r = await fetch(url, opts);
  const b = await r.json();
  if (!r.ok) throw new Error(b.error || r.status);
  return b;
}

const post = (url, body) => j(url, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });

export const api = {
  status: () => j('/v1/status'),
  queue: id => j('/v1/queue?spec=' + encodeURIComponent(id) + '&limit=200'),
  answered: id => j('/v1/answered?spec=' + encodeURIComponent(id) + '&limit=200'),
  tune: id => j('/v1/tune?spec=' + encodeURIComponent(id)),
  wiki: () => j('/v1/wiki'),
  label: body => post('/v1/label', body),
  redo: body => post('/v1/redo', body),
  tuneRun: body => post('/v1/tune/run', body),
  tuneAdopt: body => post('/v1/tune/adopt', body),
  wikiClaim: body => post('/v1/wiki/claim', body),
};
