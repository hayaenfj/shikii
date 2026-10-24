<script>
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { wiki } = $props();
  let material = $state({});
  let busy = $state('');
  let selected = $state(null);
  let navigation = 0;
  let lessons = $derived(selected ? [selected] : wiki.lessons || []);
  let effect = $derived(wiki.memory_effect);

  // AI が何を蓄積しているか(0.8.0): 記事を関連リンクで繋がる群にまとめて階層で見せる。群の名は、群の中でいちばん多く指されている記事の題。
  // 繋がりの無い記事は「単独の記事」。各群は畳め、記事を押すと本文・出典へ(既存の表示)
  let all = $derived(wiki.lessons || []);
  let groups = $derived.by(() => {
    const byId = new Map(all.map(p => [p.id, p]));
    const parent = new Map(all.map(p => [p.id, p.id]));
    const find = id => { while (parent.get(id) !== id) { parent.set(id, parent.get(parent.get(id))); id = parent.get(id); } return id; };
    const union = (a, b) => { if (byId.has(a) && byId.has(b)) parent.set(find(a), find(b)); };
    const inbound = new Map();
    for (const p of all) for (const r of p.related || []) { union(p.id, r); inbound.set(r, (inbound.get(r) || 0) + 1); }
    const members = new Map();
    for (const p of all) { const root = find(p.id); if (!members.has(root)) members.set(root, []); members.get(root).push(p); }
    const out = [], single = [];
    for (const pages of members.values()) {
      if (pages.length === 1) { single.push(pages[0]); continue; }
      pages.sort((a, b) => (inbound.get(b.id) || 0) - (inbound.get(a.id) || 0) || a.title.localeCompare(b.title));
      out.push({ id: pages[0].id, title: pages[0].title, pages });
    }
    out.sort((a, b) => b.pages.length - a.pages.length || a.title.localeCompare(b.title));
    if (single.length) out.push({ id: 'single', title: t('notebookSingle'), pages: single.sort((a, b) => a.title.localeCompare(b.title)) });
    return out;
  });
  let sourceCount = $derived(new Set(all.flatMap(p => (p.sources || []).map(s => s.id))).size);
  let linkCount = $derived(all.reduce((n, p) => n + (p.related || []).length, 0));
  let open = $state(new Set());
  function toggle(id) { const next = new Set(open); if (next.has(id)) next.delete(id); else next.add(id); open = next; }
  function show(id) { related(id); }

  async function related(id) {
    const request = ++navigation;
    busy = id;
    try {
      const result = await api.notebook({ operation: 'query', query: 'lesson:' + id });
      if (request !== navigation) return;
      if (result.lessons?.[0]) selected = result.lessons[0];
      else toast(t('notebookIssues'));
    } catch (e) { if (request === navigation) toast(t('error')(e.message)); }
    if (request === navigation) busy = '';
  }

  function back() {
    navigation++;
    selected = null;
    busy = '';
  }

  async function source(id) {
    busy = id;
    try {
      const result = await api.notebook({ operation: 'query', sources: [id] });
      for (const value of result.source_material || []) material[value.id] = { ...value, originals: result.originals?.[value.id] };
      for (const value of result.observations || []) material[value.id] = value;
    } catch (e) { toast(t('error')(e.message)); }
    busy = '';
  }
  // 見た目(Tailwind)。何度も出る箱と印だけここで名前を付ける
  const box = 'my-6 rounded-[14px] border border-line bg-surface p-5 max-sm:p-[15px]';
  const h3 = 'mt-[22px] mb-4 text-[15px] font-bold';
  const meta = 'mb-3 flex flex-wrap items-center gap-[9px] text-[11px] text-muted';
  const card = 'item lesson mt-[18px] mb-5 rounded-[15px] border border-line bg-surface p-[22px] shadow-card max-sm:rounded-xl max-sm:px-3.5 max-sm:py-4 [&_.text]:text-[13px] [&_.text]:whitespace-pre-wrap [&_.text_p]:leading-[1.9] [&_b]:text-[16px]';
</script>

