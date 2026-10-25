<script>
  // The ways-of-asking section: the latest tune result, the list of candidates, the winning candidate side by side with the current wording, the person's "same meaning / different", and a button to measure candidates.
  // The machine checks the numbers. The agent checks whether the meaning is the same and adds a translation (0.9.2): if the same, it is adopted automatically, and the person can choose "keep it / revert".
  // When it cannot decide, and when the agent has not checked yet, the person can choose "same meaning / different"
  import { t } from '../lib/i18n.svelte.js';
  import { load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp } = $props();
  let tu = $derived(sp.tune);
  let latest = $derived(tu && tu.latest);
  let check = $derived((latest?.meaning_checks || []).filter(c => c.by === 'agent').pop());
  let personDone = $derived((latest?.meaning_checks || []).some(c => c.by !== 'agent'));
  // The AI adopted it and the person has not answered yet (keep it / revert)
  let review = $derived(!!(latest && latest.auto_adopted && latest.adopted && !latest.reverted && !personDone && latest.winner_wording));
  let showDiff = $derived(!!(latest && latest.winner_wording && !latest.reverted && (review || (latest.adoptable && !latest.adopted && check?.verdict !== 'different'))));

  async function run(propose) {
    try { await api.tuneRun({ spec: sp.id, propose }); toast(t('running')); load(); }
    catch (e) { toast(t('error')(e.message)); }
  }

  // The person writes a candidate in their own words. It is only added (scored by the next tune, adopted by a person). Option descriptions are "id: description" lines
  let candName = $state('');
  let candQuestion = $state('');
  let candOptions = $state('');
  let adding = $state(false);
  function parseOptions(text) {
    const out = {};
    for (const line of text.split('\n')) {
      const i = line.indexOf(':');
      if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    }
    return out;
  }
  async function addVariant() {
    adding = true;
    try {
      const res = await api.tuneVariant({ spec: sp.id, name: candName, question: candQuestion, options: parseOptions(candOptions) });
      toast(t('candAdded')(res.added), 6000);
      candName = ''; candQuestion = ''; candOptions = '';
      load();
    } catch (e) { toast(t('error')(e.message)); }
    adding = false;
  }

  async function decide(keep) {
    try {
      await api.tuneReview({ spec: sp.id, run: latest.run, keep });
      toast(keep ? t('keptToast') : t('undoneToast'), 6000);
      load();
    } catch (e) { toast(t('error')(e.message)); }
  }

  async function judge(same) {
    try {
      const note = same ? '' : (prompt(t('notePrompt')) || '');
      const res = await api.tuneAdopt({ spec: sp.id, run: latest.run, meaning_same: same, note });
      toast(same ? t('adoptedToast')(res.path.split('/').pop()) : t('rejected'), 6000);
      load();
    } catch (e) { toast(t('error')(e.message)); }
  }
</script>

