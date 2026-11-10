import Knowledge from '../src/components/Knowledge.svelte';
import Projects from '../src/components/Projects.svelte';
import { load, data } from '../src/lib/state.svelte.js';
import { mount, unmount, flushSync } from 'svelte';
import LangPicker from '../src/components/LangPicker.svelte';
import SpecPanel from '../src/components/SpecPanel.svelte';
import Wording from '../src/components/Wording.svelte';
import Notebook from '../src/components/Notebook.svelte';
import Handoff from '../src/components/Handoff.svelte';
import Lessons from '../src/components/Lessons.svelte';
import ItemCard from '../src/components/ItemCard.svelte';
import Answer from '../src/components/Answer.svelte';
import WordingAsk from '../src/components/WordingAsk.svelte';
import AiView from '../src/components/AiView.svelte';
import App from '../src/App.svelte';
import { i18n, t } from '../src/lib/i18n.svelte.js';
import { ui, skipped, wordingNeedsPerson } from '../src/lib/state.svelte.js';
import Toast from '../src/components/Toast.svelte';
import { toastState } from '../src/lib/toast.svelte.js';
import { loadingSpecs, tunesOnce, utilityTable, variantPrefix, collectConflicts, mediaUtilities, mediaHolds, styleAt, shownAt } from './ui-fixtures.js';



export function checkLang(target) {
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


// 0.9.2 redesign (P6): the AI page shows status by default. With nothing waiting for the person, the "to answer (you)" list is not there (allowed lists: results and answered),
// a question whose wording cannot tell the answers apart says there is nothing for the person to do and its full bar is gray, and the review quality is one sentence with the numbers folded
export function checkAiPageStatus(target) {
  const lang = i18n.lang; i18n.lang = 'ja'; const saved = { ...ui };
  Object.assign(ui, { done: false, results: false });
  const item = { trace_id: 'r1', at: '2026-10-23T00:00:00Z', band: 'act', top: 'yes', p: 0.9, model: 'm', inputs: { request: 'x' }, answerable: true, waits_for_person: false, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  const sp = { id: 'done-as-asked', type: 'noul', titles: { ja: '頼まれたことを終えたか' }, questions: {}, keys: [{ model: 'm', hash: 'h', n: 52, state: 'not_separable' }], min_n: 50, records: 60, labels: 52, unlabeled: 1, waiting_for_person: 0, awaiting_results: 1, person_answers: true, truth_source: 'person', queue: { items: [item] }, tune: null,
    review_quality: { scored: 16, uncertain: 0, checked: 15, correct: 14, uncertain_checked: 0 } };
  const app = mount(SpecPanel, { target, props: { sp } }); flushSync();
  const lanes = [...target.querySelectorAll('.answer-tabs [data-lane]')].map(b => b.dataset.lane);
  const status = JSON.stringify(lanes) === JSON.stringify(['results', 'done']) && !!target.querySelector('.answer-tabs [data-lane="results"].active') && target.querySelectorAll('.item').length === 1 && !target.textContent.includes(t('empty'));
  const line = target.querySelector('.learning-line')?.textContent || '';
  const bar = target.querySelector('[role="progressbar"] > div');
  const nothingToDo = line.includes('あなたがすることはありません') && !!bar && bar.classList.contains('bg-muted') && !bar.classList.contains('bg-accent');
  const rq = target.querySelector('.review-quality');
  const summary = rq?.querySelector('summary')?.textContent || '';
  const oneSentence = !!rq && summary === t('reviewQuality')(sp.review_quality) && !!rq.querySelector('details:not([open])') && (rq.textContent || '').includes('16');
  unmount(app); Object.assign(ui, saved); i18n.lang = lang;
  const ok = status && nothingToDo && oneSentence;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (AI page: status by default ' + JSON.stringify(lanes) + '=' + status + ', cannot tell apart says nothing to do with a gray bar=' + nothingToDo + ', review quality one sentence, numbers folded=' + oneSentence + ')');
  return ok;
}


// 0.9.2 redesign (P7): each page says in one line what it is for (exactly one .page-purpose), and the how-to guide talks about the page that is open
export async function checkPagePurpose(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const item = { trace_id: 'p1', at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [] } : url.startsWith('v1/queue') ? { items: [item] } : url.startsWith('v1/tune') ? null : { items: [] } });
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const seen = {};
  for (const [page, how] of [['answer', 'howTo'], ['wiki', 'howToKnowledge'], ['ai', 'howToAI']]) {
    data.state = null; Object.assign(ui, { page, tab: 'needs-plan', done: false, results: false, pinned: '', showHelp: true });
    const app = mount(App, { target }); await settle(); await settle();
    const purpose = target.querySelectorAll('.page-purpose').length === 1;
    const steps = [...target.querySelectorAll('.howto li')].map(li => li.textContent);
    seen[page] = purpose && JSON.stringify(steps) === JSON.stringify(t(how));
    unmount(app);
  }
  globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  const ok = seen.answer && seen.wiki && seen.ai;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (one purpose line and a how-to for the open page: ' + JSON.stringify(seen) + ')');
  return ok;
}


