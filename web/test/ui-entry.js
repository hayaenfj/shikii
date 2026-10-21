import Projects from '../src/components/Projects.svelte';
import { load, data } from '../src/lib/state.svelte.js';
// 画面の振る舞いの検査(jsdom): (1) 言語の選択を change すると、言語・文・保存値が変わる。(2) ?trace= で指された判定が列の先頭に来る。
// 文字の有無を見る Go のテストでは、onchange が外れても気づけない(監査 7 の 2)。vite の lib ビルドで 1 つに組み、scripts/check-ui.mjs が jsdom の中で run() を呼ぶ。
import { mount, unmount, flushSync } from 'svelte';
import LangPicker from '../src/components/LangPicker.svelte';
import SpecPanel from '../src/components/SpecPanel.svelte';
import Wording from '../src/components/Wording.svelte';
import Notebook from '../src/components/Notebook.svelte';
import ItemCard from '../src/components/ItemCard.svelte';
import { i18n, t } from '../src/lib/i18n.svelte.js';
import { ui } from '../src/lib/state.svelte.js';

function checkLang(target) {
  const app = mount(LangPicker, { target });
  const sel = target.querySelector('#lang');
  const before = i18n.lang;
  const next = before === 'ja' ? 'en' : 'ja';
  sel.value = next;
  sel.dispatchEvent(new window.Event('change', { bubbles: true }));
  flushSync();
  const want = next === 'ja' ? 'わかった' : 'got it';
  const ok = i18n.lang === next && t('gotIt') === want && localStorage.getItem('shikii.lang') === next;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (lang ' + before + ' -> ' + i18n.lang + ', gotIt=' + t('gotIt') + ', stored=' + localStorage.getItem('shikii.lang') + ')');
  unmount(app);
  return ok;
}

// hook の「あとで答える → …/?spec=x&trace=t」で来た人には、その 1 件が先頭に出る(無ければ元の順)
function checkPinned(target) {
  const item = (trace, text) => ({ trace_id: trace, at: '2026-10-18T00:00:00Z', band: 'ask', top: 'a', p: 0.6, model: 'm', inputs: { message: text }, inputs_hash: trace, raw: { a: 0.6, b: 0.4 }, stale: false, answerable: true, choices: [{ truth: 'a', label: 'a' }, { truth: 'b', label: 'b' }], rung: 'minimal', audit: false });
  const sp = { id: 'x', type: 'choice', question: 'q', titles: {}, questions: {}, keys: [], min_n: 20, records: 3, labels: 0, unlabeled: 3, queue: { items: [item('t1', 'FIRST'), item('t2', 'SECOND'), item('t3', 'THIRD')] }, tune: null };
  ui.pinned = 't3';
  ui.view = 'one';
  let app = mount(SpecPanel, { target, props: { sp } });
  flushSync();
  const first = target.querySelector('.item');
  const pinnedFirst = !!first && first.textContent.includes('THIRD');
  unmount(app);
  ui.pinned = '';
  app = mount(SpecPanel, { target, props: { sp } });
  flushSync();
  const plain = target.querySelector('.item');
  const originalOrder = !!plain && plain.textContent.includes('FIRST');
  unmount(app);
  const ok = pinnedFirst && originalOrder;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (pinned trace first=' + pinnedFirst + ', original order without pin=' + originalOrder + ')');
  return ok;
}

