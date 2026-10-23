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
import ItemCard from '../src/components/ItemCard.svelte';
import Tabs from '../src/components/Tabs.svelte';
import App from '../src/App.svelte';
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
  ui.done = true; ui.project = '';
  const app3 = mount(SpecPanel, { target, props: { sp: { ...sp, queue: { items: [] }, answered: [item('a1', 'ANSWERED-ALPHA', 'alpha'), item('b1', 'ANSWERED-BETA', 'beta')] } } });
  flushSync();
  [...target.querySelectorAll('.chips.projects button')].find(b => b.textContent.startsWith('beta'))?.click();
  flushSync();
  const answeredProject = target.querySelectorAll('.item').length === 1 && target.querySelector('.item').textContent.includes('ANSWERED-BETA');
  unmount(app3);
  ui.project = ''; ui.done = false;
  ui.view = 'one';
  const ok = hasChips && onlyBeta && notStranded && answeredProject;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (project chips=' + chips.length + ', filtered to beta=' + onlyBeta + ', other spec not stranded=' + notStranded + ', answered project=' + answeredProject + ')');
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

// 効き目の要約(0.7.4): /v1/status の summary が画面の先頭に 1 行で出て、開くと止める検査の一覧と最後の結果が読める。
async function checkSummary(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const summary = { person: { records: 20, waits: 2, recent_records: 20, recent_waits: 2, by_person: 1, by_result: 13, per_thousand: 100, recent_per_thousand: 100 },
    memory: { delivered: 4, helpful: 1, unused: 2, harmful: 0, unknown: 1, pending: 0, awaiting_feedback: 0, recent_delivered: 4, recent_helpful: 1, corrections: { delivered: 0 } },
    guards: { total: 3, enabled: 3, stopped: 1, fixed: 1, rituals: { plan: { asked: 5, done: 2, fixed: 1 }, reflect: { asked: 4, done: 4, fixed: 0 } }, items: [{ id: 'changelog-pair', kind: 'paired-files', title: '変更履歴は英語と日本語を揃える', enabled: true, last: { pass: false, at: '2026-10-23T00:00:00Z', reason: 'CHANGELOG_ja.md が変わっていない' } }] } };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs(), summary, model: { name: 'TypeSafe Jev', endpoint: 'https://api.typesafe.ai' } } : { items: [] } });
  Object.assign(ui, { tab: '', advanced: false, done: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const details = target.querySelector('#summary');
  const line = details?.querySelector('.summary-line')?.textContent || '';
  // 先頭はまず「あなたがやること」(人待ちの合計 4 = 4 問 × 1)、次に AI の状態(モデル名・自分で判断できる問い 0 / 4・教訓 4・検査 3)
  const oneLine = line.startsWith('あなたがやること: 4 件') && line.includes('TypeSafe Jev') && line.includes('0 / 4') && line.includes('届いた教訓 4') && line.includes('検査 3');
  const closed = !!details && !details.open;
  details?.querySelector('summary')?.click(); flushSync();
  const opened = !!details && details.open;
  const body = details?.querySelector('.summary-guards code')?.textContent === 'changelog-pair' && (details?.textContent || '').includes('CHANGELOG_ja.md が変わっていない') && details.querySelectorAll('.summary-you-list button').length === 4 && (details.textContent || '').includes('学習中 0 / 50') && (details.querySelector('[data-rituals]')?.textContent || '').includes('計画の整理 2 / 5');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = oneLine && closed && opened && body;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (効き目の要約: 1行=' + oneLine + ', 閉じて始まる=' + closed + ', クリックで開く=' + opened + ', 検査の一覧と最後の結果=' + body + ')');
  return ok;
}


// 0.8.0: 人が答えない判定(wiki の矛盾)は、確認待ちの印を出さず、カードに答えのボタンを出さず、案内を 1 行出す。キー操作も押さない。
async function checkMachineOnlySpec(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const specs = loadingSpecs().concat([{ id: 'wiki-conflict', type: 'noul', titles: { ja: 'wiki の矛盾' }, questions: {}, keys: [], min_n: 50, records: 3, labels: 0, unlabeled: 3, waiting_for_person: 0, awaiting_results: 3, person_answers: false, truth_source: 'results' }]);
  const item = { trace_id: 'wc1', at: '2026-10-23T00:00:00Z', band: 'discard', top: 'no', p: 0.8, model: 'm', inputs: { lesson: 'a', new_lesson: 'b' }, inputs_hash: 'wc1', raw: { yes: 0.2, no: 0.8 }, stale: false, answerable: true, choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], rung: 'minimal', audit: false };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs } : url.startsWith('v1/queue?spec=wiki-conflict') ? { items: [item] } : { items: [] } });
  Object.assign(ui, { tab: 'wiki-conflict', advanced: false, done: false, pinned: '', view: 'list' });
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
  Object.assign(ui, { tab: 'wiki', advanced: true, done: false, results: false, pinned: '' });
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


