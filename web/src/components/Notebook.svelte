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
</script>

<section class="adv notebook">
  <h2>{t('notebookTitle')} {#if wiki.notebook_project}<span class="pill project">{wiki.notebook_project}</span>{/if}</h2>
  <p class="k">{t('notebookHow')}</p>
  {#if wiki.knowledge_usage}
    <div class="memory-effect">
      <h3>{t('knowledgeUsageTitle')}</h3>
      <p class="k">{t('knowledgeUsageHow')}</p>
      <div class="meta">
        {#each ['prepared', 'with_matches', 'with_citations'] as key}
          <span class="pill" data-knowledge={key}>{t('knowledgeUsageCount')(key, wiki.knowledge_usage[key])}</span>
        {/each}
      </div>
      <p class="k">{t('knowledgeUsageCost')(wiki.knowledge_usage.lookup_milliseconds, wiki.knowledge_usage.context_bytes)}</p>
    </div>
  {/if}
  {#if wiki.maintenance?.length}
    <div class="memory-effect">
      <h3>{t('knowledgeMaintenanceTitle')}</h3>
      <p class="k">{t('knowledgeMaintenanceHow')}</p>
      <ul>{#each wiki.maintenance as task}<li data-maintenance={task.page_id}>{task.title}</li>{/each}</ul>
    </div>
  {/if}
  {#if effect || wiki.memory_effect_error}
    <div class="memory-effect">
      <h3>{t('memoryEffectTitle')}</h3>
      {#if wiki.memory_effect_error}
        <p class="audit">{t('memoryEffectError')}</p>
      {:else}
        <p class="k">{t('memoryEffectHow')(effect.window)}</p>
        <div class="meta">
          {#each ['delivered', 'helpful', 'unused', 'harmful', 'unknown', 'pending'] as key}
            <span class="pill" data-memory={key}>{t('memoryEffectCount')(key, effect.stats[key])}</span>
          {/each}
        </div>
        {#if effect.corrections}
          <h3>{t('correctionTitle')}</h3>
          <p class="k">{t('correctionHow')}</p>
          <div class="meta">
            {#each ['delivered', 'followed', 'repeated', 'not_applicable', 'unknown', 'pending'] as key}
              <span class="pill" data-correction={key}>{t('correctionCount')(key, effect.corrections[key])}</span>
            {/each}
          </div>
        {/if}
        {#each effect.recent || [] as item (item.source_id + item.quote.key)}
          <details class="text">
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
  {#if !lessons.length}<div class="empty">{t('notebookEmpty')}</div>{/if}
  {#if selected}<button type="button" class="weak" data-notebook-back onclick={back}>{t('notebookBack')}</button>{/if}
  {#each lessons as page (page.id)}
    <div class="item lesson">
      <div class="meta"><span class="pill">{t('notebookHypothesis')}</span></div>
      <div class="text"><b>{page.title}</b><p>{page.lesson}</p><div class="k">{t('notebookWhen')}: {page.applies_to}</div></div>
      {#if page.body}
        <details class="text wiki-article" open={!!selected}>
          <summary>{t('notebookBody')}</summary>
          {#each page.body.split(/\n\s*\n/) as block}
            {#if /^#{1,6} /.test(block)}
              <h4>{block.replace(/^#{1,6} /, '')}</h4>
            {:else}<p>{block}</p>{/if}
          {/each}
        </details>
      {/if}
      {#if page.related?.length}
        <div class="text"><span class="k">{t('notebookRelated')}</span>
          <div class="btns">
            {#each page.related as id}
              <button type="button" class="weak" data-related={id} disabled={!!busy} onclick={() => related(id)}>{wiki.lessons?.find(p => p.id === id)?.title || id.slice(0, 8)}</button>
            {/each}
          </div>
        </div>
      {/if}
      {#if page.correction}
        <div class="text">
          <p>{t('correctionQuote')}: {page.correction.quote}</p>
          <p>{t('correctionAction')}: {page.correction.action}</p>
        </div>
      {/if}
      <div class="btns">
        {#each page.sources || [] as ref}
          <button type="button" class="weak" disabled={busy === ref.id} onclick={() => source(ref.id)}>{t('notebookSource')} {ref.id.slice(0, 6)}</button>
        {/each}
      </div>
      {#each page.sources || [] as ref}
        {#if material[ref.id]}
          <pre class="text source-material">{JSON.stringify(material[ref.id], null, 2)}</pre>
        {/if}
      {/each}
    </div>
  {/each}
</section>

<style>
  .wiki-article p { white-space: pre-wrap; overflow-wrap: anywhere; }
  .wiki-article h4 { margin-bottom: .5rem; }
</style>
