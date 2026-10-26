import Knowledge from '../src/components/Knowledge.svelte';
import Projects from '../src/components/Projects.svelte';
import { load, data } from '../src/lib/state.svelte.js';
// Checks of the page's behavior (jsdom): (1) a change on the language picker changes the language, the text and the saved value. (2) the decision named by ?trace= comes first in the list.
// The Go tests that look for text would not notice if onchange came off (audit 7-2). Built into one file with vite's lib build; scripts/check-ui.mjs calls run() inside jsdom.
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

// A person who came from the hook's "answer later → …/?spec=x&trace=t" sees that item first (otherwise the original order)
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

// 0.9.1: the Answer screen gathers the questions waiting for a person into one list and shows one card at a time. The question's text at the top of the card and above the buttons, and the work's marker.
// Questions only the AI answers and records that do not wait for a person are not shown. Skipping gives the next question's card, and the remaining count is the total over all questions
function checkAnswerMerge(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const item = (trace, text, project, waits = true) => ({ trace_id: trace, at: '2026-10-19T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: text }, inputs_hash: trace, raw: { yes: 0.6, no: 0.4 }, stale: false, answerable: true, waits_for_person: waits, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], rung: 'minimal', audit: false, project });
  const mk = (id, q, items, person = true) => ({ id, type: 'noul', titles: { ja: '題:' + q }, questions: { ja: q }, keys: [], min_n: 50, records: 3, labels: 0, unlabeled: items.length, waiting_for_person: person ? items.filter(i => i.waits_for_person).length : 0, person_answers: person, queue: { items }, tune: null });
  const specs = [mk('wiki-conflict', 'AI の問い', [item('w1', 'MACHINE', 'alpha')], false), mk('needs-plan', '始める前に聞くべきだったか', [item('n1', 'NEEDS-1', 'alpha'), item('n0', 'NOT-WAITING', 'alpha', false)]), mk('done-as-asked', '頼まれたことを終えたか', [item('d1', 'DONE-1', 'beta'), item('d2', 'DONE-2', 'beta')])];
  ui.pinned = ''; skipped.clear();
  const app = mount(Answer, { target, props: { st: { specs } } });
  flushSync();
  const cards = () => [...target.querySelectorAll('.item')];
  const one = cards().length === 1 && cards()[0].textContent.includes('NEEDS-1') && !target.textContent.includes('MACHINE') && !target.textContent.includes('NOT-WAITING');
  // the short title on top of the card and the question once, above the buttons (0.9.2 redesign P1)
  const asked = cards()[0]?.querySelector('.card-question')?.textContent === '題:始める前に聞くべきだったか' && target.querySelector('.answer-heading')?.textContent.includes('始める前に聞くべきだったか') && !!cards()[0]?.querySelector('.project');
  const left = (target.querySelector('[data-answer-left]')?.textContent || '').includes('3');
  target.querySelector('.btns button.skip')?.click(); flushSync();
  const next = cards().length === 1 && cards()[0].textContent.includes('DONE-1') && cards()[0].querySelector('.card-question')?.textContent === '題:頼まれたことを終えたか';
  unmount(app); skipped.clear(); i18n.lang = lang;
  const ok = one && asked && left && next;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer screen: only one card, no AI questions or records that do not wait=' + one + ', question and work marker on the card and above the buttons=' + asked + ', remaining is the total=' + left + ', skip gives the next question\'s card=' + next + ')');
  return ok;
}

// 0.9.2: the agent checks the meaning of a way of asking and adds a translation. When the AI adopted it, the translation and "keep it / revert";
// when it cannot decide, the translation, its view of the difference, and "same meaning / different". Only these 2 (when there is something for the person to do) appear on the Answer screen
async function checkWordingAgent(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const calls = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url, opts) => { if (opts && opts.body) calls.push({ url, body: JSON.parse(opts.body) }); return { ok: true, json: async () => ({ reverted: true, specs: [] }) }; };
  const base = { run: 'r1', winner: 'unasked-first', model: 'm', truths: 50, at: '2026-10-25T00:00:00Z', candidates: [{ name: 'current', top1: 0.3, brier: 0.3, prec_at_act: 0.1, rec_at_act: 0.1, change: '' }, { name: 'unasked-first', top1: 0.8, brier: 0.2, prec_at_act: 0.8, rec_at_act: 0.3, change: 'q' }], decision: 'd', winner_wording: { question: 'Did it change unasked files?' }, current_wording: { question: 'Were the changes within scope?' } };
  const agent = (verdict, extra = {}) => ({ by: 'agent', verdict, same: verdict === 'same', current_translation: '範囲の中で変えたかの和訳', candidate_translation: '頼まれていない物を変えたかの和訳', difference: verdict === 'same' ? '' : '聞く順だけ違う', ...extra });
  const adopted = { ...base, adoptable: true, adopted: true, auto_adopted: true, adopted_path: 'decisions/x-v3.yaml', meaning_checks: [agent('same')] };
  const unsure = { ...base, adoptable: true, adopted: false, meaning_checks: [agent('unsure')] };
  const waiting = { ...base, adoptable: true, adopted: false };
  const kept = { ...adopted, meaning_checks: [agent('same'), { by: 'page', same: true }] };
  const mountWith = latest => { const app = mount(Wording, { target, props: { sp: { id: 'changes-in-scope', type: 'noul', tune: { running: false, proposer: false, variants: [], latest } } } }); flushSync(); return app; };
  let app = mountWith(adopted);
  const text = () => target.textContent;
  const adoptedShows = text().includes('範囲の中で変えたかの和訳') && text().includes('頼まれていない物を変えたかの和訳') && !!target.querySelector('[data-wording-keep]') && !!target.querySelector('[data-wording-undo]');
  target.querySelector('[data-wording-undo]')?.click();
  await new Promise(r => setTimeout(r, 0));
  const undoSent = calls.some(c => c.url.endsWith('v1/tune/review') && c.body.run === 'r1' && c.body.keep === false);
  unmount(app);
  app = mountWith(unsure);
  const unsureShows = text().includes('聞く順だけ違う') && text().includes('頼まれていない物を変えたかの和訳') && !!target.querySelector('[data-wording-same]') && !target.querySelector('[data-wording-undo]');
  unmount(app);
  globalThis.fetch = realFetch; i18n.lang = lang;
  const needs = [wordingNeedsPerson(adopted), wordingNeedsPerson(unsure), wordingNeedsPerson(waiting), wordingNeedsPerson(kept), wordingNeedsPerson({ ...adopted, reverted: true, adopted: false })];
  const answerOnlyWhenPerson = JSON.stringify(needs) === JSON.stringify([true, true, false, false, false]);
  const ok = adoptedShows && undoSent && unsureShows && answerOnlyWhenPerson;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (AI adopted: translation and revert=' + adoptedShows + ', revert is sent=' + undoSent + ', cannot decide: translation and difference=' + unsureShows + ', Answer screen only when the person has something to do=' + answerOnlyWhenPerson + ' ' + JSON.stringify(needs) + ')');
  return ok;
}

// 0.9.2 redesign (P1): the Answer card says the question once (a short title on top, the question only above the buttons), the AI's guess stays folded
// until the person opens "see the AI's guess", the answer buttons look the same before that (only the class top for Enter differs), and "ask the AI again" is in the fold
async function checkAnswerCardOnce(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const it = { trace_id: 'once', at: '2026-10-22T00:00:00Z', band: 'ask', top: 'yes', p: .83, model: 'm', answerable: true, inputs: { request: '依頼の本文' }, choices: [{ truth: 'yes', label: '確認してほしかった' }, { truth: 'no', label: '確認なしで進めてよかった' }] };
  const q = '始める前に確認したほうがよかったですか？';
  const app = mount(ItemCard, { target, props: { sp: { id: 'needs-plan', type: 'noul' }, it: { ...it, top: 'yes', p: .17 }, question: q, title: '始める前に確認してほしかったか' } });
  flushSync();
  const card = target.querySelector('.item');
  const count = (s, w) => s.split(w).length - 1;
  // the question is in the page twice (the top of the card for phones, the stuck bar for wider screens) but shown once at each width (audit 0.9.2 ui low-1)
  const rules = mediaUtilities(globalThis.__shikiiCSS || '');
  const shownText = w => { const c = card.cloneNode(true); for (const e of [...c.querySelectorAll('*')]) if (!shownAt(rules, e, w)) e.remove(); return c.textContent; };
  const once = count(shownText(1440), q) === 1 && count(shownText(390), q) === 1 && target.querySelector('.card-question')?.textContent === '始める前に確認してほしかったか' && (target.querySelector('.answer-heading')?.textContent || '').includes(q);
  // what can be seen before opening the fold: the text outside closed details (summary included)
  const seen = () => { const c = card.cloneNode(true); for (const d of c.querySelectorAll('details:not([open])')) for (const ch of [...d.children]) if (ch.tagName !== 'SUMMARY') ch.remove(); return c.textContent; };
  const fold = target.querySelector('details.guess-fold');
  const choices = [...target.querySelectorAll('.btns button.answer-choice')];
  const looks = new Set(choices.map(b => [...b.classList].filter(c => c !== 'top').sort().join(' ')));
  const hidden = !!fold && !fold.open && !!fold.querySelector('.guess') && !seen().includes('83%') && !seen().includes(t('sameAsAI')) && looks.size === 1 && !seen().includes(t('bandAsk'));
  const enter = target.querySelector('.btns button.top')?.dataset.truth === 'no';
  const redoInFold = !target.querySelector('.btns .redo') && !!fold?.querySelector('button.redo');
  const noPosition = !/\d+ \/ \d+/.test(seen());
  if (fold) { fold.open = true; fold.dispatchEvent(new window.Event('toggle')); flushSync(); }
  const shown = seen().includes('83%') && (target.querySelector('.btns button.top')?.textContent || '').includes(t('sameAsAI'));
  unmount(app); i18n.lang = lang;
  const ok = once && hidden && enter && redoInFold && noPosition && shown;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer card: question once=' + once + ', guess folded and buttons look the same=' + hidden + ', Enter still means the AI guess=' + enter + ', ask again in the fold=' + redoInFold + ', no "1 / N"=' + noPosition + ', guess after opening=' + shown + ')');
  return ok;
}