// The summary of effects (0.9.1; the band at the top in 0.7.4): "Answer" at the top shows the total waiting for a person. AI status shows delivered lessons, the work routine, guards and their last results,
// and learning progress per question, one line each. The Answer screen does not show the AI's internal numbers
export async function checkSummary(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const summary = { person: { records: 20, waits: 2, recent_records: 20, recent_waits: 2, by_person: 1, by_result: 13, per_thousand: 100, recent_per_thousand: 100 },
    memory: { delivered: 4, helpful: 1, unused: 2, harmful: 0, unknown: 1, pending: 0, awaiting_feedback: 0, recent_delivered: 4, recent_helpful: 1, corrections: { delivered: 0 } },
    guards: { total: 3, enabled: 3, stopped: 1, fixed: 1, rituals: { plan: { asked: 5, done: 2, fixed: 1 }, reflect: { asked: 4, done: 4, fixed: 0 }, language: { asked: 4, done: 3, fixed: 1 } }, items: [{ id: 'changelog-pair', kind: 'paired-files', title: '変更履歴は英語と日本語を揃える', enabled: true, last: { pass: false, at: '2026-10-23T00:00:00Z', reason: 'CHANGELOG_ja.md が変わっていない' }, unverified: { at: '2026-10-24T00:00:00Z', reason: 'HEAD at the request was amended' } }] } };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs(), summary, model: { name: 'TypeSafe Jev', endpoint: 'https://api.typesafe.ai' } } : { items: [] } });
  Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const badge = target.querySelector('[data-page="answer"] .badge')?.textContent === '4';
  const quietAnswer = !target.querySelector('#summary') && !target.textContent.includes('TypeSafe Jev') && !target.textContent.includes('学習中');
  target.querySelector('[data-page="ai"]')?.click(); flushSync(); await settle(); await settle();
  const box = target.querySelector('#summary');
  const body = box?.querySelector('.summary-guards code')?.textContent === 'changelog-pair' && (box.querySelector('[data-guard-unverified]')?.textContent || '').includes('HEAD at the request was amended') && (box?.textContent || '').includes('CHANGELOG_ja.md が変わっていない') && (box?.textContent || '').includes('TypeSafe Jev') && (box.querySelector('[data-rituals]')?.textContent || '').includes('進め方を整理 2 / 5') && (box.querySelector('[data-rituals]')?.textContent || '').includes('最後の返事を人の言葉で書く 3 / 4 回(止めてから書き直した 1)');
  const specs = target.querySelectorAll('[data-ai-specs] [data-tab]').length === 4 && (target.querySelector('[data-ai-specs]')?.textContent || '').includes('学習中 0 / 50');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = badge && quietAnswer && body && specs;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (summary of effects: total waiting on Answer=' + badge + ', no internal numbers on the Answer screen=' + quietAnswer + ', guards and last results in AI status=' + body + ', progress per question=' + specs + ')');
  return ok;
}