// 監査 R5-3: 人が答える問いの列。「あなたがやること」は人待ちの合計(未回答の合計ではない)で、AI だけの問いは数えない。
// 最初に開く欄は人待ちのある問い。印は人待ちの数。「未回答(あなた)」にはボタン付きのカード、「次の依頼・振り返りで確定する」には読むだけのカード。
async function checkLanesForPersonSpec(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const mk = (id, unlabeled, waiting, person) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 5, labels: 0, unlabeled, waiting_for_person: waiting, awaiting_results: unlabeled - waiting, person_answers: person, truth_source: person ? 'person' : 'results' });
  const specs = [mk('wiki-conflict', 4, 0, false), mk('prompt-kind', 2, 1, true), mk('needs-plan', 3, 2, true)];
  const item = (id, waits, audit) => ({ trace_id: id, at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: id, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, audit, waits_for_person: waits, choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], rung: 'minimal' });
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs, summary: { person: {}, memory: {}, guards: {} }, model: { name: 'TypeSafe Jev' } } : url.startsWith('v1/queue?spec=prompt-kind') ? { items: [item('r1', false, false), item('y1', true, true)] } : { items: [] } });
  Object.assign(ui, { tab: '', advanced: false, done: false, results: false, pinned: '', view: 'list', filter: 'all' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const line = target.querySelector('#summary .summary-line')?.textContent || '';
  const you = line.startsWith('あなたがやること: 3 件') && line.includes('0 / 2');
  const first = !!target.querySelector('[data-tab="prompt-kind"].active');
  const badges = [...target.querySelectorAll('#tabs .badge')].map(b => b.textContent).join(',') === '1,2';
  const youCards = target.querySelectorAll('.item').length === 1 && target.querySelectorAll('.item .btns button').length > 0;
  target.querySelector('.answer-tabs [data-lane="results"]')?.click(); flushSync(); await settle();
  // 人が正解を付ける問い: 「AI が決めた(答えなくてよい)」の列。読めて、違うと思えば直せる(ボタンは残る)
  const resultsCards = target.querySelectorAll('.item').length === 1 && target.querySelectorAll('.item .btns button').length > 0 && (target.querySelector('.lane-results')?.textContent || '').includes('AI が確信') && (target.querySelector('.answer-tabs [data-lane="results"]')?.textContent || '').includes('AI が決めた');
  let clicked = 0;
  for (const b of target.querySelectorAll('.item button')) b.addEventListener('click', () => { clicked++; });
  document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); flushSync();
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = you && first && badges && youCards && resultsCards && clicked === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (人が答える問いの列: あなたがやること=' + you + ', 最初は人待ちの問い=' + first + ', 印=' + badges + ', 未回答にボタン=' + youCards + ', AI が決めた列=' + resultsCards + ', キーで押さない=' + (clicked === 0) + ')');
  return ok;
}