// 0.9.2 redesign (P2): on the Answer screen the wording box is only the one question for the person. Allowed inside: the translations, the AI's view,
// the English original in a fold, and only the buttons for that question (same / different, or keep / undo). No form, table, candidate list or "measure"
async function checkAnswerWordingBox(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const base = { run: 'r1', winner: 'unasked-first', model: 'm', truths: 50, at: '2026-10-25T00:00:00Z', have_holdout: true, holdout: 0.7, decision: 'unasked-first may be adopted (page, or --write)', candidates: [{ name: 'current', top1: 0.3, brier: 0.3, prec_at_act: 0.1, rec_at_act: 0.1, change: '' }, { name: 'unasked-first', top1: 0.8, brier: 0.2, prec_at_act: 0.8, rec_at_act: 0.3, change: 'q' }], winner_wording: { question: 'Did it change unasked files?' }, current_wording: { question: 'Were the changes within scope?' } };
  const agent = verdict => ({ by: 'agent', verdict, current_translation: '範囲の中で変えたか', candidate_translation: '頼まれていない物を変えたか', difference: verdict === 'same' ? '' : '聞く順だけ違う' });
  const sp = latest => ({ id: 'changes-in-scope', type: 'noul', titles: { ja: '必要な物だけか' }, questions: {}, keys: [], min_n: 50, records: 1, labels: 0, unlabeled: 0, waiting_for_person: 0, person_answers: true, queue: { items: [] }, tune: { running: false, proposer: true, variants: [{ name: 'x' }], latest } });
  const allowed = { unsure: ['wordingSame', 'wordingDifferent'], adopted: ['wordingKeep', 'wordingUndo'] };
  const results = {};
  for (const [name, latest] of [['unsure', { ...base, adoptable: true, adopted: false, meaning_checks: [agent('unsure')] }], ['adopted', { ...base, adoptable: true, adopted: true, auto_adopted: true, adopted_path: 'decisions/x-v2.yaml', meaning_checks: [agent('same')] }]]) {
    const app = mount(Answer, { target, props: { st: { specs: [sp(latest)] } } }); flushSync();
    const box = target.querySelector('[data-answer-wording]');
    const buttons = box ? [...box.querySelectorAll('button')] : [];
    const onlyItsButtons = buttons.length === 2 && buttons.every(b => allowed[name].some(k => k in b.dataset));
    const noTools = !!box && !box.querySelector('form, table, input, textarea, select') && !box.textContent.includes('--write') && !box.textContent.includes('top-1');
    const reads = !!box && box.textContent.includes('頼まれていない物を変えたか') && box.textContent.includes('範囲の中で変えたか') && (name === 'adopted' || box.textContent.includes('聞く順だけ違う'));
    results[name] = onlyItsButtons && noTools && reads;
    unmount(app);
  }
  i18n.lang = lang;
  const ok = results.unsure && results.adopted;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer wording box is one question: AI unsure=' + results.unsure + ', AI adopted=' + results.adopted + ')');
  return ok;
}

// 0.9.2 redesign (P7): with nothing waiting, the Answer screen says only "nothing for you to do right now" (allowed text: the title and that sentence)
function checkAnswerNothingToDo(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const sp = { id: 'needs-plan', type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 0, labels: 0, unlabeled: 0, waiting_for_person: 0, person_answers: true, queue: { items: [] }, tune: null };
  skipped.clear();
  const app = mount(Answer, { target, props: { st: { specs: [sp] } } }); flushSync();
  const text = (target.querySelector('[data-answer]')?.textContent || '').replace(/[\s✓]/g, '');
  const ok = text === (t('answerTitle') + t('answerEmpty')).replace(/\s/g, '') && !target.querySelector('[data-answer] button');
  unmount(app); i18n.lang = lang;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer screen with nothing waiting says only that: ' + JSON.stringify(text) + ')');
  return ok;
}

// 0.9.2 redesign (P6): the AI page shows status by default. With nothing waiting for the person, the "to answer (you)" list is not there (allowed lists: results and answered),
// a question whose wording cannot tell the answers apart says there is nothing for the person to do and its full bar is gray, and the review quality is one sentence with the numbers folded
function checkAiPageStatus(target) {
  const lang = i18n.lang; i18n.lang = 'ja'; const saved = { ...ui };
  Object.assign(ui, { done: false, results: false });
  const item = { trace_id: 'r1', at: '2026-10-23T00:00:00Z', band: 'act', top: 'yes', p: 0.9, model: 'm', inputs: { request: 'x' }, answerable: true, waits_for_person: false, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }] };
  const sp = { id: 'done-as-asked', type: 'noul', titles: { ja: '頼まれたことを終えたか' }, questions: {}, keys: [{ model: 'm', hash: 'h', n: 52, state: 'not_separable' }], min_n: 50, records: 60, labels: 52, unlabeled: 1, waiting_for_person: 0, awaiting_results: 1, person_answers: true, truth_source: 'person', queue: { items: [item] }, tune: null,
    review_quality: { scored: 16, uncertain: 0, checked: 15, correct: 14, uncertain_checked: 0, random_checked: 1, random_correct: 1, audit_every: 5 } };
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
async function checkPagePurpose(target) {
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

// The ways-of-asking section has inputs for a person to write a candidate (and option descriptions for a choice). Sending it delivers the name, question and options parsed from "id: description" to v1/tune/variant
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
  // A yes / no question has no input for option descriptions
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

// The summary of effects (0.9.1; the band at the top in 0.7.4): "Answer" at the top shows the total waiting for a person. AI status shows delivered lessons, the work routine, guards and their last results,
// and learning progress per question, one line each. The Answer screen does not show the AI's internal numbers
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
  const body = box?.querySelector('.summary-guards code')?.textContent === 'changelog-pair' && (box?.textContent || '').includes('CHANGELOG_ja.md が変わっていない') && (box?.textContent || '').includes('TypeSafe Jev') && (box.querySelector('[data-rituals]')?.textContent || '').includes('進め方を整理 2 / 5');
  const specs = target.querySelectorAll('[data-ai-specs] [data-tab]').length === 4 && (target.querySelector('[data-ai-specs]')?.textContent || '').includes('学習中 0 / 50');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = badge && quietAnswer && body && specs;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (summary of effects: total waiting on Answer=' + badge + ', no internal numbers on the Answer screen=' + quietAnswer + ', guards and last results in AI status=' + body + ', progress per question=' + specs + ')');
  return ok;
}


// 0.8.0: a decision a person does not answer (wiki conflict) shows no waiting marker and no answer buttons on the card, and shows a one-line notice. Keys do not press anything either.
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


// 0.8.0: the Knowledge section shows "what the AI is accumulating" as a hierarchy of clusters connected by related links. Opening a cluster lists its articles, and pressing a title goes to the text. Articles with no connections are "standalone articles"
async function checkNotebookTree(target) {
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui };
  const mk = (id, title, related = []) => ({ id, revision: 'r' + id, title, lesson: title + 'の決まり', applies_to: title + 'の時', sources: [{ id: 'src' + id }], related, status: 'hypothesis' });
  // a2 is linked from 2 (avoids ties, because the order of titles on a tie depends on the locale)
  const lessons = [mk('a1', '公開の手順', ['a2']), mk('a2', '監査'), mk('a3', '変更履歴', ['a2']), mk('b1', 'コミットの説明', ['b2']), mk('b2', 'コミットの作者'), mk('c1', '単独')];
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: loadingSpecs() } : url === 'v1/wiki' ? { claims: [], lessons, sources: [] } : url.startsWith('v1/wiki/notebook') ? { lessons: [lessons[1]] } : { items: [] } });
  Object.assign(ui, { page: 'wiki', done: false, results: false, pinned: '' });
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const app = mount(App, { target }); await settle(); await settle();
  const groups = [...target.querySelectorAll('[data-tree-group]')].map(g => g.dataset.treeGroup);
  const grouped = groups.length === 3 && groups[0] === 'a2' && groups[1] === 'b2' && groups[2] === 'single'; // largest cluster first. A cluster's name is its most-linked article
  const closed = !target.querySelector('[data-tree-page]');
  target.querySelector('[data-tree-group="a2"] .tree-toggle')?.click(); flushSync();
  const pages = [...target.querySelectorAll('[data-tree-group="a2"] [data-tree-page]')].map(b => b.dataset.treePage);
  const opened = pages.length === 3 && pages.includes('a1') && pages.includes('a3');
  const how = (target.querySelector('[data-wiki-tree] .k')?.textContent || '').includes('6');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = grouped && closed && opened && how;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (knowledge hierarchy: clusters=' + grouped + ' ' + JSON.stringify(groups) + ', starts collapsed=' + closed + ', opening shows articles=' + opened + ', overview=' + how + ')');
  return ok;
}


