<script>
  // 絞り込み(全部 / AI が迷った / 抜き取り / その他 + 件数)、1 件ずつ / 一覧(localStorage shikii.view)、未回答 / 回答済み
  import { t } from '../lib/i18n.svelte.js';
  import { ui, setView } from '../lib/state.svelte.js';

  let { counts, labels } = $props();
  const kinds = ['all', 'ask', 'audit', 'other'];
</script>

<div class="chips">
  {#each kinds as k}
    <button type="button" class:active={ui.filter === k} onclick={() => { ui.filter = k; }}>{t('chip_' + k)} {counts[k]}</button>
  {/each}
  <span class="sep"></span>
  <button type="button" class:active={ui.view === 'one'} onclick={() => setView('one')}>{t('viewOne')}</button>
  <button type="button" class:active={ui.view === 'list'} onclick={() => setView('list')}>{t('viewList')}</button>
  <span class="sep"></span>
  <button type="button" class:active={!ui.done} onclick={() => { ui.done = false; }}>{t('viewTodo')}</button>
  <button type="button" class:active={ui.done} onclick={() => { ui.done = true; }}>{t('viewDone')} {labels || 0}</button>
</div>
