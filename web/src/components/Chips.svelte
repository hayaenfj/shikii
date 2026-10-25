<script>
  // 列の切替(0.8.0): 人が答える問いは「未回答(あなた)」「次の依頼・振り返りで確定する」「回答済み」。
  // AI だけが答える問い(wiki)は「振り返りで確定する」「確定済み」だけ(人が答える列は無い)。絞り込みと表示の切替は 0.9.1 で外した
  import { t } from '../lib/i18n.svelte.js';
  import { ui } from '../lib/state.svelte.js';
  let { counts, labels, machineOnly = false, resultsCount = 0, settledByAI = false } = $props(); // settledByAI: 人が正解を付ける問い(結果は来ない)。列の名は「AI が決めた(答えなくてよい)」
  const you = () => { ui.done = false; ui.results = false; };
  const results = () => { ui.done = false; ui.results = true; };
  const done = () => { ui.done = true; };
  let youActive = $derived(!machineOnly && !ui.done && !ui.results);
  let resultsActive = $derived(!ui.done && (machineOnly || ui.results));
  // 区切りボタン(segmented)の見た目。選ばれている物は active(検査が見る印)
  const seg = on => 'rounded-[7px] border px-3 py-1.5 text-[12px] max-sm:px-[9px] max-sm:text-[10px] [&>span]:ml-[5px] [&>span]:text-[10px] [&>span]:text-muted ' + (on ? 'active border-line bg-surface text-fg shadow-[0_2px_3px_#00000008]' : 'border-transparent bg-transparent text-muted');
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