// Audit R5-3: the lists of questions a person answers. The marker on "Answer" at the top is the total waiting for a person (not the total unanswered), and AI-only questions are not counted.
// The question first opened in AI status is one with something waiting for a person. The marker is the count waiting for a person. "Unanswered (you)" has cards with buttons, and "Settled by the next request or reflection" has read-only cards.
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
async function checkKeysOnlyOnDecisions(target) {
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


// Audit 0.9.1 (A1, M8, M9, M10): answering moves on to the next card, and "remaining" and the marker on "Answer" at the top go down. With 0 waiting, the empty view; before the list is loaded, loading
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
  // with 0 waiting, the empty view; if the list is not loaded yet, loading
  let a = mount(Answer, { target, props: { st: { specs: [spec('needs-plan', 0)] } } }); flushSync();
  const empty = !!target.querySelector('[data-answer-empty]') && !target.querySelector('.item');
  unmount(a);
  a = mount(Answer, { target, props: { st: { specs: [spec('needs-plan', 2)] } } }); flushSync();
  const loading = !!target.querySelector('[data-answer] [role="status"]') && !target.querySelector('[data-answer-empty]');
  unmount(a); i18n.lang = lang;
  const ok = next && left && empty && loading;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (answering gives the next card=' + next + ', remaining and marker go down=' + left + ', 0 waiting is the empty view=' + empty + ', loading before the list=' + loading + ')');
  return ok;
}

// Audit 0.9.1 (A2): even if you answer while the 15-second reload is waiting for the list's response, that old response does not bring back the answered card
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (answered card does not come back from an old reload=' + !back + ')');
  return ok;
}


// Re-audit 0.9.1 (low-UI1): switching screens while an answer is being sent does not drop the load of the screen switched to (only loads already waiting before the answer are dropped)
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
  console.log((shown ? 'check-ui: ok' : 'check-ui: FAIL') + ' (load of the screen switched to while sending is not dropped=' + shown + ')');
  return shown;
}

// Re-audit 0.9.1 (W4, W5): after answering, "remaining" goes down only for records not yet answered that wait for a person (not for a changed answer, nor for records that do not wait for a person)
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (remaining goes down only for new waits: changed answer=' + relabel + ', not waiting for a person=' + notWaiting + ', new wait=' + fresh + ')');
  return ok;
}


// 0.9.1 → 0.9.2 redesign (P3): the rules box at the top of Knowledge shows only the "いつも" (always) rules, and "see all rules" opens the rest (always first, then newest).
// The "new" marker goes only on rules changed in the last 7 days, and not at all when every rule is new (it would not tell anything apart).
// The text panel sits beside the rules box, so on a wide screen pressing a rule does not scroll; on a phone it scrolls to the text
function checkRulesPanel(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  try { localStorage.removeItem('shikii.scene'); } catch { /* fine if missing */ }
  const mk = (i, kind, scene) => ({ id: String(i).padStart(16, '0'), title: 'ルール ' + i, lesson: '教訓 ' + i, applies_to: 'a', kind, scene, sources: [], related: [] });
  const lessons = [mk(1, 'rule', 'コミットする時'), mk(2, 'rule', 'いつも'), mk(3, 'howto', 'コミットする時'), mk(4, 'rule', '公開する時'), mk(5, 'rule', ' Always ')]; // English always, with surrounding spaces (audit low-4)
  const recent = new Date(Date.now() - 2 * 86400000).toISOString(), old = '2026-01-01T00:00:00Z';
  const wiki = { lessons, lesson_meta: { [lessons[0].id]: { updated: recent }, [lessons[1].id]: { updated: old }, [lessons[3].id]: { updated: old }, [lessons[4].id]: { updated: '2025-01-01T00:00:00Z' } }, inbox: [{ kind: 'harmful', lesson_id: lessons[2].id, lesson: 'x', count: 1 }] };
  const app = mount(Lessons, { target, props: { wiki } }); flushSync();
  const panel = target.querySelector('[data-rules]');
  const rows = () => [...target.querySelectorAll('[data-rules] [data-rule]')].map(b => b.dataset.rule);
  const named = (panel?.textContent || '').includes(t('rulesTitle'));
  const alwaysOnly = rows().join(',') === [lessons[1].id, lessons[4].id].join(',');
  target.querySelector('[data-rules-all]')?.click(); flushSync();
  const order = rows().join(',') === [lessons[1].id, lessons[4].id, lessons[0].id, lessons[3].id].join(','); // いつも (and always) first, then newest first
  const fresh = !!target.querySelector('[data-rule="' + lessons[0].id + '"] [data-rule-new]') && !target.querySelector('[data-rule="' + lessons[3].id + '"] [data-rule-new]');
  unmount(app);
  // when every rule is new, no marker at all
  const allNew = Object.fromEntries(lessons.map(l => [l.id, { updated: recent }]));
  const app2 = mount(Lessons, { target, props: { wiki: { ...wiki, lesson_meta: allNew } } }); flushSync();
  target.querySelector('[data-rules-all]')?.click(); flushSync();
  const noMarkWhenAllNew = rows().length === 4 && !target.querySelector('[data-rule-new]');
  unmount(app2);
  const realMatch = globalThis.matchMedia, realScroll = Element.prototype.scrollIntoView;
  const scrolled = [];
  Element.prototype.scrollIntoView = function () { scrolled.push(this.dataset.lessonDetail !== undefined); };
  const settleMicro = () => { flushSync(); return new Promise(r => queueMicrotask(r)); };
  return (async () => {
    try {
      globalThis.matchMedia = () => ({ matches: false }); // wide
      let a = mount(Lessons, { target, props: { wiki } }); flushSync();
      const beside = !!panel && target.querySelector('[data-rules]')?.closest('[data-lesson-grid]') === target.querySelector('[data-lesson-detail]')?.closest('[data-lesson-grid]') && !!target.querySelector('[data-lesson-grid]');
      target.querySelector('[data-rule="' + lessons[1].id + '"]')?.click(); await settleMicro();
      const opens = (target.querySelector('[data-lesson-detail]')?.textContent || '').includes('教訓 2');
      const wideStays = scrolled.length === 0;
      unmount(a);
      globalThis.matchMedia = () => ({ matches: true }); // phone
      a = mount(Lessons, { target, props: { wiki } }); flushSync();
      target.querySelector('[data-rule="' + lessons[1].id + '"]')?.click(); await settleMicro();
      const phoneScrolls = scrolled.length === 1 && scrolled[0];
      unmount(a); i18n.lang = lang;
      const ok = named && alwaysOnly && order && fresh && noMarkWhenAllNew && beside && opens && wideStays && phoneScrolls;
      console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (rules box: name=' + named + ', only always at first=' + alwaysOnly + ', all: always first then newest=' + order + ', recent marker=' + fresh + ', no marker when all are new=' + noMarkWhenAllNew + ', text beside the rules=' + beside + ', pressing shows the text=' + opens + ', no scroll on a wide screen=' + wideStays + ', scrolls on a phone=' + phoneScrolls + ')');
      return ok;
    } finally {
      globalThis.matchMedia = realMatch; Element.prototype.scrollIntoView = realScroll;
    }
  })();
}

