<script>
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, setTab } from '../lib/state.svelte.js';
  let { specs, hasWiki, active } = $props();
  const stages = [
    { id: 'before', label: 'navBefore', ids: ['prompt-kind', 'needs-plan'] },
    { id: 'after', label: 'navAfter', ids: ['changes-in-scope', 'done-as-asked'] },
    { id: 'other', label: 'navOther', ids: [] },
    { id: 'machine', label: 'navMachine', ids: [] }, // AI だけが答える問い(wiki の矛盾・差し込み先)。人の仕事ではない(0.8.0)
  ];
  const builtins = stages.flatMap(s => s.ids);
  const itemsFor = stage => stage.id === 'machine' ? specs.filter(s => s.person_answers === false) : stage.id === 'other' ? specs.filter(s => !builtins.includes(s.id) && s.person_answers !== false) : stage.ids.map(id => specs.find(s => s.id === id)).filter(Boolean);
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
            {#if sp.waiting_for_person}<span class="badge">{sp.waiting_for_person}</span>{/if}
          </button>
        {/each}
      </div>
    {/if}
  {/each}
  {#if hasWiki}
    <div class="nav-group">
      <div class="nav-group-label">{t('navKnowledge')}</div>
      <button type="button" data-tab="wiki" aria-current={active === 'wiki' ? 'page' : undefined} class:active={active === 'wiki'} onclick={() => setTab('wiki')}><span class="nav-icon" aria-hidden="true">◇</span><span class="nav-title">{t('tabKnowledge')}</span></button>
    </div>
  {/if}
  <div class="nav-note"><span class="note-mark" aria-hidden="true">↗</span><p>{t('navNote')}</p></div>
</nav>
