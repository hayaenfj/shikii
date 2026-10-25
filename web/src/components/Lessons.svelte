<script>
  // 知識の正面(0.9.0): 人が全部の記事を読まないで済むよう、場面を選び → 種類ごとに題 → 押すと本文、の順で引く。
  // 人に見てほしいのは機械が決められない例外だけ(受け箱)。種類と場面は記事を保存する時にエージェントが付け、
  // 付いていない記事(版を上げる前の物)は「未分類」に出る(hook が 5 本ずつ埋めさせる)。並びは役立った回数と新しさの決まった計算。
  import { t } from '../lib/i18n.svelte.js';

  let { wiki } = $props();
  const KINDS = ['rule', 'howto', 'criteria', 'fact', 'history'];
  const NONE = '__none__';
  const read = (k, d) => { try { return localStorage.getItem(k) ?? d; } catch { return d; } };
  const write = (k, v) => { try { localStorage.setItem(k, v); } catch { /* 保存できない環境でも表示は動く */ } };

  let lessons = $derived(wiki.lessons || []);
  let meta = $derived(wiki.lesson_meta || {});
  let scene = $state(read('shikii.scene', ''));
  let selectedId = $state('');
  let expanded = $state(new Set());
  let opened = $state(new Set()); // 押して全文を出した行

  const score = l => (meta[l.id]?.helpful || 0) * 1e12 + Date.parse(meta[l.id]?.updated || 0); // 役立った回数、次に新しさ
  let scenes = $derived.by(() => {
    const n = new Map();
    let none = 0;
    for (const l of lessons) { if (l.scene && l.kind) n.set(l.scene, (n.get(l.scene) || 0) + 1); else none++; }
    return { list: [...n.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0], 'ja')), none };
  });
  const inScene = l => !scene || (scene === NONE ? !(l.kind && l.scene) : l.scene === scene && !!l.kind);
  let byKind = $derived(KINDS.map(k => ({ kind: k, items: lessons.filter(l => l.kind === k && inScene(l)).sort((a, b) => score(b) - score(a)) })).filter(g => g.items.length));
  let unclassified = $derived(lessons.filter(l => !(l.kind && l.scene)));
  let selected = $derived(lessons.find(l => l.id === selectedId));
  let byId = $derived(new Map(lessons.map(l => [l.id, l])));
  let inbox = $derived(wiki.inbox || []);
  let stats = $derived(wiki.memory_effect?.stats);
  let corrections = $derived(wiki.memory_effect?.corrections);

  let detailEl = $state();
  // 狭い画面(1 列)では本文の欄が下にあるので、題を押したら本文まで送る
  function showLesson(id) {
    selectedId = id;
    if (typeof matchMedia === 'function' && matchMedia('(max-width: 1023px)').matches) queueMicrotask(() => detailEl?.scrollIntoView?.({ block: 'start', behavior: 'smooth' }));
  }
  function pick(s) { scene = s; write('shikii.scene', s); }
  function toggleKind(k) { const next = new Set(expanded); next.has(k) ? next.delete(k) : next.add(k); expanded = next; }
  function toggleText(key) { const next = new Set(opened); next.has(key) ? next.delete(key) : next.add(key); opened = next; }
  const shown = (key, text, n = 48) => !text || text.length <= n || opened.has(key) ? text : text.slice(0, n) + '…';
  const fmtDate = s => s ? new Date(s).toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' }) : '';

  const frame = 'rounded-[14px] border border-line bg-surface';
  const panel = frame + ' p-4 max-sm:p-3';
  // 欄の見出し(0.9.1): 薄い灰色の帯に、本文の濃い色の太字と緑の縦線。灰色の小さな字だと下の項目に溶け込んだ
  const head = 'section-head mt-0 mb-3 flex items-center gap-2 rounded-lg bg-surface2 px-2.5 py-2 text-[13px] font-bold tracking-[.02em] text-fg before:block before:h-3.5 before:w-1 before:rounded-sm before:bg-accent';
</script>