// 0.8.0: a decision a person does not answer (wiki conflict) shows no waiting marker and no answer buttons on the card, and shows a one-line notice. Keys do not press anything either.
export async function checkMachineOnlySpec(target) {
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
  // Lists: there is no "Unanswered" list, and the "Settled by reflection" list is selected (0.8.0)
  const lanes = !target.querySelector('.answer-tabs [data-lane="you"]') && target.querySelector('.answer-tabs [data-lane="results"]')?.classList.contains('active') && (target.querySelector('.lane-results')?.textContent || '').includes('振り返り');
  let clicked = 0;
  for (const b of target.querySelectorAll('.item button')) b.addEventListener('click', () => { clicked++; });
  document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); flushSync();
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = !!card && buttons === 0 && notice && !badge && clicked === 0 && !!lanes;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (decision a person does not answer: card=' + !!card + ', answer buttons=' + buttons + ', notice=' + notice + ', no waiting marker=' + !badge + ', keys do not press=' + (clicked === 0) + ', list is settled by reflection=' + !!lanes + ')');
  return ok;
}



// Audit R5-3: the lists of questions a person answers. The marker on "Answer" at the top is the total waiting for a person (not the total unanswered), and AI-only questions are not counted.
// The question first opened in AI status is one with something waiting for a person. The marker is the count waiting for a person. "Unanswered (you)" has cards with buttons, and "Settled by the next request or reflection" has read-only cards.
export async function checkLanesForPersonSpec(target) {
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
  // A question a person puts truths on: the "AI decided (no need to answer)" list. It can be read, and corrected if you think it is wrong (the buttons stay)
  const resultsCards = target.querySelectorAll('.item').length === 1 && target.querySelectorAll('.item .btns button').length > 0 && (target.querySelector('.lane-results')?.textContent || '').includes('AI が自信を持って') && (target.querySelector('.answer-tabs [data-lane="results"]')?.textContent || '').includes('AI が決めた');
  let clicked = 0;
  for (const b of target.querySelectorAll('.item button')) b.addEventListener('click', () => { clicked++; });
  document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); flushSync();
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = you && first && badges && youCards && resultsCards && clicked === 0;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (lists of questions a person answers: Answer marker is the total waiting=' + you + ', first is a question with something waiting=' + first + ', markers=' + badges + ', buttons on unanswered=' + youCards + ', AI-decided list=' + resultsCards + ', keys do not press=' + (clicked === 0) + ')');
  return ok;
}


// Audit R2: keys (1 to 9 / Enter / →) only work on the Answer screen. They do not press the first .item's buttons in the Knowledge section (related knowledge, make it a claim),
// nor the buttons of answered cards in AI status.
export async function checkKeysOnlyOnDecisions(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  // A lesson with sources: the first .item shows a "see the original" button (checking "not pressed" with nothing to press would not be a check; audit R3)
  const lesson = { id: 'l1', revision: 'r1', title: '教訓', lesson: '教訓の本文', applies_to: '条件', sources: [{ id: 'src1234567890ab' }], related: [], status: 'hypothesis' };
  const answered = { trace_id: 'done1', at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: 'done1', raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, truth: 'yes', choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false };
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const press = () => { document.dispatchEvent(new window.KeyboardEvent('keydown', { key: '1', bubbles: true })); document.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true })); flushSync(); };
  // the Knowledge section
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons: [lesson], sources: [] } : { items: [] } });
  Object.assign(ui, { page: 'wiki', done: false, pinned: '' });
  let app = mount(App, { target }); await settle(); await settle();
  let buttons = target.querySelectorAll('.item .btns button');
  let clicked = 0;
  for (const b of buttons) b.addEventListener('click', () => { clicked++; });
  press();
  const wikiButtons = buttons.length, wikiClicked = clicked;
  unmount(app);
  // the answered list
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (keys only on the Answer screen: Knowledge buttons=' + wikiButtons + ' clicked=' + wikiClicked + ', answered buttons=' + doneButtons + ' clicked=' + doneClicked + ')');
  return ok;
}


