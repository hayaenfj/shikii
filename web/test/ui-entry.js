import Knowledge from '../src/components/Knowledge.svelte';
import Projects from '../src/components/Projects.svelte';
import { load, data } from '../src/lib/state.svelte.js';
// 画面の振る舞いの検査(jsdom): (1) 言語の選択を change すると、言語・文・保存値が変わる。(2) ?trace= で指された判定が列の先頭に来る。
// 文字の有無を見る Go のテストでは、onchange が外れても気づけない(監査 7 の 2)。vite の lib ビルドで 1 つに組み、scripts/check-ui.mjs が jsdom の中で run() を呼ぶ。
import { mount, unmount, flushSync } from 'svelte';
import LangPicker from '../src/components/LangPicker.svelte';
import SpecPanel from '../src/components/SpecPanel.svelte';
import Wording from '../src/components/Wording.svelte';
import Notebook from '../src/components/Notebook.svelte';
import Handoff from '../src/components/Handoff.svelte';
import Lessons from '../src/components/Lessons.svelte';
import ItemCard from '../src/components/ItemCard.svelte';
import Answer from '../src/components/Answer.svelte';
import AiView from '../src/components/AiView.svelte';
import App from '../src/App.svelte';
import { i18n, t } from '../src/lib/i18n.svelte.js';
import { ui, skipped } from '../src/lib/state.svelte.js';
import Toast from '../src/components/Toast.svelte';
import { toastState } from '../src/lib/toast.svelte.js';

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
  const sp = { id: 'x', type: 'choice', question: 'q', titles: {}, questions: {}, keys: [], min_n: 20, records: 3, labels: 0, unlabeled: 3, waiting_for_person: 3, person_answers: true, queue: { items: [item('t1', 'FIRST'), item('t2', 'SECOND'), item('t3', 'THIRD')] }, tune: null };
  ui.pinned = 't3';
  let app = mount(Answer, { target, props: { st: { specs: [sp] } } });
  flushSync();
  const first = target.querySelector('.item');
  const pinnedFirst = !!first && first.textContent.includes('THIRD');
  unmount(app);
  ui.pinned = '';
  app = mount(Answer, { target, props: { st: { specs: [sp] } } });
  flushSync();
  const plain = target.querySelector('.item');
  const originalOrder = !!plain && plain.textContent.includes('FIRST');
  unmount(app);
  const ok = pinnedFirst && originalOrder;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (pinned trace first=' + pinnedFirst + ', original order without pin=' + originalOrder + ')');
  return ok;
}

// 0.9.1: 答える画面は、人が答える問いの待ちを 1 列にまとめ、1 枚ずつ出す。札の先頭とボタンの上にその問いの文、仕事の印。
// AI だけが答える問いと、人を待たない記録は出さない。スキップすると次の問いの札になり、残りの数は全部の問いの合計
function checkAnswerMerge(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const item = (trace, text, project, waits = true) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: waits, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false, project });
  const mk = (id, q, items, person = true) => ({ id, type: 'noul', titles: {}, questions: { ja: q }, keys: [], min_n: 50, records: 3, labels: 0, unlabeled: items.length, waiting_for_person: person ? items.filter(i => i.waits_for_person).length : 0, person_answers: person, queue: { items }, tune: null });
  const specs = [mk('wiki-conflict', 'AI の問い', [item('w1', 'MACHINE', 'alpha')], false), mk('needs-plan', '始める前に聞くべきだったか', [item('n1', 'NEEDS-1', 'alpha'), item('n0', 'NOT-WAITING', 'alpha', false)]), mk('done-as-asked', '頼まれたことを終えたか', [item('d1', 'DONE-1', 'beta'), item('d2', 'DONE-2', 'beta')])];
  ui.pinned = ''; skipped.clear();
  const app = mount(Answer, { target, props: { st: { specs } } });
  flushSync();
  const cards = () => [...target.querySelectorAll('.item')];
  const one = cards().length === 1 && cards()[0].textContent.includes('NEEDS-1') && !target.textContent.includes('MACHINE') && !target.textContent.includes('NOT-WAITING');
  const asked = cards()[0]?.querySelector('.card-question')?.textContent === '始める前に聞くべきだったか' && target.querySelector('.answer-heading')?.textContent.includes('始める前に聞くべきだったか') && !!cards()[0]?.querySelector('.project');
  const left = (target.querySelector('[data-answer-left]')?.textContent || '').includes('3');
  target.querySelector('.btns button.skip')?.click(); flushSync();
  const next = cards().length === 1 && cards()[0].textContent.includes('DONE-1') && cards()[0].querySelector('.card-question')?.textContent === '頼まれたことを終えたか';
  unmount(app); skipped.clear(); i18n.lang = lang;
  const ok = one && asked && left && next;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (答える画面: 1 枚だけで AI の問いと待たない記録は出さない=' + one + ', 札とボタンの上に問い・仕事の印=' + asked + ', 残りは合計=' + left + ', スキップで次の問いの札=' + next + ')');
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

// 効き目の要約(0.9.1。0.7.4 では先頭の帯): 上の「答える」に人待ちの合計が出る。AI の様子に、届けた教訓・作業の手順・止める検査と最後の結果、
// 問いごとの学習の進みが 1 行ずつ出る。答える画面には AI の内側の数を出さない
async function checkSummary(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const summary = { person: { records: 20, waits: 2, recent_records: 20, recent_waits: 2, by_person: 1, by_result: 13, per_thousand: 100, recent_per_thousand: 100 },
    memory: { delivered: 4, helpful: 1, unused: 2, harmful: 0, unknown: 1, pending: 0, awaiting_feedback: 0, recent_delivered: 4, recent_helpful: 1, corrections: { delivered: 0 } },
    guards: { total: 3, enabled: 3, stopped: 1, fixed: 1, rituals: { plan: { asked: 5, done: 2, fixed: 1 }, reflect: { asked: 4, done: 4, fixed: 0 } }, items: [{ id: 'changelog-pair', kind: 'paired-files', title: '変更履歴は英語と日本語を揃える', enabled: true, last: { pass: false, at: '2026-10-23T00:00:00Z', reason: 'CHANGELOG_ja.md が変わっていない' } }] } };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs(), summary, model: { name: 'TypeSafe Jev', endpoint: 'https://api.typesafe.ai' } } : { items: [] } });
  Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const badge = target.querySelector('[data-page="answer"] .badge')?.textContent === '4';
  const quietAnswer = !target.querySelector('#summary') && !target.textContent.includes('TypeSafe Jev') && !target.textContent.includes('学習中');
  target.querySelector('[data-page="ai"]')?.click(); flushSync(); await settle(); await settle();
  const box = target.querySelector('#summary');
  const body = box?.querySelector('.summary-guards code')?.textContent === 'changelog-pair' && (box?.textContent || '').includes('CHANGELOG_ja.md が変わっていない') && (box?.textContent || '').includes('TypeSafe Jev') && (box.querySelector('[data-rituals]')?.textContent || '').includes('計画の整理 2 / 5');
  const specs = target.querySelectorAll('[data-ai-specs] [data-tab]').length === 4 && (target.querySelector('[data-ai-specs]')?.textContent || '').includes('学習中 0 / 50');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = badge && quietAnswer && body && specs;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (効き目の要約: 答えるに人待ちの合計=' + badge + ', 答える画面に内側の数を出さない=' + quietAnswer + ', AI の様子に検査と最後の結果=' + body + ', 問いごとの進み=' + specs + ')');
  return ok;
}


// 0.8.0: 人が答えない判定(wiki の矛盾)は、確認待ちの印を出さず、カードに答えのボタンを出さず、案内を 1 行出す。キー操作も押さない。
async function checkMachineOnlySpec(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const specs = loadingSpecs().concat([{ id: 'wiki-conflict', type: 'noul', titles: { ja: 'wiki の矛盾' }, questions: {}, keys: [], min_n: 50, records: 3, labels: 0, unlabeled: 3, waiting_for_person: 0, awaiting_results: 3, person_answers: false, truth_source: 'results' }]);
  const item = { trace_id: 'wc1', at: '2026-10-23T00:00:00Z', band: 'discard', top: 'no', p: 0.8, model: 'm', inputs: { lesson: 'a', new_lesson: 'b' }, inputs_hash: 'wc1', raw: { yes: 0.2, no: 0.8 }, stale: false, answerable: true, choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], rung: 'minimal', audit: false };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs } : url.startsWith('v1/queue?spec=wiki-conflict') ? { items: [item] } : { items: [] } });
  Object.assign(ui, { page: 'ai', tab: 'wiki-conflict', done: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const card = target.querySelector('.item');
  const buttons = target.querySelectorAll('.item .btns button').length;
  const notice = (target.querySelector('.machine-only')?.textContent || '').includes('結果');
  const badge = !!target.querySelector('[data-tab="wiki-conflict"] .badge');
  // 列: 「未回答」の列が無く、「振り返りで確定する」の列が選ばれている(0.8.0)
  const lanes = !target.querySelector('.answer-tabs [data-lane="you"]') && target.querySelector('.answer-tabs [data-lane="results"]')?.classList.contains('active') && (target.querySelector('.lane-results')?.textContent || '').includes('振り返り');
  let clicked = 0;
  for (const b of target.querySelectorAll('.item button')) b.addEventListener('click', () => { clicked++; });
  document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); flushSync();
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = !!card && buttons === 0 && notice && !badge && clicked === 0 && !!lanes;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (人が答えない判定: カード=' + !!card + ', 答えのボタン=' + buttons + ', 案内=' + notice + ', 確認待ちの印なし=' + !badge + ', キーで押さない=' + (clicked === 0) + ', 列は振り返りで確定する=' + !!lanes + ')');
  return ok;
}