// hook の記録には仕事の印(project)が付く。2 つ以上の仕事があれば絞り込みが出て、押すとその仕事のカードだけになり、カードに印が見える
function checkProject(target) {
  const item = (trace, text, project) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], rung: 'minimal', audit: false, project });
  const sp = { id: 'done-as-asked', type: 'noul', question: 'q', titles: {}, questions: {}, keys: [], min_n: 50, records: 3, labels: 0, unlabeled: 3, queue: { items: [item('p1', 'ALPHA-1', 'alpha'), item('p2', 'ALPHA-2', 'alpha'), item('p3', 'BETA-1', 'beta')] }, tune: null };
  ui.pinned = '';
  ui.project = '';
  ui.view = 'list';
  const app = mount(SpecPanel, { target, props: { sp } });
  flushSync();
  const chips = [...target.querySelectorAll('.chips.projects button')];
  const beta = chips.find(b => b.textContent.startsWith('beta'));
  const hasChips = chips.length === 3 && !!beta;
  if (beta) beta.click();
  flushSync();
  const items = [...target.querySelectorAll('.item')];
  const onlyBeta = items.length === 1 && items[0].textContent.includes('BETA-1') && !!items[0].querySelector('.project');
  unmount(app);
  // 別の判定に切り替えて、そこに beta が無ければ絞らない(空の列にならない)
  const other = { ...sp, id: 'needs-plan', queue: { items: [item('q1', 'ALPHA-ONLY', 'alpha')] }, records: 1, unlabeled: 1 };
  const app2 = mount(SpecPanel, { target, props: { sp: other } });
  flushSync();
  const notStranded = target.querySelectorAll('.item').length === 1;
  unmount(app2);
  ui.project = '';
  ui.view = 'one';
  const ok = hasChips && onlyBeta && notStranded;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (project chips=' + chips.length + ', filtered to beta=' + onlyBeta + ', other spec not stranded=' + notStranded + ')');
  return ok;
}

// 聞き方の欄に、人が候補を書く入力がある(多択なら選択肢の説明も)。送ると v1/tune/variant に名前・質問・「id: 説明」を解いた options が届く
async function checkWording(target) {
  const calls = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url, opts) => { if (!opts || !opts.body) return { ok: true, json: async () => ({ specs: [] }) }; calls.push({ url, body: JSON.parse(opts.body) }); return { ok: true, json: async () => ({ added: "plain-words", variants: ["plain-words"] }) }; };
  const sp = { id: 'intent', type: 'choice', tune: { running: false, proposer: false, variants: [] } };
  const app = mount(Wording, { target, props: { sp } });
  flushSync();
  const form = target.querySelector('form.add-variant');
  const name = form && form.querySelector('input[name=name]');
  const question = form && form.querySelector('textarea[name=question]');
  const options = form && form.querySelector('textarea[name=options]');
  const hasForm = !!(name && question && options);
  if (hasForm) {
    name.value = 'plain-words'; name.dispatchEvent(new window.Event('input', { bubbles: true }));
    question.value = 'What does this message want?'; question.dispatchEvent(new window.Event('input', { bubbles: true }));
    options.value = 'other: greetings and thanks only\n\nnot-a-line'; options.dispatchEvent(new window.Event('input', { bubbles: true }));
    flushSync();
    form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
    await new Promise(r => setTimeout(r, 0));
  }
  unmount(app);
  // はい / いいえ の問いには選択肢の説明の入力は無い
  const noul = mount(Wording, { target, props: { sp: { id: 'same-issue', type: 'noul', tune: { running: false, proposer: false, variants: [] } } } });
  flushSync();
  const noOptionsForNoul = !target.querySelector('textarea[name=options]');
  unmount(noul);
  globalThis.fetch = realFetch;
  const call = calls.find(c => c.url.endsWith('v1/tune/variant'));
  const sent = !!call && call.body.spec === 'intent' && call.body.name === 'plain-words' && call.body.question === 'What does this message want?' && JSON.stringify(call.body.options) === JSON.stringify({ other: 'greetings and thanks only' });
  const ok = hasForm && sent && noOptionsForNoul;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (rewording form=' + hasForm + ', sent to v1/tune/variant=' + sent + ', no options box for noul=' + noOptionsForNoul + ')');
  return ok;
}

export async function run() {
  const target = document.getElementById('app');
  const contextOK = checkTurnContext(target);
  const binaryOK = await checkBinaryPrediction(target);
  const ok = contextOK && binaryOK && checkLang(target) && checkPinned(target) && checkProject(target) && await checkWording(target) && await checkNotebook(target) && checkMemoryEffect(target) && checkCorrection(target) && await checkReview(target) && checkFullText(target) && await checkProjectEntrances(target);
  if (!ok) process.exit(1);
}

