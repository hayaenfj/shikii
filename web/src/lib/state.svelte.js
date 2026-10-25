// The page's state, and loading from the server.
// ui.page: the open screen (0.9.1). answer = Answer (the questions waiting for a person, in one list), wiki = Knowledge, ai = AI status (learning progress per question, what was answered, ways of asking)
// ui.tab: the question open in AI status. ui.done / ui.results: that question's list (answered / settled by the outcome). What is saved goes to localStorage (shikii.*)
// data: /v1/status and what the current screen needs (Answer: the waiting questions' lists and ways of asking; Knowledge: wiki; AI status: the open question's list and ways of asking). null means not loaded yet
import { SvelteSet } from 'svelte/reactivity';
import { api } from './api.js';
import { read, write } from './prefs.js';

// With ?spec=<id>&trace=<trace_id> in the URL, start with that decision first on the Answer screen (comes from the hook's "answer later → URL")
const params = new URLSearchParams(typeof location !== 'undefined' ? location.search : '');
const pages = ['answer', 'wiki', 'ai'];
const savedPage = read('shikii.page');
export const ui = $state({
  showHelp: read('shikii.help') !== 'hidden',
  page: params.get('trace') ? 'answer' : pages.includes(savedPage) ? savedPage : '',
  tab: params.get('spec') || read('shikii.tab'),
  pinned: params.get('trace') || '',
  listSize: 30,
  done: false,
  results: false, // looking at the "settled by the next request or reflection" list (records a person does not answer; 0.8.0)
});
export const skipped = new SvelteSet();
export const data = $state({ state: null, loadError: '' });

export function setShowHelp(v) { ui.showHelp = v; write('shikii.help', v ? 'shown' : 'hidden'); }
export function setTab(v) { ui.tab = v; write('shikii.tab', v); }
export function setPage(v) { ui.page = v; write('shikii.page', v); }

// Questions a person answers (excluding the wiki questions only the AI answers)
export const personSpecs = st => (st?.specs || []).filter(s => s.person_answers !== false);

// The screen to open: the chosen one; otherwise Answer if something is waiting; otherwise Knowledge (AI status on a page without a wiki)
export function selectedPage(st, selection = ui) {
  if (!st?.specs?.length) return null;
  if (pages.includes(selection.page)) return selection.page === 'wiki' && st.projects ? 'ai' : selection.page;
  return personSpecs(st).some(s => s.waiting_for_person > 0) ? 'answer' : 'wiki';
}

// The question to open in AI status. The chosen one; otherwise one with something waiting; otherwise the first
export function selectedTab(st, selection = ui) {
  if (!st?.specs?.length) return null;
  return st.specs.find(s => s.id === selection.tab)?.id
    || st.specs.find(s => s.waiting_for_person > 0)?.id || st.specs[0].id;
}

// The questions to load on the Answer screen: questions a person answers that have something waiting, and the one named in the URL
export function answerSpecs(st, selection = ui) {
  return personSpecs(st).filter(s => s.waiting_for_person > 0 || s.id === params.get('spec') && selection.pinned);
}

let loadVersion = 0;
// dropPendingLoads drops the responses of pending reloads (so that after answering, the card does not come back from the old list from before the answer)
export function loadMark() { return loadVersion; }
// If another load (switching screens or the 15-second reload) started while the answer was being sent, drop that too and reload with the current state
// (the card does not come back from a list older than the answer, and the screen switched to still appears. Final audit 0.9.1 low-F1)
export function dropPendingLoads(mark = loadVersion) { const raced = loadVersion !== mark; loadVersion++; if (raced) load(); }
// Loads only status and what the current screen needs. A slow old response is not applied to a newer selection.
export async function load() {
  const version = ++loadVersion;
  const selection = { page: ui.page, tab: ui.tab, done: ui.done, pinned: ui.pinned };
  try {
    const st = await api.status();
    if (version !== loadVersion) return;
    if (!Array.isArray(st.projects)) {
      const page = selectedPage(st, selection);
      if (page === 'wiki') {
        st.wiki = await api.wiki();
      } else if (page === 'answer') {
        await Promise.all(answerSpecs(st, selection).map(async sp => {
          const [rows, tune] = await Promise.all([api.queue(sp.id), api.tune(sp.id).catch(() => null)]);
          sp.queue = rows; sp.tune = tune;
        }));
      } else if (page === 'ai') {
        const tab = selectedTab(st, selection);
        const sp = st.specs.find(s => s.id === tab);
        // When looking at answered, the unanswered list is not loaded.
        const [rows, tune] = await Promise.all([
          selection.done ? api.answered(tab) : api.queue(tab),
          !selection.done ? api.tune(tab).catch(() => null) : null,
        ]);
        if (selection.done) sp.answered = rows.items;
        else sp.queue = rows;
        sp.tune = tune;
      }
    }
    if (version === loadVersion) { data.state = st; data.loadError = ''; }
  } catch (e) {
    if (version === loadVersion) data.loadError = e.message;
  }
}
// Whether there is something for a person to do about candidate ways of asking (0.9.2). Only when the agent confirmed the meaning is the same and adopted it automatically (keep it or revert),
// or when the agent could not decide. Not shown when it has not been checked yet, was said to differ, the person answered, or it was reverted
export function wordingNeedsPerson(latest) {
  if (!latest || latest.reverted) return false;
  const checks = latest.meaning_checks || [];
  if (checks.some(c => c.by !== 'agent')) return false;
  const agent = checks.filter(c => c.by === 'agent').pop();
  if (!agent) return false;
  if (latest.auto_adopted && latest.adopted) return true;
  return agent.verdict === 'unsure' && !!latest.adoptable && !latest.adopted && !!latest.winner_wording;
}