// 0.8.0: 知識の欄は「AI が蓄積しているもの」を関連リンクで繋がる群に階層で見せる。群を開くと記事が並び、題を押すと本文へ。繋がりの無い記事は「単独の記事」
async function checkNotebookTree(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const mk = (id, title, related = []) => ({ id, revision: 'r' + id, title, lesson: title + 'の決まり', applies_to: title + 'の時', sources: [{ id: 'src' + id }], related, status: 'hypothesis' });
  // a2 は 2 本に指される(同数の時の題の順は locale 依存なので避ける)
  const lessons = [mk('a1', '公開の手順', ['a2']), mk('a2', '監査'), mk('a3', '変更履歴', ['a2']), mk('b1', 'コミットの説明', ['b2']), mk('b2', 'コミットの作者'), mk('c1', '単独')];
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons, sources: [] } : url.startsWith('v1/wiki/notebook') ? { lessons: [lessons[1]] } : { items: [] } });
  Object.assign(ui, { page: 'wiki', done: false, results: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const groups = [...target.querySelectorAll('[data-tree-group]')].map(g => g.dataset.treeGroup);
  const grouped = groups.length === 3 && groups[0] === 'a2' && groups[1] === 'b2' && groups[2] === 'single'; // 大きい群から。群の名はいちばん指されている記事
  const closed = !target.querySelector('[data-tree-page]');
  target.querySelector('[data-tree-group="a2"] .tree-toggle')?.click(); flushSync();
  const pages = [...target.querySelectorAll('[data-tree-group="a2"] [data-tree-page]')].map(b => b.dataset.treePage);
  const opened = pages.length === 3 && pages.includes('a1') && pages.includes('a3');
  const how = (target.querySelector('[data-wiki-tree] .k')?.textContent || '').includes('6');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = grouped && closed && opened && how;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (知識の階層: 群=' + grouped + ' ' + JSON.stringify(groups) + ', 畳んで始まる=' + closed + ', 開くと記事=' + opened + ', 概要=' + how + ')');
  return ok;
}


// 監査 R5-3: 人が答える問いの列。上の「答える」の印は人待ちの合計(未回答の合計ではない)で、AI だけの問いは数えない。
// AI の様子で最初に開く問いは人待ちのある問い。印は人待ちの数。「未回答(あなた)」にはボタン付きのカード、「次の依頼・振り返りで確定する」には読むだけのカード。
async function checkLanesForPersonSpec(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const mk = (id, unlabeled, waiting, person) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 5, labels: 0, unlabeled, waiting_for_person: waiting, awaiting_results: unlabeled - waiting, person_answers: person, truth_source: person ? 'person' : 'results' });
  const specs = [mk('wiki-conflict', 4, 0, false), mk('prompt-kind', 2, 1, true), mk('needs-plan', 3, 2, true)];
  const item = (id, waits, audit) => ({ trace_id: id, at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: id, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, audit, waits_for_person: waits, choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], rung: 'minimal' });
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs, summary: { person: {}, memory: {}, guards: {} }, model: { name: 'TypeSafe Jev' } } : url.startsWith('v1/queue?spec=prompt-kind') ? { items: [item('r1', false, false), item('y1', true, true)] } : { items: [] } });
  Object.assign(ui, { page: 'ai', tab: '', done: false, results: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const you = target.querySelector('[data-page="answer"] .badge')?.textContent === '3';
  const first = !!target.querySelector('[data-tab="prompt-kind"].active');
  const badges = [...target.querySelectorAll('[data-ai-specs] .badge')].map(b => b.textContent).join(',') === '1,2';
  const youCards = target.querySelectorAll('.item').length === 1 && target.querySelectorAll('.item .btns button').length > 0;
  target.querySelector('.answer-tabs [data-lane="results"]')?.click(); flushSync(); await settle();
  // 人が正解を付ける問い: 「AI が決めた(答えなくてよい)」の列。読めて、違うと思えば直せる(ボタンは残る)
  const resultsCards = target.querySelectorAll('.item').length === 1 && target.querySelectorAll('.item .btns button').length > 0 && (target.querySelector('.lane-results')?.textContent || '').includes('AI が確信') && (target.querySelector('.answer-tabs [data-lane="results"]')?.textContent || '').includes('AI が決めた');
  let clicked = 0;
  for (const b of target.querySelectorAll('.item button')) b.addEventListener('click', () => { clicked++; });
  document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); flushSync();
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = you && first && badges && youCards && resultsCards && clicked === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (人が答える問いの列: 答えるの印は人待ちの合計=' + you + ', 最初は人待ちの問い=' + first + ', 印=' + badges + ', 未回答にボタン=' + youCards + ', AI が決めた列=' + resultsCards + ', キーで押さない=' + (clicked === 0) + ')');
  return ok;
}

// 監査 R2: キー操作(1〜9 / Enter / →)は答える画面でだけ効く。知識の欄では最初の .item のボタン(関連する知識・主張にする)を、
// AI の様子では回答済みの札のボタンを押さない。
async function checkKeysOnlyOnDecisions(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  // 出典のある教訓: 最初の .item に「原文を見る」のボタンが出る(押す物が無い状態で「押されない」を見ても検査にならない。監査 R3)
  const lesson = { id: 'l1', revision: 'r1', title: '教訓', lesson: '教訓の本文', applies_to: '条件', sources: [{ id: 'src1234567890ab' }], related: [], status: 'hypothesis' };
  const answered = { trace_id: 'done1', at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: 'done1', raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, truth: 'yes', choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false };
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const press = () => { document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); document.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true })); flushSync(); };
  // 知識の欄
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [lesson], sources: [] } : { items: [] } });
  Object.assign(ui, { page: 'wiki', done: false, pinned: '' });
  let app = mount(App, { target }); await settle(); await settle();
  let buttons = target.querySelectorAll('.item .btns button');
  let clicked = 0;
  for (const b of buttons) b.addEventListener('click', () => { clicked++; });
  press();
  const wikiButtons = buttons.length, wikiClicked = clicked;
  unmount(app);
  // 回答済みの列
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url.startsWith('v1/answered') ? { items: [answered] } : { items: [] } });
  Object.assign(ui, { page: 'ai', tab: 'needs-plan', done: true, pinned: '' });
  app = mount(App, { target }); await settle(); await settle();
  buttons = target.querySelectorAll('.item .btns button');
  clicked = 0;
  for (const b of buttons) b.addEventListener('click', () => { clicked++; });
  press();
  const doneButtons = buttons.length, doneClicked = clicked;
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = wikiButtons > 0 && wikiClicked === 0 && doneButtons > 0 && doneClicked === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (キー操作は答える画面だけ: 知識の欄 buttons=' + wikiButtons + ' clicked=' + wikiClicked + ', 回答済み buttons=' + doneButtons + ' clicked=' + doneClicked + ')');
  return ok;
}


// 監査 0.9.1(A1・M8・M9・M10): 答えると次の札に進み、「残り」と上の「答える」の印が減る。待ちが 0 なら空の表示、列を読む前は読み込み中
async function checkAnswerAdvances(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const item = (trace, text) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false });
  const spec = (id, n) => ({ id, type: 'noul', titles: {}, questions: { ja: id }, keys: [], min_n: 50, records: n, labels: 0, unlabeled: n, waiting_for_person: n, person_answers: true });
  globalThis.fetch = async (url, opts) => ({ ok: true, json: async () => url === 'v1/status' ? { specs: [spec('needs-plan', 2)] } : url.startsWith('v1/queue') ? { items: [item('a', 'FIRST'), item('b', 'SECOND')] } : url.startsWith('v1/tune') ? null : opts?.body ? { truth: 'yes' } : {} });
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' }); skipped.clear();
  const app = mount(App, { target }); await settle(); await settle();
  target.querySelector('.item .btns button[data-truth="yes"]')?.click(); await settle(); await settle();
  const next = !!target.querySelector('.item')?.textContent.includes('SECOND') && !target.querySelector('.item')?.textContent.includes('FIRST');
  const left = target.querySelector('[data-answer-left]')?.textContent === '残り 1 件' && target.querySelector('[data-page="answer"] .badge')?.textContent === '1';
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  // 待ちが 0 なら空の表示、列をまだ読んでいなければ読み込み中
  let a = mount(Answer, { target, props: { st: { specs: [spec('needs-plan', 0)] } } }); flushSync();
  const empty = !!target.querySelector('[data-answer-empty]') && !target.querySelector('.item');
  unmount(a);
  a = mount(Answer, { target, props: { st: { specs: [spec('needs-plan', 2)] } } }); flushSync();
  const loading = !!target.querySelector('[data-answer] [role="status"]') && !target.querySelector('[data-answer-empty]');
  unmount(a); i18n.lang = lang;
  const ok = next && left && empty && loading;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (答えると次の札=' + next + ', 残りと印が減る=' + left + ', 待ち 0 は空の表示=' + empty + ', 列の前は読み込み中=' + loading + ')');
  return ok;
}

