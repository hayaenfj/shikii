// 画面の状態と、サーバーからの読み込み。
// ui.page: 開いている画面(0.9.1)。answer = 答える(人が答える問いの待ちを 1 列に)、wiki = 知識、ai = AI の様子(問いごとの学習の進み・答えた分・聞き方)
// ui.tab: AI の様子で開いている問い。ui.done / ui.results: その問いの列(回答済み / 結果で確定する)。保存するものは localStorage(shikii.*)
// data: /v1/status と、表示中の画面の分(答える: 待っている問いの列と聞き方、知識: wiki、AI の様子: 開いた問いの列と聞き方)。null は読み込み前
import { SvelteSet } from 'svelte/reactivity';
import { api } from './api.js';
import { read, write } from './prefs.js';

// URL の ?spec=<id>&trace=<trace_id> で、その判定を答える画面の先頭に出して始める(hook の「あとで答える → URL」から来る)
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
  results: false, // 「次の依頼・振り返りで確定する」の列を見ている(人は答えない記録。0.8.0)
});
export const skipped = new SvelteSet();
export const data = $state({ state: null, loadError: '' });

export function setShowHelp(v) { ui.showHelp = v; write('shikii.help', v ? 'shown' : 'hidden'); }
export function setTab(v) { ui.tab = v; write('shikii.tab', v); }
export function setPage(v) { ui.page = v; write('shikii.page', v); }

// 人が答える問い(AI だけが答える wiki の問いを除く)
export const personSpecs = st => (st?.specs || []).filter(s => s.person_answers !== false);

// 開く画面: 選んだ物、無ければ待ちがあれば答える、無ければ知識(wiki の無い画面では AI の様子)
export function selectedPage(st, selection = ui) {
  if (!st?.specs?.length) return null;
  if (pages.includes(selection.page)) return selection.page === 'wiki' && st.projects ? 'ai' : selection.page;
  return personSpecs(st).some(s => s.waiting_for_person > 0) ? 'answer' : 'wiki';
}

// AI の様子で開く問い。選んだ物、無ければ待ちのある物、無ければ先頭
export function selectedTab(st, selection = ui) {
  if (!st?.specs?.length) return null;
  return st.specs.find(s => s.id === selection.tab)?.id
    || st.specs.find(s => s.waiting_for_person > 0)?.id || st.specs[0].id;
}

// 答える画面で読む問い: 人が答える問いのうち待ちがある物と、URL で指された物
export function answerSpecs(st, selection = ui) {
  return personSpecs(st).filter(s => s.waiting_for_person > 0 || s.id === params.get('spec') && selection.pinned);
}

let loadVersion = 0;
// dropPendingLoads は、待っている読み直しの応答を捨てる(答えた後に、答える前の古い列で札が戻らないように)
export function loadMark() { return loadVersion; }
// 答えを送っている間に別の読み込み(画面の切り替えや 15 秒の読み直し)が始まっていたら、それも捨てて今の状態で読み直す
// (答えより前の列で札が戻らず、切り替えた画面も出る。最終監査 0.9.1 低-F1)
export function dropPendingLoads(mark = loadVersion) { const raced = loadVersion !== mark; loadVersion++; if (raced) load(); }
// status と表示中の画面の分だけを読む。遅い古い応答は新しい選択へ反映しない。
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
        // 回答済みでは未回答の列を読まない。
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
