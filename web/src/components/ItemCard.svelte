<script>
  // One card: the text, the answer buttons (numbered), "not …" (partial truth !top), skip, and "ask the AI again" (redo). On an answered card, another button changes the answer (the newest answer is used).
  // Key handling (App.svelte) presses .btns button on the first .item card, so do not change the classes here (top = the AI's guess for Enter, skip)
  // On the Answer screen (question passed; 0.9.2): a short title on top and the question once, above the buttons. The AI's guess, "same as the AI" and "ask the AI again"
  // stay in the fold "see the AI's guess" until the person opens it, and the answer buttons look the same before that (the person is not pulled toward the AI).
  // On the AI page (no question) the guess is shown as before, because there a person reviews what the AI did.
  // Phones (below 520px; audit 0.9.2 ui low-1): the answer bar still sticks to the bottom, but holds only the buttons, skip and the folded guess. The question moves to the top of the card
  // (shown there once, the bar's copy is hidden), the number marks are hidden and the two answers share a row, so the stuck bar stays small (about a sixth of the screen)
  import { t, loc, fmtDateTime } from '../lib/i18n.svelte.js';
  import { skipped, load, dropPendingLoads, loadMark } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { plainText } from '../lib/text.js';

  let { sp, it, readonly = false, question = '', title = '' } = $props(); // readonly: a decision a person does not answer (the AI answers, and the truth comes from the outcome). No buttons (0.8.0)
  let busy = $state(false);
  let peek = $state(false); // the fold "see the AI's guess" is open (Answer screen)
  let redoRes = $state(null); // The redo result (before, now, and agreement with the answer). The text is built when drawing, so it changes when the language is switched

  let onAnswer = $derived(!!question);
  let showGuess = $derived(!onAnswer || peek);
  let heading = $derived(onAnswer ? (title && title !== question ? title : question) : '');
  let barQuestion = $derived(onAnswer && heading !== question ? question : '');

  const name = c => loc(c.labels, c.label);
  const choiceOf = truth => it.choices.find(c => c.truth === truth) || { label: truth };
  // Who gave the truth (allowlist): only sources a person answers through are "your answer"; an agent's (mcp) and the outcome's are named; any other source is just "truth"
  const truthWho = { ui: 'yourAnswer', ask: 'yourAnswer', manual: 'yourAnswer', audit: 'yourAnswer', file: 'yourAnswer', replay: 'yourAnswer', mcp: 'agentAnswer', outcome: 'resultAnswer' };
  const truthKey = source => Object.hasOwn(truthWho, source) ? truthWho[source] : 'truthAnswer';
  // For noul, p is the probability of "yes" and top is always yes. The prediction on the page is the side with the higher probability.
  const prediction = x => sp.type === 'noul' && x.top === 'yes' && x.p < 0.5 ? { truth: 'no', p: 1 - x.p } : { truth: x.top, p: x.p };
  let guess = $derived(prediction(it));
  let topName = $derived(guess.truth ? name(choiceOf(guess.truth)) : '');

  // Field names: the hook questions' inputs and the wiki questions' inputs in the person's words; any other key as it is
  function inputLabel(key) {
    return { request: t('inputRequest'), agreed: t('inputAgreed'), open: t('inputOpen'), summary: t('inputSummary'), files: t('inputFiles'), previous: t('inputPrevious'),
      title: t('inputTitle'), lesson: t('inputLesson'), applies_to: t('inputAppliesTo'), new_lesson: t('inputNewLesson'), new_applies: t('inputNewApplies') }[key] || key;
  }
  // The preface at the start of the pre-work summary is part of the input passed to the model. On the page the heading says the same thing, so it is removed (only at the start)
  // the preface Go puts in front of the agent's organizing (cmd/shikii/hook_planning.go): English from 0.9.2, Japanese in records made before
  const planningPrefixes = ["AI's organizing (not permission to act, not the person's truth): ", 'AIによる整理（実行許可や人の正解ではない）: '];
  const shown = (k, v) => {
    const text = it.original_inputs?.[k] ?? v;
    const s = sp.id === 'needs-plan' && (k === 'agreed' || k === 'open') && typeof text === 'string' ? planningPrefixes.reduce((rest, p) => rest.startsWith(p) ? rest.slice(p.length) : rest, text) : text;
    if (typeof s === 'string' && s.trim() === '(none)') return t('noneValue'); // what the hook writes when there was nothing
    return plainText(s);
  };
  const order = { request: 0, agreed: 1, open: 2, summary: 3, files: 4, previous: 5 };
  let inputs = $derived(Object.entries(it.inputs || {}).sort(([a], [b]) => (order[a] ?? 4) - (order[b] ?? 4)));
  // The AI's reading before work (agreed / open) is for reference: folded together right after the request
  const isReading = k => sp.id === 'needs-plan' && (k === 'agreed' || k === 'open');
  let reading = $derived(inputs.filter(([k]) => isReading(k)));

  async function answer(truth) {
    busy = true;
    try {
      const mark = loadMark();
      const res = await api.label({ trace_id: it.trace_id, spec: sp.id, truth, source: it.audit ? 'audit' : 'ui' });
      dropPendingLoads(mark); // reload responses issued before answering would bring the answered card back, so drop them (audit 0.9.1 low-5)
      if (!it.truth) {
        sp.queue.items = sp.queue.items.filter(x => x.trace_id !== it.trace_id); sp.unlabeled = Math.max(0, (sp.unlabeled || 1) - 1); sp.labels++;
        if (it.waits_for_person !== false) sp.waiting_for_person = Math.max(0, (sp.waiting_for_person || 1) - 1); // also decrease "remaining" and the marker at the top (audit 0.9.1 low-to-medium-4)
      }
      if (res.calibrated) { toast(t('calibratedToast') + '\n' + (res.calibration || '').split('\n').slice(-2).join('\n'), 8000); }
      else { toast(t('done')(res.truth)); }
      if (it.truth || it.review || res.calibrated) await load();
    } catch (e) { toast(t('error')(e.message)); }
    finally { busy = false; }
  }

  // Judge the same text again with the current way of asking. Show before and now side by side, and the agreement with your answer
  async function redo() {
    busy = true;
    try {
      const res = await api.redo({ trace_id: it.trace_id, spec: sp.id });
      redoRes = res;
      if (res.recorded) await load();
    } catch (e) { toast(t('error')(e.message)); }
    busy = false;
  }

  const bandOf = x => x.band === 'ask' ? t('bandAsk') : x.band === 'act' ? t('bandAct') : x.band === 'abstain' ? t('bandAbstain') : t('bandDiscard');
  const word = x => { const g = prediction(x); return name(choiceOf(g.truth || '-')) + ' ' + Math.round((g.p || 0) * 100) + '%'; };
  function redoLine(res) {
    let line = t('redoResult')(word(res.before), word(res.after));
    if (res.truth) {
      const top = prediction(res.after).truth;
      const correct = sp.type === 'noul' ? (res.truth.startsWith('!') ? top !== res.truth.slice(1) : top === res.truth) : res.correct_after;
      line += ' · ' + (correct ? t('redoAgree') : t('redoDisagree'));
    }
    return line;
  }
  // Look (Tailwind): input fields change color by kind (request is amber, summary is green)
  const fieldBox = k => 'input-field mb-3.5 overflow-hidden rounded-[11px] border border-line ' + (k === 'request' ? 'border-l-[3px] border-l-request bg-requestbg' : k === 'summary' ? 'border-l-[3px] border-l-accent bg-surface2' : 'bg-surface2');
  const fieldLabel = k => 'field-label flex items-center gap-[7px] px-[15px] pt-[11px] text-[12px] font-semibold tracking-[.03em] max-sm:px-3 max-sm:pt-2.5 ' + (k === 'request' ? 'text-request' : k === 'summary' ? 'text-accent' : 'text-fg');
  const fieldDot = k => 'h-[5px] w-[5px] rounded-full ' + (k === 'request' ? 'bg-request' : 'bg-accent');
  const body = k => 'message-body px-[15px] pt-2 pb-[15px] whitespace-pre-wrap max-sm:px-3 max-sm:pb-3 ' + (k === 'files' ? 'font-mono text-[12px] leading-[1.7]' : k === 'previous' ? 'text-[13px] leading-[1.9] text-fg' : 'text-[15px] leading-[1.9] max-sm:text-[14px]');
  const limit = 'text-limit mx-3 mt-1.5 mb-3 rounded-md bg-amberbg px-2.5 py-1.5 text-[12px] text-amber';
  const fold = 'mt-1.5 mb-[18px] rounded-[9px] border border-dashed border-line text-[12px] text-muted [&[open]>summary]:border-b [&[open]>summary]:border-line';
