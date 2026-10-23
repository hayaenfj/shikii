<script>
  // 列の切替(0.8.0): 人が答える問いは「未回答(あなた)」「次の依頼・振り返りで確定する」「回答済み」。
  // AI だけが答える問い(wiki)は「振り返りで確定する」「確定済み」だけ(人が答える列は無い)。絞り込みは「未回答(あなた)」でだけ
  import { t } from '../lib/i18n.svelte.js';
  import { ui, setView } from '../lib/state.svelte.js';
  let { counts, labels, machineOnly = false, resultsCount = 0, settledByAI = false } = $props(); // settledByAI: 人が正解を付ける問い(結果は来ない)。列の名は「AI が決めた(答えなくてよい)」
  const kinds = ['all', 'ask', 'audit'];
  const you = () => { ui.done = false; ui.results = false; };
  const results = () => { ui.done = false; ui.results = true; };
  const done = () => { ui.done = true; };
  let youActive = $derived(!machineOnly && !ui.done && !ui.results);
  let resultsActive = $derived(!ui.done && (machineOnly || ui.results));
</script>

<div class="queue-toolbar">
  <div class="segmented answer-tabs">
    {#if !machineOnly}
      <button type="button" data-lane="you" class:active={youActive} aria-pressed={youActive} onclick={you}>{t('viewTodo')} <span>{counts.all}</span></button>
    {/if}
    <button type="button" data-lane="results" class:active={resultsActive} aria-pressed={resultsActive} onclick={results}>{t(machineOnly ? 'viewResultsMachine' : settledByAI ? 'viewSettledByAI' : 'viewResults')} <span>{resultsCount}</span></button>
    <button type="button" data-lane="done" class:active={ui.done} aria-pressed={ui.done} onclick={done}>{t(machineOnly ? 'viewDoneMachine' : 'viewDone')} <span>{labels || 0}</span></button>
  </div>
  <div class="segmented view-switch" aria-label={t('viewLabel')}>
    <button type="button" class:active={ui.view === 'one'} aria-pressed={ui.view === 'one'} onclick={() => setView('one')}>{t('viewOne')}</button>
    <button type="button" class:active={ui.view === 'list'} aria-pressed={ui.view === 'list'} onclick={() => setView('list')}>{t('viewList')}</button>
  </div>
</div>
{#if youActive}
  <div class="chips queue-filters" aria-label={t('filterLabel')}>
    {#each kinds as k}
      <button type="button" class:active={ui.filter === k} aria-pressed={ui.filter === k} onclick={() => { ui.filter = k; }}>{t('chip_' + k)} <span>{counts[k]}</span></button>
    {/each}
  </div>
{/if}
