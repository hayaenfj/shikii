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


export async function checkUtilityConflicts(target) {
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
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r' }, audit: true, review_reason: 'uncertain', review: { truth: 'yes', reason: 'why', source_material: { reply: 'reply' } } }],
    [{ id: 'done-as-asked', type: 'noul' }, { ...base, inputs: { request: 'r' }, audit: true, review_reason: 'disagree', pair: { model: 'no', model_confidence: .7, agent: 'yes', agent_reason: 'why' }, review: { truth: 'yes', reason: 'why', source_material: { reply: 'reply' } } }],
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


// Audit 0.9.2 ui low-1: on a phone (390px) the answer bar on the Answer screen still sticks to the bottom, and stays small: inside it only the buttons (two to a row, no number marks),
// skip and the closed guess fold are shown; the question is shown once, at the top of the card (at 1440px once, above the buttons, in the stuck bar). Read from the shipped CSS by width
export function checkPhoneAnswerBar(target) {
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