// 監査 R2: キー操作(1〜9 / Enter / →)は判定の欄の未回答でだけ効く。知識の欄では最初の .item のボタン(関連する知識・主張にする)を押さない。
async function checkKeysOnlyOnDecisions(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  // 出典のある教訓: 最初の .item に「原文を見る」のボタンが出る(押す物が無い状態で「押されない」を見ても検査にならない。監査 R3)
  const lesson = { id: 'l1', revision: 'r1', title: '教訓', lesson: '教訓の本文', applies_to: '条件', sources: [{ id: 'src1234567890ab' }], related: [], status: 'hypothesis' };
  const answered = { trace_id: 'done1', at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: 'done1', raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, truth: 'yes', choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false };
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const press = () => { document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); document.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true })); flushSync(); };
  // 知識の欄
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [lesson], sources: [] } : { items: [] } });
  Object.assign(ui, { tab: 'wiki', advanced: true, done: false, pinned: '' });
  let app = mount(App, { target }); await settle(); await settle();
  let buttons = target.querySelectorAll('.item .btns button');
  let clicked = 0;
  for (const b of buttons) b.addEventListener('click', () => { clicked++; });
  press();
  const wikiButtons = buttons.length, wikiClicked = clicked;
  unmount(app);
  // 回答済みの列
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url.startsWith('v1/answered') ? { items: [answered] } : { items: [] } });
  Object.assign(ui, { tab: 'needs-plan', advanced: false, done: true, pinned: '' });
  app = mount(App, { target }); await settle(); await settle();
  buttons = target.querySelectorAll('.item .btns button');
  clicked = 0;
  for (const b of buttons) b.addEventListener('click', () => { clicked++; });
  press();
  const doneButtons = buttons.length, doneClicked = clicked;
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = wikiButtons > 0 && wikiClicked === 0 && doneButtons > 0 && doneClicked === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (キー操作は未回答の判定だけ: 知識の欄 buttons=' + wikiButtons + ' clicked=' + wikiClicked + ', 回答済み buttons=' + doneButtons + ' clicked=' + doneClicked + ')');
  return ok;
}

export async function run() {
  const target = document.getElementById('app');
  const articleOK = await checkNotebookArticle(target);
  const planningOK = checkPlanningContext(target);
  const calibrationOK = checkCalibrationReport(target);
  const contextOK = checkTurnContext(target);
  const binaryOK = await checkBinaryPrediction(target);
  const navigationOK = checkNavigation(target);
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
  const ok = summaryOK && keysOK && machineOnlyOK && treeOK && lanesOK && planningOK && calibrationOK && loadingOK && orderingOK && lazyNavOK && evidenceOK && contextOK && binaryOK && navigationOK && focusOK && landingOK && checkLang(target) && checkPinned(target) && checkProject(target) && await checkWording(target) && await checkNotebook(target) && checkMemoryEffect(target) && checkCorrection(target) && await checkReview(target) && checkFullText(target) && await checkProjectEntrances(target);
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
  const separate = target.textContent.includes('以前の質問') && target.textContent.includes('過去の形式') && !target.querySelector('.answer-choice') && !target.querySelector('button.weak.adv[title]');
  unmount(legacy);
  i18n.lang = lang;
  const ok = named && evidence && separate;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (依頼・合意・未決事項の順=' + named + ', 整理と根拠を文字として表示=' + evidence + ', 過去の形式を区別=' + separate + ')');
  return ok;
}

function checkCalibrationReport(target) {
  const saved = { advanced: ui.advanced, done: ui.done }, lang = i18n.lang;
  ui.advanced = true; ui.done = false; i18n.lang = 'ja';
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
  const make = (id, inputs) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 1, labels: 0, unlabeled: 1, tune: null, queue: { items: [{ trace_id: id, answerable: true, inputs, original_inputs: { previous: '前の仕事は完了しました。末尾 <img src=x>' }, text_status: {}, choices: [] }] } });
  ui.done = false; ui.filter = 'all'; ui.project = ''; ui.view = 'one'; ui.pinned = '';
  let app = mount(SpecPanel, { target, props: { sp: make('needs-plan', { previous: '前の仕事は完了しました。', request: '今回の新しい依頼' }) } });
  flushSync();
  const first = target.querySelector('.text [data-input]');
  const previous = target.querySelector('details.previous-context');
  const before = first?.dataset.input === 'request' && first.textContent.includes('今回の依頼') && first.textContent.includes('今回の新しい依頼') && target.querySelector('.decision-phase')?.textContent.includes('始める前');
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