// 0.9.2 redesign (P3): Knowledge is one screen for looking things up. A rule is listed once (in the rules box, or in the list when a scene or words narrow it),
// scenes with only one page go under "その他" (other), a search box narrows titles and lessons, the AI's internal numbers (delivered / helped counts, effects) are not here,
// and the text shows Markdown marks as plain text
function checkKnowledgeFront(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  try { localStorage.removeItem('shikii.scene'); } catch { /* fine if missing */ }
  const mk = (i, kind, scene, extra = {}) => ({ id: String(i).padStart(16, '0'), title: '記事 ' + i, lesson: '教訓 ' + i, applies_to: 'a', kind, scene, sources: [], related: [], ...extra });
  const lessons = [mk(1, 'rule', 'いつも'), mk(2, 'rule', 'いつも'), mk(3, 'rule', 'コミットする時'), mk(4, 'rule', 'コミットする時', { title: 'コミットの作者' }), mk(5, 'howto', 'コミットする時', { body: '## 背景\n\n- 項目A\n- `make test` を流す' }), mk(6, 'howto', '公開する時'), mk(7, 'fact', '版を出す時'), mk(8, '', '')];
  const wiki = { lessons, lesson_meta: { [lessons[4].id]: { helpful: 3 } }, inbox: [], memory_effect: { stats: { delivered: 5, helpful: 1, unused: 3, harmful: 1 }, corrections: {}, recent: [{ request: 'r', quote: { lesson: 'l' }, assessment: { result: 'helpful' } }] } };
  const app = mount(Lessons, { target, props: { wiki } }); flushSync();
  const q = sel => target.querySelector(sel);
  // each rule at most once among the rows a person can press (the text panel and related links are not rows)
  const once = () => { const ids = [...target.querySelectorAll('[data-rule], [data-open]')].filter(b => !b.closest('[data-lesson-detail]')).map(b => b.dataset.rule || b.dataset.open); return lessons.filter(l => l.kind === 'rule').every(l => ids.filter(x => x === l.id).length <= 1); };
  const onceAtFirst = once();
  q('[data-rules-all]')?.click(); flushSync();
  const onceOpened = once();
  const scenes = [...target.querySelectorAll('[data-scenes] [data-scene]')].map(b => b.dataset.scene);
  const grouped = JSON.stringify(scenes) === JSON.stringify(['コミットする時', 'いつも', '__other__', '__none__']);
  q('[data-scene="__other__"]')?.click(); flushSync();
  const listed = () => [...target.querySelectorAll('[data-kinds] [data-open]')].map(b => b.dataset.open);
  const other = JSON.stringify(listed().sort()) === JSON.stringify([lessons[5].id, lessons[6].id]);
  q('[data-scene="コミットする時"]')?.click(); flushSync();
  const scene = JSON.stringify(listed().sort()) === JSON.stringify([lessons[2].id, lessons[3].id, lessons[4].id]) && once();
  q('[data-scene-clear]')?.click(); flushSync();
  const box = q('input[data-lesson-search]');
  if (box) { box.value = '作者'; box.dispatchEvent(new window.Event('input', { bubbles: true })); flushSync(); }
  const searched = !!box && JSON.stringify(listed()) === JSON.stringify([lessons[3].id]) && once();
  if (box) { box.value = ''; box.dispatchEvent(new window.Event('input', { bubbles: true })); flushSync(); }
  const quiet = !q('[data-lessons-summary]') && !q('[data-effect]') && !target.textContent.includes('役立った') && !target.textContent.includes('届け');
  target.querySelector('[data-open="' + lessons[4].id + '"]')?.click(); flushSync();
  const body = q('[data-lesson-detail]')?.textContent || '';
  const plain = body.includes('背景') && body.includes('・項目A') && body.includes('make test を流す') && !body.includes('##') && !body.includes('- 項目') && !body.includes('`');
  unmount(app); i18n.lang = lang;
  const ok = onceAtFirst && onceOpened && grouped && other && scene && searched && quiet && plain;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Knowledge front: rules listed once=' + onceAtFirst + '/' + onceOpened + ', scenes with one page under other=' + grouped + ' ' + JSON.stringify(scenes) + ', other lists them=' + other + ', a scene lists its rules too=' + scene + ', search narrows=' + searched + ', no AI numbers=' + quiet + ', Markdown as plain text=' + plain + ')');
  return ok;
}

// Final audit 0.9.1 (low-F1): an old list from a reload that started while the answer was being sent does not bring back the answered card
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
  holdQueue = true; const reload = load(); await settle(); await settle(); // a reload starts while sending, and its list is still from before the answer
  releaseLabel?.({ truth: 'yes' }); await settle(); await settle();
  releaseQueue?.({ items: [item('a', 'FIRST'), item('b', 'SECOND')] }); await reload; await settle(); await settle(); await settle();
  const back = !!target.querySelector('.item')?.textContent.includes('FIRST');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  console.log((!back ? 'check-ui: ok' : 'check-ui: FAIL') + ' (answered card does not come back from a reload during sending=' + !back + ')');
  return !back;
}

export async function run() {
  const target = document.getElementById('app');
  const quietOK = checkAnswerNothingToDo(target);
  const knowledgeFrontOK = checkKnowledgeFront(target);
  const aiStatusOK = checkAiPageStatus(target);
  const purposeOK = await checkPagePurpose(target);
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
  const rulesOK = await checkRulesPanel(target);
  const advanceOK = await checkAnswerAdvances(target);
  const revertOK = await checkAnswerNotRevertedByOldLoad(target);
  const onceOK = await checkAnswerCardOnce(target);
  const wordingBoxOK = await checkAnswerWordingBox(target);
  const auditOK = [await checkAuditWordingWithoutWaiting(target), await checkWordingLanding(target), await checkAnswerKeys(target), await checkWordingAskSends(target), checkPhoneAnswerBar(target), checkWordingPlain(target)].every(Boolean);
  const ok = auditOK && aiStatusOK && purposeOK && knowledgeFrontOK && onceOK && wordingBoxOK && quietOK && reloadOK && rulesOK && switchOK && countOK && advanceOK && revertOK && lessonsOK && handoffOK && conflictsOK && summaryOK && keysOK && machineOnlyOK && treeOK && lanesOK && planningOK && calibrationOK && loadingOK && orderingOK && lazyNavOK && evidenceOK && contextOK && binaryOK && navigationOK && focusOK && landingOK && checkLang(target) && checkPinned(target) && checkAnswerMerge(target) && await checkWording(target) && await checkWordingAgent(target) && await checkNotebook(target) && checkMemoryEffect(target) && checkCorrection(target) && await checkReview(target) && checkFullText(target) && await checkProjectEntrances(target);
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (knowledge text=' + article + ', HTML shown as text=' + escaped + ', opens related=' + followed + ', back to the list=' + returned + ')');
  return ok;
}

function checkPlanningContext(target) {
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (calibration reason summarized=' + summaries + ', diagnostic log collapsed=' + closed + ', skipped kept distinct from current state=' + states + ')');
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
    const body = JSON.parse(opts.body);
    calls.push({ url, body });
    // a long source comes in two parts; the page must read the second one and show the whole body
    if (body.sources?.[0] === '0000000000000003') return { ok: true, json: async () => body.cursor === 'part-2' ? { observations: [{ id: '0000000000000003', content: 'PART-TWO-END' }], observation_part: { id: '0000000000000003', from: 9, to: 21, bytes: 21 } } : { observations: [{ id: '0000000000000003', content: 'PART-ONE-' }], observation_part: { id: '0000000000000003', from: 0, to: 9, bytes: 21 }, next_cursor: 'part-2' } };
    return { ok: true, json: async () => ({ source_material: [{ id: '0000000000000001', request: 'README installation', reply: '<img src=x onerror=alert(1)> missing startup steps' }], originals: { '0000000000000001': [{ original_inputs: { summary: 'FULL-TEXT-END' } }] }, observations: [{ id: '0000000000000002', kind: 'test', content: 'OBSERVED-TEST-PASS', version: 'fixture-v1', observed_at: '2026-01-01T00:00:00Z' }] }) };
  };
  const wiki = { notebook_project: 'alpha', pending_sources_count: 1, knowledge_usage: { window: 256, prepared: 3, with_matches: 2, with_citations: 1, citations: 1, context_bytes: 450, lookup_milliseconds: 2.5 }, maintenance: [{ page_id: 'a', title: 'Source review', revision: 'r1', key: 'k1' }], lessons: [{ id: 'a', title: '<img src=x onerror=alert(1)>', lesson: 'Include model startup steps', applies_to: 'README installation', sources: [{ id: '0000000000000001', hash: 'h' }, { id: '0000000000000002', hash: 'h2' }, { id: '0000000000000003', hash: 'h3' }] }] };
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
  if (buttons[2]) buttons[2].click();
  for (let n = 0; n < 5; n++) await new Promise(resolve => setTimeout(resolve, 0));
  flushSync();
  const parts = target.textContent.includes('PART-ONE-PART-TWO-END') && !target.textContent.includes('observation_part') && calls.some(c => c.body.cursor === 'part-2');
  const usage = target.querySelector('[data-knowledge="prepared"]')?.textContent.includes('3') && target.querySelector('[data-knowledge="with_citations"]')?.textContent.includes('1');
  const maintenance = target.querySelector('[data-maintenance="a"]')?.textContent.includes('Source review');
  const inertText = !target.querySelector('img') && target.textContent.includes('<img src=x onerror=alert(1)>');
  unmount(app);
  globalThis.fetch = realFetch;
  const ok = hasLesson && readSource && observed && parts && usage && maintenance && inertText;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (notebook lesson=' + hasLesson + ', source lookup=' + readSource + ', observed sources=' + observed + ', long source parts joined=' + parts + ', usage=' + usage + ', maintenance=' + maintenance + ', quoted HTML is text=' + inertText + ')');
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

// Do not mistake the AI's reply from before this request for the target of this pre-work decision.
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (3 screens=' + pages + ', current position=' + current + ', screen choice kept=' + remembered + ', question choice kept=' + tab + ')');
  return ok;
}

