<script>
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { wiki } = $props();
  let material = $state({});
  let busy = $state('');
  let lessons = $derived(wiki.lessons || []);
  let effect = $derived(wiki.memory_effect);

  async function source(id) {
    busy = id;
    try {
      const result = await api.notebook({ operation: 'query', sources: [id] });
      for (const value of result.source_material || []) material[value.id] = value;
    } catch (e) { toast(t('error')(e.message)); }
    busy = '';
  }
</script>

<section class="adv notebook">
  <h2>{t('notebookTitle')} {#if wiki.notebook_project}<span class="pill project">{wiki.notebook_project}</span>{/if}</h2>
  <p class="k">{t('notebookHow')}</p>
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
        {#each effect.recent || [] as item (item.source_id + item.quote.key)}
          <details class="text">
            <summary>{t('memoryEffectCount')(item.assessment.result, 1)} · {item.request}</summary>
            <p>{item.quote.lesson || item.quote.reply}</p>
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
  {#each lessons as page (page.id)}
    <div class="item lesson">
      <div class="meta"><span class="pill">{t('notebookHypothesis')}</span></div>
      <div class="text"><b>{page.title}</b><p>{page.lesson}</p><div class="k">{t('notebookWhen')}: {page.applies_to}</div></div>
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
