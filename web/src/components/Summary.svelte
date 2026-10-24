<script>
  // 画面の先頭(0.8.0): まず「あなたがやること」、次に「AI の状態」。`shikii status` と同じ数。クリックで詳細。
  import { t, loc, fmtDateTime } from '../lib/i18n.svelte.js';
  import { setTab } from '../lib/state.svelte.js';
  let { summary, specs = [], model = null } = $props();
  let m = $derived(summary.memory || {});
  let g = $derived(summary.guards || {});
  let p = $derived(summary.person || {});
  const when = s => (s ? fmtDateTime(s) : '');
  // 人が答える問いだけを数える。AI だけの問い(wiki)は AI の状態に出す
  let yours = $derived(specs.filter(s => s.person_answers !== false));
  let machine = $derived(specs.filter(s => s.person_answers === false));
  let waiting = $derived(yours.filter(s => s.waiting_for_person > 0));
  let waits = $derived(waiting.reduce((n, s) => n + s.waiting_for_person, 0));
  const live = s => (s.keys || []).filter(k => k.state !== 'stale');
  const auto = s => live(s).some(k => k.state === 'calibrated');
  const learned = s => live(s).reduce((n, k) => Math.max(n, k.n), 0);
  let autoCount = $derived(yours.filter(auto).length);
  let resultsWait = $derived(specs.reduce((n, s) => n + (s.awaiting_results || 0), 0));
  const head = 'mt-2.5 mb-1 text-[12px] font-bold tracking-[.04em] text-muted';
</script>

<details class="summary-card mb-4 rounded-xl border border-line bg-surface text-[13px]" id="summary">
  <summary class="flex cursor-pointer list-none items-center gap-2.5 px-3.5 py-2.5 [&::-webkit-details-marker]:hidden"><span class="text-accent" aria-hidden="true">◐</span><span class="summary-line flex-1"><strong class="summary-you">{waits ? t('summaryYou')(waits) : t('summaryYouNone')}</strong> <span class="summary-ai">{t('summaryAI')(model?.name || '', autoCount, yours.length, m, g)}</span></span><span class="text-[11px] text-muted">{t('summaryOpen')}</span></summary>
  <div class="grid gap-2.5 border-t border-line px-3.5 pt-1 pb-3 [&_p]:m-0">
    <section>
      <h3 class={head}>{t('summaryYouHead')}</h3>
      {#if waiting.length}
        <ul class="summary-you-list">
          {#each waiting as s (s.id)}
            <li><button type="button" class="weak" onclick={() => setTab(s.id)}>{loc(s.titles, s.id)} <span class="badge inline-block rounded-md bg-amberbg px-[7px] py-px text-[10px] text-amber tabular-nums max-sm:px-[5px] max-sm:text-[9px]">{s.waiting_for_person}</span></button> <span class="text-muted">{t('summaryGoTo')}</span></li>
          {/each}
        </ul>
      {:else}<p>{t('summaryYouNoneLong')}</p>{/if}
      <p class="text-muted">{t('summaryPersonRow')(p)}</p>
    </section>
    <section>
      <h3 class={head}>{t('summaryAIHead')}</h3>
      <p>{t('summaryModel')(model?.name || '', model?.endpoint || '')}</p>
      <ul class="summary-specs">
        {#each yours as s (s.id)}
          <li><strong>{loc(s.titles, s.id)}</strong>: {auto(s) ? t('summarySpecAuto') : t('summarySpecLearning')(learned(s), s.min_n)}{#if s.awaiting_results} · {t('summarySpecResults')(s.awaiting_results)}{/if}</li>
        {/each}
        {#each machine as s (s.id)}
          <li><strong>{loc(s.titles, s.id)}</strong>: {t('summarySpecMachine')(s.awaiting_results || 0)}</li>
        {/each}
      </ul>
      {#if m.delivered}
        <p>{t('summaryMemoryRow')(m)}</p>
        {#if m.corrections?.delivered}<p>{t('summaryCorrections')(m.corrections)}</p>{/if}
      {:else}<p class="text-muted">{t('summaryMemoryNone')}</p>{/if}
      {#if g.rituals}
        <p class="summary-rituals" data-rituals>{t('summaryRituals')(g.rituals)}</p>
      {/if}
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
  </div>
</details>