// 監査 0.9.1(A2): 15 秒ごとの読み直しが列の応答を待っている間に答えても、その古い応答で答えた札が戻らない
async function checkAnswerNotRevertedByOldLoad(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const item = (trace, text) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false });
  const spec = { id: 'needs-plan', type: 'noul', titles: {}, questions: { ja: 'q' }, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 2, waiting_for_person: 2, person_answers: true };
  let hold = false, release;
  globalThis.fetch = async (url, opts) => {
    if (hold && url.startsWith('v1/queue')) { hold = false; return { ok: true, json: () => new Promise(r => { release = r; }) }; }
    return { ok: true, json: async () => url === 'v1/status' ? { specs: [{ ...spec }] } : url.startsWith('v1/queue') ? { items: [item('a', 'FIRST'), item('b', 'SECOND')] } : url.startsWith('v1/tune') ? null : opts?.body ? { truth: 'yes' } : {} };
  };
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' }); skipped.clear();
  const app = mount(App, { target }); await settle(); await settle();
  hold = true; const reload = load(); await settle();
  target.querySelector('.item .btns button[data-truth="yes"]')?.click(); await settle(); await settle();
  release?.({ items: [item('a', 'FIRST'), item('b', 'SECOND')] }); await reload; await settle();
  const back = !!target.querySelector('.item')?.textContent.includes('FIRST');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  const ok = !back;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (答えた札は古い読み直しで戻らない=' + !back + ')');
  return ok;
}


// 再監査 0.9.1(低-UI1): 答えを送っている間に画面を切り替えても、切り替えた画面の読み込みは捨てない(答える前から待っていた分だけ捨てる)
async function checkSwitchDuringAnswerKeepsLoad(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const item = (trace, text) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false });
  const spec = { id: 'needs-plan', type: 'noul', titles: {}, questions: { ja: 'q' }, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 2, waiting_for_person: 2, person_answers: true };
  let releaseLabel, releaseWiki;
  globalThis.fetch = async (url, opts) => {
    if (url === 'v1/label') return { ok: true, json: () => new Promise(r => { releaseLabel = r; }) };
    if (url === 'v1/wiki') return { ok: true, json: () => new Promise(r => { releaseWiki = r; }) };
    return { ok: true, json: async () => url === 'v1/status' ? { specs: [{ ...spec }] } : url.startsWith('v1/queue') ? { items: [item('a', 'FIRST'), item('b', 'SECOND')] } : url.startsWith('v1/tune') ? null : {} };
  };
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' }); skipped.clear();
  const app = mount(App, { target }); await settle(); await settle();
  target.querySelector('.item .btns button[data-truth="yes"]')?.click(); await settle();
  target.querySelector('[data-page="wiki"]')?.click(); await settle(); await settle();
  releaseLabel?.({ truth: 'yes' }); await settle(); await settle();
  releaseWiki?.({ lessons: [], notebook_issues: [] }); await settle(); await settle(); await settle();
  const shown = !!data.state?.wiki;
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  console.log((shown ? 'check-ui: ok' : 'check-ui: FAIL') + ' (答えの送信中に切り替えた画面の読み込みは捨てない=' + shown + ')');
  return shown;
}

// 再監査 0.9.1(W4・W5): 答えた後に「残り」を減らすのは、まだ答えていない、人を待つ記録だけ(答えの直しと、人を待たない記録では減らさない)
async function checkAnswerCountOnlyForNewWaits(target) {
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ ok: true, json: async () => ({ truth: 'yes' }) });
  const base = { at: '2026-10-22T00:00:00Z', band: 'ask', top: 'yes', p: .6, answerable: true, inputs: { request: '依頼' }, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  const run = async it => {
    const sp = { id: 'needs-plan', type: 'noul', queue: { items: [it] }, unlabeled: 1, labels: 0, waiting_for_person: 3 };
    const app = mount(ItemCard, { target, props: { sp, it } }); flushSync();
    target.querySelector('button[data-truth="no"]')?.click();
    await new Promise(r => setTimeout(r, 0)); flushSync();
    unmount(app);
    return sp.waiting_for_person;
  };
  const relabel = await run({ ...base, trace_id: 'w4', truth: 'yes', truth_at: base.at });
  const notWaiting = await run({ ...base, trace_id: 'w5', waits_for_person: false });
  const fresh = await run({ ...base, trace_id: 'w6', waits_for_person: true });
  globalThis.fetch = realFetch;
  const ok = relabel === 3 && notWaiting === 3 && fresh === 2;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (残りを減らすのは新しい人待ちだけ: 答えの直し=' + relabel + ', 人を待たない=' + notWaiting + ', 新しい人待ち=' + fresh + ')');
  return ok;
}


// 0.9.1: 知識の一番上に「ルール」の欄。AI が作ったルールも人が一覧で見られる(場面で絞っても全部出る)。「いつも」のルールが先頭、
// 最近 7 日に増えた・変わったルールに印。押すと本文
function checkRulesPanel(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  try { localStorage.removeItem('shikii.scene'); } catch { /* 無くてよい */ }
  const mk = (i, kind, scene) => ({ id: String(i).padStart(16, '0'), title: 'ルール ' + i, lesson: '教訓 ' + i, applies_to: 'a', kind, scene, sources: [], related: [] });
  const lessons = [mk(1, 'rule', 'コミットする時'), mk(2, 'rule', 'いつも'), mk(3, 'howto', 'コミットする時'), mk(4, 'rule', '公開する時'), mk(5, 'rule', ' Always ')]; // 英語の always と前後の空白(監査 低-4)
  const recent = new Date(Date.now() - 2 * 86400000).toISOString(), old = '2026-01-01T00:00:00Z';
  const wiki = { lessons, lesson_meta: { [lessons[0].id]: { updated: recent }, [lessons[1].id]: { updated: old }, [lessons[3].id]: { updated: old }, [lessons[4].id]: { updated: '2025-01-01T00:00:00Z' } }, inbox: [{ kind: 'harmful', lesson_id: lessons[2].id, lesson: 'x', count: 1 }] };
  const app = mount(Lessons, { target, props: { wiki } }); flushSync();
  const panel = target.querySelector('[data-rules]');
  const rows = () => [...target.querySelectorAll('[data-rules] [data-rule]')].map(b => b.dataset.rule);
  const top = !!panel && !!(panel.compareDocumentPosition(target.querySelector('[data-scenes]')) & 4) && !!(panel.compareDocumentPosition(target.querySelector('[data-inbox]')) & 4); // 受け箱より上(監査 低-3)
  const named = (panel?.textContent || '').includes('このプロジェクトで決められたルール');
  const order = rows().join(',') === [lessons[1].id, lessons[4].id, lessons[0].id, lessons[3].id].join(','); // いつも(always も)が先、その中も新しい順
  const fresh = !!target.querySelector('[data-rule="' + lessons[0].id + '"] [data-rule-new]') && !target.querySelector('[data-rule="' + lessons[3].id + '"] [data-rule-new]');
  target.querySelector('[data-scene="公開する時"]')?.click(); flushSync();
  const allWhenFiltered = rows().length === 4;
  target.querySelector('[data-rule="' + lessons[3].id + '"]')?.click(); flushSync();
  const opens = (target.querySelector('[data-lesson-detail]')?.textContent || '').includes('教訓 4');
  unmount(app); i18n.lang = lang;
  const ok = top && named && order && fresh && allWhenFiltered && opens;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (ルールの欄: 一番上=' + top + ', 名前=' + named + ', いつもが先頭=' + order + ', 最近の印=' + fresh + ', 絞っても全部=' + allWhenFiltered + ', 押すと本文=' + opens + ')');
  return ok;
}

