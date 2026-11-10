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



// A person who came from the hook's "answer later → …/?spec=x&trace=t" sees that item first (otherwise the original order)
export function checkPinned(target) {
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
export function checkAnswerMerge(target) {
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
export async function checkWordingAgent(target) {
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
export async function checkAnswerCardOnce(target) {
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
export async function checkAnswerWordingBox(target) {
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
export function checkAnswerNothingToDo(target) {
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


// The ways-of-asking section has inputs for a person to write a candidate (and option descriptions for a choice). Sending it delivers the name, question and options parsed from "id: description" to v1/tune/variant
export async function checkWording(target) {
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



// Audit 0.9.1 (A1, M8, M9, M10): answering moves on to the next card, and "remaining" and the marker on "Answer" at the top go down. With 0 waiting, the empty view; before the list is loaded, loading
export async function checkAnswerAdvances(target) {
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


// You can answer after reading the conversation, and a yes/no question does not show a duplicate "not …".
export function checkAnswerFocus(target) {
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


// Audit 0.9.2 ui middle-1 (the auditor's RED check): a wording the AI could not decide on, for a question with no card waiting, reaches the person on the Answer screen
// (the hook tells the person to choose on the page)
export async function checkAuditWordingWithoutWaiting(target) {
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
export async function checkWordingLanding(target) {
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
export async function checkAnswerKeys(target) {
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
export async function checkWordingAskSends(target) {
  const lang = i18n.lang; i18n.lang = 'ja';
  const realFetch = globalThis.fetch, realPrompt = globalThis.prompt;
  const calls = [];
  globalThis.fetch = async (url, opts) => { if (opts?.body) calls.push({ url, body: JSON.parse(opts.body) }); return { ok: true, json: async () => url.endsWith('adopt') ? { path: '/p/decisions/x-v2.yaml' } : url === 'v1/status' ? { specs: [] } : {} }; };
  globalThis.prompt = () => 'the note';
  const base = { candidates: [], model: 'synthetic', truths: 20, at: '2026-11-10T00:00:00Z', run: 'r42', winner: 'w', adoptable: true, winner_wording: { question: 'new?' }, current_wording: { question: 'old?' } };
  const agent = verdict => ({ by: 'agent', verdict, current_translation: 'now-ja', candidate_translation: 'new-ja', difference: 'diff-ja' });
  const adopted = { ...base, adopted: true, auto_adopted: true, adopted_path: 'decisions/x-v2.yaml', meaning_checks: [agent('same')] };
  const unsure = { ...base, adopted: false, meaning_checks: [agent('unsure')] };
  const sp = latest => ({ id: 'changes-in-scope', titles: {}, tune: { variants: [], latest } });
  const press = async (latest, sel, component = WordingAsk) => { const app = mount(component, { target, props: { sp: sp(latest) } }); flushSync(); target.querySelector(sel)?.click(); await new Promise(r => setTimeout(r, 0)); unmount(app); return calls.pop(); };
  const keep = await press(adopted, '[data-wording-keep]');
  const undo = await press(adopted, '[data-wording-undo]');
  const same = await press(unsure, '[data-wording-same]');
  const diff = await press(unsure, '[data-wording-different]');
  const is = (c, url, body) => !!c && c.url === url && JSON.stringify(c.body) === JSON.stringify(body);
  const sent = is(keep, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: true }) && is(undo, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: false })
    && is(same, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: true, note: '' }) && is(diff, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: false, note: 'the note' });
  const aiKeep = await press(adopted, '[data-wording-keep]', Wording);
  const aiUndo = await press(adopted, '[data-wording-undo]', Wording);
  const aiSame = await press(unsure, '[data-wording-same]', Wording);
  const aiDiff = await press(unsure, '[data-wording-different]', Wording);
  const aiSent = is(aiKeep, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: true }) && is(aiUndo, 'v1/tune/review', { spec: 'changes-in-scope', run: 'r42', keep: false })
    && is(aiSame, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: true, note: '' }) && is(aiDiff, 'v1/tune/adopt', { spec: 'changes-in-scope', run: 'r42', meaning_same: false, note: 'the note' });
  const successFetch = globalThis.fetch;
  let failuresRecover = true;
  for (const component of [WordingAsk, Wording]) {
    for (const [latest, selector] of [[adopted, '[data-wording-keep]'], [unsure, '[data-wording-same]']]) {
      globalThis.fetch = async (url, opts) => opts?.body ? { ok: false, status: 409, json: async () => ({ error: 'synthetic refusal' }) } : successFetch(url, opts);
      const app = mount(component, { target, props: { sp: sp(latest) } }); flushSync();
      target.querySelector(selector)?.click(); await new Promise(r => setTimeout(r, 0)); flushSync();
      failuresRecover &&= !!target.querySelector(selector) && !target.querySelector(selector).disabled && toastState.msg.includes('synthetic refusal');
      unmount(app);
    }
  }
  globalThis.fetch = successFetch;
  // the names in the box and in the fold "see the English original" are the same pair
  const names = latest => { const app = mount(WordingAsk, { target, props: { sp: sp(latest) } }); flushSync(); const box = [...target.querySelectorAll('[data-answer-wording] .k')].map(e => e.textContent); const fold = [...target.querySelectorAll('[data-answer-wording] details p')].map(p => p.textContent.split(':')[0]); unmount(app); return { box, fold }; };
  const a = names(adopted), u = names(unsure);
  const consistent = JSON.stringify(a.box) === JSON.stringify([t('wordingBefore'), t('wordingAfter')]) && JSON.stringify(a.fold) === JSON.stringify(a.box) && JSON.stringify(u.fold) === JSON.stringify(u.box);
  globalThis.fetch = realFetch; globalThis.prompt = realPrompt; i18n.lang = lang;
  const ok = sent && aiSent && failuresRecover && consistent;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (wording operations on both screens=' + (sent && aiSent) + ', rejected writes recover=' + failuresRecover + ', wording box sends: keep / undo / same / different with the latest run=' + sent + ', the same names in the box and the fold=' + consistent + ' ' + JSON.stringify({ a, u }) + ')');
  return ok;
}


// Audit 0.9.2 ui low-4: the AI page's wording section. The inputs of "write a rewording" have names a screen reader reads (a label), and the rewordings' names
// (unasked-first) are not the words people read: they are only small secondary text after a plain label (outside the folded numbers)
export function checkWordingPlain(target) {
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