function checkCorrection(target) {
  const correction = { source_id: 'source', quote: 'まだ英語が残っています <img src=x>', action: '案内の全文を確認する' };
  const effect = { window: 256, stats: {}, corrections: { delivered: 7, followed: 2, repeated: 1, not_applicable: 1, unknown: 1, pending: 2 }, recent: [{ source_id: 'turn', request: '案内を直す', quote: { key: 'lesson:one:old', correction }, assessment: { result: 'unknown', correction_result: 'repeated', decision: '一部だけ確認した', evidence: '同じ箇所への指摘を原文で確認した' } }] };
  const app = mount(Notebook, { target, props: { wiki: { memory_effect: effect, lessons: [{ id: 'lesson', title: '案内の確認', lesson: '全文を見る', applies_to: '案内の更新', correction, sources: [] }] } } });
  flushSync();
  const count = key => target.querySelector('[data-correction="' + key + '"]')?.textContent;
  const counts = count('delivered')?.includes('7') && count('followed')?.includes('2') && count('repeated')?.includes('1') && count('pending')?.includes('2');
  const chain = target.textContent.includes(correction.quote) && target.textContent.includes(correction.action) && target.textContent.includes('同じ箇所への指摘を原文で確認した') && !!target.querySelector('[data-correction-result="repeated"]');
  const inert = !target.querySelector('img');
  unmount(app);
  const ok = counts && chain && inert;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (correction counts=' + counts + ', quote action and outcome=' + chain + ', inert correction=' + inert + ')');
  return ok;
}

function checkMemoryEffect(target) {
  const effect = { window: 256, stats: { delivered: 9, helpful: 2, unused: 1, harmful: 1, unknown: 2, pending: 3, awaiting_feedback: 1 }, recent: [{ source_id: 'turn', request: '導入の確認', quote: { key: 'lesson:one:old', revision: 'old', lesson: '元の教訓 <img src=x onerror=alert(1)>' }, assessment: { result: 'harmful', decision: '古い手順に従って失敗した', evidence: '合成の検査で不一致を確認' } }] };
  let app = mount(Notebook, { target, props: { wiki: { memory_effect: effect } } });
  flushSync();
  const section = target.querySelector('.memory-effect');
  const counts = section?.querySelector('[data-memory="delivered"]')?.textContent.includes('9') && section?.querySelector('[data-memory="helpful"]')?.textContent.includes('2') && section?.querySelector('[data-memory="pending"]')?.textContent.includes('3');
  const evidence = section?.textContent.includes('古い手順に従って失敗した') && section?.textContent.includes('合成の検査で不一致を確認') && section?.textContent.includes('old');
  const inert = section?.textContent.includes('<img src=x onerror=alert(1)>') && !section?.querySelector('img');
  unmount(app);
  app = mount(Notebook, { target, props: { wiki: { memory_effect_error: true } } });
  flushSync();
  const failed = !!target.querySelector('.memory-effect .audit') && !target.querySelector('[data-memory="delivered"]');
  unmount(app);
  const ok = counts && evidence && inert && failed;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (memory counts=' + counts + ', decision and evidence=' + evidence + ', inert quote=' + inert + ', read error stays visible=' + failed + ')');
  return ok;
}

function checkFullText(target) {
  const prefix = '確認した文章。'.repeat(100);
  const end = '最後まで読めます <img src=x onerror=alert(1)>';
  const it = { trace_id: 'full-text', band: 'ask', inputs: { summary: prefix }, original_inputs: { summary: prefix + end }, text_status: {}, answerable: true, choices: [] };
  const sp = { id: 'done-as-asked' };
  let app = mount(ItemCard, { target, props: { sp, it } });
  flushSync();
  const complete = target.textContent.includes(end) && !target.querySelector('img') && !target.querySelector('.text-limit');
  unmount(app);
  app = mount(ItemCard, { target, props: { sp, it: { ...it, original_inputs: {}, text_status: { summary: 'legacy' } } } });
  flushSync();
  const legacy = !!target.querySelector('.text-limit') && !target.textContent.includes(end);
  unmount(app);
  const ok = complete && legacy;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (full reply end=' + complete + ', old storage limit explained=' + legacy + ')');
  return ok;
}

