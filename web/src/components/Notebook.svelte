<script>
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { wiki } = $props();
  let material = $state({});
  let busy = $state('');
  let lessons = $derived(wiki.lessons || []);

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
