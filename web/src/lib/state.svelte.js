// 画面の状態と、サーバーからの読み込み。
// ui: 開いているタブ(判定ごと。「くわしく」なら知識のタブも)、見せ方(1 件ずつ / 一覧)、絞り込み(全部 / 迷い / 抜き取り / その他)、
//     未回答 / 回答済み、あとで答える(スキップ)、かんたん表示 / くわしく、使い方の案内。保存するものは localStorage(shikii.*)
// data: /v1/status の結果に、spec ごとの待ち(queue)・聞き方(tune)・知識(wiki)を足したもの。null は読み込み前
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

// 全部を読み直す。spec ごとの tune と wiki は失敗しても画面を止めない(null にして欄を畳む)
export async function load() {
  const st = await api.status();
  if (Array.isArray(st.projects)) { data.state = st; return; }
  for (const sp of st.specs) {
    sp.queue = await api.queue(sp.id);
    try { sp.tune = await api.tune(sp.id); } catch (e) { sp.tune = null; }
  }
  try { st.wiki = await api.wiki(); } catch (e) { st.wiki = null; }
  data.state = st;
}