async function checkReview(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, opts) => {
    if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) });
    return { ok: true, json: async () => opts?.body ? { truth: 'no' } : { specs: [] } };
  };
  const item = { trace_id: 'review-1', band: 'ask', top: 'yes', p: .9, at: '2026-10-20T00:00:00Z', audit: true, review_reason: 'random', answerable: true, inputs: { request: '起動手順を日本語にして', summary: '直しました' }, choices: [{ truth: 'yes', label: '完了' }, { truth: 'no', label: '未完了' }], review: { truth: 'yes', reason: '<img src=x onerror=alert(1)> 仮採点の根拠', source_material: { reply: '英語の手順が残っています' } } };
  const sp = { id: 'done-as-asked', queue: { items: [item] }, labels: 0, unlabeled: 1 };
  const app = mount(ItemCard, { target, props: { sp, it: item } });
  flushSync();
  const originalReply = target.textContent.includes('英語の手順が残っています');
  const unprimed = !target.querySelector('.guess') && !target.querySelector('button.top') && !target.querySelector('.review-evidence[open]');
  const evidence = target.querySelector('.review-evidence')?.textContent.includes('仮採点の根拠') && !target.querySelector('img');
  target.querySelector('button[data-truth="no"]')?.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  const sent = calls.some(call => call.url.endsWith('v1/label') && call.body.truth === 'no' && call.body.source === 'audit' && call.body.trace_id === item.trace_id);
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = originalReply && unprimed && evidence && sent;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (review original reply=' + originalReply + ', unprimed choices=' + unprimed + ', inert evidence=' + evidence + ', human audit sent=' + sent + ')');
  return ok;
}

async function checkNotebook(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, opts) => {
    calls.push({ url, body: JSON.parse(opts.body) });
    return { ok: true, json: async () => ({ source_material: [{ id: '0000000000000001', request: 'README installation', reply: '<img src=x onerror=alert(1)> missing startup steps' }] }) };
  };
  const wiki = { notebook_project: 'alpha', pending_sources_count: 1, lessons: [{ id: 'a', title: '<img src=x onerror=alert(1)>', lesson: 'Include model startup steps', applies_to: 'README installation', sources: [{ id: '0000000000000001', hash: 'h' }] }] };
  const app = mount(Notebook, { target, props: { wiki } });
  flushSync();
  const hasLesson = target.textContent.includes('Include model startup steps') && target.textContent.includes('alpha');
  const button = target.querySelector('button');
  if (button) button.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  flushSync();
  const readSource = target.textContent.includes('missing startup steps') && calls[0]?.body.operation === 'query' && calls[0]?.body.sources?.[0] === '0000000000000001';
  const inertText = !target.querySelector('img') && target.textContent.includes('<img src=x onerror=alert(1)>');
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = hasLesson && readSource && inertText;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (notebook lesson=' + hasLesson + ', source lookup=' + readSource + ', quoted HTML is text=' + inertText + ')');
  return ok;
}

async function checkProjectEntrances(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async url => { calls.push(url); return { ok: true, json: async () => ({ projects: [{ name: '<img src=x>', path: '/work/alpha', url: '/p/abc/' }], specs: [] }) }; };

  await load();
  const app = mount(Projects, { target, props: { projects: data.state.projects } });
  flushSync();
  const scopedLink = target.querySelector('a')?.getAttribute('href') === '/p/abc/';
  const namesOnly = target.textContent.includes('/work/alpha') && !target.querySelector('img');
  const noQueues = calls.length === 1 && calls[0] === 'v1/status';
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = scopedLink && namesOnly && noQueues;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (project entrance=' + scopedLink + ', escaped project names=' + namesOnly + ', no combined queue=' + noQueues + ')');
  return ok;
}