// 着手前・作業後を分け、選んだ判定が保存され、キーボードでも現在地が分かる。
function checkNavigation(target) {
  i18n.lang = 'ja';
  const specs = ['changes-in-scope', 'done-as-asked', 'needs-plan', 'prompt-kind'].map(id => ({ id, titles: {}, unlabeled: id === 'needs-plan' ? 3 : 0 }));
  const app = mount(Tabs, { target, props: { specs, active: 'needs-plan', hasWiki: false } });
  flushSync();
  const before = target.querySelector('[data-stage="before"]');
  const after = target.querySelector('[data-stage="after"]');
  const grouped = before?.querySelectorAll('[data-tab]').length === 2 && after?.querySelectorAll('[data-tab]').length === 2 && before.textContent.includes('作業前') && after.textContent.includes('作業後');
  const selected = target.querySelector('[data-tab="needs-plan"]')?.getAttribute('aria-current') === 'page';
  target.querySelector('[data-tab="done-as-asked"]')?.click();
  flushSync();
  const remembered = ui.tab === 'done-as-asked' && localStorage.getItem('shikii.tab') === 'done-as-asked';
  unmount(app);
  const ok = grouped && selected && remembered;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (判定の時点を区別=' + grouped + ', 現在地=' + selected + ', 選択を保持=' + remembered + ')');
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
  const answerPrompt = target.querySelector('.answer-heading')?.textContent.includes('あなたの判断');
  unmount(app);
  const ok = readingOrder && clearChoices && answerPrompt;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (会話から回答の順=' + readingOrder + ', 重複しない二択=' + clearChoices + ', 答える場所=' + answerPrompt + ')');
  return ok;
}

// 初めて開く時は確認待ちへ案内し、選択済みの判定は勝手に切り替えない。
async function checkLanding(target) {
  const realFetch = globalThis.fetch, savedState = data.state, savedTab = ui.tab;
  const specs = ['done-as-asked', 'needs-plan'].map(id => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: id === 'needs-plan' ? 2 : 0, waiting_for_person: id === 'needs-plan' ? 2 : 0, person_answers: true, queue: { items: [] } }));
  const calls = [];
  const item = { trace_id: 'keyboard', band: 'ask', top: 'yes', p: .8, at: '2026-10-22T00:00:00Z', answerable: true, inputs: { request: '依頼' }, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push(JSON.parse(opts.body)); return { ok: true, json: async () => url === 'v1/status' ? { specs } : url.startsWith('v1/queue') ? { items: url.includes('needs-plan') ? [item] : [] } : opts?.body ? { truth: 'yes' } : {} }; };
  data.state = { specs }; ui.tab = 'missing'; ui.done = false;
  const app = mount(App, { target }); flushSync();
  await new Promise(resolve => setTimeout(resolve, 0)); flushSync();
  const pendingFirst = target.querySelector('.decision-panel h2')?.textContent.includes('needs-plan');
  ui.tab = 'done-as-asked'; flushSync();
  const respectedChoice = target.querySelector('.decision-panel h2')?.textContent.includes('done-as-asked');
  ui.tab = 'needs-plan'; flushSync();
  const navButton = target.querySelector('[data-tab="needs-plan"]'); navButton.focus();
  navButton.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  await new Promise(resolve => setTimeout(resolve, 0));
  const safeNavigation = calls.length === 0;
  document.body.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  await new Promise(resolve => setTimeout(resolve, 0));
  const shortcutWorks = calls.length === 1 && calls[0].truth === 'yes';
  unmount(app); globalThis.fetch = realFetch; data.state = savedState; ui.tab = savedTab;
  const ok = pendingFirst && respectedChoice && safeNavigation && shortcutWorks;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (初回は確認待ち=' + pendingFirst + ', 選択済みは保持=' + respectedChoice + ', ナビ操作で回答しない=' + safeNavigation + ', 回答のキー操作=' + shortcutWorks + ')');
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

