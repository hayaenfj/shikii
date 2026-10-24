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
  // 見た目: 選ばれているタブは active(検査と aria-current が見る印)
  const tab = on => 'my-0.5 flex min-h-11 w-full items-center gap-[9px] rounded-[9px] border px-[9px] py-2.5 text-left text-[13px] leading-normal max-sm:gap-1.5 max-sm:px-1.5 max-sm:py-2 max-sm:text-[11px] ' + (on ? 'active border-accent/25 bg-accentbg font-[650] text-accent' : 'border-transparent bg-transparent hover:bg-surface2');
  const icon = on => 'grid h-[23px] flex-[0_0_22px] place-items-center rounded-md border text-[10px] tabular-nums max-sm:hidden ' + (on ? 'border-accent/40 text-accent' : 'border-line text-muted');
  const label = 'flex items-center gap-[7px] px-2.5 pb-[9px] text-[11px] text-muted max-md:pb-1 max-sm:pl-[7px] max-sm:text-[10px]';
</script>

<nav id="tabs" class="flex flex-col gap-[22px] max-md:grid max-md:grid-cols-2 max-md:gap-3" aria-label={t('decisionNav')}>
  <div class="-mb-[7px] px-2.5 text-[11px] font-semibold tracking-[.08em] text-muted max-md:hidden">{t('decisionNav')}</div>
  {#each stages as stage}
    {@const items = itemsFor(stage)}
    {#if items.length}
      <div class="nav-group max-md:min-w-0" data-stage={stage.id}>
        <div class={label}><span class="stage-dot block h-[5px] w-[5px] rounded-full {stage.id === 'after' ? 'bg-accent' : 'bg-amber'}" aria-hidden="true"></span>{t(stage.label)}</div>
        {#each items as sp, index (sp.id)}
          {@const on = active === sp.id}
          <button type="button" data-tab={sp.id} aria-current={on ? 'page' : undefined} class={tab(on)} onclick={() => setTab(sp.id)}>
            <span class={icon(on)} aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>
            <span class="min-w-0 flex-1 wrap-anywhere">{loc(sp.titles, sp.id)}</span>
            {#if sp.waiting_for_person}<span class="badge inline-block rounded-md px-[7px] py-px text-[10px] tabular-nums max-sm:px-[5px] max-sm:text-[9px] {on ? 'bg-surface text-accent' : 'bg-amberbg text-amber'}">{sp.waiting_for_person}</span>{/if}
          </button>
        {/each}
      </div>
    {/if}
  {/each}
  {#if hasWiki}
    <div class="nav-group max-md:min-w-0">
      <div class={label}>{t('navKnowledge')}</div>
      <button type="button" data-tab="wiki" aria-current={active === 'wiki' ? 'page' : undefined} class={tab(active === 'wiki')} onclick={() => setTab('wiki')}><span class={icon(active === 'wiki')} aria-hidden="true">◇</span><span class="min-w-0 flex-1 wrap-anywhere">{t('tabKnowledge')}</span></button>
    </div>
  {/if}
  <div class="mt-1 flex items-start gap-[9px] border-t border-line px-2.5 pt-[17px] pb-[5px] text-[11px] text-muted max-md:hidden"><span class="text-[19px] leading-[1.3] text-accent" aria-hidden="true">↗</span><p class="m-0">{t('navNote')}</p></div>
</nav>