// この依頼より前のAIの返答を、今回の着手前の判定対象と取り違えない。
function checkTurnContext(target) {
  i18n.lang = 'ja';
  const make = (id, inputs) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 1, labels: 0, unlabeled: 1, tune: null, queue: { items: [{ trace_id: id, answerable: true, inputs, original_inputs: { previous: '前の仕事は完了しました。末尾 <img src=x>' }, text_status: {}, choices: [] }] } });
  ui.done = false; ui.filter = 'all'; ui.project = ''; ui.view = 'one'; ui.pinned = '';
  let app = mount(SpecPanel, { target, props: { sp: make('needs-plan', { previous: '前の仕事は完了しました。', request: '今回の新しい依頼' }) } });
  flushSync();
  const first = target.querySelector('.text [data-input]');
  const previous = target.querySelector('details.previous-context');
  const before = first?.dataset.input === 'request' && first.textContent.includes('今回の依頼') && first.textContent.includes('今回の新しい依頼') && target.querySelector('.decision-phase')?.textContent.includes('着手前');
  const folded = !!previous && !previous.open && previous.querySelector('summary')?.textContent.includes('この依頼より前のAIの返答') && previous.textContent.includes('末尾 <img src=x>') && !target.querySelector('img');
  if (previous) previous.open = true;
  const readable = previous?.open && previous.textContent.includes('前の仕事は完了しました。末尾');
  unmount(app);
  app = mount(SpecPanel, { target, props: { sp: make('done-as-asked', { files: 'changed.go', request: '今回の新しい依頼', summary: '今回の作業を終えました。' }) } });
  flushSync();
  const fields = [...target.querySelectorAll('.text [data-input]')];
  const after = fields[0]?.dataset.input === 'request' && fields[1]?.dataset.input === 'summary' && fields[1].textContent.includes('今回の最後の返答') && target.querySelector('.decision-phase')?.textContent.includes('作業後') && !target.querySelector('details.previous-context');
  unmount(app);
  const ok = before && folded && readable && after;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (着手前の対象=' + before + ', 前の完了返答を折りたたむ=' + folded + ', 原文を読める=' + readable + ', 作業後の対象=' + after + ')');
  return ok;
}

// noulのtop=yesは確率の対象。46%なら画面の予想と「AIと同じ」はno/54%になる。
async function checkBinaryPrediction(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push(JSON.parse(opts.body)); return { ok: true, json: async () => url.endsWith('v1/redo') ? ({ before: { top: 'yes', p: .46, band: 'discard' }, after: { top: 'yes', p: .3, band: 'discard' }, truth: 'no', correct_after: false }) : ({ truth: 'no' }) }; };
  const it = { trace_id: 'binary', answerable: true, band: 'discard', top: 'yes', p: .46, inputs: { request: '今回の依頼' }, choices: [{ truth: 'yes', label: '先に合わせる' }, { truth: 'no', label: 'そのまま進めてよい' }] };
  const sp = { id: 'needs-plan', type: 'noul', queue: { items: [it] }, unlabeled: 1, labels: 0 };
  let app = mount(ItemCard, { target, props: { sp, it } });
  flushSync();
  const guess = target.querySelector('.guess')?.textContent;
  const negative = guess?.includes('そのまま進めてよい') && guess.includes('54%') && target.querySelector('button.top')?.dataset.truth === 'no';
  target.querySelector('.btns button.adv')?.click();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const redone = target.querySelector('.redo-out')?.textContent.includes('そのまま進めてよい 70%') && target.querySelector('.redo-out')?.textContent.includes('あなたの答えと一致しました');
  target.querySelector('button.top')?.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  const submitted = calls.some(c => c.trace_id === 'binary' && c.truth === 'no');
  unmount(app);
  app = mount(ItemCard, { target, props: { sp: { id: 'custom', type: 'choice' }, it: { ...it, top: 'yes' } } });
  flushSync();
  const unchangedChoice = target.querySelector('button.top')?.dataset.truth === 'yes' && target.querySelector('.guess')?.textContent.includes('46%');
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = negative && submitted && unchangedChoice && redone;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (二択の予想=' + negative + ', 同じ答えの保存=' + submitted + ', 多択の予想=' + unchangedChoice + ', 再判定の一致=' + redone + ')');
  return ok;
}
