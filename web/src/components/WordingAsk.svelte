<script>
  // The wording box on the Answer screen (0.9.2): only the one question a person answers about a new way of asking.
  // When the AI checked the meaning and switched: "keep it / undo". When the AI could not decide: "same meaning / different".
  // It shows the translations and the AI's view of the difference; the English original is in a fold. The numbers, candidate list,
  // the form for writing a wording and "measure" stay on the AI page (Wording.svelte). Only shown when wordingNeedsPerson (state.svelte.js) says so
  import { t, loc } from '../lib/i18n.svelte.js';
  import { load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp } = $props();
  let latest = $derived(sp.tune.latest);
  let check = $derived((latest.meaning_checks || []).filter(c => c.by === 'agent').pop() || {});
  let adopted = $derived(!!(latest.auto_adopted && latest.adopted));
  let busy = $state(false);

  async function decide(keep) {
    busy = true;
    try { await api.tuneReview({ spec: sp.id, run: latest.run, keep }); toast(keep ? t('keptToast') : t('undoneToast'), 6000); load(); }
    catch (e) { toast(t('error')(e.message)); }
    busy = false;
  }
  async function judge(same) {
    busy = true;
    try {
      const note = same ? '' : (prompt(t('notePrompt')) || '');
      const res = await api.tuneAdopt({ spec: sp.id, run: latest.run, meaning_same: same, note });
      toast(same ? t('adoptedToast')(res.path.split('/').pop()) : t('rejected'), 6000);
      load();
    } catch (e) { toast(t('error')(e.message)); }
    busy = false;
  }
  const side = 'rounded-[10px] border border-line bg-surface2 px-3.5 py-3';
</script>

<div class="rounded-[15px] border border-line bg-surface p-[22px] shadow-card max-sm:p-4" data-answer-wording={sp.id}>
  <h3 class="mt-0 mb-1 text-[17px]">{t(adopted ? 'answerWordingAdopted' : 'answerWording')}</h3>
  <p class="mt-0 mb-3 text-[13px] text-muted">{loc(sp.titles, sp.id)}</p>
  <div class="mb-3 grid grid-cols-2 gap-3 max-md:grid-cols-1">
    <div class={side}><div class="k mb-1">{t(adopted ? 'wordingBefore' : 'current')}</div><div class="text-[15px]" data-wording-ja>{check.current_translation || latest.current_wording?.question || ''}</div></div>
    <div class={side}><div class="k mb-1">{t(adopted ? 'wordingAfter' : 'wordingNew')}</div><div class="text-[15px]" data-wording-ja>{check.candidate_translation || latest.winner_wording?.question || ''}</div></div>
  </div>
  <p class="mt-0 mb-3 text-[14px]" data-wording-agent>{adopted ? t('wordingAgentAdopted') : t('wordingAgentUnsure')(check.difference || '')}</p>
  <div class="btns flex flex-wrap items-center gap-2.5">
    {#if adopted}
      <button type="button" class="answer-choice min-h-[48px] flex-[1_1_200px]" data-wording-keep disabled={busy} onclick={() => decide(true)}>{t('keepWording')}</button>
      <button type="button" class="answer-choice min-h-[48px] flex-[1_1_200px]" data-wording-undo disabled={busy} onclick={() => decide(false)}>{t('undoWording')}</button>
    {:else}
      <button type="button" class="answer-choice min-h-[48px] flex-[1_1_200px]" data-wording-same disabled={busy} onclick={() => judge(true)}>{t('sameMeaning')}</button>
      <button type="button" class="answer-choice min-h-[48px] flex-[1_1_200px]" data-wording-different disabled={busy} onclick={() => judge(false)}>{t('changedMeaning')}</button>
    {/if}
  </div>
  <details class="mt-3 text-[13px] text-muted"><summary class="cursor-pointer">{t('wordingOriginal')}</summary>
    <p class="mb-1">{t('current')}: {latest.current_wording?.question || ''}</p>
    <p class="mt-1">{t('wordingNew')}: {latest.winner_wording?.question || ''}</p>
  </details>
</div>