// 最終監査 0.9.1(低-F1): 答えを送っている間に始まった読み直しの古い列で、答えた札が戻らない
async function checkReloadDuringAnswerSend(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const item = (trace, text) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false });
  const spec = { id: 'needs-plan', type: 'noul', titles: {}, questions: { ja: 'q' }, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 2, waiting_for_person: 2, person_answers: true };
  let holdQueue = false, releaseQueue, releaseLabel, labeled = false;
  globalThis.fetch = async (url, opts) => {
    if (url === 'v1/label') return { ok: true, json: () => new Promise(r => { releaseLabel = v => { labeled = true; r(v); }; }) };
    if (holdQueue && url.startsWith('v1/queue')) { holdQueue = false; return { ok: true, json: () => new Promise(r => { releaseQueue = r; }) }; }
    return { ok: true, json: async () => url === 'v1/status' ? { specs: [{ ...spec }] } : url.startsWith('v1/queue') ? { items: labeled ? [item('b', 'SECOND')] : [item('a', 'FIRST'), item('b', 'SECOND')] } : url.startsWith('v1/tune') ? null : {} };
  };
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' }); skipped.clear();
  const app = mount(App, { target }); await settle(); await settle();
  target.querySelector('.item .btns button[data-truth="yes"]')?.click(); await settle();
  holdQueue = true; const reload = load(); await settle(); await settle(); // 送っている間に読み直しが始まり、列はまだ答える前の物
  releaseLabel?.({ truth: 'yes' }); await settle(); await settle();
  releaseQueue?.({ items: [item('a', 'FIRST'), item('b', 'SECOND')] }); await reload; await settle(); await settle(); await settle();
  const back = !!target.querySelector('.item')?.textContent.includes('FIRST');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  console.log((!back ? 'check-ui: ok' : 'check-ui: FAIL') + ' (送っている間の読み直しで答えた札が戻らない=' + !back + ')');
  return !back;
}

export async function run() {
  const target = document.getElementById('app');
  const articleOK = await checkNotebookArticle(target);
  const planningOK = checkPlanningContext(target);
  const calibrationOK = checkCalibrationReport(target);
  const contextOK = checkTurnContext(target);
  const binaryOK = await checkBinaryPrediction(target);
  const navigationOK = await checkNavigation(target);
  const focusOK = checkAnswerFocus(target);
  const landingOK = await checkLanding(target);
  const evidenceOK = checkClaimEvidence(target);
  const loadingOK = await checkFocusedLoading(target);
  const orderingOK = await checkLoadOrdering();
  const lazyNavOK = await checkLazyNavigation(target);
  const summaryOK = await checkSummary(target);
  const keysOK = await checkKeysOnlyOnDecisions(target);
  const machineOnlyOK = await checkMachineOnlySpec(target);
  const treeOK = await checkNotebookTree(target);
  const lanesOK = await checkLanesForPersonSpec(target);
  const conflictsOK = await checkUtilityConflicts(target);
  const handoffOK = await checkHandoff(target);
  const lessonsOK = checkLessons(target);
  const reloadOK = await checkReloadDuringAnswerSend(target);
  const switchOK = await checkSwitchDuringAnswerKeepsLoad(target);
  const countOK = await checkAnswerCountOnlyForNewWaits(target);
  const rulesOK = checkRulesPanel(target);
  const advanceOK = await checkAnswerAdvances(target);
  const revertOK = await checkAnswerNotRevertedByOldLoad(target);
  const ok = reloadOK && rulesOK && switchOK && countOK && advanceOK && revertOK && lessonsOK && handoffOK && conflictsOK && summaryOK && keysOK && machineOnlyOK && treeOK && lanesOK && planningOK && calibrationOK && loadingOK && orderingOK && lazyNavOK && evidenceOK && contextOK && binaryOK && navigationOK && focusOK && landingOK && checkLang(target) && checkPinned(target) && checkAnswerMerge(target) && await checkWording(target) && await checkNotebook(target) && checkMemoryEffect(target) && checkCorrection(target) && await checkReview(target) && checkFullText(target) && await checkProjectEntrances(target);
  if (!ok || !articleOK) process.exit(1);
}

async function checkNotebookArticle(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  const related = { id: '0000000000000002', title: '関連する背景', lesson: '関連の要約', body: '## 理由\n\n出典に残る経緯', related: ['0000000000000001'], sources: [] };
  globalThis.fetch = async (url, opts) => { calls.push(JSON.parse(opts.body)); return { ok: true, json: async () => ({ lessons: [related] }) }; };
  const wiki = { lessons: [{ id: '0000000000000001', title: '導入', lesson: '短い検索用要約', body: '## 背景\n\n<img src=x onerror=alert(1)>\n\n## 未確定\n\nまだ未検証の条件', related: [related.id], sources: [] }] };
  const app = mount(Notebook, { target, props: { wiki } }); flushSync();
  const details = target.querySelector('.wiki-article');
  const article = !!details && details.textContent.includes('まだ未検証の条件') && !!details.querySelector('h4');
  const escaped = !!details && details.textContent.includes('<img src=x onerror=alert(1)>') && !target.querySelector('img');
  target.querySelector('[data-related]')?.click();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const followed = calls[0]?.query === 'lesson:' + related.id && target.textContent.includes('出典に残る経緯');
  let release;
  globalThis.fetch = async () => ({ ok: true, json: () => new Promise(resolve => { release = resolve; }) });
  target.querySelector('[data-related]')?.click();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  target.querySelector('[data-notebook-back]')?.click(); flushSync();
  release?.({ lessons: [related] });
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const returned = target.textContent.includes('短い検索用要約') && !target.textContent.includes('出典に残る経緯');
  unmount(app); globalThis.fetch = realFetch;
  const ok = article && escaped && followed && returned;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (知識本文=' + article + ', HTMLを文字表示=' + escaped + ', 関連を開く=' + followed + ', 一覧へ戻る=' + returned + ')');
  return ok;
}

function checkPlanningContext(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const sp = { id: 'needs-plan', type: 'noul', display_options: {} };
  const it = { trace_id: 'planning', answerable: true, band: 'ask', p: .6, top: 'yes', model: 'm', at: '2026-10-22T00:00:00Z', choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], inputs: { open: '列の並びが未決 <img src=x>', agreed: 'AIによる整理（実行許可や人の正解ではない）: CSVとして出力する範囲は合意済み\n今回の人の発言: AIによる整理（実行許可や人の正解ではない）: と書いた', request: '出力機能を追加して' } };
  const app = mount(ItemCard, { target, props: { sp, it } }); flushSync();
  const labels = [...target.querySelectorAll('.field-label')].map(el => el.textContent);
  const named = labels[0] === t('inputRequest') && labels[1]?.includes('もう決まっている') && labels[2]?.includes('まだ決まっていない');
  // モデルへ渡す前置き「AIによる整理（…）: 」は画面では外す(先頭だけ。人の発言の中の同じ文字は残す)
  const agreedShown = target.querySelector('[data-input=agreed] .message-body')?.textContent || '';
  const evidence = agreedShown.startsWith('CSVとして出力する範囲は合意済み') && agreedShown.includes('今回の人の発言: AIによる整理（実行許可や人の正解ではない）: と書いた') && target.textContent.includes(it.inputs.open) && !target.querySelector('img');
  unmount(app);
  const legacy = mount(ItemCard, { target, props: { sp, it: { ...it, truth: 'no', truth_at: it.at, legacy_planning: true, record_question: '以前の質問' } } }); flushSync();
  const separate = target.textContent.includes('以前の質問') && target.textContent.includes('過去の形式') && !target.querySelector('.answer-choice') && !target.querySelector('button.weak.redo[title]');
  unmount(legacy);
  i18n.lang = lang;
  const ok = named && evidence && separate;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (依頼・合意・未決事項の順=' + named + ', 整理と根拠を文字として表示=' + evidence + ', 過去の形式を区別=' + separate + ')');
  return ok;
}

