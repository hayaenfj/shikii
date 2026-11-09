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




// 0.8.0: the Knowledge section shows "what the AI is accumulating" as a hierarchy of clusters connected by related links. Opening a cluster lists its articles, and pressing a title goes to the text. Articles with no connections are "standalone articles"
export async function checkNotebookTree(target) {
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



// 0.9.1 → 0.9.2 redesign (P3): the rules box at the top of Knowledge shows only the "いつも" (always) rules, and "see all rules" opens the rest (always first, then newest).
// The "new" marker goes only on rules changed in the last 7 days, and not at all when every rule is new (it would not tell anything apart).
// The text panel sits beside the rules box, so on a wide screen pressing a rule does not scroll; on a phone it scrolls to the text
export function checkRulesPanel(target) {
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
export function checkKnowledgeFront(target) {
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


export async function checkNotebookArticle(target) {
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


export async function checkNotebook(target) {
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


// Shows the API's evidence, and does not make undefined out of missing items.
export function checkClaimEvidence(target) {
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


// Work records (0.8.1): starts collapsed per folder; opening shows files; pressing shows text as text (no HTML rendered) and images as images. When empty, it says so.
export async function checkHandoff(target) {
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
export function checkLessons(target) {
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


// Editing keeps the opened revision through conflicts; deletion needs an explicit confirmation.
export async function checkManageArticle(target) {
  localStorage.removeItem('shikii.scene');
  const article = { id: '0000000000000001', revision: '0000000000000002', title: 'Example article', lesson: 'Summary', applies_to: 'Docs', body: 'Detail', rationale: 'Choose local files; reject another form.', recheck: [{ path: 'behavior.txt', state: 'changed' }], kind: 'howto', scene: 'Docs', sources: [], passages: [{ role: 'current', quote: 'Detail', conditions: 'Current workflow', reason: 'Checked source.' }] };
  const realFetch = globalThis.fetch;
  const sent = [], changed = [];
  let fail = true;
  globalThis.fetch = async (url, opts) => {
    if (url !== 'v1/wiki/article') throw new Error('unexpected request');
    const body = JSON.parse(opts.body); sent.push(body);
    return fail ? { ok: false, status: 409, json: async () => ({ error: 'changed' }) } : { ok: true, json: async () => ({ ...article, ...body, revision: '0000000000000003', status: body.action === 'delete' ? 'deleted' : 'hypothesis' }) };
  };
  const app = mount(Lessons, { target, props: { wiki: { lessons: [article] }, onchange: p => changed.push(p) } }); flushSync();
  const settle = async () => { await new Promise(r => setTimeout(r, 0)); flushSync(); };
  const click = selector => { target.querySelector(selector)?.click(); flushSync(); };
  try {
    click('[data-open]');
    const reasoning = target.querySelector('[data-article-rationale]')?.textContent.includes('reject another form') && target.querySelector('[data-article-recheck]')?.textContent.includes('behavior.txt');
    click('[data-article-edit]');
    const title = target.querySelector('[name="title"]');
    if (!title) { console.log('check-ui: FAIL (article editor missing)'); return false; }
    title.value = 'Changed title'; title.dispatchEvent(new window.Event('input', { bubbles: true })); flushSync();
    const interpreted = target.querySelector('[data-passage-role="current"]')?.textContent.includes('Detail');
    const quote = target.querySelector('[data-passage-row] textarea');
    const bodyField = target.querySelector('[name="body"]');
    if (quote) { quote.value = 'Updated detail'; quote.dispatchEvent(new window.Event('input', { bubbles: true })); }
    bodyField.value = 'Updated detail'; bodyField.dispatchEvent(new window.Event('input', { bubbles: true })); flushSync();
    const isolated = article.passages[0].quote === 'Detail';
    target.querySelector('[data-article-form]').dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true })); await settle();
    const conflict = target.querySelector('[name="title"]')?.value === 'Changed title' && !!target.querySelector('[data-article-error]') && changed.length === 0 && sent[0]?.revision === article.revision;
    fail = false;
    target.querySelector('[data-article-form]').dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true })); await settle();
    const saved = changed[0]?.title === 'Changed title' && changed[0]?.rationale === article.rationale && changed[0]?.body === 'Updated detail' && changed[0]?.passages?.[0]?.quote === 'Updated detail' && sent[0]?.passages?.[0]?.conditions === 'Current workflow' && !target.querySelector('[data-article-form]');
    click('[data-article-delete]');
    const explains = !!target.querySelector('[data-article-delete-confirm]') && sent.length === 2;
    click('[data-article-cancel]');
    const cancelled = sent.length === 2 && !target.querySelector('[data-article-delete-confirm]');
    click('[data-article-delete]'); click('[data-article-delete-confirm]'); await settle();
    const deleted = changed[1]?.status === 'deleted' && sent[2]?.action === 'delete';
    const ok = interpreted && isolated && reasoning && conflict && saved && explains && cancelled && deleted;
    console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (article editing: conflict retains draft=' + conflict + ', saved=' + saved + ', confirms=' + explains + ', cancel sends nothing=' + cancelled + ', deletes=' + deleted + ')');
    return ok;
  } finally { unmount(app); globalThis.fetch = realFetch; }
}


export async function checkBackgroundArticleDraft(target) {
  const originalFetch=globalThis.fetch, originalState=data.state, originalUI={...ui};
  const article={id:'0000000000000001',revision:'0000000000000002',title:'Original',lesson:'Summary',applies_to:'Docs',body:'Body',kind:'howto',scene:'Docs',sources:[]};
  let waits=0;
  const status=()=>({specs:[{id:'done-as-asked',type:'noul',question:'Done?',titles:{},questions:{},keys:[],person_answers:true,waiting_for_person:waits}],project_name:'Audit'});
  globalThis.fetch=async url=>({ok:true,json:async()=>url==='v1/status'?status():url==='v1/wiki'?{lessons:[article]}:url.startsWith('v1/tune')?{}:{items:[]}});
  const settle=async()=>{await new Promise(r=>setTimeout(r,0));flushSync();};
  localStorage.removeItem('shikii.scene');localStorage.removeItem('shikii.knowledgeTab');
  Object.assign(ui,{page:'',tab:'',done:false,pinned:'',showHelp:false});
  data.state={...status(),wiki:{lessons:[article]}};
  const app=mount(App,{target}); await settle();await settle();
  try {
    target.querySelector('[data-open]').click();flushSync();
    target.querySelector('[data-article-edit]').click();flushSync();
    const title=target.querySelector('[name="title"]');
    title.value='Unsent draft';title.dispatchEvent(new window.Event('input',{bubbles:true}));flushSync();
    waits=1;await load();await settle();
    const kept=target.querySelector('[name="title"]')?.value==='Unsent draft';
    console.log((kept?'check-ui: ok':'check-ui: FAIL')+' (L0150-21 background status update preserves open article draft='+kept+')');
    return kept;
  } finally {unmount(app);globalThis.fetch=originalFetch;data.state=originalState;Object.assign(ui,originalUI);}
}


export async function checkCompareArticle(target) {
  localStorage.removeItem('shikii.scene');
  const article = { id:'0000000000000001', revision:'0000000000000002', title:'Shared article', lesson:'Summary', applies_to:'Docs', body:'Base paragraph', kind:'howto', scene:'Docs', sources:[] };
  const realFetch=globalThis.fetch, sent=[], changed=[];
  const merged={...article, revision:'0000000000000003',body:'Peer paragraph\n\nMy paragraph <script>throw 1</script>'};
  globalThis.fetch=async(url,opts)=>{
    const body=JSON.parse(opts.body);sent.push(body);
    if(body.action==='reconcile') return {ok:true,json:async()=>({status:'ready',current:{id:article.id,revision:merged.revision},result:merged})};
    if(body.revision===article.revision) return {ok:false,status:409,json:async()=>({error:'changed'})};
    return {ok:true,json:async()=>({...merged,...body,revision:'0000000000000004'})};
  };
  const app=mount(Lessons,{target,props:{wiki:{lessons:[article]},onchange:p=>changed.push(p)}});flushSync();
  const settle=async()=>{await new Promise(r=>setTimeout(r,0));flushSync();};
  const click=selector=>{target.querySelector(selector)?.click();flushSync();};
  try {
    click('[data-open]');click('[data-article-edit]');
    const input=target.querySelector('[name="body"]');input.value='My paragraph';input.dispatchEvent(new window.Event('input',{bubbles:true}));flushSync();
    target.querySelector('[data-article-form]').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));await settle();
    click('[data-article-compare]');await settle();
    const preview=sent.length===2 && changed.length===0 && sent[1].revision===article.revision && target.querySelector('[data-article-comparison]')?.textContent.includes('Peer paragraph') && !target.querySelector('[data-article-comparison] script') && target.querySelector('[data-article-save]')?.disabled;
    click('[data-article-merge-save]');await settle();
    const saved=changed.length===1 && sent[2]?.revision===merged.revision && changed[0].body===merged.body;
    const ok=preview&&saved;
    console.log((ok?'check-ui: ok':'check-ui: FAIL')+' (peer comparison: preview saves nothing and escapes HTML='+preview+', guarded combined save='+saved+')');
    return ok;
  } finally {unmount(app);globalThis.fetch=realFetch;}
}


