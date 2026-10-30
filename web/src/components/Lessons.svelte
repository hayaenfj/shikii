<script>
  // The front of Knowledge (0.9.0; one screen for looking things up from 0.9.2): a search box, then scenes → titles per kind → the text beside them.
  // The rules box shows only the "いつも" (always) rules, and "see all rules" opens the rest. When a scene or words narrow the list, the rules box steps aside and the rules
  // are in the list instead, so a rule is never listed twice. Scenes with one page are grouped under "other". What a person should look at (the inbox) appears only when there is something.
  // The AI's internal numbers (deliveries, helped counts, effects) are on the AI page and in the folded detailed records, not here
  import { t } from '../lib/i18n.svelte.js';
  import { plainText } from '../lib/text.js';
  import { applyArticle } from '../lib/state.svelte.js';
  import ArticleEditor from './ArticleEditor.svelte';

  let { wiki, onchange = applyArticle } = $props();
  let editing = $state(null);
  function saved(page) { editing = null; if (page.status === 'deleted') selectedId = ''; onchange(page); }
  const KINDS = ['rule', 'howto', 'criteria', 'fact', 'history'];
  const NONE = '__none__', OTHER = '__other__';
  const read = (k, d) => { try { return localStorage.getItem(k) ?? d; } catch { return d; } };
  const write = (k, v) => { try { localStorage.setItem(k, v); } catch { /* the display still works where it cannot save */ } };

  let lessons = $derived(wiki.lessons || []);
  let meta = $derived(wiki.lesson_meta || {});
  let scene = $state(read('shikii.scene', ''));
  let words = $state('');
  let selectedId = $state('');
  let expanded = $state(new Set());
  let allRules = $state(false);

  const score = l => (meta[l.id]?.helpful || 0) * 1e12 + Date.parse(meta[l.id]?.updated || 0); // times helpful, then recency
  let sceneCount = $derived.by(() => { const n = new Map(); for (const l of lessons) if (l.scene && l.kind) n.set(l.scene, (n.get(l.scene) || 0) + 1); return n; });
  let scenes = $derived.by(() => {
    const list = [...sceneCount.entries()].filter(([, n]) => n > 1).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0], 'ja'));
    const other = [...sceneCount.values()].filter(n => n === 1).length;
    return { list, other, none: lessons.filter(l => !(l.kind && l.scene)).length };
  });
  const inScene = l => !scene || (scene === NONE ? !(l.kind && l.scene) : scene === OTHER ? !!(l.kind && l.scene) && sceneCount.get(l.scene) === 1 : l.scene === scene && !!l.kind);
  const matches = l => { const w = words.trim().toLowerCase(); return !w || [l.title, l.lesson, l.applies_to, l.body, l.scene].some(x => (x || '').toLowerCase().includes(w)); };
  let narrowed = $derived(!!scene || !!words.trim());
  // Without narrowing, the rules are in the rules box only; with narrowing, in the list only
  let byKind = $derived(KINDS.filter(k => narrowed || k !== 'rule').map(k => ({ kind: k, items: lessons.filter(l => l.kind === k && inScene(l) && matches(l)).sort((a, b) => score(b) - score(a)) })).filter(g => g.items.length));
  let unclassified = $derived(lessons.filter(l => !(l.kind && l.scene) && matches(l)));
  let selected = $derived(lessons.find(l => l.id === selectedId));
  let byId = $derived(new Map(lessons.map(l => [l.id, l])));
  let inbox = $derived(wiki.inbox || []);
  const isAlways = l => ['いつも', 'always'].includes((l.scene || '').trim().toLowerCase());
  const updated = l => Date.parse(meta[l.id]?.updated || 0) || 0;
  const recentRule = l => Date.now() - updated(l) < 7 * 86400000;
  let rules = $derived(lessons.filter(l => l.kind === 'rule').sort((a, b) => (isAlways(b) - isAlways(a)) || (updated(b) - updated(a))));
  let shownRules = $derived(allRules ? rules : rules.filter(isAlways));
  let everyRuleNew = $derived(rules.length > 0 && rules.every(recentRule));

  let detailEl = $state();
  // The text panel is beside the lists on wide and middle screens; on a phone (1 column) it is below, so pressing a title scrolls to it
  function showLesson(id) {
    selectedId = id;
    if (typeof matchMedia === 'function' && matchMedia('(max-width: 519px)').matches) queueMicrotask(() => detailEl?.scrollIntoView?.({ block: 'start', behavior: 'smooth' }));
  }
  function pick(s) { scene = s; write('shikii.scene', s); }
  function toggleKind(k) { const next = new Set(expanded); next.has(k) ? next.delete(k) : next.add(k); expanded = next; }
  const fmtDate = s => s ? new Date(s).toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' }) : '';

  const frame = 'rounded-[14px] border border-line bg-surface';
  const panel = frame + ' p-4 max-sm:p-3';
  const head = 'section-head mt-0 mb-2 flex items-center gap-2 text-[15px] font-bold text-fg';
  const row = on => 'flex w-full items-baseline gap-2 rounded-md border-0 px-1.5 py-1.5 text-left text-[14px] ' + (on ? 'bg-accentbg font-bold' : 'bg-transparent');
  const chip = on => 'flex w-full items-baseline gap-2 rounded-lg border-0 px-2 py-1.5 text-left text-[14px] max-lg:w-auto max-lg:shrink-0 max-lg:rounded-full max-lg:border max-lg:border-line max-lg:px-3 max-lg:py-1 ' + (on ? 'bg-accentbg font-bold text-accent' : 'bg-transparent');
