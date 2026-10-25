<script>
  // The section at the bottom of AI status (0.9.1; in 0.8.0 it was the band at the top of the page): the decision model, the share of decisions waiting for a person, whether delivered lessons worked,
  // the routine before and after work, and guards. The same numbers as `shikii status`
  import { t, fmtDateTime } from '../lib/i18n.svelte.js';
  let { summary, model = null } = $props();
  let m = $derived(summary.memory || {});
  let g = $derived(summary.guards || {});
  let p = $derived(summary.person || {});
  const when = s => (s ? fmtDateTime(s) : '');
  // Section headings (0.9.1): a light gray band with bold text in the dark body color and a green vertical bar. Small gray text blended into the items below
  const head = 'section-head mt-0 mb-3 flex items-center gap-2 rounded-lg bg-surface2 px-2.5 py-2 text-[13px] font-bold tracking-[.02em] text-fg before:block before:h-3.5 before:w-1 before:rounded-sm before:bg-accent';
  const panel = 'rounded-[14px] border border-line bg-surface p-4 max-sm:p-3 [&_p]:m-0 [&_p]:mb-1.5';
</script>

<div class="mt-5 grid grid-cols-2 gap-4 text-[13px] max-lg:grid-cols-1" id="summary">
  <section class={panel}>
    <h3 class={head}>{t('summaryMemoryHead')}</h3>
    {#if m.delivered}
      <p data-memory-row>{t('summaryMemoryRow')(m)}</p>
      {#if m.corrections?.delivered}<p>{t('summaryCorrections')(m.corrections)}</p>{/if}
    {:else}<p class="text-muted">{t('summaryMemoryNone')}</p>{/if}
    {#if g.rituals}<p class="summary-rituals" data-rituals>{t('summaryRituals')(g.rituals)}</p>{/if}
  </section>
  <section class={panel}>
    <h3 class={head}>{t('summaryGuardsHead')}</h3>
    {#if g.items?.length}
      <ul class="summary-guards m-0 grid list-none gap-1.5 p-0">
        {#each g.items as it (it.id)}
          <li><span class="font-semibold">{t('guardTitle')(it.id, it.title)}</span> <span class="rounded-[5px] border px-1.5 py-px text-[12px] {it.enabled ? 'border-accent text-accent' : 'border-line'}">{t(it.builtin ? 'summaryGuardBuiltin' : it.enabled ? 'summaryGuardOn' : 'summaryGuardOff')}</span> <code class="text-muted">{it.id}</code>
            {#if it.last}<div class="text-muted">{it.last.pass ? t('summaryGuardLastPass')(when(it.last.at)) : t('summaryGuardLastStop')(when(it.last.at), it.last.reason)}</div>{/if}
          </li>
        {/each}
      </ul>
    {:else}<p class="text-muted">{t('summaryGuardsNone')}</p>{/if}
  </section>
  <section class="{panel} col-span-2 max-lg:col-span-1">
    <h3 class={head}>{t('summaryModelHead')}</h3>
    <p>{t('summaryModel')(model?.name || '', model?.endpoint || '')}</p>
    <p class="text-muted">{t('summaryPersonRow')(p)}</p>
  </section>
</div>
