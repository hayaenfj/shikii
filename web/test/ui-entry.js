// 画面の振る舞いの検査(jsdom): (1) 言語の選択を change すると、言語・文・保存値が変わる。(2) ?trace= で指された判定が列の先頭に来る。
// 文字の有無を見る Go のテストでは、onchange が外れても気づけない(監査 7 の 2)。vite の lib ビルドで 1 つに組み、scripts/check-ui.mjs が jsdom の中で run() を呼ぶ。
import { mount, unmount, flushSync } from 'svelte';
import LangPicker from '../src/components/LangPicker.svelte';
import SpecPanel from '../src/components/SpecPanel.svelte';
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
  ui.project = '';
  ui.view = 'one';
  const ok = hasChips && onlyBeta;
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (project chips=' + chips.length + ', filtered to beta=' + onlyBeta + ')');
  return ok;
}

export function run() {
  const target = document.getElementById('app');
  const ok = checkLang(target) && checkPinned(target) && checkProject(target);
  if (!ok) process.exit(1);
}