// 表示中の情報だけを取得する。回答済み・知識・くわしくへの切替も取りこぼさない。
async function checkFocusedLoading() {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { tab: ui.tab, advanced: ui.advanced, done: ui.done };
  let calls = [];
  globalThis.fetch = async url => { calls.push(url); return { ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [] } : url.startsWith('v1/tune') ? { variants: [] } : { items: [] } }; };
  ui.tab = 'needs-plan'; ui.advanced = false; ui.done = false;
  await load();
  const simple = calls.length === 2 && calls[1] === 'v1/queue?spec=needs-plan&limit=200';
  calls = []; ui.advanced = true; await load();
  const advanced = calls.length === 3 && calls.includes('v1/tune?spec=needs-plan') && !calls.includes('v1/wiki');
  calls = []; ui.done = true; await load();
  const answered = calls.length === 2 && calls.includes('v1/answered?spec=needs-plan&limit=200') && !calls.some(url => url.startsWith('v1/queue') || url.startsWith('v1/tune'));
  calls = []; ui.tab = 'wiki'; await load();
  const knowledge = calls.length === 2 && calls[1] === 'v1/wiki' && !!data.state.wiki;
  globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = simple && advanced && answered && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (必要な2要求=' + simple + ', 詳細は表示時だけ=' + advanced + ', 回答済みを取得=' + answered + ', 知識は選択時だけ=' + knowledge + ')');
  return ok;
}

// 遅い古い応答で切替後の情報を上書きせず、取得失敗と再試行を表示へ反映する。
async function checkLoadOrdering() {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { tab: ui.tab, advanced: ui.advanced, done: ui.done };
  let release, blocked = true, version = 0;
  const wait = new Promise(resolve => { release = resolve; });
  globalThis.fetch = async url => {
    if (url === 'v1/status') { const current = ++version; return { ok: true, json: async () => ({ specs: loadingSpecs(), version: current }) }; }
    if (url.startsWith('v1/queue?spec=needs-plan') && blocked) { blocked = false; return { ok: true, json: async () => { await wait; return { items: [] }; } }; }
    return { ok: true, json: async () => ({ items: [] }) };
  };
  ui.tab = 'needs-plan'; ui.advanced = false; ui.done = false;
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

// 実際のナビから遅延読取りを行い、未読の知識も開ける。
async function checkLazyNavigation(target) {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { tab: ui.tab, advanced: ui.advanced, done: ui.done };
  let calls = [], holdQueue = false, releaseQueue;
  globalThis.fetch = async url => {
    calls.push(url);
    if (holdQueue && url.startsWith('v1/queue')) return { ok: true, json: () => new Promise(resolve => { releaseQueue = resolve; }) };
    return { ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [] } : { items: [] } };
  };
  data.state = null; ui.tab = 'needs-plan'; ui.advanced = false; ui.done = false;
  const app = mount(App, { target }); flushSync();
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  await settle(); const initial = calls.length === 2 && !!target.querySelector('.decision-panel');
  calls = []; target.querySelector('[data-tab="done-as-asked"]')?.click(); flushSync(); await settle();
  const switched = calls.includes('v1/queue?spec=done-as-asked&limit=200') && !!target.querySelector('.decision-panel');
  calls = []; target.querySelector('.answer-tabs [data-lane="done"]')?.click(); flushSync(); await settle();
  const answered = calls.length === 2 && calls.includes('v1/answered?spec=done-as-asked&limit=200') && target.querySelector('.answer-tabs button span')?.textContent === '1';
  holdQueue = true;
  target.querySelector('.answer-tabs [data-lane="you"]')?.click(); flushSync(); await settle();
  const waiting = !!target.querySelector('[role="status"]') && !target.querySelector('.decision-panel');
  holdQueue = false; releaseQueue?.({ items: [] }); await settle();
  target.querySelector('#mode')?.click(); flushSync(); await settle(); calls = [];
  const wiki = target.querySelector('[data-tab="wiki"]'); wiki?.click(); flushSync(); await settle();
  const knowledge = !!wiki && calls.filter(url => url === 'v1/wiki').length === 1 && !calls.some(url => url.startsWith('v1/queue')) && target.querySelector('.notebook h2')?.textContent.includes(t('notebookTitle'));
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = initial && switched && answered && waiting && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (初回2要求=' + initial + ', タブ切替で取得=' + switched + ', 回答済みと未回答件数=' + answered + ', 切替中は読込み待ち=' + waiting + ', 未読の知識を開く=' + knowledge + ')');
  return ok;
}