function checkCalibrationReport(target) {
  const saved = { done: ui.done }, lang = i18n.lang;
  ui.done = false; i18n.lang = 'ja';
  let summaries = true, closed = true, states = true;
  for (const reason of ['quality', 'insufficient', 'written', 'stale']) {
    const result = { reason, human_truths: reason === 'insufficient' ? 2 : 50, min_n: 50, model: 'm', hash: reason === 'stale' ? 'old' : 'current', at: '2026-10-22 00:00:00' };
    const sp = { id: 'needs-plan', type: 'noul', min_n: 50, keys: [{ model: 'm', hash: 'current', n: 50, state: reason === 'written' ? 'calibrated' : 'ready', method: 'identity' }], records: 50, labels: 50, queue: { items: [] }, calibration_result: result, last_calibration: 'diagnostic marker: long technical log' };
    const app = mount(SpecPanel, { target, props: { sp } }); flushSync();
    const report = target.querySelector('.calibration-report');
    const detail = report?.querySelector('details');
    closed &&= !!detail && !detail.open && detail.querySelector('pre')?.textContent.includes('diagnostic marker') && !target.querySelector('.decision-panel > pre');
    const text = report?.querySelector('.calibration-summary')?.textContent || '';
    if (reason === 'quality') summaries &&= text.includes('50') && text.includes('予測') && !text.includes('不足');
    if (reason === 'insufficient') summaries &&= text.includes('2') && text.includes('50') && text.includes('届いていません');
    if (reason === 'written') summaries &&= text.includes('保存しました');
    const progress = target.querySelector('.learning-line')?.textContent || '';
    if (reason === 'quality') states &&= progress.includes('見送り') && !progress.includes('線を引けます') && !target.querySelector('.pill.ready')?.textContent.includes('線を引けます');
    if (reason === 'stale') states &&= progress.includes(t('plainReady')) && !progress.includes('線を引けます');
    if (reason === 'written') states &&= progress.includes(t('plainAuto'));
    unmount(app);
  }
  Object.assign(ui, saved); i18n.lang = lang;
  const ok = summaries && closed && states;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (較正の理由を要約=' + summaries + ', 診断ログを折りたたむ=' + closed + ', 見送りと現在の状態を区別=' + states + ')');
  return ok;
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
    return { ok: true, json: async () => ({ source_material: [{ id: '0000000000000001', request: 'README installation', reply: '<img src=x onerror=alert(1)> missing startup steps' }], originals: { '0000000000000001': [{ original_inputs: { summary: 'FULL-TEXT-END' } }] }, observations: [{ id: '0000000000000002', kind: 'test', content: 'OBSERVED-TEST-PASS', version: 'fixture-v1', observed_at: '2026-01-01T00:00:00Z' }] }) };
  };
  const wiki = { notebook_project: 'alpha', pending_sources_count: 1, knowledge_usage: { window: 256, prepared: 3, with_matches: 2, with_citations: 1, citations: 1, context_bytes: 450, lookup_milliseconds: 2.5 }, maintenance: [{ page_id: 'a', title: 'Source review', revision: 'r1', key: 'k1' }], lessons: [{ id: 'a', title: '<img src=x onerror=alert(1)>', lesson: 'Include model startup steps', applies_to: 'README installation', sources: [{ id: '0000000000000001', hash: 'h' }, { id: '0000000000000002', hash: 'h2' }] }] };
  const app = mount(Notebook, { target, props: { wiki } });
  flushSync();
  const hasLesson = target.textContent.includes('Include model startup steps') && target.textContent.includes('alpha');
  const buttons = [...target.querySelectorAll('.lesson .btns button')];
  if (buttons[0]) buttons[0].click();
  await new Promise(resolve => setTimeout(resolve, 0));
  flushSync();
  const readSource = target.textContent.includes('missing startup steps') && calls[0]?.body.operation === 'query' && calls[0]?.body.sources?.[0] === '0000000000000001';
  if (buttons[1]) buttons[1].click();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const observed = target.textContent.includes('FULL-TEXT-END') && target.textContent.includes('OBSERVED-TEST-PASS');
  const usage = target.querySelector('[data-knowledge="prepared"]')?.textContent.includes('3') && target.querySelector('[data-knowledge="with_citations"]')?.textContent.includes('1');
  const maintenance = target.querySelector('[data-maintenance="a"]')?.textContent.includes('Source review');
  const inertText = !target.querySelector('img') && target.textContent.includes('<img src=x onerror=alert(1)>');
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = hasLesson && readSource && observed && usage && maintenance && inertText;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (notebook lesson=' + hasLesson + ', source lookup=' + readSource + ', observed sources=' + observed + ', usage=' + usage + ', maintenance=' + maintenance + ', quoted HTML is text=' + inertText + ')');
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
  const make = (id, inputs) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 1, labels: 0, unlabeled: 1, tune: null, queue: { items: [{ trace_id: id, answerable: true, inputs, original_inputs: { previous: '前の仕事は**完了**しました。末尾 <img src=x>' }, text_status: {}, choices: [] }] } });
  ui.done = false; ui.filter = 'all'; ui.project = ''; ui.view = 'one'; ui.pinned = '';
  let app = mount(SpecPanel, { target, props: { sp: make('needs-plan', { previous: '前の仕事は完了しました。', request: '今回の新しい依頼' }) } });
  flushSync();
  const first = target.querySelector('.text [data-input]');
  const previous = target.querySelector('details.previous-context');
  const before = first?.dataset.input === 'request' && first.textContent.includes('今回の依頼') && first.textContent.includes('今回の新しい依頼') && target.querySelector('.decision-phase')?.textContent.includes('始める前');
  const folded = !!previous && !previous.open && previous.querySelector('summary')?.textContent.includes('この依頼より前のAIの返答') && previous.textContent.includes('末尾 <img src=x>') && !target.querySelector('img');
  if (previous) previous.open = true;
  const readable = previous?.open && previous.textContent.includes('前の仕事は完了しました。末尾') && !previous.textContent.includes('**'); // 折り畳みの中も Markdown の記号を外す(再監査 W9)
  // 字の大きさの class は 1 つだけ(text-[13px] と text-[15px] が並ぶと、生成順で 15px が勝つ: 監査 A-1)
  const sizes = (previous?.querySelector('.message-body')?.className.match(/(?:^|\s)text-\[\d+px\]/g) || []);
  const oneSize = sizes.length === 1 && sizes[0].trim() === 'text-[13px]';
  unmount(app);
  app = mount(SpecPanel, { target, props: { sp: make('done-as-asked', { files: 'changed.go', request: '今回の新しい依頼', summary: '今回の作業を終えました。' }) } });
  flushSync();
  const fields = [...target.querySelectorAll('.text [data-input]')];
  const after = fields[0]?.dataset.input === 'request' && fields[1]?.dataset.input === 'summary' && fields[1].textContent.includes('今回の最後の返答') && target.querySelector('.decision-phase')?.textContent.includes('作業後') && !target.querySelector('details.previous-context');
  unmount(app);
  const ok = before && folded && readable && oneSize && after;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (着手前の対象=' + before + ', 前の完了返答を折りたたむ=' + folded + ', 原文を読める=' + readable + ', 前の返答の字の大きさは1つ=' + oneSize + ', 作業後の対象=' + after + ')');
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
  target.querySelector('.btns button.redo')?.click();
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

// 0.9.1: 上の切替は「答える・知識・AI の様子」の 3 つだけ。今の画面が分かり(aria-current)、押すと選んだ画面が保存される。
// AI の様子では作業前・作業後の問いが並び、押すと選んだ問いが保存される
async function checkNavigation(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui }; const lang = i18n.lang; i18n.lang = 'ja';
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [] } : { items: [] } });
  Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const pages = [...target.querySelectorAll('#tabs [data-page]')].map(b => b.dataset.page).join(',') === 'answer,wiki,ai' && !target.querySelector('#tabs [data-tab]');
  const current = target.querySelector('[data-page="answer"]')?.getAttribute('aria-current') === 'page';
  target.querySelector('[data-page="wiki"]')?.click(); await settle(); await settle();
  const remembered = ui.page === 'wiki' && localStorage.getItem('shikii.page') === 'wiki' && target.querySelector('[data-page="wiki"]')?.getAttribute('aria-current') === 'page';
  target.querySelector('[data-page="ai"]')?.click(); await settle(); await settle();
  target.querySelector('[data-tab="done-as-asked"]')?.click(); await settle();
  const tab = ui.tab === 'done-as-asked' && localStorage.getItem('shikii.tab') === 'done-as-asked' && target.querySelector('[data-tab="done-as-asked"]')?.getAttribute('aria-current') === 'true';
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  const ok = pages && current && remembered && tab;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (画面は 3 つ=' + pages + ', 現在地=' + current + ', 画面の選択を保持=' + remembered + ', 問いの選択を保持=' + tab + ')');
  return ok;
}

// 会話を読んでから答えられ、二択では重複する「〜ではない」を出さない。
function checkAnswerFocus(target) {
  const it = { trace_id: 'focus', at: '2026-10-22T00:00:00Z', band: 'discard', top: 'yes', p: .46, answerable: true, inputs: { request: '新しい依頼' }, choices: [{ truth: 'yes', label: '先に相談する' }, { truth: 'no', label: 'そのまま進める' }] };
  const app = mount(ItemCard, { target, props: { sp: { id: 'needs-plan', type: 'noul' }, it } });
  flushSync();
  const conversation = target.querySelector('.text');
  const guess = target.querySelector('.guess');
  const answers = target.querySelector('.btns');
  const readingOrder = !!conversation && !!guess && !!answers && !!(conversation.compareDocumentPosition(guess) & 4) && !!(guess.compareDocumentPosition(answers) & 4);
  const buttons = [...target.querySelectorAll('button[data-truth]')];
  const clearChoices = buttons.length === 2 && buttons.every(b => ['yes', 'no'].includes(b.dataset.truth));
  const answerPrompt = target.querySelector('.answer-heading')?.textContent.includes('あなたの判断') && !target.querySelector('.card-question');
  unmount(app);
  // 答える画面(question を渡す): 札の先頭とボタンの上に問い、ボタンの帯は画面の下に貼り付く。AI の内側の印(帯・モデル)は出さない
  const app2 = mount(ItemCard, { target, props: { sp: { id: 'needs-plan', type: 'noul' }, it, question: '始める前に聞くべきだったか' } });
  flushSync();
  const asked = target.querySelector('.card-question')?.textContent === '始める前に聞くべきだったか' && target.querySelector('.answer-heading')?.textContent.includes('始める前に聞くべきだったか') && target.querySelector('.answer-bar')?.classList.contains('sticky') && !target.textContent.includes('discard');
  unmount(app2);
  const ok = readingOrder && clearChoices && answerPrompt && asked;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (会話から回答の順=' + readingOrder + ', 重複しない二択=' + clearChoices + ', 答える場所=' + answerPrompt + ', 答える画面の札は問いを出す=' + asked + ')');
  return ok;
}

