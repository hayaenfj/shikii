<script>
  // 効き目の要約(0.7.4): `shikii status` と同じ数を画面の先頭に 1 行で出し、クリックで詳細(届いた教訓・止める検査・人が答える判定)を開く。
  import { t, fmtDateTime } from '../lib/i18n.svelte.js';
  let { summary } = $props();
  let m = $derived(summary.memory || {});
  let g = $derived(summary.guards || {});
  let p = $derived(summary.person || {});
  const when = s => (s ? fmtDateTime(s) : '');
</script>

<details class="summary-card" id="summary">
  <summary><span class="summary-mark" aria-hidden="true">◐</span><span class="summary-line">{t('summaryLine')(m, g, p)}</span><span class="summary-hint">{t('summaryOpen')}</span></summary>
  <div class="summary-body">
    <section>
      <h3>{t('summaryMemoryHead')}</h3>
      {#if m.delivered}
        <p>{t('summaryMemoryRow')(m)}</p>
        {#if m.corrections?.delivered}<p>{t('summaryCorrections')(m.corrections)}</p>{/if}
      {:else}<p class="muted">{t('summaryMemoryNone')}</p>{/if}
    </section>
    <section>
      <h3>{t('summaryGuardsHead')}</h3>
      {#if g.items?.length}
        <ul class="summary-guards">
          {#each g.items as it (it.id)}
            <li><code>{it.id}</code> <span class="kind">{it.kind}</span> <span class="state" class:on={it.enabled}>{t(it.enabled ? 'summaryGuardOn' : 'summaryGuardOff')}</span> {it.title}
              {#if it.last}<div class="muted">{it.last.pass ? t('summaryGuardLastPass')(when(it.last.at)) : t('summaryGuardLastStop')(when(it.last.at), it.last.reason)}</div>{/if}
            </li>
          {/each}
        </ul>
      {:else}<p class="muted">{t('summaryGuardsNone')}</p>{/if}
    </section>
    <section>
      <h3>{t('summaryPersonHead')}</h3>
      <p>{t('summaryPersonRow')(p)}</p>
    </section>
  </div>
</details>