<section class="lessons" data-lessons>
  <h2 class="text-[25px] max-md:text-[23px]">{t('lessonsTitle')} {#if wiki.notebook_project}<span class="pill project">{wiki.notebook_project}</span>{/if}</h2>
  {#if stats}<p class="k" data-lessons-summary>{t('lessonsSummary')(stats, corrections || {}, lessons.length)}</p>{/if}

  <div class="{panel} mb-4" data-inbox>
    <h3 class={head}>{t('inboxTitle')}</h3>
    {#if inbox.length}
      <ul class="m-0 list-none p-0">
        {#each inbox as it, i (i)}
          <li class="border-b border-line py-1.5 last:border-0" data-inbox-item={it.kind}>
            <span class="audit">{t('inboxKind')(it.kind)}</span>
            {#if it.kind === 'guard_draft'}
              {it.title} <span class="k">shikii guard enable {it.guard_id}</span>
            {:else}
              <button type="button" class="weak" data-open={it.lesson_id} onclick={() => showLesson(it.lesson_id)}>{byId.get(it.lesson_id)?.title || it.lesson}</button>{#if it.count > 1}<span class="k" data-inbox-count> {t('inboxTimes')(it.count)}</span>{/if}
            {/if}
          </li>
        {/each}
      </ul>
    {:else}<p class="k m-0">{t('inboxEmpty')}</p>{/if}
  </div>

  <div class="grid grid-cols-[220px_minmax(260px,1fr)_minmax(280px,1.2fr)] items-start gap-3.5 max-lg:grid-cols-[200px_1fr] max-sm:grid-cols-1">
    <nav class="{frame} p-2" aria-label={t('scenesTitle')} data-scenes>
      <div class="k mx-2 mt-0.5 mb-1.5">{t('scenesHead')(lessons.length)}{#if scene} · <button type="button" class="weak p-0" data-scene-clear onclick={() => pick('')}>{t('scenesClear')}</button>{/if}</div>
      {#each scenes.list as [name, n] (name)}
        <button type="button" class="flex w-full rounded-lg border-0 px-2 py-1.5 text-left {scene === name ? 'bg-accentbg font-bold text-accent' : 'bg-transparent'}" aria-pressed={scene === name} data-scene={name} onclick={() => pick(name)}><span class="min-w-0 flex-1 wrap-anywhere">{name}</span><span class="text-muted tabular-nums">{n}</span></button>
      {/each}
      {#if scenes.none}
        <button type="button" class="flex w-full rounded-lg border-0 px-2 py-1.5 text-left {scene === NONE ? 'bg-accentbg font-bold text-accent' : 'bg-transparent'}" aria-pressed={scene === NONE} data-scene={NONE} onclick={() => pick(NONE)}><span class="flex-1">{t('scenesNone')}</span><span class="text-muted tabular-nums">{scenes.none}</span></button>
      {/if}
    </nav>

    <div data-kinds>
      {#if scene === NONE}
        <div class="{panel} mb-3"><p class="k m-0 mb-2">{t('unclassifiedHow')}</p>
          <ul class="m-0 list-none p-0">{#each unclassified as l (l.id)}<li><button type="button" class="weak px-0 text-left" data-open={l.id} onclick={() => showLesson(l.id)}>{l.title}</button></li>{/each}</ul>
        </div>
      {:else}
        {#if scene}<p class="k mt-0">{t('sceneFor')(scene)}</p>{/if}
        {#each byKind as g (g.kind)}
          {@const open = expanded.has(g.kind) || !!scene}
          {@const items = open ? g.items : g.items.slice(0, 4)}
          <div class="{panel} mb-3" data-kind={g.kind}>
            <h3 class="mt-0 mb-1.5 flex items-baseline gap-2 text-[15px]">{t('kindName')(g.kind)} <span class="k font-normal">{t('kindHint')(g.kind)}</span><span class="ml-auto text-[12px] text-muted">{t('kindCount')(g.items.length)}</span></h3>
            <ul class="m-0 list-none p-0">
              {#each items as l (l.id)}
                <li><button type="button" class="flex w-full gap-2 rounded-md border-0 px-1.5 py-1 text-left {selectedId === l.id ? 'bg-accentbg font-bold' : 'bg-transparent'}" data-open={l.id} onclick={() => showLesson(l.id)}><span class="min-w-0 flex-1 wrap-anywhere">{l.title}</span>{#if !scene}<span class="text-[12px] whitespace-nowrap text-muted">{l.scene}</span>{/if}</button></li>
              {/each}
            </ul>
            {#if !scene && g.items.length > 4}<button type="button" class="weak px-0" data-kind-more={g.kind} onclick={() => toggleKind(g.kind)}>{open ? t('kindLess') : t('kindMore')(g.items.length - 4)}</button>{/if}
          </div>
        {/each}
        {#if !byKind.length}<div class="empty-state">{t('lessonsEmpty')}</div>{/if}
      {/if}
    </div>

    <div class="{panel} sticky top-4 max-lg:col-span-2 max-sm:col-span-1 max-sm:static" bind:this={detailEl} data-lesson-detail>
      {#if selected}
        <h3 class="mt-0 mb-2 text-[17px]">{selected.title}</h3>
        <p class="m-0 mb-2 flex flex-wrap gap-1.5">
          {#if selected.kind}<span class="pill">{t('kindName')(selected.kind)}</span>{/if}
          {#if selected.scene}<span class="pill">{selected.scene}</span>{:else}<span class="pill ready">{t('scenesNone')}</span>{/if}
          {#if meta[selected.id]?.helpful}<span class="pill calibrated">{t('helpfulCount')(meta[selected.id].helpful)}</span>{/if}
        </p>
        <p class="whitespace-pre-wrap">{selected.lesson}</p>
        <div class="k">{t('appliesTo')}</div><p class="mt-0">{selected.applies_to}</p>
        {#if selected.body}<div class="k">{t('bodyTitle')}</div><div class="text rounded-lg bg-bg p-2.5 text-[13px] whitespace-pre-wrap wrap-anywhere">{selected.body}</div>{/if}
        {#if (selected.related || []).length}
          <div class="k mt-2">{t('relatedTitle')}</div>
          <p class="mt-0 flex flex-wrap gap-1.5">{#each selected.related as r (r)}{#if byId.get(r)}<button type="button" class="weak" data-open={r} onclick={() => showLesson(r)}>{byId.get(r).title}</button>{/if}{/each}</p>
        {/if}
        <div class="k mt-2">{t('lessonFoot')((selected.sources || []).length, fmtDate(meta[selected.id]?.updated))}</div>
      {:else}<p class="k m-0">{t('detailEmpty')}</p>{/if}
    </div>
  </div>

  {#if wiki.memory_effect?.recent?.length}
    <div class="{panel} mt-4" data-effect>
      <h3 class={head}>{t('effectTitle')}</h3>
      <ul class="m-0 list-none p-0">
        {#each wiki.memory_effect.recent as r, i (i)}
          <li class="border-b border-line py-1.5 last:border-0">
            <span class="pill {r.assessment?.result === 'helpful' ? 'calibrated' : r.assessment?.result === 'harmful' ? 'ready' : ''}">{t('effectResult')(r.assessment?.result)}</span>
            <button type="button" class="weak px-1 text-left" data-clip={'req' + i} onclick={() => toggleText('req' + i)}>{shown('req' + i, r.request)}{#if (r.request || '').length > 48}<span class="text-accent"> {opened.has('req' + i) ? t('clipLess') : t('clipMore')}</span>{/if}</button>
            <div class="k">{t('effectLesson')} <button type="button" class="weak px-0 text-left" data-clip={'les' + i} onclick={() => toggleText('les' + i)}>{shown('les' + i, r.quote?.lesson, 60)}{#if (r.quote?.lesson || '').length > 60}<span class="text-accent"> {opened.has('les' + i) ? t('clipLess') : t('clipMore')}</span>{/if}</button></div>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</section>
