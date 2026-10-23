<script>
  // 画面の先頭(0.7.6): まず「あなたがやること」、次に「AI の状態」。`shikii status` と同じ数。クリックで詳細。
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
</script>

<details class="summary-card" id="summary">
  <summary><span class="summary-mark" aria-hidden="true">◐</span><span class="summary-line"><strong class="summary-you">{waits ? t('summaryYou')(waits) : t('summaryYouNone')}</strong> <span class="summary-ai">{t('summaryAI')(model?.name || '', autoCount, yours.length, m, g)}</span></span><span class="summary-hint">{t('summaryOpen')}</span></summary>
  <div class="summary-body">
    <section>
      <h3>{t('summaryYouHead')}</h3>
      {#if waiting.length}
        <ul class="summary-you-list">
          {#each waiting as s (s.id)}
            <li><button type="button" class="weak" onclick={() => setTab(s.id)}>{loc(s.titles, s.id)} <span class="badge">{s.waiting_for_person}</span></button> <span class="muted">{t('summaryGoTo')}</span></li>
          {/each}
        </ul>
      {:else}<p>{t('summaryYouNoneLong')}</p>{/if}
      <p class="muted">{t('summaryPersonRow')(p)}</p>
    </section>
    <section>
      <h3>{t('summaryAIHead')}</h3>
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
      {:else}<p class="muted">{t('summaryMemoryNone')}</p>{/if}
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
  </div>
</details>
