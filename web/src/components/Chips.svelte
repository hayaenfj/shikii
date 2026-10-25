<script>
  // Switching lists (0.8.0): questions a person answers have "Unanswered (you)", "Settled by the next request or reflection" and "Answered".
  // Questions only the AI answers (wiki) have only "Settled by reflection" and "Settled" (no list for a person to answer). Filtering and view switching were removed in 0.9.1
  import { t } from '../lib/i18n.svelte.js';
  import { ui } from '../lib/state.svelte.js';
  let { counts, labels, machineOnly = false, resultsCount = 0, settledByAI = false } = $props(); // settledByAI: a question a person puts truths on (no outcome comes). The list is named "AI decided (no need to answer)"
  const you = () => { ui.done = false; ui.results = false; };
  const results = () => { ui.done = false; ui.results = true; };
  const done = () => { ui.done = true; };
  let youActive = $derived(!machineOnly && !ui.done && !ui.results);
  let resultsActive = $derived(!ui.done && (machineOnly || ui.results));
  // The look of the segmented buttons. The selected one is active (a marker the checks look at)
  const seg = on => 'rounded-[7px] border px-3 py-1.5 text-[12px] max-sm:px-[9px] [&>span]:ml-[5px] [&>span]:text-[12px] [&>span]:text-muted ' + (on ? 'active border-line bg-surface text-fg shadow-[0_2px_3px_#00000008]' : 'border-transparent bg-transparent text-muted');
</script>

<div class="flex items-center justify-between gap-4 border-t border-line pt-3 pb-2.5 max-sm:flex-wrap max-sm:gap-2">
  <div class="answer-tabs flex gap-[3px] rounded-[10px] border border-line bg-surface2 p-[3px]">
    {#if !machineOnly}
      <button type="button" data-lane="you" class={seg(youActive)} aria-pressed={youActive} onclick={you}>{t('viewTodo')} <span>{counts.all}</span></button>
    {/if}
    <button type="button" data-lane="results" class={seg(resultsActive)} aria-pressed={resultsActive} onclick={results}>{t(machineOnly ? 'viewResultsMachine' : settledByAI ? 'viewSettledByAI' : 'viewResults')} <span>{resultsCount}</span></button>
    <button type="button" data-lane="done" class={seg(ui.done)} aria-pressed={ui.done} onclick={done}>{t(machineOnly ? 'viewDoneMachine' : 'viewDone')} <span>{labels || 0}</span></button>
  </div>
</div>
