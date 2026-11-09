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



// Audit 0.9.1 (A2): even if you answer while the 15-second reload is waiting for the list's response, that old response does not bring back the answered card
export async function checkAnswerNotRevertedByOldLoad(target) {
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
export async function checkSwitchDuringAnswerKeepsLoad(target) {
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
export async function checkAnswerCountOnlyForNewWaits(target) {
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


// Final audit 0.9.1 (low-F1): an old list from a reload that started while the answer was being sent does not bring back the answered card
export async function checkReloadDuringAnswerSend(target) {
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

// Fetch only what the current screen needs, plus the ways of asking of every question a person answers (the wording box on the Answer screen, the marker on "Answer" and the landing
// screen need them; audit 0.9.2 ui middle-1). The Answer screen: the lists of questions with something waiting; AI status: the open question's list (only answered when looking at answered); Knowledge: only when chosen
export async function checkFocusedLoading() {
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
export async function checkLoadOrdering() {
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
export async function checkLazyNavigation(target) {
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