// You can answer after reading the conversation, and a yes/no question does not show a duplicate "not …".
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
  // Answer screen (question passed): the question at the top of the card and above the buttons, and the button bar sticks to the bottom of the screen. The AI's internal markers (band, model) are not shown
  const app2 = mount(ItemCard, { target, props: { sp: { id: 'needs-plan', type: 'noul' }, it, question: '始める前に聞くべきだったか', title: '始める前の確認' } });
  flushSync();
  const asked = target.querySelector('.card-question')?.textContent === '始める前の確認' && target.querySelector('.answer-heading')?.textContent.includes('始める前に聞くべきだったか') && target.querySelector('.answer-bar')?.classList.contains('sticky') && !target.textContent.includes('discard');
  unmount(app2);
  const ok = readingOrder && clearChoices && answerPrompt && asked;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (conversation then answer=' + readingOrder + ', yes/no without duplicates=' + clearChoices + ', where to answer=' + answerPrompt + ', Answer screen card shows the question=' + asked + ')');
  return ok;
}

// On first open, the Answer screen if something waits for a person, otherwise Knowledge. A chosen screen is not switched on its own. Pressing Enter on the switch at the top does not answer
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
  // with nothing waiting for a person, it starts from Knowledge
  specs = mkSpecs(0); data.state = null; Object.assign(ui, { page: '', tab: '' });
  app = mount(App, { target }); await settle(); await settle();
  const quietToKnowledge = target.querySelector('[data-page="wiki"]')?.getAttribute('aria-current') === 'page' && !target.querySelector('[data-page="answer"] .badge');
  unmount(app); globalThis.fetch = realFetch; data.state = savedState; Object.assign(ui, saved);
  const ok = pendingFirst && respectedChoice && safeNavigation && shortcutWorks && quietToKnowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer screen first=' + pendingFirst + ', chosen screen kept=' + respectedChoice + ', switching does not answer=' + safeNavigation + ', answer keys=' + shortcutWorks + ', Knowledge when nothing waits=' + quietToKnowledge + ')');
  return ok;
}

// Shows the API's evidence, and does not make undefined out of missing items.
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (evidence shown=' + complete + ', missing items shown=' + partial + ', shown as text=' + escaped + ')');
  return ok;
}


function loadingSpecs() {
  return ['needs-plan', 'done-as-asked', 'prompt-kind', 'changes-in-scope'].map(id => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 1, waiting_for_person: 1, person_answers: true }));
}

// every question a person answers has its way of asking fetched exactly once per load (one request per question, not per card)
const tunesOnce = (calls, ids) => ids.every(id => calls.filter(u => u === 'v1/tune?spec=' + id).length === 1) && calls.filter(u => u.startsWith('v1/tune')).length === ids.length;
// Fetch only what the current screen needs, plus the ways of asking of every question a person answers (the wording box on the Answer screen, the marker on "Answer" and the landing
// screen need them; audit 0.9.2 ui middle-1). The Answer screen: the lists of questions with something waiting; AI status: the open question's list (only answered when looking at answered); Knowledge: only when chosen
async function checkFocusedLoading() {
  const realFetch = globalThis.fetch, state = data.state;
  const saved = { page: ui.page, tab: ui.tab, done: ui.done };
  let calls = [];
  const machine = { id: 'wiki-place', type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 1, waiting_for_person: 0, person_answers: false };
  const specs = () => [...loadingSpecs().map(s => s.id === 'prompt-kind' ? { ...s, waiting_for_person: 0 } : s), machine];
  globalThis.fetch = async url => { calls.push(url); return { ok: true, json: async () => url === 'v1/status' ? { specs: specs() } : url === 'v1/wiki' ? { claims: [] } : url.startsWith('v1/tune') ? { variants: [] } : { items: [] } }; };
  const person = ['needs-plan', 'done-as-asked', 'prompt-kind', 'changes-in-scope'];
  ui.page = 'answer'; ui.done = false;
  await load();
  const waiting = ['needs-plan', 'done-as-asked', 'changes-in-scope'];
  // the cards: only the questions with something waiting; the ways of asking: every question a person answers, also prompt-kind with nothing waiting (not the AI-only one)
  const answer = calls.length === 8 && waiting.every(id => calls.includes('v1/queue?spec=' + id + '&limit=200')) && tunesOnce(calls, person) && !calls.includes('v1/queue?spec=prompt-kind&limit=200') && !calls.some(u => u.includes('wiki-place')) && !calls.includes('v1/wiki');
  calls = []; ui.page = 'ai'; ui.tab = 'needs-plan'; await load();
  const simple = calls.length === 6 && calls.includes('v1/queue?spec=needs-plan&limit=200') && tunesOnce(calls, person) && !calls.includes('v1/wiki');
  // a question only the AI answers: its own way of asking too
  calls = []; ui.tab = 'wiki-place'; await load();
  const machineTab = calls.length === 7 && calls.includes('v1/queue?spec=wiki-place&limit=200') && tunesOnce(calls, [...person, 'wiki-place']);
  calls = []; ui.tab = 'needs-plan'; ui.done = true; await load();
  const answered = calls.length === 6 && calls.includes('v1/answered?spec=needs-plan&limit=200') && !calls.some(url => url.startsWith('v1/queue')) && tunesOnce(calls, person);
  calls = []; ui.page = 'wiki'; await load();
  const knowledge = calls.length === 6 && calls.includes('v1/wiki') && tunesOnce(calls, person) && !calls.some(url => url.startsWith('v1/queue')) && !!data.state.wiki;
  globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = answer && simple && machineTab && answered && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer screen: cards only for questions with waits, ways of asking for every question you answer=' + answer + ', AI status=' + simple + ', AI-only question=' + machineTab + ', answered fetched=' + answered + ', Knowledge only when chosen=' + knowledge + ')');
  return ok;
}

// A slow old response does not overwrite the information after a switch, and fetch failures and retries are reflected in the display.
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (old response ignored=' + latest + ', failure shown=' + failed + ', recovered by retry=' + recovered + ')');
  return ok;
}

// Lazy loading is driven by actual switches, and unloaded knowledge can be opened too. Switching questions or lists in AI status also loads only what is needed
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
  const person = loadingSpecs().map(s => s.id);
  await settle(); const initial = calls.length === 6 && calls.includes('v1/queue?spec=needs-plan&limit=200') && tunesOnce(calls, person) && !!target.querySelector('.decision-panel'); // status, the list, the ways of asking of the questions you answer
  calls = []; target.querySelector('[data-tab="done-as-asked"]')?.click(); flushSync(); await settle();
  const switched = calls.includes('v1/queue?spec=done-as-asked&limit=200') && !!target.querySelector('.decision-panel');
  calls = []; target.querySelector('.answer-tabs [data-lane="done"]')?.click(); flushSync(); await settle();
  const answered = calls.length === 6 && calls.includes('v1/answered?spec=done-as-asked&limit=200') && tunesOnce(calls, person) && target.querySelector('.answer-tabs button span')?.textContent === '1';
  holdQueue = true;
  target.querySelector('.answer-tabs [data-lane="you"]')?.click(); flushSync(); await settle();
  const waiting = !!target.querySelector('[role="status"]') && !target.querySelector('.decision-panel');
  holdQueue = false; releaseQueue?.({ items: [] }); await settle();
  calls = [];
  const wiki = target.querySelector('[data-page="wiki"]'); wiki?.click(); flushSync(); await settle();
  const knowledge = !!wiki && calls.filter(url => url === 'v1/wiki').length === 1 && !calls.some(url => url.startsWith('v1/queue')) && target.querySelector('.notebook h2')?.textContent.includes(t('notebookTitle'));
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  const ok = initial && switched && answered && waiting && knowledge;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (3 requests at first=' + initial + ', fetched on question switch=' + switched + ', answered and unanswered counts=' + answered + ', waiting while switching=' + waiting + ', opens unloaded knowledge=' + knowledge + ')');
  return ok;
}