<section class="notebook">
  <h2 class="text-[25px] max-md:text-[23px]">{t('notebookTitle')} {#if wiki.notebook_project}<span class="pill project">{wiki.notebook_project}</span>{/if}</h2>
  <p class="k">{t('notebookHow')}</p>
  {#if wiki.knowledge_usage}
    <div class="memory-effect {box}">
      <h3 class={h3}>{t('knowledgeUsageTitle')}</h3>
      <p class="k">{t('knowledgeUsageHow')}</p>
      <div class={meta}>
        {#each ['prepared', 'with_matches', 'with_citations'] as key}
          <span class="pill" data-knowledge={key}>{t('knowledgeUsageCount')(key, wiki.knowledge_usage[key])}</span>
        {/each}
      </div>
      <p class="k">{t('knowledgeUsageCost')(wiki.knowledge_usage.lookup_milliseconds, wiki.knowledge_usage.context_bytes)}</p>
    </div>
  {/if}
  {#if wiki.maintenance?.length}
    <div class="memory-effect {box}">
      <h3 class={h3}>{t('knowledgeMaintenanceTitle')}</h3>
      <p class="k">{t('knowledgeMaintenanceHow')}</p>
      <ul>{#each wiki.maintenance as task}<li data-maintenance={task.page_id}>{task.title}</li>{/each}</ul>
    </div>
  {/if}
  {#if effect || wiki.memory_effect_error}
    <div class="memory-effect {box}">
      <h3 class={h3}>{t('memoryEffectTitle')}</h3>
      {#if wiki.memory_effect_error}
        <p class="audit">{t('memoryEffectError')}</p>
      {:else}
        <p class="k">{t('memoryEffectHow')(effect.window)}</p>
        <div class={meta}>
          {#each ['delivered', 'helpful', 'unused', 'harmful', 'unknown', 'pending'] as key}
            <span class="pill" data-memory={key}>{t('memoryEffectCount')(key, effect.stats[key])}</span>
          {/each}
        </div>
        {#if effect.corrections}
          <h3 class={h3}>{t('correctionTitle')}</h3>
          <p class="k">{t('correctionHow')}</p>
          <div class={meta}>
            {#each ['delivered', 'followed', 'repeated', 'not_applicable', 'unknown', 'pending'] as key}
              <span class="pill" data-correction={key}>{t('correctionCount')(key, effect.corrections[key])}</span>
            {/each}
          </div>
        {/if}
        {#each effect.recent || [] as item (item.source_id + item.quote.key)}
          <details class="text mb-3.5 wrap-anywhere">
            <summary>{t('memoryEffectCount')(item.assessment.result, 1)} · {item.request}</summary>
            <p>{item.quote.lesson || item.quote.reply}</p>
            {#if item.quote.correction}
              <p>{t('correctionQuote')}: {item.quote.correction.quote}</p>
              <p>{t('correctionAction')}: {item.quote.correction.action}</p>
              <p data-correction-result={item.assessment.correction_result || 'unknown'}>{t('correctionCount')(item.assessment.correction_result || 'unknown', 1)}</p>
            {/if}
            <div class="k">{item.quote.key}</div>
            <p>{t('memoryEffectDecision')}: {item.assessment.decision || t('memoryEffectUnknown')}</p>
            <p>{t('memoryEffectEvidence')}: {item.assessment.evidence}</p>
          </details>
        {/each}
      {/if}
    </div>
  {/if}
  {#if wiki.pending_sources_count}<p class="k">{t('notebookPending')(wiki.pending_sources_count)}</p>{/if}
  {#if (wiki.notebook_issues || []).length}<p class="audit">{t('notebookIssues')}</p>{/if}
  {#if !lessons.length}<div class="empty-state">{t('notebookEmpty')}</div>{/if}
  {#if !selected && all.length}
    <div class="memory-effect wiki-tree {box}" data-wiki-tree>
      <h3 class={h3}>{t('notebookTreeTitle')}</h3>
      <p class="k">{t('notebookTreeHow')(all.length, sourceCount, linkCount, groups.filter(g => g.id !== 'single').length)}</p>
      <ul class="m-0 list-none p-0 [&_li]:my-[.15rem]">
        {#each groups as g (g.id)}
          <li class="tree-group" data-tree-group={g.id}>
            <button type="button" class="weak tree-toggle font-semibold" aria-expanded={open.has(g.id)} onclick={() => toggle(g.id)}><span aria-hidden="true">{open.has(g.id) ? '▾' : '▸'}</span> {g.title} <span class="badge inline-block rounded-md bg-amberbg px-[7px] py-px text-[10px] text-amber tabular-nums">{g.pages.length}</span></button>
            {#if open.has(g.id)}
              <ul class="mt-[.2rem] mb-[.6rem] list-none pl-[1.4rem]">
                {#each g.pages as p (p.id)}
                  <li><button type="button" class="weak tree-page text-left" data-tree-page={p.id} disabled={!!busy} onclick={() => show(p.id)}>{p.title}</button> <span class="k">{p.applies_to}</span></li>
                {/each}
              </ul>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}
  {#if selected}<button type="button" class="weak" data-notebook-back onclick={back}>{t('notebookBack')}</button>{/if}
  {#each lessons as page (page.id)}
    <div class={card}>
      <div class={meta}><span class="pill">{t('notebookHypothesis')}</span></div>
      <div class="text mb-3.5 wrap-anywhere"><b>{page.title}</b><p>{page.lesson}</p><div class="k">{t('notebookWhen')}: {page.applies_to}</div></div>
      {#if page.body}
        <details class="text wiki-article mb-3.5 wrap-anywhere [&_h4]:mb-2 [&_p]:wrap-anywhere [&_p]:whitespace-pre-wrap" open={!!selected}>
          <summary>{t('notebookBody')}</summary>
          {#each page.body.split(/\n\s*\n/) as block}
            {#if /^#{1,6} /.test(block)}
              <h4>{block.replace(/^#{1,6} /, '')}</h4>
            {:else}<p>{block}</p>{/if}
          {/each}
        </details>
      {/if}
      {#if page.related?.length}
        <div class="text mb-3.5 wrap-anywhere"><span class="k">{t('notebookRelated')}</span>
          <div class="btns flex flex-wrap items-center gap-2.5">
            {#each page.related as id}
              <button type="button" class="weak" data-related={id} disabled={!!busy} onclick={() => related(id)}>{wiki.lessons?.find(p => p.id === id)?.title || id.slice(0, 8)}</button>
            {/each}
          </div>
        </div>
      {/if}
      {#if page.correction}
        <div class="text mb-3.5 wrap-anywhere">
          <p>{t('correctionQuote')}: {page.correction.quote}</p>
          <p>{t('correctionAction')}: {page.correction.action}</p>
        </div>
      {/if}
      <div class="btns flex flex-wrap items-center gap-2.5">
        {#each page.sources || [] as ref}
          <button type="button" class="weak" disabled={busy === ref.id} onclick={() => source(ref.id)}>{t('notebookSource')} {ref.id.slice(0, 6)}</button>
        {/each}
      </div>
      {#each page.sources || [] as ref}
        {#if material[ref.id]}
          <pre class="text source-material mb-3.5 rounded-[9px] bg-surface2 p-[15px] wrap-anywhere">{JSON.stringify(material[ref.id], null, 2)}</pre>
        {/if}
      {/each}
    </div>
  {/each}
</section>