export function checkPlanningContext(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const sp = { id: 'needs-plan', type: 'noul', display_options: {} };
  const it = { trace_id: 'planning', answerable: true, band: 'ask', p: .6, top: 'yes', model: 'm', at: '2026-10-22T00:00:00Z', choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }], inputs: { open: '列の並びが未決 <img src=x>', agreed: 'AIによる整理（実行許可や人の正解ではない）: CSVとして出力する範囲は合意済み\n今回の人の発言: AIによる整理（実行許可や人の正解ではない）: と書いた', request: '出力機能を追加して' } };
  const app = mount(ItemCard, { target, props: { sp, it } }); flushSync();
  const labels = [...target.querySelectorAll('.field-label')].map(el => el.textContent);
  const named = labels[0] === t('inputRequest') && labels[1]?.includes('もう決まっている') && labels[2]?.includes('まだ決まっていない');
  // The preface "AIによる整理（…）: " passed to the model is removed on the page (only at the start; the same text inside the person's message stays)
  const agreedShown = target.querySelector('[data-input=agreed] .message-body')?.textContent || '';
  const evidence = agreedShown.startsWith('CSVとして出力する範囲は合意済み') && agreedShown.includes('今回の人の発言: AIによる整理（実行許可や人の正解ではない）: と書いた') && target.textContent.includes(it.inputs.open) && !target.querySelector('img');
  unmount(app);
  // 0.9.2: the preface is English from now on ("AI's organizing (…): "); records made before keep the Japanese one, and both are removed
  const en = mount(ItemCard, { target, props: { sp, it: { ...it, trace_id: 'planning-en', inputs: { ...it.inputs, agreed: "AI's organizing (not permission to act, not the person's truth): The CSV range is agreed" } } } }); flushSync();
  const englishRemoved = (target.querySelector('[data-input=agreed] .message-body')?.textContent || '') === 'The CSV range is agreed';
  unmount(en);
  const legacy = mount(ItemCard, { target, props: { sp, it: { ...it, truth: 'no', truth_at: it.at, legacy_planning: true, record_question: '以前の質問' } } }); flushSync();
  const separate = target.textContent.includes('以前の質問') && target.textContent.includes('過去の形式') && !target.querySelector('.answer-choice') && !target.querySelector('button.weak.redo[title]');
  unmount(legacy);
  // 0.9.2: a truth an agent sent through MCP (source mcp) is not shown as the person's answer
  const viaMCP = mount(ItemCard, { target, props: { sp, it: { ...it, trace_id: 'planning-mcp', truth: 'no', truth_at: it.at, source: 'mcp' } } }); flushSync();
  const agentTruth = !target.textContent.includes('あなたの答え') && target.textContent.includes('エージェントが送った答え');
  unmount(viaMCP);
  // a truth from the outcome (results, reflection) is not the person's answer either; a person's own answer (ui) still is
  const viaOutcome = mount(ItemCard, { target, props: { sp, it: { ...it, trace_id: 'planning-outcome', truth: 'no', truth_at: it.at, source: 'outcome' } } }); flushSync();
  const outcomeTruth = !target.textContent.includes('あなたの答え') && target.textContent.includes('結果から付いた答え');
  unmount(viaOutcome);
  const viaUnknown = mount(ItemCard, { target, props: { sp, it: { ...it, trace_id: 'planning-unknown', truth: 'no', truth_at: it.at, source: 'replay-x' } } }); flushSync();
  const unknownTruth = !target.textContent.includes('あなたの答え') && target.textContent.includes('正解: ');
  unmount(viaUnknown);
  const viaPerson = mount(ItemCard, { target, props: { sp, it: { ...it, trace_id: 'planning-ui', truth: 'no', truth_at: it.at, source: 'ui' } } }); flushSync();
  const personTruth = target.textContent.includes('あなたの答え');
  unmount(viaPerson);
  i18n.lang = lang;
  const ok = named && evidence && englishRemoved && separate && agentTruth && outcomeTruth && unknownTruth && personTruth;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (order of request, agreed, open=' + named + ', summary and grounds shown as text=' + evidence + ', English preface removed=' + englishRemoved + ', old format kept distinct=' + separate + ', MCP truth not shown as yours=' + agentTruth + ', outcome truth not shown as yours=' + outcomeTruth + ', unknown source not shown as yours=' + unknownTruth + ', person truth shown as yours=' + personTruth + ')');
  return ok;
}


