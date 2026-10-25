<script>
  // AI の様子の下の欄(0.9.1。0.8.0 では画面の先頭の帯だった): 判定モデル、人を待つ判定の割合、届けた教訓が効いたか、
  // 作業の前と後の手順、止める検査。`shikii status` と同じ数
  import { t, fmtDateTime } from '../lib/i18n.svelte.js';
  let { summary, model = null } = $props();
  let m = $derived(summary.memory || {});
  let g = $derived(summary.guards || {});
  let p = $derived(summary.person || {});
  const when = s => (s ? fmtDateTime(s) : '');
  const head = 'mt-0 mb-2 text-[12px] font-bold tracking-[.04em] text-muted';
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
          <li><code>{it.id}</code> <span class="text-[11px] text-muted">{it.kind}</span> <span class="rounded-[5px] border px-1.5 py-px text-[10px] {it.enabled ? 'border-accent text-accent' : 'border-line'}">{t(it.builtin ? 'summaryGuardBuiltin' : it.enabled ? 'summaryGuardOn' : 'summaryGuardOff')}</span> {it.title}
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
