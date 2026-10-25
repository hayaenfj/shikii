<script>
  // AI の様子(0.9.1): 人が普段見なくてよい物をここにまとめる。問いごとの学習の進み(1 行ずつ。押すとその問いの欄)、
  // 届けた教訓が効いたか、止める検査、作業の前と後の手順。`shikii status` と同じ数
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, data, setTab, selectedTab } from '../lib/state.svelte.js';
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
  // 正解は足りたのに線を引けない理由(0.8.3)。引けていれば出さない
  const stuckLine = s => auto(s) ? '' : live(s).some(k => k.state === 'not_separable') ? t('not_separable')(learned(s), s.min_n) : live(s).some(k => k.state === 'one_answer') ? t('one_answer')(learned(s), s.min_n) : '';
  const status = s => s.person_answers === false ? t('summarySpecMachine')(s.awaiting_results || 0) + (stuckLine(s) ? ' · ' + stuckLine(s) : '') : auto(s) ? t('summarySpecAuto') : stuckLine(s) || t('summarySpecLearning')(learned(s), s.min_n);
  const row = on => 'flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left ' + (on ? 'active border-accent/40 bg-accentbg' : 'border-transparent bg-transparent hover:bg-surface2');
</script>

{#snippet specRow(s)}
  <li><button type="button" data-tab={s.id} aria-current={active === s.id ? 'true' : undefined} class={row(active === s.id)} onclick={() => setTab(s.id)}>
    <span class="min-w-0 flex-1"><strong class="block text-[14px] wrap-anywhere">{loc(s.titles, s.id)}</strong><span class="text-[12px] text-muted">{status(s)}{#if s.awaiting_results && s.person_answers !== false} · {t('summarySpecResults')(s.awaiting_results)}{/if}</span></span>
    {#if s.waiting_for_person}<span class="badge rounded-md bg-amberbg px-[7px] py-px text-[11px] text-amber tabular-nums">{s.waiting_for_person}</span>{/if}
  </button></li>
{/snippet}

<section data-ai>
  <h2 class="mb-1 text-[25px] max-md:text-[23px]">{t('aiTitle')}</h2>
  <p class="k mt-0 mb-5">{t('aiHow')}</p>
  <div class="grid grid-cols-[minmax(240px,320px)_minmax(0,1fr)] items-start gap-5 max-lg:grid-cols-1">
    <div class="rounded-[14px] border border-line bg-surface p-3" data-ai-specs>
      <h3 class="mx-1 mt-1 mb-1.5 text-[12px] font-bold tracking-[.04em] text-muted">{t('aiYours')}</h3>
      <ul class="m-0 grid list-none gap-0.5 p-0">{#each yours as s (s.id)}{@render specRow(s)}{/each}</ul>
      {#if machine.length}
        <h3 class="mx-1 mt-4 mb-1.5 text-[12px] font-bold tracking-[.04em] text-muted">{t('aiMachine')}</h3>
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