export function checkCalibrationReport(target) {
  const saved = { done: ui.done }, lang = i18n.lang;
  ui.done = false; i18n.lang = 'ja';
  let summaries = true, closed = true, states = true;
  // quality-own: a question whose other wordings nothing tries (not one of the hook's questions); one_answer: every truth on one side (0.9.4 audit)
  for (const kind of ['quality', 'quality-own', 'one_answer', 'insufficient', 'written', 'stale']) {
    const reason = kind === 'quality-own' ? 'quality' : kind;
    const result = { reason, tries_wordings: kind !== 'quality-own', human_truths: reason === 'insufficient' ? 2 : 50, min_n: 50, model: 'm', hash: reason === 'stale' ? 'old' : 'current', at: '2026-10-22 00:00:00' };
    const sp = { id: 'needs-plan', type: 'noul', min_n: 50, keys: [{ model: 'm', hash: 'current', n: 50, state: reason === 'written' ? 'calibrated' : 'ready', method: 'identity' }], records: 50, labels: 50, queue: { items: [] }, calibration_result: result, last_calibration: 'diagnostic marker: long technical log' };
    const app = mount(SpecPanel, { target, props: { sp } }); flushSync();
    const report = target.querySelector('.calibration-report');
    const detail = report?.querySelector('details');
    closed &&= !!detail && !detail.open && detail.querySelector('pre')?.textContent.includes('diagnostic marker') && !target.querySelector('.decision-panel > pre');
    const text = report?.querySelector('.calibration-summary')?.textContent || '';
    if (kind === 'quality') summaries &&= text.includes('50') && text.includes('見分けられません') && text.includes('別の聞き方を試します') && text.includes('あなたがすることはありません') && !text.includes('不足');
    if (kind === 'quality-own') summaries &&= text.includes('50') && text.includes('見分けられません') && !text.includes('別の聞き方を試します') && !text.includes('あなたがすることはありません');
    if (kind === 'one_answer') summaries &&= text.includes('50') && text.includes('片方だけ') && !text.includes('見分けられません') && !text.includes('別の聞き方');
    if (reason === 'insufficient') summaries &&= text.includes('2') && text.includes('50') && text.includes('届いていません');
    if (reason === 'written') summaries &&= text.includes('保存しました');
    const progress = target.querySelector('.learning-line')?.textContent || '';
    if (reason === 'quality' || reason === 'one_answer') states &&= progress.includes('見送り') && !progress.includes('線を引けます') && !target.querySelector('.pill.ready')?.textContent.includes('線を引けます');
    if (reason === 'stale') states &&= progress.includes(t('plainReady')) && !progress.includes('線を引けます');
    if (reason === 'written') states &&= progress.includes(t('plainAuto'));
    unmount(app);
  }
  Object.assign(ui, saved); i18n.lang = lang;
  const ok = summaries && closed && states;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (calibration reason summarized=' + summaries + ', diagnostic log collapsed=' + closed + ', skipped kept distinct from current state=' + states + ')');
  return ok;
}


