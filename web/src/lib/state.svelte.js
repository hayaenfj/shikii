// 画面の状態と、サーバーからの読み込み。
// ui: 開いているタブ(判定ごと。「くわしく」なら知識のタブも)、見せ方(1 件ずつ / 一覧)、絞り込み(全部 / 迷い / 抜き取り / その他)、
//     未回答 / 回答済み、あとで答える(スキップ)、かんたん表示 / くわしく、使い方の案内。保存するものは localStorage(shikii.*)
// data: /v1/status と、表示中の列・聞き方または知識。null は読み込み前
import { SvelteSet } from 'svelte/reactivity';
import { api } from './api.js';
import { read, write } from './prefs.js';

// URL の ?spec=<id>&trace=<trace_id> で、その判定を開いた状態で始める(hook の「あとで答える → URL」から来る)。pinned の 1 件は列の先頭に出る
const params = new URLSearchParams(typeof location !== 'undefined' ? location.search : '');
export const ui = $state({
  advanced: read('shikii.mode') === 'advanced',
  showHelp: read('shikii.help') !== 'hidden',
  tab: params.get('spec') || read('shikii.tab'),
  pinned: params.get('trace') || '',
  view: read('shikii.view') || 'one',
  filter: 'all',
  project: '', // 仕事(記録の project の印)での絞り込み。'' は全部。一つのプロジェクトの中で付けた印だけを絞る

  listSize: 30,
  done: false,
});
export const skipped = new SvelteSet();
export const data = $state({ state: null, loadError: '' });

export function setAdvanced(v) { ui.advanced = v; write('shikii.mode', v ? 'advanced' : 'simple'); }
export function setShowHelp(v) { ui.showHelp = v; write('shikii.help', v ? 'shown' : 'hidden'); }
export function setTab(v) { ui.tab = v; write('shikii.tab', v); }
export function setView(v) { ui.view = v; write('shikii.view', v); }

// ナビと読取りで同じ選択規則を使う。知識は未読でも、くわしく表示から開ける。
export function selectedTab(st, selection = ui) {
  if (!st?.specs?.length) return null;
  if (selection.advanced && selection.tab === 'wiki') return 'wiki';
  return st.specs.find(s => s.id === selection.tab)?.id
    || st.specs.find(s => s.unlabeled > 0)?.id || st.specs[0].id;
}

let loadVersion = 0;
// statusと表示中の欄だけを更新する。遅い古い応答は新しい選択へ反映しない。
export async function load() {
  const version = ++loadVersion;
  const selection = { tab: ui.tab, advanced: ui.advanced, done: ui.done };
  try {
    const st = await api.status();
    if (version !== loadVersion) return;
    if (!Array.isArray(st.projects)) {
      const tab = selectedTab(st, selection);
      if (tab === 'wiki') {
        st.wiki = await api.wiki();
      } else if (tab) {
        const sp = st.specs.find(s => s.id === tab);
        // 回答済みでは未回答の列を読まない。
        sp.queue = { items: [] };
        const [rows, tune] = await Promise.all([
          selection.done ? api.answered(tab) : api.queue(tab),
          selection.advanced && !selection.done ? api.tune(tab).catch(() => null) : null,
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
