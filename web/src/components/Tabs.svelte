<script>
  // タブ: 判定ごとに 1 つ(待ち件数の印付き)。知識のタブは「くわしく」の時だけ。選んだタブは localStorage shikii.tab
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, setTab } from '../lib/state.svelte.js';

  let { specs, hasWiki, active } = $props();
</script>

<nav id="tabs">
  {#each specs as sp (sp.id)}
    <button type="button" data-tab={sp.id} class:active={active === sp.id} onclick={() => setTab(sp.id)}>
      {loc(sp.titles, sp.id)}
      {#if sp.unlabeled}<span class="badge">{sp.unlabeled}</span>{/if}
    </button>
  {/each}
  {#if ui.advanced && hasWiki}
    <button type="button" data-tab="wiki" class="adv" class:active={active === 'wiki'} onclick={() => setTab('wiki')}>{t('tabKnowledge')}</button>
  {/if}
</nav>