// Audit B (0.8.1, Tailwind): do not stack utilities on the same element that set the same property to different values under the same variant.
// Tailwind v4 generates utilities in "a fixed order per property → name order", and the order in the class attribute does not decide which wins
// (e.g. bg-transparent is generated after bg-accentbg and bg-surface, so even when added conditionally, transparent always wins).
// Builds a table of class → property from the utilities layer of the shipped CSS (dist/assets/index.css), and looks at every classList of the rendered elements.
// Different variants (max-sm: / hover: / [&>span]: …) are meant to override by width or state, so they are not checked. Values derived from --tw-* are not checked either
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
      if (sel.slice(i).trim() && !name.includes(':')) continue; // compound rules such as .pill.calibrated and .empty-state p
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
        if (vb !== undefined && vb !== va) out.add(el.tagName.toLowerCase() + ': ' + names[a] + ' and ' + names[b] + ' (' + prop + ')');
      }
    }
  }
}
async function checkUtilityConflicts(target) {
  const table = utilityTable(globalThis.__shikiiCSS || '');
  if (table.size < 50) { console.log('check-ui: FAIL (cannot read the utilities of the shipped CSS: ' + table.size + ')'); return false; }
  const realFetch = globalThis.fetch; const state = data.state; const saved = { ...ui }; const lang = i18n.lang; i18n.lang = 'ja';
  const out = new Set();
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  // 1. The whole page: selected tab, list and filter, summary, how-to, unanswered cards (same as the AI / list)
  const mk = (id, unlabeled, waiting, person) => ({ id, type: 'noul', titles: {}, questions: {}, keys: [{ model: 'm', hash: 'h', n: 3, state: 'collecting' }], min_n: 50, records: 5, labels: 1, unlabeled, waiting_for_person: waiting, awaiting_results: unlabeled - waiting, person_answers: person, truth_source: person ? 'person' : 'results', last_calibration: 'log', calibration_result: { reason: 'insufficient', human_truths: 2, min_n: 50, model: 'm', hash: 'h', at: '2026-10-23' }, tune: { running: false, proposer: true, variants: [{ name: 'v1' }], latest: { run: 'r', adoptable: true, adopted: false, winner: 'v1', model: 'm', truths: 3, at: '2026-10-23T00:00:00Z', have_holdout: true, holdout: 0.5, decision: 'd', candidates: [{ name: 'current', top1: 0.5, brier: 0.2, prec_at_act: 0.5, rec_at_act: 0.5, change: '' }, { name: 'v1', top1: 0.6, brier: 0.1, prec_at_act: 0.6, rec_at_act: 0.6, change: 'c' }], winner_wording: { question: 'q1', options: { a: 'x' }, steps: [{ ask: 'a', yes: 'y', no: 'n' }] }, current_wording: { question: 'q0', options: { a: 'y' } }, meaning_checks: [{ by: 'agent', verdict: 'unsure', same: false, current_translation: 'j0', candidate_translation: 'j1', difference: 'd' }] } } });
  const specs = [mk('wiki-conflict', 4, 0, false), mk('prompt-kind', 2, 1, true), mk('needs-plan', 3, 2, true)];
  const item = (id, project) => ({ trace_id: id, at: '2026-10-23T00:00:00Z', band: 'ask', top: 'yes', p: 0.6, model: 'm', inputs: { request: 'x' }, inputs_hash: id, raw: { yes: 0.6, no: 0.4 }, stale: true, answerable: true, audit: true, waits_for_person: true, project, rung: 'context', choices: [{ truth: 'yes', label: 'yes' }, { truth: 'no', label: 'no' }] });
  const summary = { person: { records: 20, waits: 2 }, memory: { delivered: 4, corrections: { delivered: 1 } }, guards: { rituals: { plan: { asked: 5, done: 2, fixed: 1 }, reflect: { asked: 4, done: 4, fixed: 0 } }, items: [{ id: 'g', kind: 'k', title: 't', enabled: true, last: { pass: false, at: '2026-10-23T00:00:00Z', reason: 'r' } }, { id: 'g2', kind: 'k', title: 't2', enabled: false }] } };
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs, summary, model: { name: 'm' } } : url.startsWith('v1/queue?spec=prompt-kind') ? { items: [item('r1', 'alpha'), item('y1', 'beta')] } : url.startsWith('v1/tune') ? specs[1].tune : { items: [] } });
  Object.assign(ui, { page: 'ai', tab: 'prompt-kind', done: false, results: false, pinned: '', showHelp: true });
  let app = mount(App, { target }); await settle(); await settle();
  collectConflicts(target, table, out);
  unmount(app);
  Object.assign(ui, { page: 'answer', tab: '' }); // Answer screen: the card (question, sticky button bar) and candidate ways of asking
  app = mount(App, { target }); await settle(); await settle();
  collectConflicts(target, table, out);
  const answerShown = !!target.querySelector('[data-answer] .answer-bar') && !!target.querySelector('[data-answer-wording]');
  unmount(app);
  if (!answerShown) out.add('the Answer screen card or the candidate ways of asking were not rendered (the check is not looking at them)');
  // 2. Cards: the folded previous reply (needs-plan), the note on the original text's limit, answered (changing the answer), the sample's provisional score, read-only
  const base = { trace_id: 'c1', at: '2026-10-23T00:00:00Z', band: 'discard', top: 'yes', p: 0.3, model: 'm', answerable: true, choices: [{ truth: 'yes', label: 'はい' }, { truth: 'no', label: 'いいえ' }], project: 'alpha' };
  const cards = [
    [{ id: 'needs-plan', type: 'noul' }, { ...base, inputs: { previous: 'p', request: 'r', agreed: 'a', open: 'o', files: 'f' }, original_inputs: { previous: 'pp' }, text_status: { request: 'limited', previous: 'legacy' } }],
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r', summary: 's' }, truth: 'yes', truth_at: base.at, redo_of: 'x' }],
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r' }, audit: true, review_reason: 'random', review: { truth: 'yes', reason: 'why', source_material: { reply: 'reply' } } }],
    [{ id: 'intent', type: 'choice' }, { ...base, top: 'a', choices: [{ truth: 'a', label: 'a' }, { truth: 'b', label: 'b' }], inputs: { message: 'm' } }],
  ];
  for (const [sp, it] of cards) { app = mount(ItemCard, { target, props: { sp, it } }); flushSync(); collectConflicts(target, table, out); unmount(app); }
  app = mount(ItemCard, { target, props: { sp: { id: 'wiki-conflict', type: 'noul' }, it: { ...base, inputs: { lesson: 'l' } }, readonly: true } }); flushSync(); collectConflicts(target, table, out); unmount(app);
  // 3. The Knowledge section (claims, hierarchy, article text, sources, effects), the work entrances, notices
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (no utilities for the same property stacked on one element: ' + list.length + ' conflicts' + (ok ? '' : '\n  ' + list.slice(0, 20).join('\n  ')) + ')');
  return ok;
}

// Work records (0.8.1): starts collapsed per folder; opening shows files; pressing shows text as text (no HTML rendered) and images as images. When empty, it says so.
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
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (work records: starts collapsed=' + folded + ', opening shows files=' + listed + ', text as is=' + text + ', image as image=' + image + ', says empty=' + empty + ')');
  return ok;
}

// The front of Knowledge (0.9.0): choosing a scene shows only that scene's titles, pressing a title shows the text, articles without a scene are "Unclassified", the inbox has only exceptions,
// 4 per kind plus "N more". HTML is shown as text.
function checkLessons(target) {
  try { localStorage.removeItem('shikii.scene'); } catch { /* fine if missing */ }
  const mk = (i, kind, scene) => ({ id: String(i).padStart(16, '0'), title: '記事 ' + i, lesson: '教訓 ' + i + ' <b>太字ではない</b>', applies_to: 'a', kind, scene, sources: [], related: [] });
  const lessons = [mk(1, 'howto', 'コミットする時'), mk(2, 'howto', 'コミットする時'), mk(3, 'howto', '公開する時'), mk(4, 'howto', '公開する時'), mk(5, 'howto', '公開する時'), mk(6, 'fact', '公開する時'), mk(7, '', '')];
  const wiki = { lessons, lesson_meta: { [lessons[4].id]: { updated: '2026-10-24T00:00:00Z', helpful: 3, harmful: 0 } }, inbox: [{ kind: 'harmful', lesson_id: lessons[0].id, lesson: '教訓 1', count: 3 }] };
  const app = mount(Lessons, { target, props: { wiki } }); flushSync();
  const q = sel => target.querySelector(sel);
  const scenes = [...target.querySelectorAll('[data-scenes] [data-scene]')].map(b => b.dataset.scene);
  const sceneList = scenes.length === 3 && scenes[0] === '公開する時' && scenes[2] === '__none__';
  const items = () => [...target.querySelectorAll('[data-kind="howto"] [data-open]')].map(b => b.textContent.trim());
  const firstFour = items().length === 4 && items()[0].startsWith('記事 5') && !!q('[data-kind-more="howto"]');
  q('[data-scene="コミットする時"]').click(); flushSync();
  const filtered = items().length === 2 && items().every(t => t === '記事 1' || t === '記事 2') && !q('[data-kind="fact"]');
  q('[data-kind="howto"] [data-open]').click(); flushSync();
  const detail = q('[data-lesson-detail]')?.textContent.includes('<b>太字ではない</b>') && !q('[data-lesson-detail] b');
  q('[data-scene="__none__"]').click(); flushSync();
  const none = q('[data-kinds]')?.textContent.includes('記事 7') && !q('[data-kind="howto"]');
  const inbox = q('[data-inbox-item="harmful"]')?.textContent.includes('記事 1') && q('[data-inbox-count]')?.textContent.includes('3');
  unmount(app);
  // with nothing for the person to look at, there is no inbox box at all
  const empty = mount(Lessons, { target, props: { wiki: { ...wiki, inbox: [] } } }); flushSync();
  const noInbox = !q('[data-inbox]');
  unmount(empty);
  const ok = sceneList && firstFour && filtered && detail && none && inbox && noInbox;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (front of Knowledge: scene list=' + sceneList + ', 4 per kind and more=' + firstFour + ', filter by scene=' + filtered + ', text as is=' + detail + ', unclassified=' + none + ', inbox=' + inbox + ', no inbox box when empty=' + noInbox + ')');
  return ok;
}