{#snippet options(opts, base)}
  {#if opts}
    <div class="opts [&>div]:my-[3px] [&>div]:text-[12px]">
      {#each Object.entries(opts) as [id, d]}
        <div class={base && base[id] !== d ? 'changed bg-amberbg p-[3px]' : ''}><span class="k">{id}</span> {d}</div>
      {/each}
    </div>
  {/if}
{/snippet}

{#snippet steps(list)}
  {#if list && list.length}
    <div class="opts [&>div]:my-[3px] [&>div]:text-[12px]">
      {#each list as s, i}
        <div class="changed bg-amberbg p-[3px]"><span class="k">{i + 1}.</span> {s.ask}{#if s.yes}{' '}<span class="k">{t('yesTo')}</span>{s.yes}{/if}{#if s.no}{' '}<span class="k">{t('noTo')}</span>{s.no}{/if}</div>
      {/each}
    </div>
  {/if}
{/snippet}

<details class="wording my-6 rounded-[11px] border border-dashed border-line bg-surface px-4 py-[13px]" open={showDiff}>
  <summary class="cursor-pointer text-[12px] text-muted">
    <b>{t('wording')}</b>
    {#if tu && tu.running}<span class="pill ready">{t('running')}</span>
    {:else if latest && latest.adopted}<span class="pill calibrated">{t('adoptedLine')(latest.adopted_path.split('/').pop())}</span>
    {:else if latest && latest.reverted}<span class="pill">{t('revertedLine')}</span>
    {:else if latest && latest.adoptable}<span class="pill ready">{t('winnerLine')} <span class="text-muted" data-variant-name>({latest.winner})</span></span>
    {:else if latest}<span class="pill">{t('keptLine')}</span>
    {:else}<span class="pill">{t('noRun')}</span>{/if}
  </summary>
  <div class="mt-4 [&_.k]:my-1.5">
    <p class="mt-0 mb-3 text-[13px]" data-wording-about>{t('wordingAbout')}</p>
    {#if latest}
      {@const cur0 = latest.candidates.find(c => c.name === 'current')}
      {@const win0 = latest.candidates.find(c => c.name === latest.winner)}
      {#if cur0}<p class="mt-0 mb-3 text-[14px] font-bold" data-wording-match>{win0 && latest.winner !== 'current' ? t('wordingMatch')(Math.round(cur0.top1 * 100), Math.round(win0.top1 * 100), latest.truths) : t('wordingMatchKept')(Math.round(cur0.top1 * 100), latest.truths)}</p>{/if}
      <details class="mb-3" data-wording-details><summary class="cursor-pointer text-[12px] text-muted">{t('wordingNumbers')}</summary>
      <div class="mb-3 flex flex-wrap items-center gap-[9px] text-[12px] text-muted"><span>{latest.model}</span><span>{t('truthsCount')(latest.truths)}</span><span>{latest.at.slice(0, 16).replace('T', ' ')}</span></div>
      <table class="cands w-full border-collapse text-[12px] [&_td]:border-b [&_td]:border-line [&_td]:p-2 [&_td]:text-left [&_th]:border-b [&_th]:border-line [&_th]:p-2 [&_th]:text-left">
        <tbody>
          <tr>{#each t('tableHead') as h}<th>{h}</th>{/each}<th></th></tr>
          {#each latest.candidates as c}
            <tr class={c.name === latest.winner && latest.winner !== 'current' ? 'win text-accent [&>td]:text-accent' : ''}>
              <td>{c.name}</td><td>{c.top1.toFixed(3)}</td><td>{c.brier.toFixed(4)}</td><td>{c.prec_at_act.toFixed(3)}</td><td>{c.rec_at_act.toFixed(3)}</td><td class="k">{c.change}</td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if latest.have_holdout}<div class="k">{t('holdoutLine')(latest.holdout.toFixed(3), latest.candidates[0].top1.toFixed(3))}</div>{/if}
      <div class="k">{t('wordingDecision')(latest.adoptable, latest.winner)}</div>
      </details>
      {#if showDiff}
        {@const w = latest.winner_wording}
        {@const cur = latest.current_wording || {}}
        {#if review}<div class="mb-2 text-[13px] font-bold" data-wording-agent>{t('wordingAgentAdopted')}</div>
        {:else if check?.verdict === 'unsure'}<div class="mb-2 text-[13px] font-bold" data-wording-agent>{t('wordingAgentUnsure')(check.difference || '')}</div>
        {:else if !check}<div class="k" data-wording-agent>{t('wordingWaitAgent')}</div>{/if}
        <div class="my-3 grid grid-cols-2 gap-4 max-md:grid-cols-1">
          <div><div class="k">{review ? t('wordingBefore') : t('current')}</div>{#if check?.current_translation}<div class="text mb-1.5 wrap-anywhere" data-wording-ja>{t('translation')}: {check.current_translation}</div>{/if}<div class="text mb-3.5 wrap-anywhere text-muted">{cur.question || ''}</div>{@render options(cur.options)}</div>
          <div><div class="k">{#if review}{t('wordingAfter')}{:else}{t('wordingNew')} <span class="text-muted" data-variant-name>({latest.winner})</span>{/if}</div>{#if check?.candidate_translation}<div class="text mb-1.5 wrap-anywhere" data-wording-ja>{t('translation')}: {check.candidate_translation}</div>{/if}<div class="text mb-3.5 wrap-anywhere text-muted">{w.question || ''}</div>{@render options(w.options, cur.options)}{@render steps(w.steps)}</div>
        </div>
        {#if review}
          <div class="btns flex flex-wrap items-center gap-2.5"><button type="button" class="top" data-wording-keep onclick={() => decide(true)}>{t('keepWording')}</button><button type="button" class="weak" data-wording-undo onclick={() => decide(false)}>{t('undoWording')}</button></div>
        {:else}
          <div class="k">{t('checkHint')}</div>
          <div class="btns flex flex-wrap items-center gap-2.5"><button type="button" class="top" data-wording-same onclick={() => judge(true)}>{t('sameMeaning')}</button><button type="button" class="weak" onclick={() => judge(false)}>{t('changedMeaning')}</button></div>
        {/if}
      {:else if check?.verdict === 'different'}
        <div class="k" data-wording-agent>{t('wordingAgentDifferent')(check.difference || '')}</div>
      {/if}
    {/if}
    {#if tu && tu.error}<div class="k">{t('error')(tu.error)}</div>{/if}
    {#if tu}
      <div class="k cands-line">{#if tu.variants && tu.variants.length}{t('candidates')(tu.variants.length)} <span class="text-muted" data-variant-name>({tu.variants.map(v => v.name).join(', ')})</span>{:else}{t('noCandidates')}{/if}</div>
      <form class="add-variant mt-4 flex flex-col gap-[9px] [&_input]:w-full [&_input]:rounded-lg [&_input]:border [&_input]:border-line [&_input]:bg-surface2 [&_input]:px-3 [&_input]:py-[9px] [&_textarea]:min-h-[90px] [&_textarea]:w-full [&_textarea]:rounded-lg [&_textarea]:border [&_textarea]:border-line [&_textarea]:bg-surface2 [&_textarea]:px-3 [&_textarea]:py-[9px]" onsubmit={e => { e.preventDefault(); addVariant(); }}>
        <div class="k">{t('addWording')}</div>
        <label class="grid gap-1 text-[12px] text-muted">{t('candNameLabel')}<input name="name" placeholder={t('candName')} bind:value={candName} required></label>
        <label class="grid gap-1 text-[12px] text-muted">{t('candQuestionLabel')}<textarea name="question" rows="2" placeholder={sp.type === 'choice' ? t('candQuestionOrOptions') : t('candQuestion')} bind:value={candQuestion} required={sp.type !== 'choice'}></textarea></label>
        {#if sp.type === 'choice'}<label class="grid gap-1 text-[12px] text-muted">{t('candOptionsLabel')}<textarea name="options" rows="2" placeholder={t('candOptions')} bind:value={candOptions}></textarea></label>{/if}
        <div class="btns flex flex-wrap items-center gap-2.5"><button type="submit" class="weak" disabled={adding}>{t('addWordingBtn')}</button></div>
      </form>
    {/if}
    {#if tu && !tu.running}
      <div class="btns flex flex-wrap items-center gap-2.5">
        <button type="button" onclick={() => run(0)}>{t('runTune')}</button>
        {#if tu.proposer}<button type="button" onclick={() => run(3)}>{t('runPropose')(3)}</button>{/if}
      </div>
    {/if}
  </div>
</details>