export function checkCorrection(target) {
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


export function checkMemoryEffect(target) {
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

// The person's past answers the AI's reflection rejected are listed in the knowledge section (they left the list of answered
// decisions): the answer in the page's words, the request, how often it went unused. The stop of the whole kind is said in one
// line. Nothing is shown when there is nothing to list. Text from requests is never rendered as HTML.
// The person can undo both: an answer's undo sends its trace to v1/answers/restore and the answer leaves the list; the stop's undo
// sends stop and the line goes away. A refused undo keeps what was there.
export async function checkRejectedAnswers(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  let refuse = false;
  globalThis.fetch = async (url, opts) => {
    if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) });
    if (refuse && opts?.body) return { ok: false, status: 409, json: async () => ({ error: 'this answer is not rejected' }) };
    return { ok: true, json: async () => opts?.body ? { restored: true } : { specs: [] } };
  };
  const settle = () => new Promise(resolve => setTimeout(resolve, 0));
  const item = { trace_id: 'aaaaaaaaaaaaaaaa', spec: 'needs-plan', request: 'Fix the README steps <img src=x onerror=alert(1)>', truth: 'yes', question: { en: 'Should it have asked you first?', ja: 'the same question in Japanese' }, answer: { en: 'It should have asked', ja: 'the same answer in Japanese' }, unused: 2, at: '2026-11-10T00:00:00Z' };
  const kept = { ...item, trace_id: 'bbbbbbbbbbbbbbbb', request: 'Keep the badges' };
  let app = mount(Notebook, { target, props: { wiki: { rejected_answers: { stopped: false, unused: 2, helpful: 0, items: [item, kept] } } } });
  flushSync();
  let section = target.querySelector('.rejected-answers');
  const listed = !!section?.querySelector('[data-rejected="aaaaaaaaaaaaaaaa"]') && (section.textContent.includes(item.answer.ja) || section.textContent.includes(item.answer.en)) && (section.textContent.includes(item.question.ja) || section.textContent.includes(item.question.en)) && section.textContent.includes('Fix the README steps') && section.textContent.includes('2') && !section.querySelector('[data-rejected-stopped]');
  const inert = section?.textContent.includes('<img src=x onerror=alert(1)>') && !section?.querySelector('img');
  refuse = true;
  section?.querySelector('[data-rejected-restore="aaaaaaaaaaaaaaaa"]')?.click();
  await settle(); flushSync();
  const refusedKept = calls.length === 1 && !!target.querySelector('[data-rejected="aaaaaaaaaaaaaaaa"]');
  refuse = false; calls.length = 0;
  target.querySelector('[data-rejected-restore="aaaaaaaaaaaaaaaa"]')?.click();
  await settle(); flushSync();
  const restored = calls.some(call => call.url.endsWith('v1/answers/restore') && call.body.trace_id === item.trace_id && !('stop' in call.body)) && !target.querySelector('[data-rejected="aaaaaaaaaaaaaaaa"]') && !!target.querySelector('[data-rejected="bbbbbbbbbbbbbbbb"]');
  unmount(app);
  calls.length = 0;
  app = mount(Notebook, { target, props: { wiki: { rejected_answers: { stopped: true, unused: 12, helpful: 0, items: [] } } } });
  flushSync();
  section = target.querySelector('.rejected-answers');
  const stopped = !!section?.querySelector('[data-rejected-stopped]') && section.querySelector('[data-rejected-stopped]').textContent.includes('12') && !section.querySelector('[data-rejected]');
  section?.querySelector('[data-rejected-stopped-restore]')?.click();
  await settle(); flushSync();
  const stopUndone = calls.some(call => call.url.endsWith('v1/answers/restore') && call.body.stop === true && !('trace_id' in call.body)) && !target.querySelector('[data-rejected-stopped]');
  unmount(app);
  app = mount(Notebook, { target, props: { wiki: { rejected_answers: { stopped: false, unused: 1, helpful: 0, items: [] } } } });
  flushSync();
  const quiet = !target.querySelector('.rejected-answers');
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = listed && inert && refusedKept && restored && stopped && stopUndone && quiet;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (rejected answer listed=' + listed + ', inert request=' + inert + ', refused undo keeps the answer=' + refusedKept + ', undo sends the answer and it leaves=' + restored + ', stop of past answers said=' + stopped + ', undo of the stop sent and the line leaves=' + stopUndone + ', nothing shown when nothing is rejected=' + quiet + ')');
  return ok;
}

