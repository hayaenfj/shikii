<script>
  // AI status (0.9.1): gathers here what a person normally need not look at. Learning progress per question (one line each; pressing it opens that question's section),
  // whether delivered lessons worked, guards, and the routine before and after work. The same numbers as `shikii status`
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, data, setTab, selectedTab, wordingNeedsPerson } from '../lib/state.svelte.js';
  import SpecPanel from './SpecPanel.svelte';
  import Summary from './Summary.svelte';

  let { st } = $props();
  let active = $derived(selectedTab(st));
  let activeSpec = $derived(st.specs.find(s => s.id === active));
  let yours = $derived(st.specs.filter(s => s.person_answers !== false));
  let machine = $derived(st.specs.filter(s => s.person_answers === false));

  const live = s => (s.keys || []).filter(k => k.state !== 'stale');
  const auto = s => live(s).some(k => k.state === 'calibrated');
  const learned = s => live(s).reduce((n, k) => Math.max(n, k.n), 0);
  // Why no line could be drawn although there are enough truths (0.8.3). Not shown if a line was drawn
  const stuckLine = s => auto(s) ? '' : live(s).some(k => k.state === 'stopped') ? t('stopped')(learned(s), s.min_n) : live(s).some(k => k.state === 'not_separable') ? t('not_separable')(learned(s), s.min_n) : live(s).some(k => k.state === 'one_answer') ? t('one_answer')(learned(s), s.min_n) : '';
  // a new wording waiting for the person's check on the Answer screen comes first (not "nothing for you to do"; audit 0.9.2 ui middle-1)
  const status = s => wordingNeedsPerson(s.tune?.latest) ? t('summarySpecWording') : s.person_answers === false ? t('summarySpecMachine')(s.awaiting_results || 0) + (stuckLine(s) ? ' · ' + stuckLine(s) : '') : auto(s) ? t('summarySpecAuto') : stuckLine(s) || t('summarySpecLearning')(learned(s), s.min_n);
  const row = on => 'flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left ' + (on ? 'active border-accent/40 bg-accentbg' : 'border-transparent bg-transparent hover:bg-surface2');
</script>

{#snippet specRow(s)}
  <li><button type="button" data-tab={s.id} aria-current={active === s.id ? 'true' : undefined} class={row(active === s.id)} onclick={() => setTab(s.id)}>
    <span class="min-w-0 flex-1"><strong class="block text-[14px] font-semibold wrap-anywhere">{loc(s.titles, s.id)}</strong><span class="text-[12px] text-muted">{status(s)}{#if s.awaiting_results && s.person_answers !== false} · {t('summarySpecResults')(s.awaiting_results)}{/if}</span></span>
    {#if s.waiting_for_person}<span class="badge rounded-md bg-amberbg px-[7px] py-px text-[12px] text-amber tabular-nums">{s.waiting_for_person}</span>{/if}
  </button></li>
{/snippet}

<section data-ai>
  <h2 class="mb-1 text-[25px] max-md:text-[23px]">{t('aiTitle')}</h2>
  <p class="page-purpose mt-0 mb-5 text-[14px] text-muted">{t('aiHow')}</p>
  <div class="grid grid-cols-[minmax(240px,320px)_minmax(0,1fr)] items-start gap-5 max-lg:grid-cols-1">
    <div class="rounded-[14px] border border-line bg-surface p-3" data-ai-specs>
      <h3 class="section-head mx-1 mt-1 mb-2 flex items-center gap-2 rounded-lg bg-surface2 px-2.5 py-2 text-[13px] font-bold tracking-[.02em] text-fg before:block before:h-3.5 before:w-1 before:rounded-sm before:bg-accent">{t('aiYours')}</h3>
      <ul class="m-0 grid list-none gap-0.5 p-0">{#each yours as s (s.id)}{@render specRow(s)}{/each}</ul>
      {#if machine.length}
        <h3 class="section-head mx-1 mt-5 mb-2 flex items-center gap-2 rounded-lg bg-surface2 px-2.5 py-2 text-[13px] font-bold tracking-[.02em] text-fg before:block before:h-3.5 before:w-1 before:rounded-sm before:bg-accent">{t('aiMachine')}</h3>
        <ul class="m-0 grid list-none gap-0.5 p-0">{#each machine as s (s.id)}{@render specRow(s)}{/each}</ul>
      {/if}
    </div>
    <div class="min-w-0">
      {#if activeSpec && (ui.done ? !!activeSpec.answered : !!activeSpec.queue)}
        {#key activeSpec.id}<SpecPanel sp={activeSpec} />{/key}
      {:else if !data.loadError}<div class="empty-state" role="status">{t('loading')}</div>{/if}
    </div>
  </div>
  {#if st.summary}<Summary summary={st.summary} model={st.model} />{/if}
</section>