// 初めて開く時は、人待ちがあれば答える画面、無ければ知識。選んだ画面は勝手に切り替えない。上の切替の上で Enter を押しても答えない
async function checkLanding(target) {
  const realFetch = globalThis.fetch, savedState = data.state, saved = { ...ui };
  const mkSpecs = waits => ['done-as-asked', 'needs-plan'].map(id => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: id === 'needs-plan' ? waits : 0, waiting_for_person: id === 'needs-plan' ? waits : 0, person_answers: true }));
  let specs = mkSpecs(2);
  const calls = [];
  const item = { trace_id: 'keyboard', band: 'ask', top: 'yes', p: .8, at: '2026-10-22T00:00:00Z', answerable: true, waits_for_person: true, inputs: { request: '依頼' }, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push(JSON.parse(opts.body)); return { ok: true, json: async () => url === 'v1/status' ? { specs } : url === 'v1/wiki' ? { claims: [], lessons: [] } : url.startsWith('v1/queue') ? { items: url.includes('needs-plan') ? [item] : [] } : opts?.body ? { truth: 'yes' } : {} }; };
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  data.state = null; Object.assign(ui, { page: '', tab: 'missing', done: false, pinned: '' });
  let app = mount(App, { target }); await settle(); await settle();
  const pendingFirst = !!target.querySelector('[data-answer] .item') && target.querySelector('[data-page="answer"]')?.getAttribute('aria-current') === 'page';
  ui.page = 'ai'; flushSync(); await settle(); await settle();
  const respectedChoice = !!target.querySelector('[data-ai]') && !target.querySelector('[data-answer]');
  ui.page = 'answer'; flushSync(); await settle(); await settle();
  const navButton = target.querySelector('[data-page="answer"]'); navButton.focus();
  navButton.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  await new Promise(resolve => setTimeout(resolve, 0));
  const safeNavigation = calls.length === 0;
  document.body.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  await new Promise(resolve => setTimeout(resolve, 0));
  const shortcutWorks = calls.length === 1 && calls[0].truth === 'yes';
  unmount(app);
  // 人待ちが無い時は知識から始まる
  specs = mkSpecs(0); data.state = null; Object.assign(ui, { page: '', tab: '' });
  app = mount(App, { target }); await settle(); await settle();
  const quietToKnowledge = target.querySelector('[data-page="wiki"]')?.getAttribute('aria-current') === 'page' && !target.querySelector('[data-page="answer"] .badge');
  unmount(app); globalThis.fetch = realFetch; data.state = savedState; Object.assign(ui, saved);
  const ok = pendingFirst && respectedChoice && safeNavigation && shortcutWorks && quietToKnowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (初回は答える画面=' + pendingFirst + ', 選んだ画面は保持=' + respectedChoice + ', 切替の操作で回答しない=' + safeNavigation + ', 回答のキー操作=' + shortcutWorks + ', 待ちが無ければ知識=' + quietToKnowledge + ')');
  return ok;
}

// API の証拠を表示し、欠けた項目から undefined を作らない。
function checkClaimEvidence(target) {
  i18n.lang = 'ja';
  const wiki = { claims: [{ path: 'patterns/example.md', is_claim: true, status: 'tentative', claim: '検証用の主張', evidence: [
    { date: '2026-10-22', model: 'fixture-model', result: '<b>fixture-result</b>', verdict: 'supports', run: 'r1' },
    { model: 'partial-model' },
  ] }] };
  const app = mount(Knowledge, { target, props: { wiki } });
  flushSync();
  const text = target.textContent;
  const complete = ['2026-10-22', 'fixture-model', '<b>fixture-result</b>', 'supports'].every(v => text.includes(v));
  const partial = text.includes('partial-model') && !text.includes('undefined') && !text.includes('null');
  const escaped = target.querySelectorAll('.text b').length === 1;
  unmount(app);
  const ok = complete && partial && escaped;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (証拠の表示=' + complete + ', 欠けた項目の表示=' + partial + ', 文字として表示=' + escaped + ')');
  return ok;
}


function loadingSpecs() {
  return ['needs-plan', 'done-as-asked', 'prompt-kind', 'changes-in-scope'].map(id => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 1, waiting_for_person: 1, person_answers: true }));
}

// 表示中の画面の分だけを取得する。答える画面は待ちのある問いの列と聞き方、AI の様子は開いた問いの列(回答済みなら回答済みだけ)、知識は選んだ時だけ
async function checkFocusedLoading() {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { page: ui.page, tab: ui.tab, done: ui.done };
  let calls = [];
  const specs = () => loadingSpecs().map(s => s.id === 'prompt-kind' ? { ...s, waiting_for_person: 0 } : s);
  globalThis.fetch = async url => { calls.push(url); return { ok: true, json: async () => url === 'v1/status' ? { specs: specs() } : url === 'v1/wiki' ? { claims: [] } : url.startsWith('v1/tune') ? { variants: [] } : { items: [] } }; };
  ui.page = 'answer'; ui.done = false;
  await load();
  const waiting = ['needs-plan', 'done-as-asked', 'changes-in-scope'];
  const answer = calls.length === 7 && waiting.every(id => calls.includes('v1/queue?spec=' + id + '&limit=200') && calls.includes('v1/tune?spec=' + id)) && !calls.some(u => u.includes('prompt-kind')) && !calls.includes('v1/wiki');
  calls = []; ui.page = 'ai'; ui.tab = 'needs-plan'; await load();
  const simple = calls.length === 3 && calls[1] === 'v1/queue?spec=needs-plan&limit=200' && calls.includes('v1/tune?spec=needs-plan') && !calls.includes('v1/wiki');
  calls = []; ui.done = true; await load();
  const answered = calls.length === 2 && calls.includes('v1/answered?spec=needs-plan&limit=200') && !calls.some(url => url.startsWith('v1/queue') || url.startsWith('v1/tune'));
  calls = []; ui.page = 'wiki'; await load();
  const knowledge = calls.length === 2 && calls[1] === 'v1/wiki' && !!data.state.wiki;
  globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = answer && simple && answered && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (答える画面は待ちのある問いだけ=' + answer + ', AI の様子は3要求=' + simple + ', 回答済みを取得=' + answered + ', 知識は選択時だけ=' + knowledge + ')');
  return ok;
}

// 遅い古い応答で切替後の情報を上書きせず、取得失敗と再試行を表示へ反映する。
async function checkLoadOrdering() {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { page: ui.page, tab: ui.tab, done: ui.done };
  let release, blocked = true, version = 0;
  const wait = new Promise(resolve => { release = resolve; });
  globalThis.fetch = async url => {
    if (url === 'v1/status') { const current = ++version; return { ok: true, json: async () => ({ specs: loadingSpecs(), version: current }) }; }
    if (url.startsWith('v1/queue?spec=needs-plan') && blocked) { blocked = false; return { ok: true, json: async () => { await wait; return { items: [] }; } }; }
    return { ok: true, json: async () => ({ items: [] }) };
  };
  ui.page = 'ai'; ui.tab = 'needs-plan'; ui.done = false;
  const slow = load(); await new Promise(resolve => setTimeout(resolve, 0));
  ui.tab = 'done-as-asked'; await load(); release(); await slow;
  const latest = data.state.version === 2;
  globalThis.fetch = async () => ({ ok: false, json: async () => ({ error: 'fixture unavailable' }) });
  await load().catch(() => {});
  const failed = data.loadError === 'fixture unavailable' && data.state.version === 2;
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs(), version: 3 } : { items: [] } });
  await load(); const recovered = !data.loadError && data.state.version === 3;
  globalThis.fetch = realFetch; data.state = state; data.loadError = ''; Object.assign(ui, saved);
  const ok = latest && failed && recovered;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (古い応答を無視=' + latest + ', 失敗を表示=' + failed + ', 再試行で復帰=' + recovered + ')');
  return ok;
}