export async function checkComparisonReason(target) {
 const realFetch=globalThis.fetch, lang=i18n.lang;
 const reason='Synthetic quotation must be updated with its body. <script>throw 1</script>';
 let passed=true;
 try {
  for(const code of ['ja','en']) {
   i18n.lang=code;localStorage.removeItem('shikii.scene');
   const article={id:'0000000000000001',revision:'0000000000000002',title:'Shared article',lesson:'Summary',applies_to:'Docs',body:'Original body',kind:'howto',scene:'Docs',sources:[]};
   globalThis.fetch=async(url,opts)=>{const b=JSON.parse(opts.body);return b.action==='reconcile'?{ok:true,json:async()=>({status:'conflict',conflicts:[],reason})}:{ok:false,status:409,json:async()=>({error:'changed'})};};
   const app=mount(Lessons,{target,props:{wiki:{lessons:[article]},onchange:()=>{passed=false}}});flushSync();
   const settle=async()=>{await new Promise(r=>setTimeout(r,0));flushSync();};
   const click=selector=>{target.querySelector(selector)?.click();flushSync();};
   try {
    click('[data-open]');click('[data-article-edit]');
    target.querySelector('[data-article-form]').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));await settle();
    click('[data-article-compare]');await settle();
    const panel=target.querySelector('[data-article-comparison]');
    const ok=panel?.textContent.includes(reason) && !panel.querySelector('script') && !target.querySelector('[data-article-merge-save]') && target.querySelector('[name="body"]')?.value===article.body;
    passed=passed&&ok;
   } finally {unmount(app);}
  }
  console.log((passed?'check-ui: ok':'check-ui: FAIL')+' (comparison validation: reasons retained, escaped, no save, input retained in both languages='+passed+')');
  return passed;
 } finally {globalThis.fetch=realFetch;i18n.lang=lang;}
}