export function checkFullText(target) {
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


export async function checkReview(target) {
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, opts) => {
    if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) });
    return { ok: true, json: async () => opts?.body ? { truth: 'no' } : { specs: [] } };
  };
  const item = { trace_id: 'review-1', band: 'ask', top: 'yes', p: .9, at: '2026-10-20T00:00:00Z', audit: true, review_reason: 'uncertain', answerable: true, inputs: { request: '起動手順を日本語にして', summary: '直しました' }, choices: [{ truth: 'yes', label: '完了' }, { truth: 'no', label: '未完了' }], review: { truth: 'yes', reason: '<img src=x onerror=alert(1)> 仮採点の根拠', source_material: { reply: '英語の手順が残っています' } } };
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


// 0.11: a decision waits for the person only when the answering AI and the agent disagreed. The card puts both answers side by side (the
// answering AI's with its confidence, the agent's with its reason) and the person picks one with the usual answer buttons
export async function checkDisagreement(target) {
  const lang = i18n.lang; i18n.lang = 'en';
  const realFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) }); return { ok: true, json: async () => ({ truth: 'no' }) }; };
  const choices = [{ truth: 'yes', label: 'Finished' }, { truth: 'no', label: 'Not finished' }];
  const item = { trace_id: 'pair-1', band: 'ask', top: 'yes', p: .8, at: '2026-10-28T00:00:00Z', audit: true, review_reason: 'disagree', waits_for_person: true, answerable: true, inputs: { request: 'translate the setup steps' }, choices,
    pair: { model: 'yes', model_confidence: .8, agent: 'no', agent_reason: 'the person said <b>English steps</b> remain' }, review: { truth: 'no', reason: 'the person said English steps remain', source_material: { reply: 'English steps remain' } } };
  const sp = { id: 'done-as-asked', type: 'noul', queue: { items: [item] }, labels: 0, unlabeled: 1, waiting_for_person: 1 };
  let ok = true; const seen = {};
  for (const question of ['', 'Was the requested work finished?']) { // the AI page and the Answer screen
    const app = mount(ItemCard, { target, props: { sp, it: item, question } }); flushSync();
    const model = target.querySelector('[data-pair-side="model"]')?.textContent || '';
    const agent = target.querySelector('[data-pair-side="agent"]')?.textContent || '';
    seen[question ? 'answer' : 'ai'] = { model: model.includes(t('pairModel')) && model.includes('Finished') && model.includes('80'), agent: agent.includes(t('pairAgent')) && agent.includes('Not finished') && agent.includes('English steps') && !target.querySelector('.ai-pair b'), notice: target.textContent.includes(t('reviewDisagree')) };
    ok = ok && Object.values(seen[question ? 'answer' : 'ai']).every(Boolean);
    unmount(app);
  }
  const app = mount(ItemCard, { target, props: { sp, it: item } }); flushSync();
  target.querySelector('button[data-truth="no"]')?.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  const sent = calls.some(c => c.url.endsWith('v1/label') && c.body.truth === 'no' && c.body.trace_id === 'pair-1');
  unmount(app);
  // an agreement is never a card, and a card without a disagreement shows no pair
  const plain = mount(ItemCard, { target, props: { sp, it: { ...item, trace_id: 'plain', review_reason: 'uncertain', pair: { ...item.pair, agent: 'unknown' } } } }); flushSync();
  const noPair = !target.querySelector('[data-pair-side]');
  unmount(plain);
  globalThis.fetch = realFetch; i18n.lang = lang;
  ok = ok && sent && noPair;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (both answers on a disagreement card=' + JSON.stringify(seen) + ', the person picks one=' + sent + ', no pair otherwise=' + noPair + ')');
  return ok;
}