// 実際の切替から遅延読取りを行い、未読の知識も開ける。AI の様子の問いの切替・列の切替でも、その分だけ読む
async function checkLazyNavigation(target) {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { page: ui.page, tab: ui.tab, done: ui.done };
  let calls = [], holdQueue = false, releaseQueue;
  globalThis.fetch = async url => {
    calls.push(url);
    if (holdQueue && url.startsWith('v1/queue')) return { ok: true, json: () => new Promise(resolve => { releaseQueue = resolve; }) };
    return { ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [] } : { items: [] } };
  };
  data.state = null; ui.page = 'ai'; ui.tab = 'needs-plan'; ui.done = false;
  const app = mount(App, { target }); flushSync();
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  await settle(); const initial = calls.length === 3 && calls.includes('v1/tune?spec=needs-plan') && !!target.querySelector('.decision-panel'); // status・列・聞き方の候補
  calls = []; target.querySelector('[data-tab="done-as-asked"]')?.click(); flushSync(); await settle();
  const switched = calls.includes('v1/queue?spec=done-as-asked&limit=200') && !!target.querySelector('.decision-panel');
  calls = []; target.querySelector('.answer-tabs [data-lane="done"]')?.click(); flushSync(); await settle();
  const answered = calls.length === 2 && calls.includes('v1/answered?spec=done-as-asked&limit=200') && target.querySelector('.answer-tabs button span')?.textContent === '1';
  holdQueue = true;
  target.querySelector('.answer-tabs [data-lane="you"]')?.click(); flushSync(); await settle();
  const waiting = !!target.querySelector('[role="status"]') && !target.querySelector('.decision-panel');
  holdQueue = false; releaseQueue?.({ items: [] }); await settle();
  calls = [];
  const wiki = target.querySelector('[data-page="wiki"]'); wiki?.click(); flushSync(); await settle();
  const knowledge = !!wiki && calls.filter(url => url === 'v1/wiki').length === 1 && !calls.some(url => url.startsWith('v1/queue')) && target.querySelector('.notebook h2')?.textContent.includes(t('notebookTitle'));
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = initial && switched && answered && waiting && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (初回3要求=' + initial + ', 問いの切替で取得=' + switched + ', 回答済みと未回答件数=' + answered + ', 切替中は読込み待ち=' + waiting + ', 未読の知識を開く=' + knowledge + ')');
  return ok;
}

// 監査 B(0.8.1、Tailwind): 同じ要素に、同じ変体で同じ性質を違う値にする utility を重ねない。
// Tailwind v4 は utility を「性質ごとの固定の順 → 名前の順」で生成し、class 属性の並びは勝敗に関係ない
// (例: bg-transparent は bg-accentbg や bg-surface より後に生成されるので、条件で足しても常に透明が勝つ)。
// 配る CSS(dist/assets/index.css)の utilities 層から class → 性質の表を作り、描いた要素の classList を全部見る。
// 変体(max-sm: / hover: / [&>span]: …)が違えば幅や状態で上書きする意図なので見ない。--tw-* から導く値も見ない
function utilityTable(css) {
  const table = new Map();
  const at = css.indexOf('@layer utilities {');
  if (at < 0) return table;
  const re = /\n\s*(\.[^{}]*?)\s*\{\n([^{}]*?)\n\s*\}/g;
  let m;
  while ((m = re.exec(css.slice(at)))) {
    const decls = m[2].split('\n').map(l => l.trim().replace(/;$/, '')).filter(l => l.includes(':') && !l.includes('var(--tw-'));
    for (const sel of m[1].split(/,\s*(?=\.)/)) {
      let i = 1, name = '';
      while (i < sel.length) {
        const c = sel[i];
        if (c === '\\') { name += sel[i + 1]; i += 2; continue; }
        if (/[\s.:>[,]/.test(c)) break;
        name += c; i++;
      }
      if (sel.slice(i).trim() && !name.includes(':')) continue; // .pill.calibrated、.empty-state p のような複合の規則
      const props = table.get(name) || new Map();
      for (const d of decls) { const k = d.indexOf(':'); props.set(d.slice(0, k).trim(), d.slice(k + 1).trim()); }
      table.set(name, props);
    }
  }
  return table;
}
function variantPrefix(name) {
  let depth = 0, last = -1;
  for (let i = 0; i < name.length; i++) { const c = name[i]; if (c === '[' || c === '(') depth++; else if (c === ']' || c === ')') depth--; else if (c === ':' && depth === 0) last = i; }
  return last < 0 ? '' : name.slice(0, last);
}
function collectConflicts(target, table, out) {
  for (const el of target.querySelectorAll('[class]')) {
    const names = [...el.classList].filter(n => table.has(n));
    for (let a = 0; a < names.length; a++) for (let b = a + 1; b < names.length; b++) {
      if (variantPrefix(names[a]) !== variantPrefix(names[b])) continue;
      for (const [prop, va] of table.get(names[a])) {
        const vb = table.get(names[b]).get(prop);
        if (vb !== undefined && vb !== va) out.add(el.tagName.toLowerCase() + ': ' + names[a] + ' と ' + names[b] + ' (' + prop + ')');
      }
    }
  }
}
async function checkUtilityConflicts(target) {
  const table = utilityTable(globalThis.__shikiiCSS || '');
  if (table.size < 50) { console.log('check-ui: FAIL (配る CSS の utility を読めない: ' + table.size + ' 個)'); return false; }
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui }; const lang = i18n.lang; i18n.lang = 'ja';
  const out = new Set();
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  // 1. 画面の全体: 選ばれたタブ・列・絞り込み、要約、使い方、未回答のカード(AI と同じ / 一覧)
  const mk = (id, unlabeled, waiting, person) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [{ model: 'm', hash: 'h', n: 3, state: 'collecting' }], min_n: 50, records: 5, labels: 1, unlabeled, waiting_for_person: waiting, awaiting_results: unlabeled - waiting, person_answers: person, truth_source: person ? 'person' : 'results', last_calibration: 'log', calibration_result: { reason: 'insufficient', human_truths: 2, min_n: 50, model: 'm', hash: 'h', at: '2026-10-23' }, tune: { running: false, proposer: true, variants: [{ name: 'v1' }], latest: { run: 'r', adoptable: true, adopted: false, winner: 'v1', model: 'm', truths: 3, at: '2026-10-23T00:00:00Z', have_holdout: true, holdout: 0.5, decision: 'd', candidates: [{ name: 'current', top1: 0.5, brier: 0.2, prec_at_act: 0.5, rec_at_act: 0.5, change: '' }, { name: 'v1', top1: 0.6, brier: 0.1, prec_at_act: 0.6, rec_at_act: 0.6, change: 'c' }], winner_wording: { question: 'q1', options: { a: 'x' }, steps: [{ ask: 'a', yes: 'y', no: 'n' }] }, current_wording: { question: 'q0', options: { a: 'y' } } } } });
  const specs = [mk('wiki-conflict', 4, 0, false), mk('prompt-kind', 2, 1, true), mk('needs-plan', 3, 2, true)];
  const item = (id, project) => ({ trace_id: id, at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: id, raw: { yes: 0.6, no: 0.4 }, stale: true, answerable: true, audit: true, waits_for_person: true, project, rung: 'context', choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }] });
  const summary = { person: { records: 20, waits: 2 }, memory: { delivered: 4, corrections: { delivered: 1 } }, guards: { rituals: { plan: { asked: 5, done: 2, fixed: 1 }, reflect: { asked: 4, done: 4, fixed: 0 } }, items: [{ id: 'g', kind: 'k', title: 't', enabled: true, last: { pass: false, at: '2026-10-23T00:00:00Z', reason: 'r' } }, { id: 'g2', kind: 'k', title: 't2', enabled: false }] } };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs, summary, model: { name: 'm' } } : url.startsWith('v1/queue?spec=prompt-kind') ? { items: [item('r1', 'alpha'), item('y1', 'beta')] } : url.startsWith('v1/tune') ? specs[1].tune : { items: [] } });
  Object.assign(ui, { page: 'ai', tab: 'prompt-kind', done: false, results: false, pinned: '', showHelp: true });
  let app = mount(App, { target }); await settle(); await settle();
  collectConflicts(target, table, out);
  unmount(app);
  Object.assign(ui, { page: 'answer', tab: '' }); // 答える画面: 札(問い・貼り付くボタンの帯)と聞き方の候補
  app = mount(App, { target }); await settle(); await settle();
  collectConflicts(target, table, out);
  const answerShown = !!target.querySelector('[data-answer] .answer-bar') && !!target.querySelector('[data-answer-wording]');
  unmount(app);
  if (!answerShown) out.add('答える画面の札か聞き方の候補が描かれていない(検査が見ていない)');
  // 2. カード: 前の返答の折りたたみ(needs-plan)、原文の上限の注意、回答済み(答えの直し)、抜き取りの仮採点、読むだけ
  const base = { trace_id: 'c1', at: '2026-10-23T00:00:00Z', band: 'discard', top: 'yes', p: 0.3, model: 'm', answerable: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], project: 'alpha' };
  const cards = [
    [{ id: 'needs-plan', type: 'noul' }, { ...base, inputs: { previous: 'p', request: 'r', agreed: 'a', open: 'o', files: 'f' }, original_inputs: { previous: 'pp' }, text_status: { request: 'limited', previous: 'legacy' } }],
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r', summary: 's' }, truth: 'yes', truth_at: base.at, redo_of: 'x' }],
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r' }, audit: true, review_reason: 'random', review: { truth: 'yes', reason: 'why', source_material: { reply: 'reply' } } }],
    [{ id: 'intent', type: 'choice' }, { ...base, top: 'a', choices: [{ truth: 'a', label: 'a' }, { truth: 'b', label: 'b' }], inputs: { message: 'm' } }],
  ];
  for (const [sp, it] of cards) { app = mount(ItemCard, { target, props: { sp, it } }); flushSync(); collectConflicts(target, table, out); unmount(app); }
  app = mount(ItemCard, { target, props: { sp: { id: 'wiki-conflict', type: 'noul' }, it: { ...base, inputs: { lesson: 'l' } }, readonly: true } }); flushSync(); collectConflicts(target, table, out); unmount(app);
  // 3. 知識の欄(主張・階層・記事の本文・出典・効き目)、仕事の入口、知らせ
  const lesson = (id, related = []) => ({ id, revision: 'r', title: 't' + id, lesson: 'l', applies_to: 'w', body: '## h\n\nbody', sources: [{ id: 'src' + id }], related, status: 'hypothesis', correction: { quote: 'q', action: 'a' } });
  const wiki = { notebook_project: 'alpha', pending_sources_count: 1, knowledge_usage: { prepared: 1, with_matches: 1, with_citations: 1, lookup_milliseconds: 1, context_bytes: 1 }, maintenance: [{ page_id: 'a', title: 'm' }],
    memory_effect: { window: 10, stats: { delivered: 1 }, corrections: { delivered: 1 }, recent: [{ source_id: 's', request: 'r', quote: { key: 'k', lesson: 'l', correction: { quote: 'q', action: 'a' } }, assessment: { result: 'helpful', correction_result: 'followed', decision: 'd', evidence: 'e' } }] },
    lessons: [lesson('a', ['b']), lesson('b')], claims: [{ path: 'p/a.md', is_claim: true, status: 'draft', claim: 'c', confirmable: true, applies_to: { models: 'm' }, evidence: [{ date: 'd', model: 'm', result: 'r', verdict: 'v' }] }, { path: 'p/b.md', is_claim: true, status: 'confirmed', claim: 'c2' }, { path: 'p/c.md', is_claim: true, status: 'tentative', claim: 'c3' }] };
  app = mount(Knowledge, { target, props: { wiki } }); flushSync(); collectConflicts(target, table, out); unmount(app);
  app = mount(Projects, { target, props: { projects: [{ name: 'n', path: '/p', url: '/p/x/' }] } }); flushSync(); collectConflicts(target, table, out); unmount(app);
  toastState.msg = 'x'; toastState.shown = true;
  app = mount(Toast, { target }); flushSync(); collectConflicts(target, table, out); unmount(app); toastState.shown = false;
  globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  const list = [...out];
  const ok = list.length === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (同じ要素に同じ性質の utility を重ねない: 衝突 ' + list.length + ' 件' + (ok ? '' : '\n  ' + list.slice(0, 20).join('\n  ')) + ')');
  return ok;
}

