<script>
  import { t } from '../lib/i18n.svelte.js';
  import { ui, setView } from '../lib/state.svelte.js';
  let { counts, labels } = $props();
  const kinds = ['all', 'ask', 'audit', 'other'];
</script>

<div class="queue-toolbar">
  <div class="segmented answer-tabs">
    <button type="button" class:active={!ui.done} aria-pressed={!ui.done} onclick={() => { ui.done = false; }}>{t('viewTodo')} <span>{counts.all}</span></button>
    <button type="button" class:active={ui.done} aria-pressed={ui.done} onclick={() => { ui.done = true; }}>{t('viewDone')} <span>{labels || 0}</span></button>
  </div>
  <div class="segmented view-switch" aria-label={t('viewLabel')}>
    <button type="button" class:active={ui.view === 'one'} aria-pressed={ui.view === 'one'} onclick={() => setView('one')}>{t('viewOne')}</button>
    <button type="button" class:active={ui.view === 'list'} aria-pressed={ui.view === 'list'} onclick={() => setView('list')}>{t('viewList')}</button>
  </div>
</div>
{#if !ui.done}
  <div class="chips queue-filters" aria-label={t('filterLabel')}>
    {#each kinds as k}
      <button type="button" class:active={ui.filter === k} aria-pressed={ui.filter === k} onclick={() => { ui.filter = k; }}>{t('chip_' + k)} <span>{counts[k]}</span></button>
    {/each}
  </div>
{/if}