export async function checkProjectEntrances(target) {
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


// Do not mistake the AI's reply from before this request for the target of this pre-work decision.
export function checkTurnContext(target) {
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
  const readable = previous?.open && previous.textContent.includes('前の仕事は完了しました。末尾') && !previous.textContent.includes('**'); // remove Markdown marks inside the fold too (re-audit W9)
  // only one font-size class (with text-[13px] and text-[15px] together, 15px wins by generation order: audit A-1)
  const sizes = (previous?.querySelector('.message-body')?.className.match(/(?:^|\s)text-\[\d+px\]/g) || []);
  const oneSize = sizes.length === 1 && sizes[0].trim() === 'text-[13px]';
  unmount(app);
  app = mount(SpecPanel, { target, props: { sp: make('done-as-asked', { files: 'changed.go', request: '今回の新しい依頼', summary: '今回の作業を終えました。' }) } });
  flushSync();
  const fields = [...target.querySelectorAll('.text [data-input]')];
  const after = fields[0]?.dataset.input === 'request' && fields[1]?.dataset.input === 'summary' && fields[1].textContent.includes('今回の最後の返答') && target.querySelector('.decision-phase')?.textContent.includes('作業後') && !target.querySelector('details.previous-context');
  unmount(app);
  const ok = before && folded && readable && oneSize && after;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (pre-work target=' + before + ', previous completion reply folded=' + folded + ', original text readable=' + readable + ', one font size for the previous reply=' + oneSize + ', post-work target=' + after + ')');
  return ok;
}


// For noul, top=yes is what the probability is for. At 46%, the page's prediction and "same as the AI" become no/54%.
export async function checkBinaryPrediction(target) {
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
  const redone = target.querySelector('.redo-out')?.textContent.includes('そのまま進めてよい 70%') && target.querySelector('.redo-out')?.textContent.includes('正解と一致しました');
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (yes/no prediction=' + negative + ', saving the same answer=' + submitted + ', choice prediction=' + unchangedChoice + ', redo agreement=' + redone + ')');
  return ok;
}


// 0.9.1: the switch at the top has only 3, "Answer, Knowledge, AI status". The current screen is marked (aria-current), and pressing saves the chosen screen.
// AI status lists the pre-work and post-work questions, and pressing saves the chosen question
export async function checkNavigation(target) {
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (3 screens=' + pages + ', current position=' + current + ', screen choice kept=' + remembered + ', question choice kept=' + tab + ')');
  return ok;
}


// On first open, the Answer screen if something waits for a person, otherwise Knowledge. A chosen screen is not switched on its own. Pressing Enter on the switch at the top does not answer
export async function checkLanding(target) {
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
  // with nothing waiting for a person, it starts from Knowledge
  specs = mkSpecs(0); data.state = null; Object.assign(ui, { page: '', tab: '' });
  app = mount(App, { target }); await settle(); await settle();
  const quietToKnowledge = target.querySelector('[data-page="wiki"]')?.getAttribute('aria-current') === 'page' && !target.querySelector('[data-page="answer"] .badge');
  unmount(app); globalThis.fetch = realFetch; data.state = savedState; Object.assign(ui, saved);
  const ok = pendingFirst && respectedChoice && safeNavigation && shortcutWorks && quietToKnowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer screen first=' + pendingFirst + ', chosen screen kept=' + respectedChoice + ', switching does not answer=' + safeNavigation + ', answer keys=' + shortcutWorks + ', Knowledge when nothing waits=' + quietToKnowledge + ')');
  return ok;
}