// 作業記録(0.8.1): フォルダごとに畳んで始まり、開くとファイル、押すと文字は文字のまま(HTML を描かない)、画像は画像で出す。空なら空と言う。
async function checkHandoff(target) {
  const realFetch = globalThis.fetch, lang = i18n.lang; i18n.lang = 'ja';
  const calls = [];
  globalThis.fetch = async url => { calls.push(url); return { ok: true, text: async () => '# 監査 A\n<script>alert(1)</script>', json: async () => ({}) }; };
  const files = [
    { path: 'audit-0.8.1/report-A.md', size: 2048, modified: '2026-10-24T03:00:00Z', kind: 'text' },
    { path: 'audit-0.8.1/before 1.png', size: 148649, modified: '2026-10-24T02:00:00Z', kind: 'image' },
    { path: 'steps.md', size: 10, modified: '2026-10-24T01:00:00Z', kind: 'text' },
  ];
  let app = mount(Handoff, { target, props: { files } }); flushSync();
  const folded = !target.querySelector('[data-handoff-file]') && target.querySelectorAll('[data-handoff-group]').length === 2 && (target.querySelector('[data-handoff-group="audit-0.8.1"] .badge')?.textContent === '2');
  target.querySelector('[data-handoff-group="audit-0.8.1"] .handoff-toggle')?.click(); flushSync();
  const listed = [...target.querySelectorAll('[data-handoff-file]')].map(b => b.dataset.handoffFile).join(',') === 'audit-0.8.1/report-A.md,audit-0.8.1/before 1.png';
  target.querySelector('[data-handoff-file="audit-0.8.1/report-A.md"]')?.click();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const pre = target.querySelector('[data-handoff-view] pre');
  const text = calls.includes('v1/handoff/file?path=audit-0.8.1%2Freport-A.md') && !!pre && pre.textContent.includes('<script>alert(1)</script>') && !target.querySelector('[data-handoff-view] script');
  target.querySelector('[data-handoff-file="audit-0.8.1/before 1.png"]')?.click(); flushSync();
  const img = target.querySelector('[data-handoff-view] img');
  const image = !!img && img.getAttribute('src') === 'v1/handoff/file?path=audit-0.8.1%2Fbefore%201.png' && !target.querySelector('[data-handoff-view] pre');
  unmount(app);
  app = mount(Handoff, { target, props: { files: [] } }); flushSync();
  const empty = !!target.querySelector('[data-handoff-empty]');
  unmount(app);
  globalThis.fetch = realFetch; i18n.lang = lang;
  const ok = folded && listed && text && image && empty;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (作業記録: 畳んで始まる=' + folded + ', 開くとファイル=' + listed + ', 文字はそのまま=' + text + ', 画像は画像=' + image + ', 空と言う=' + empty + ')');
  return ok;
}

// 知識の正面(0.9.0): 場面を選ぶとその場面の題だけ、題を押すと本文、場面の無い記事は「未分類」、受け箱は例外だけ、長い依頼は押すと全文、
// 種類ごとに 4 本と「ほか N 本」。HTML は文字のまま出す。
function checkLessons(target) {
  try { localStorage.removeItem('shikii.scene'); } catch { /* 無くてよい */ }
  const mk = (i, kind, scene) => ({ id: String(i).padStart(16, '0'), title: '記事 ' + i, lesson: '教訓 ' + i + ' <b>太字ではない</b>', applies_to: 'a', kind, scene, sources: [], related: [] });
  const lessons = [mk(1, 'rule', 'コミットする時'), mk(2, 'rule', 'コミットする時'), mk(3, 'rule', '公開する時'), mk(4, 'rule', '公開する時'), mk(5, 'rule', '公開する時'), mk(6, 'howto', '公開する時'), mk(7, '', '')];
  const long = '依頼の文がとても長い。'.repeat(10);
  const wiki = { lessons, lesson_meta: { [lessons[4].id]: { updated: '2026-10-24T00:00:00Z', helpful: 3, harmful: 0 } }, inbox: [{ kind: 'harmful', lesson_id: lessons[0].id, lesson: '教訓 1', count: 3 }],
    memory_effect: { stats: { delivered: 5, helpful: 1, unused: 3, harmful: 1 }, corrections: {}, recent: [{ request: long, quote: { lesson: '届けた教訓' }, assessment: { result: 'harmful' } }] } };
  const app = mount(Lessons, { target, props: { wiki } }); flushSync();
  const q = sel => target.querySelector(sel);
  const scenes = [...target.querySelectorAll('[data-scenes] [data-scene]')].map(b => b.dataset.scene);
  const sceneList = scenes.length === 3 && scenes[0] === '公開する時' && scenes[2] === '__none__';
  const ruleItems = () => [...target.querySelectorAll('[data-kind="rule"] [data-open]')].map(b => b.textContent.trim());
  const firstFour = ruleItems().length === 4 && ruleItems()[0].startsWith('記事 5') && !!q('[data-kind-more="rule"]');
  q('[data-scene="コミットする時"]').click(); flushSync();
  const filtered = ruleItems().length === 2 && ruleItems().every(t => t === '記事 1' || t === '記事 2') && !q('[data-kind="howto"]');
  q('[data-kind="rule"] [data-open]').click(); flushSync();
  const detail = q('[data-lesson-detail]')?.textContent.includes('<b>太字ではない</b>') && !q('[data-lesson-detail] b');
  q('[data-scene="__none__"]').click(); flushSync();
  const none = q('[data-kinds]')?.textContent.includes('記事 7') && !q('[data-kind="rule"]');
  const inbox = q('[data-inbox-item="harmful"]')?.textContent.includes('記事 1') && q('[data-inbox-count]')?.textContent.includes('3');
  const clip = q('[data-clip="req0"]');
  const short = clip && !clip.textContent.includes(long);
  clip?.click(); flushSync();
  const full = !!clip && q('[data-clip="req0"]').textContent.includes(long);
  unmount(app);
  const ok = sceneList && firstFour && filtered && detail && none && inbox && short && full;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (知識の正面: 場面の一覧=' + sceneList + ', 種類ごとに 4 本と続き=' + firstFour + ', 場面で絞る=' + filtered + ', 本文は文字のまま=' + detail + ', 未分類=' + none + ', 受け箱=' + inbox + ', 押す前は短い=' + short + ', 押すと全文=' + full + ')');
  return ok;
}