</script>

<section class="lessons" data-lessons>
  <div class="mb-4">
    <input type="search" class="w-full rounded-[10px] border border-line bg-surface px-3.5 py-2.5 text-[15px]" placeholder={t('lessonSearch')} aria-label={t('lessonSearch')} bind:value={words} data-lesson-search>
  </div>

  {#if inbox.length}
    <div class="{panel} mb-4 border-amber" data-inbox>
      <h3 class={head}>{t('inboxTitle')}</h3>
      <ul class="m-0 list-none p-0">
        {#each inbox as it, i (i)}
          <li class="border-b border-line py-1.5 last:border-0" data-inbox-item={it.kind}>
            <span class="audit">{t('inboxKind')(it.kind)}</span>
            {#if it.kind === 'guard_draft'}
              {it.title} <span class="k">{t('inboxGuardHow')}</span>
            {:else}
              <button type="button" class="weak" data-open={it.lesson_id} onclick={() => showLesson(it.lesson_id)}>{byId.get(it.lesson_id)?.title || it.lesson}</button>{#if it.count > 1}<span class="k" data-inbox-count> {t('inboxTimes')(it.count)}</span>{/if}
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <div class="grid grid-cols-[220px_minmax(0,1fr)_minmax(0,1.1fr)] items-start gap-3.5 max-lg:grid-cols-2 max-sm:grid-cols-1" data-lesson-grid>
    <nav class="{frame} flex flex-col gap-0.5 p-2 max-lg:col-span-2 max-lg:flex-row max-lg:flex-wrap max-lg:gap-1.5 max-sm:col-span-1 max-sm:flex-nowrap max-sm:overflow-x-auto" aria-label={t('scenesTitle')} data-scenes>
      <div class="k mx-2 mt-0.5 mb-1 max-lg:hidden">{t('scenesHead')}</div>
      {#if scene}<button type="button" class={chip(false)} data-scene-clear onclick={() => pick('')}><span class="flex-1">{t('scenesClear')}</span></button>{/if}
      {#each scenes.list as [name, n] (name)}
        <button type="button" class={chip(scene === name)} aria-pressed={scene === name} data-scene={name} onclick={() => pick(name)}><span class="min-w-0 flex-1 wrap-anywhere max-lg:whitespace-nowrap">{name}</span><span class="text-muted tabular-nums">{n}</span></button>
      {/each}
      {#if scenes.other}
        <button type="button" class={chip(scene === OTHER)} aria-pressed={scene === OTHER} data-scene={OTHER} onclick={() => pick(OTHER)}><span class="flex-1 max-lg:whitespace-nowrap">{t('scenesOther')}</span><span class="text-muted tabular-nums">{scenes.other}</span></button>
      {/if}
      {#if scenes.none}
        <button type="button" class={chip(scene === NONE)} aria-pressed={scene === NONE} data-scene={NONE} onclick={() => pick(NONE)}><span class="flex-1 max-lg:whitespace-nowrap">{t('scenesNone')}</span><span class="text-muted tabular-nums">{scenes.none}</span></button>
      {/if}
    </nav>

    <div class="min-w-0" data-kinds>
      {#if !narrowed}
        <div class="{panel} mb-3" data-rules>
          <h3 class={head}>{t('rulesTitle')}</h3>
          {#if shownRules.length}
            <ul class="m-0 grid list-none gap-0.5 p-0">
              {#each shownRules as l (l.id)}
                <li><button type="button" class={row(selectedId === l.id)} data-rule={l.id} onclick={() => showLesson(l.id)}><span class="min-w-0 flex-1 wrap-anywhere">{l.title}</span>{#if recentRule(l) && !everyRuleNew}<span class="badge rounded-md bg-amber px-1.5 py-px text-[12px] text-bg" data-rule-new>{t('ruleNew')}</span>{/if}</button></li>
              {/each}
            </ul>
          {:else if !rules.length}<p class="k m-0">{t('rulesEmpty')}</p>{/if}
          {#if rules.length > shownRules.length || allRules}<button type="button" class="weak mt-1 px-1.5" data-rules-all onclick={() => { allRules = !allRules; }}>{allRules ? t('kindLess') : t('rulesAll')(rules.length)}</button>{/if}
        </div>
      {/if}
      {#if scene === NONE}
        <div class="{panel} mb-3"><p class="k m-0 mb-2">{t('unclassifiedHow')}</p>
          <ul class="m-0 list-none p-0">{#each unclassified as l (l.id)}<li><button type="button" class={row(selectedId === l.id)} data-open={l.id} onclick={() => showLesson(l.id)}>{l.title}</button></li>{/each}</ul>
        </div>
      {:else}
        {#each byKind as g (g.kind)}
          {@const open = expanded.has(g.kind) || narrowed}
          {@const items = open ? g.items : g.items.slice(0, 4)}
          <div class="{panel} mb-3" data-kind={g.kind}>
            <h3 class="{head} items-baseline">{t('kindName')(g.kind)} <span class="text-[13px] font-normal text-muted">{t('kindHint')(g.kind)}</span></h3>
            <ul class="m-0 list-none p-0">
              {#each items as l (l.id)}
                <li><button type="button" class={row(selectedId === l.id)} data-open={l.id} onclick={() => showLesson(l.id)}><span class="min-w-0 flex-1 wrap-anywhere">{l.title}</span>{#if !scene}<span class="text-[12px] whitespace-nowrap text-muted max-sm:hidden">{l.scene}</span>{/if}</button></li>
              {/each}
            </ul>
            {#if !narrowed && g.items.length > 4}<button type="button" class="weak px-1.5" data-kind-more={g.kind} onclick={() => toggleKind(g.kind)}>{open ? t('kindLess') : t('kindMore')(g.items.length - 4)}</button>{/if}
          </div>
        {/each}
        {#if narrowed && !byKind.length}<div class="empty-state">{t('lessonsEmpty')}</div>{/if}
      {/if}
    </div>

    <div class="{panel} sticky top-4 min-w-0 max-sm:static {selected ? '' : 'max-sm:hidden'}" bind:this={detailEl} data-lesson-detail>
      {#if selected}
        <h3 class="mt-0 mb-2 text-[18px]">{selected.title}</h3>
        <div class="mb-3 flex gap-2">
          <button type="button" class="weak" onclick={() => { editing = { article: { ...selected }, action: 'edit' }; }} data-article-edit>{t('articleEdit')}</button>
          <button type="button" class="weak" onclick={() => { editing = { article: { ...selected }, action: 'delete' }; }} data-article-delete>{t('articleDelete')}</button>
        </div>
        <p class="m-0 mb-2 flex flex-wrap gap-1.5">
          {#if selected.kind}<span class="pill">{t('kindName')(selected.kind)}</span>{/if}
          {#if selected.scene}<span class="pill">{selected.scene}</span>{:else}<span class="pill ready">{t('scenesNone')}</span>{/if}
        </p>
        <p class="text-[15px] whitespace-pre-wrap">{selected.lesson}</p>
        <div class="k">{t('appliesTo')}</div><p class="mt-0">{selected.applies_to}</p>
        {#if selected.body}<div class="k">{t('bodyTitle')}</div><div class="text rounded-lg bg-bg p-2.5 text-[14px] whitespace-pre-wrap wrap-anywhere">{plainText(selected.body)}</div>{/if}
        {#if (selected.related || []).length}
          <div class="k mt-2">{t('relatedTitle')}</div>
          <p class="mt-0 flex flex-wrap gap-1.5">{#each selected.related as r (r)}{#if byId.get(r)}<button type="button" class="weak" onclick={() => showLesson(r)}>{byId.get(r).title}</button>{/if}{/each}</p>
        {/if}
        <div class="k mt-2">{t('lessonFoot')((selected.sources || []).length, fmtDate(meta[selected.id]?.updated))}</div>
      {:else}<p class="k m-0">{t('detailEmpty')}</p>{/if}
    </div>
  </div>
</section>

{#if editing}<ArticleEditor article={editing.article} action={editing.action} oncancel={() => { editing = null; }} onsaved={saved} />{/if}