// ---- audit 0.9.2 (ui) ----

// The utilities of the shipped CSS with the media query each one is under, in CSS order: [{ name, media: [..], decls: Map }]. Only rules for one class (what the markup's classes produce)
function mediaUtilities(css) {
  const out = [];
  const at = css.indexOf('@layer utilities {');
  if (at < 0) return out;
  const stack = [];
  let i = at + '@layer utilities {'.length, head = '';
  for (let depth = 1; i < css.length && depth > 0; i++) {
    const c = css[i];
    if (c === '{') {
      const sel = head.trim(); head = '';
      if (sel.startsWith('@')) { stack.push(sel); depth++; continue; }
      const end = css.indexOf('}', i);
      const body = css.slice(i + 1, end);
      if (body.includes('{')) { stack.push(sel); depth++; continue; } // nested (e.g. &:hover) — not one class
      const decls = new Map();
      for (const d of body.split(';')) { const k = d.indexOf(':'); if (k > 0) decls.set(d.slice(0, k).trim(), d.slice(k + 1).trim()); }
      for (const one of sel.split(/,\s*/)) {
        const m = one.match(/^\.((?:\\.|[^\s.:>[,\\])+)$/);
        if (m) out.push({ name: m[1].replace(/\\(.)/g, '$1'), media: stack.filter(s => s.startsWith('@media')), decls });
      }
      i = end;
    } else if (c === '}') { depth--; stack.pop(); head = ''; }
    else head += c;
  }
  return out;
}
// whether a media query holds at a width (light colors, a pointer without hover, motion allowed)
function mediaHolds(q, width) {
  let m;
  if ((m = q.match(/^@media not all and \(min-width: (\d+)px\)$/))) return width < Number(m[1]);
  if ((m = q.match(/^@media \(min-width: (\d+)px\)$/))) return width >= Number(m[1]);
  return false;
}
// The properties the utilities on an element give it at a width (later rules win, as in the shipped CSS)
function styleAt(rules, el, width) {
  const style = new Map(), names = new Set(el.classList);
  for (const r of rules) if (names.has(r.name) && r.media.every(q => mediaHolds(q, width))) for (const [k, v] of r.decls) style.set(k, v);
  return style;
}
const shownAt = (rules, el, width) => { for (let e = el; e && e.nodeType === 1; e = e.parentElement) if (styleAt(rules, e, width).get('display') === 'none') return false; return true; };

// Audit 0.9.2 ui middle-1 (the auditor's RED check): a wording the AI could not decide on, for a question with no card waiting, reaches the person on the Answer screen
// (the hook tells the person to choose on the page)
async function checkAuditWordingWithoutWaiting(target) {
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const latest = { run: 'r9', winner: 'w', adoptable: true, adopted: false, winner_wording: { question: 'new?' }, current_wording: { question: 'old?' }, meaning_checks: [{ by: 'agent', verdict: 'unsure', current_translation: 'now-ja', candidate_translation: 'new-ja', difference: 'diff-ja' }] };
  const specs = () => [{ id: 'done-as-asked', type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 0, waiting_for_person: 0, person_answers: true }];
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: specs() } : url.startsWith('v1/tune') ? { latest } : url === 'v1/wiki' ? { claims: [], lessons: [] } : { items: [] } });
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, results: false, pinned: '', showHelp: false });
  const app = mount(App, { target }); await settle(); await settle(); await settle();
  const shown = !!target.querySelector('[data-answer-wording]');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved);
  console.log((shown ? 'check-ui: ok' : 'check-ui: FAIL') + ' (audit: undecided wording with no card waiting is on the Answer screen=' + shown + ')');
  return shown;
}

// Audit 0.9.2 ui middle-1 (the rest): with no card waiting and no screen chosen, a wording box opens the page on "Answer"; the marker on "Answer" and "remaining" count it;
// the AI page's list says that question has something for the person (not "nothing for you to do"). With the box answered (a person's check), it is Knowledge again
async function checkWordingLanding(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const unsure = { run: 'r9', winner: 'w', adoptable: true, adopted: false, winner_wording: { question: 'new?' }, current_wording: { question: 'old?' }, meaning_checks: [{ by: 'agent', verdict: 'unsure', current_translation: 'now-ja', candidate_translation: 'new-ja', difference: 'diff-ja' }] };
  let latest = unsure;
  const keys = [{ model: 'm', hash: 'h', n: 60, state: 'not_separable' }];
  const specs = () => ['done-as-asked', 'needs-plan'].map(id => ({ id, type: 'noul', titles: { ja: id }, questions: {}, keys, min_n: 50, records: 60, labels: 60, unlabeled: 0, waiting_for_person: 0, person_answers: true }));
  globalThis.fetch = async url => ({ ok: true, json: async () => url === 'v1/status' ? { specs: specs() } : url === 'v1/tune?spec=done-as-asked' ? { latest } : url.startsWith('v1/tune') ? {} : url === 'v1/wiki' ? { claims: [], lessons: [] } : { items: [] } });
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  data.state = null; Object.assign(ui, { page: '', tab: '', done: false, results: false, pinned: '', showHelp: false });
  let app = mount(App, { target }); await settle(); await settle(); await settle();
  const lands = target.querySelector('[data-page="answer"]')?.getAttribute('aria-current') === 'page' && !!target.querySelector('[data-answer-wording="done-as-asked"]');
  const counted = target.querySelector('[data-page="answer"] .badge')?.textContent === '1' && target.querySelector('[data-answer-left]')?.textContent === t('answerLeft')(1);
  ui.page = 'ai'; ui.tab = 'needs-plan'; flushSync(); await settle(); await settle(); await settle();
  const row = target.querySelector('[data-tab="done-as-asked"]')?.textContent || '';
  const aiRow = row.includes(t('summarySpecWording')) && !row.includes(t('not_separable')(60, 50)) && target.querySelector('[data-page="answer"] .badge')?.textContent === '1';
  unmount(app);
  latest = { ...unsure, meaning_checks: [...unsure.meaning_checks, { by: 'page', same: true }] };
  data.state = null; Object.assign(ui, { page: '', tab: '' });
  app = mount(App, { target }); await settle(); await settle(); await settle();
  const quiet = target.querySelector('[data-page="wiki"]')?.getAttribute('aria-current') === 'page' && !target.querySelector('[data-page="answer"] .badge');
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang;
  const ok = lands && counted && aiRow && quiet;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (a wording box with no card waiting: opens on Answer=' + lands + ', counted in the marker and remaining=' + counted + ', AI page list says it waits=' + aiRow + ', answered box is quiet again=' + quiet + ')');
  return ok;
}

// Audit 0.9.2 ui test hole 1: the keys on the Answer screen. 1 to 9 press that numbered answer (here 3 choices, "2" is the second), Enter sends the AI's guess even when it is
// not the first button (noul with p < 0.5: the guess is "no", the second button), → skips to the next card. Each is sent / seen as exactly that
async function checkAnswerKeys(target) {
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui };
  const calls = [];
  const choiceItem = id => ({ trace_id: id, at: '2026-10-22T00:00:00Z', band: 'ask', top: 'b', p: 0.6, model: 'm', answerable: true, waits_for_person: true, inputs: { message: 'CHOICE-' + id }, choices: [{ truth: 'a', label: 'A' }, { truth: 'b', label: 'B' }, { truth: 'c', label: 'C' }] });
  const noulItem = id => ({ trace_id: id, at: '2026-10-22T00:00:00Z', band: 'ask', top: 'yes', p: 0.2, model: 'm', answerable: true, waits_for_person: true, inputs: { request: 'NOUL-' + id }, choices: [{ truth: 'yes', label: 'Y' }, { truth: 'no', label: 'N' }] });
  const spec = (id, type, n) => ({ id, type, titles: {}, questions: { ja: id }, keys: [], min_n: 50, records: n, labels: 0, unlabeled: n, waiting_for_person: n, person_answers: true });
  let specs = [spec('prompt-kind', 'choice', 2)], queue = { 'prompt-kind': [choiceItem('c1'), choiceItem('c2')] };
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push(JSON.parse(opts.body)); return { ok: true, json: async () => url === 'v1/status' ? { specs } : url.startsWith('v1/queue') ? { items: queue[new URLSearchParams(url.split('?')[1]).get('spec')] || [] } : opts?.body ? { truth: 'x' } : {} }; };
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const key = k => { document.body.dispatchEvent(new window.KeyboardEvent('keydown', { key: k, bubbles: true })); flushSync(); };
  data.state = null; Object.assign(ui, { page: 'answer', tab: '', done: false, pinned: '', showHelp: false }); skipped.clear();
  let app = mount(App, { target }); await settle(); await settle();
  key('ArrowRight'); await settle();
  const skip = calls.length === 0 && skipped.has('c1') && (target.querySelector('.item')?.textContent || '').includes('CHOICE-c2');
  key('2'); await settle();
  const digit = calls.length === 1 && calls[0].trace_id === 'c2' && calls[0].truth === 'b';
  unmount(app); skipped.clear(); calls.length = 0;
  specs = [spec('needs-plan', 'noul', 1)]; queue = { 'needs-plan': [noulItem('n1')] }; data.state = null;
  app = mount(App, { target }); await settle(); await settle();
  const second = target.querySelectorAll('.item .btns button')[1]?.classList.contains('top');
  key('Enter'); await settle();
  const enter = second && calls.length === 1 && calls[0].trace_id === 'n1' && calls[0].truth === 'no';
  unmount(app); globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); skipped.clear();
  const ok = skip && digit && enter;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (Answer keys: → skips to the next card=' + skip + ', 2 sends the second answer=' + digit + ', Enter sends the AI guess (second button)=' + enter + ' ' + JSON.stringify(calls) + ')');
  return ok;
}

