<script>
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, setTab } from '../lib/state.svelte.js';
  let { specs, hasWiki, active } = $props();
  const stages = [
    { id: 'before', label: 'navBefore', ids: ['prompt-kind', 'needs-plan'] },
    { id: 'after', label: 'navAfter', ids: ['changes-in-scope', 'done-as-asked'] },
    { id: 'other', label: 'navOther', ids: [] },
  ];
  const builtins = stages.flatMap(s => s.ids);
  const itemsFor = stage => stage.id === 'other' ? specs.filter(s => !builtins.includes(s.id)) : stage.ids.map(id => specs.find(s => s.id === id)).filter(Boolean);
</script>

<nav id="tabs" aria-label={t('decisionNav')}>
  <div class="nav-heading">{t('decisionNav')}</div>
  {#each stages as stage}
    {@const items = itemsFor(stage)}
    {#if items.length}
      <div class="nav-group" data-stage={stage.id}>
        <div class="nav-group-label"><span class="stage-dot" aria-hidden="true"></span>{t(stage.label)}</div>
        {#each items as sp, index (sp.id)}
          <button type="button" data-tab={sp.id} aria-current={active === sp.id ? 'page' : undefined} class:active={active === sp.id} onclick={() => setTab(sp.id)}>
            <span class="nav-icon" aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>
            <span class="nav-title">{loc(sp.titles, sp.id)}</span>
            {#if sp.unlabeled}<span class="badge">{sp.unlabeled}</span>{/if}
          </button>
        {/each}
      </div>
    {/if}
  {/each}
  {#if ui.advanced && hasWiki}
    <div class="nav-group">
      <div class="nav-group-label">{t('navKnowledge')}</div>
      <button type="button" data-tab="wiki" aria-current={active === 'wiki' ? 'page' : undefined} class:active={active === 'wiki'} onclick={() => setTab('wiki')}><span class="nav-icon" aria-hidden="true">◇</span><span class="nav-title">{t('tabKnowledge')}</span></button>
    </div>
  {/if}
  <div class="nav-note"><span class="note-mark" aria-hidden="true">↗</span><p>{t('navNote')}</p></div>
</nav>