</script>

{#snippet field(k, v)}
  <div class={fieldBox(k)} data-input={k}><div class={fieldLabel(k)}><span class={fieldDot(k)} aria-hidden="true"></span>{inputLabel(k)}</div><div class={body(k)}>{shown(k, v)}</div>
    {#if it.text_status?.[k]}<div class={limit}>{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
  </div>
{/snippet}

{#snippet guessBlock()}
  <div class="guess prediction flex items-center justify-between gap-[15px] py-3"><div><strong class="block text-[14px] font-[550]">{t('aiGuess')(topName, (guess.p * 100).toFixed(0))}</strong>{#if !onAnswer}<span class="mt-[3px] block text-[12px] text-muted">{bandOf(it)}{#if it.audit} · {t('auditShort')}{/if}</span>{/if}</div><div class="grid h-[46px] flex-[0_0_46px] place-items-center rounded-full bg-[conic-gradient(var(--color-accent)_var(--confidence),var(--color-line)_0)] p-[3px]" style="--confidence:{Math.round(guess.p * 100)}%" aria-hidden="true"><span class="grid h-10 w-10 place-items-center rounded-full bg-surface text-[12px] text-accent tabular-nums">{Math.round(guess.p * 100)}%</span></div></div>
{/snippet}

<article class="item {it.band} mb-5 rounded-[15px] border bg-surface shadow-card max-sm:rounded-xl max-sm:px-3.5 max-sm:py-4 {onAnswer ? 'card p-[26px]' : 'p-[22px]'} {it.band === 'ask' && !onAnswer ? 'border-[color-mix(in_srgb,var(--color-amber)_45%,var(--color-line))]' : 'border-line'}" aria-busy={busy}>
  {#if onAnswer}<h3 class="card-question mt-0 mb-1.5 text-[19px] leading-[1.55] font-[650] max-sm:text-[17px]">{heading}</h3>{#if barQuestion}<p class="card-question-phone mt-0 mb-3 text-[15px] leading-[1.6] font-semibold sm:hidden">{barQuestion}</p>{/if}{/if}
  <div class="mb-4 flex items-center gap-2.5 text-[12px] text-muted max-sm:mb-3 max-sm:gap-[5px]">{#if !onAnswer}<span class="font-semibold">{t('conversationRecord')}</span>{/if}{#if it.project}<span class="project rounded-[5px] border border-line px-1.5 py-px">{it.project}</span>{/if}{#if it.at}<time class="ml-auto" datetime={it.at}>{fmtDateTime(it.at)}</time>{/if}</div>
  {#if it.truth}
    <div class="rounded-lg bg-accentbg px-3 py-2.5 text-[13px] text-accent"><span aria-hidden="true">✓</span> {t(truthKey(it.source))(name(choiceOf(it.truth)), fmtDateTime(it.truth_at))}{#if it.redo_of}{' '}<span class="k">· {t('wasRedone')}</span>{/if}</div>
    {#if !it.legacy_planning}<div class="mt-[5px] mb-[15px] text-[12px] text-muted">{t('changeAnswer')}</div>{/if}
  {/if}
  {#if it.legacy_planning}<div class="audit legacy-planning"><strong>{t('legacyPlanning')}</strong>{#if it.record_question}<p>{it.record_question}</p>{/if}</div>{/if}
  {#if it.review_reason === 'disagree' || it.review_reason === 'outcome' || it.review}<div class="audit mb-[15px] rounded-[9px] border border-amber bg-amberbg px-3.5 py-2.5 text-[13px]">{t(it.review_reason === 'disagree' ? 'reviewDisagree' : it.review_reason === 'outcome' ? 'reviewOutcome' : 'reviewUncertain')}</div>{/if}
  {#if it.review_reason === 'disagree' && it.pair}<!-- 0.11: the two AIs answered differently; both answers side by side, and the person picks one with the buttons below -->
    <div class="ai-pair mb-[15px] grid gap-2.5 sm:grid-cols-2">
      <div class="rounded-[9px] border border-line bg-surface2 px-3.5 py-2.5 text-[13px]" data-pair-side="model"><span class="block text-[12px] text-muted">{t('pairModel')}</span><strong class="text-[15px] font-[600]">{name(choiceOf(it.pair.model))}</strong> <span class="text-[12px] text-muted tabular-nums">{t('pairConfidence')(Math.round((it.pair.model_confidence || 0) * 100))}</span></div>
      <div class="rounded-[9px] border border-line bg-surface2 px-3.5 py-2.5 text-[13px]" data-pair-side="agent"><span class="block text-[12px] text-muted">{t('pairAgent')}</span><strong class="text-[15px] font-[600]">{name(choiceOf(it.pair.agent))}</strong>{#if it.pair.agent_reason}<p class="mt-1 mb-0 whitespace-pre-wrap wrap-anywhere">{plainText(it.pair.agent_reason)}</p>{/if}</div>
    </div>
  {/if}
  {#if !onAnswer && (it.rung === 'context' || it.audit || it.stale || it.model)}<div class="mb-3 flex flex-wrap items-center gap-[9px] text-[12px] text-muted">
    {#if it.rung === 'context'}<span>{t('withContext')}</span>{/if}
    {#if it.audit}<span class="audit">{t('audit')}</span>{/if}
    {#if it.stale}<span>{t('changed')}</span>{/if}
    {#if it.model}<span class="ml-auto">{t('modelName')(it.model)}</span>{/if}
  </div>{/if}
  <div class="text conversation mb-3.5 wrap-anywhere">
    {#if it.answerable}
      {#each inputs as [k, v]}
        {#if isReading(k)}
          {#if k === reading[0][0]}
            <details class="ai-reading {fold}"><summary class="cursor-pointer px-[13px] py-[11px]">{t('aiReading')}</summary><div class="p-2.5 pb-0">{#each reading as [rk, rv]}{@render field(rk, rv)}{/each}</div></details>
          {/if}
        {:else if sp.id === 'needs-plan' && k === 'previous'}
          <details class="previous-context {fold}" data-input={k}>
            <summary class="cursor-pointer px-[13px] py-[11px]">{inputLabel(k)}</summary>
            <div class={body(k)}>{plainText(it.original_inputs?.[k] ?? v)}
              {#if it.text_status?.[k]}<div class={limit}>{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
            </div>
          </details>
        {:else}
          {@render field(k, v)}
        {/if}
      {/each}
    {:else}<span class="k">{t('noText')(it.inputs_hash)}</span>{/if}
  </div>
  {#if it.review}
    <div class="text followup wrap-anywhere {fieldBox('reply')}"><div class={fieldLabel('reply')}><span class={fieldDot('reply')} aria-hidden="true"></span>{t('reviewReply')}</div><div class={body('reply')}>{plainText(it.review.source_material.reply)}</div></div>
    <details class="review-evidence mb-[15px] text-[12px]"><summary>{t('reviewEvidence')}</summary><p>{it.review.truth}: {it.review.reason}</p></details>
  {:else if it.top && !onAnswer}
    <div class="border-t border-line pt-1.5 pb-2">{@render guessBlock()}</div>
  {/if}
  {#if readonly && !it.legacy_planning}<p class="k readonly-note">{t('readonlyNote')}</p>{/if}
  {#if !it.legacy_planning && !readonly}<div class="answer-bar border-t border-line pt-[17px] {onAnswer ? 'sticky bottom-0 -mx-[26px] rounded-b-[15px] bg-surface px-[26px] pb-4 shadow-[0_-8px_12px_-10px_#0003] max-sm:-mx-3.5 max-sm:rounded-b-xl max-sm:px-3.5 max-sm:pt-2.5 max-sm:pb-2.5' : ''}">
    <div class="answer-heading mb-3 flex items-baseline justify-between gap-3 max-lg:flex-wrap max-lg:gap-1 {onAnswer ? 'max-sm:hidden' : ''}"><span class="text-[15px] leading-[1.6] font-semibold">{onAnswer ? (barQuestion || t('chooseAnswer')) : t('yourDecision')}</span>{#if !onAnswer || barQuestion || it.truth}<small class="text-[12px] text-muted">{it.truth ? t('changeAnswerShort') : t('chooseAnswer')}</small>{/if}</div>
    <div class="btns flex flex-wrap items-center gap-2.5" role="group" aria-label={t('yourDecision')}>
      {#each it.choices as c, i}
        {@const top = !it.review && c.truth === guess.truth}
        {@const chosen = c.truth === it.truth}
        <button type="button" class="answer-choice flex min-h-[57px] flex-[1_1_calc(50%-10px)] items-center gap-3 rounded-[10px] px-[15px] py-3 text-left max-sm:px-3 max-sm:py-2.5 {onAnswer ? 'max-sm:min-h-[48px] max-sm:gap-2' : 'max-sm:min-h-[53px] max-sm:flex-[1_1_100%]'} {top ? 'top' : ''} {chosen ? 'active border-accent' : ''}" aria-pressed={chosen} data-truth={c.truth} title={c.description || ''} disabled={busy} onclick={() => answer(c.truth)}>
          <span data-num class="num grid h-6 flex-[0_0_24px] place-items-center rounded-md border border-line text-[12px] text-muted {onAnswer ? 'max-sm:hidden' : ''}">{i + 1}</span><span class="flex-1 text-[15px] max-sm:text-[14px]"><span>{name(c)}</span>{#if top && showGuess}<small class="mt-0.5 block text-[12px] text-accent">{t('sameAsAI')}</small>{/if}</span><span class="text-[13px] {chosen ? 'text-accent' : 'text-muted opacity-50'}" aria-hidden="true">{chosen ? '✓' : ''}</span>
        </button>
      {/each}
      {#if it.answerable && it.top && !it.review && sp.type !== 'noul' && showGuess}
        <button type="button" class="weak partial-answer basis-full text-left" data-truth={'!' + guess.truth} disabled={busy} onclick={() => answer('!' + guess.truth)}>{t('notThat')(topName)}</button>
      {/if}
      {#if !it.truth}<button type="button" class="weak skip ml-auto border-transparent {onAnswer ? 'max-sm:py-1' : ''}" disabled={busy} onclick={() => skipped.add(it.trace_id)}>{t('skip')} <span aria-hidden="true">→</span></button>{/if}
      {#if it.answerable && !onAnswer}<button type="button" class="weak redo" title={t('redoHint')} disabled={busy} onclick={redo}>{t('redo')}</button>{/if}
    </div>
    {#if onAnswer && it.top && !it.review}
      <details class="guess-fold mt-3 text-[13px] text-muted max-sm:mt-1.5" bind:open={peek}>
        <summary class="cursor-pointer">{t('guessFold')}</summary>
        {@render guessBlock()}
        {#if it.answerable}<button type="button" class="weak redo" title={t('redoHint')} disabled={busy} onclick={redo}>{t('redo')}</button>{/if}
      </details>
    {/if}
  </div>{/if}
  <div class="redo-out mt-2.5 text-[12px] text-amber empty:hidden" aria-live="polite">{redoRes ? redoLine(redoRes) : ''}</div>
</article>