// Audit 0.9.2 ui test hole 2: what the wording box on the Answer screen sends. "Keep it" → review keep:true, "undo" → keep:false, "same meaning" → adopt meaning_same:true,
// "different" → adopt meaning_same:false with the note; every one with the latest run's id. Also (low-4) the fold's names match the box's ("wording until now / from now on" after the AI switched)
async function checkWordingAskSends(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, realPrompt = globalThis.prompt;
  const calls = [];
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) }); return { ok: true, json: async () => url.endsWith('adopt') ? { path: '/p/decisions/x-v2.yaml' } : url === 'v1/status' ? { specs: [] } : {} }; };
  globalThis.prompt = () => 'the note';
  const base = { run: 'r42', winner: 'w', adoptable: true, winner_wording: { question: 'new?' }, current_wording: { question: 'old?' } };
  const agent = verdict => ({ by: 'agent', verdict, current_translation: 'now-ja', candidate_translation: 'new-ja', difference: 'diff-ja' });
  const adopted = { ...base, adopted: true, auto_adopted: true, adopted_path: 'decisions/x-v2.yaml', meaning_checks: [agent('same')] };
  const unsure = { ...base, adopted: false, meaning_checks: [agent('unsure')] };
  const sp = latest => ({ id: 'changes-in-scope', titles: {}, tune: { latest } });
  const press = async (latest, sel) => { const app = mount(WordingAsk, { target, props: { sp: sp(latest) } }); flushSync(); target.querySelector(sel)?.click(); await new Promise(r => setTimeout(r, 0)); unmount(app); return calls.pop(); };
  const keep = await press(adopted, '[data-wording-keep]');
  const undo = await press(adopted, '[data-wording-undo]');
  const same = await press(unsure, '[data-wording-same]');
  const diff = await press(unsure, '[data-wording-different]');
  const is = (c, url, body) => !!c && c.url === url && JSON.stringify(c.body) === JSON.stringify(body);
  const sent = is(keep, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: true }) && is(undo, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: false })
    && is(same, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: true, note: '' }) && is(diff, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: false, note: 'the note' });
  // the names in the box and in the fold "see the English original" are the same pair
  const names = latest => { const app = mount(WordingAsk, { target, props: { sp: sp(latest) } }); flushSync(); const box = [...target.querySelectorAll('[data-answer-wording] .k')].map(e => e.textContent); const fold = [...target.querySelectorAll('[data-answer-wording] details p')].map(p => p.textContent.split(':')[0]); unmount(app); return { box, fold }; };
  const a = names(adopted), u = names(unsure);
  const consistent = JSON.stringify(a.box) === JSON.stringify([t('wordingBefore'), t('wordingAfter')]) && JSON.stringify(a.fold) === JSON.stringify(a.box) && JSON.stringify(u.fold) === JSON.stringify(u.box);
  globalThis.fetch = realFetch; globalThis.prompt = realPrompt; i18n.lang = lang;
  const ok = sent && consistent;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (wording box sends: keep / undo / same / different with the latest run=' + sent + ', the same names in the box and the fold=' + consistent + ' ' + JSON.stringify({ a, u }) + ')');
  return ok;
}

// Audit 0.9.2 ui low-1: on a phone (390px) the answer bar on the Answer screen still sticks to the bottom, and stays small: inside it only the buttons (two to a row, no number marks),
// skip and the closed guess fold are shown; the question is shown once, at the top of the card (at 1440px once, above the buttons, in the stuck bar). Read from the shipped CSS by width
function checkPhoneAnswerBar(target) {
  const rules = mediaUtilities(globalThis.__shikiiCSS || '');
  const lang = i18n.lang; i18n.lang = 'ja';
  const q = 'Should it have asked first? (a long question)';
  const it = { trace_id: 'phone', at: '2026-10-22T00:00:00Z', band: 'ask', top: 'yes', p: .8, answerable: true, inputs: { request: 'req' }, choices: [{ truth: 'yes', label: 'It should have asked' }, { truth: 'no', label: 'Fine to go ahead' }] };
  const app = mount(ItemCard, { target, props: { sp: { id: 'needs-plan', type: 'noul' }, it, question: q, title: 'Asked first?' } });
  flushSync();
  const bar = target.querySelector('.answer-bar');
  const stuck = w => { const s = bar ? styleAt(rules, bar, w) : new Map(); return s.get('position') === 'sticky' && /(^0|\* 0\)$)/.test(s.get('bottom') || ''); };
  const visibleWith = (w, text) => [...target.querySelectorAll('.item *')].filter(e => shownAt(rules, e, w) && [...e.childNodes].some(n => n.nodeType === 3 && n.textContent.includes(text)));
  const phoneQuestion = visibleWith(390, q), wideQuestion = visibleWith(1440, q);
  const once = phoneQuestion.length === 1 && !bar.contains(phoneQuestion[0]) && wideQuestion.length === 1 && bar.contains(wideQuestion[0]);
  // what is visible in the bar on a phone: the answers, skip and the fold's summary
  const shownInBar = [...bar.querySelectorAll('*')].filter(e => shownAt(rules, e, 390) && !(e.closest('details:not([open])') && !e.closest('summary')) && [...e.childNodes].some(n => n.nodeType === 3 && n.textContent.trim()));
  const allowedInBar = shownInBar.every(e => e.closest('.btns button.answer-choice, .btns button.skip, details.guess-fold > summary') && !e.closest('[data-num]'));
  const choices = [...bar.querySelectorAll('button.answer-choice')];
  const shareRow = choices.length === 2 && choices.every(b => !/100%/.test(styleAt(rules, b, 390).get('flex') || ''));
  unmount(app); i18n.lang = lang;
  const ok = rules.length > 100 && stuck(390) && stuck(1440) && once && allowedInBar && shareRow;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (answer bar sticks on a phone=' + stuck(390) + ' and wide=' + stuck(1440) + ', question once at each width=' + once + ', only buttons, skip and the fold in the phone bar=' + allowedInBar + ', two answers share a row=' + shareRow + ', utilities read=' + rules.length + ')');
  return ok;
}

// Audit 0.9.2 ui low-4: the AI page's wording section. The inputs of "write a rewording" have names a screen reader reads (a label), and the rewordings' names
// (unasked-first) are not the words people read: they are only small secondary text after a plain label (outside the folded numbers)
function checkWordingPlain(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const latest = { run: 'r1', winner: 'unasked-first', model: 'm', truths: 50, at: '2026-10-25T00:00:00Z', adoptable: true, adopted: false, candidates: [{ name: 'current', top1: 0.3, brier: 0.3, prec_at_act: 0.1, rec_at_act: 0.1, change: '' }, { name: 'unasked-first', top1: 0.8, brier: 0.2, prec_at_act: 0.8, rec_at_act: 0.3, change: 'q' }], decision: 'd', winner_wording: { question: 'Did it change unasked files?' }, current_wording: { question: 'Were the changes within scope?' }, meaning_checks: [{ by: 'agent', verdict: 'unsure', current_translation: 'now-ja', candidate_translation: 'new-ja', difference: 'diff-ja' }] };
  const app = mount(Wording, { target, props: { sp: { id: 'prompt-kind', type: 'choice', tune: { running: false, proposer: false, variants: [{ name: 'each-file' }, { name: 'unasked-first' }], latest } } } });
  flushSync();
  const fields = [...target.querySelectorAll('input, textarea, select')];
  const named = fields.length === 3 && fields.every(f => (f.getAttribute('aria-label') || '').trim() || (f.closest('label')?.textContent || '').trim() || (f.id && target.querySelector('label[for="' + f.id + '"]')?.textContent.trim()));
  const names = ['unasked-first', 'each-file'];
  const loose = [...target.querySelectorAll('*')].filter(e => !e.closest('[data-wording-details]') && !e.closest('[data-variant-name]') && [...e.childNodes].some(n => n.nodeType === 3 && names.some(v => n.textContent.includes(v))));
  const secondary = target.querySelectorAll('[data-variant-name]').length >= 2 && [...target.querySelectorAll('[data-variant-name]')].every(e => e.classList.contains('text-muted') && e.previousSibling?.textContent?.trim());
  unmount(app); i18n.lang = lang;
  const ok = named && loose.length === 0 && secondary;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (AI page wording: inputs have labels=' + named + ', rewording names only as secondary text=' + (loose.length === 0 && secondary) + ' ' + JSON.stringify(loose.map(e => e.textContent.slice(0, 40))) + ')');
  return ok;
}
