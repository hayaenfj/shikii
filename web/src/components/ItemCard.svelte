<script>
  // One card: the AI's prediction and confidence, the text, the answer buttons (numbered; the AI's prediction is "same as the AI"), "not …" (partial truth !top),
  // skip, and "ask the AI again" (redo). On an answered card, another button changes the answer (the newest answer is used).
  // Key handling (App.svelte) presses .btns button on the first .item card, so do not change the classes here (top / skip)
  // question (0.9.1): on the Answer screen the question appears at the top of the card and above the buttons, and the AI's internal markers (band, model) are hidden. AI status shows them
  import { t, loc, fmtDateTime } from '../lib/i18n.svelte.js';
  import { skipped, load, dropPendingLoads, loadMark } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp, it, pos = '', readonly = false, question = '' } = $props(); // readonly: a decision a person does not answer (the AI answers, and the truth comes from the outcome). No buttons (0.8.0)
  let busy = $state(false);
  let redoRes = $state(null); // The redo result (before, now, and agreement with the answer). The text is built when drawing, so it changes when the language is switched

  const name = c => loc(c.labels, c.label);
  const choiceOf = truth => it.choices.find(c => c.truth === truth) || { label: truth };
  // For noul, p is the probability of "yes" and top is always yes. The prediction on the page is the side with the higher probability.
  const prediction = x => sp.type === 'noul' && x.top === 'yes' && x.p < 0.5 ? { truth: 'no', p: 1 - x.p } : { truth: x.top, p: x.p };
  let guess = $derived(prediction(it));
  let topName = $derived(guess.truth ? name(choiceOf(guess.truth)) : '');
  let bandWord = $derived(it.band === 'ask' ? t('bandAsk') : it.band === 'act' ? t('bandAct') : t('bandDiscard'));

  const isHook = () => ['prompt-kind', 'needs-plan', 'changes-in-scope', 'done-as-asked'].includes(sp.id);
  function inputLabel(key) {
    if (!isHook()) return key;
    return { request: t('inputRequest'), agreed: t('inputAgreed'), open: t('inputOpen'), summary: t('inputSummary'), files: t('inputFiles'), previous: t('inputPrevious') }[key] || key;
  }
  // The preface at the start of the pre-work summary is part of the input passed to the model. On the page the heading says the same thing, so it is removed (only at the start)
  // the preface Go puts in front of the agent's organizing (cmd/shikii/hook_planning.go): English from 0.9.2, Japanese in records made before
  const planningPrefixes = ["AI's organizing (not permission to act, not the person's truth): ", 'AIによる整理（実行許可や人の正解ではない）: '];
  const shown = (k, v) => {
    const text = it.original_inputs?.[k] ?? v;
    const s = sp.id === 'needs-plan' && (k === 'agreed' || k === 'open') && typeof text === 'string' ? planningPrefixes.reduce((rest, p) => rest.startsWith(p) ? rest.slice(p.length) : rest, text) : text;
    return plainText(s);
  };
  let inputs = $derived.by(() => {
    const entries = Object.entries(it.inputs || {});
    if (!isHook()) return entries;
    const order = { request: 0, agreed: 1, open: 2, summary: 3, files: 4, previous: 5 };
    return entries.sort(([a], [b]) => (order[a] ?? 4) - (order[b] ?? 4));
  });

  // The agent's replies are written in Markdown. The page shows plain text, so only the bold and heading marks are removed (no HTML is rendered)
  const plainText = v => typeof v === 'string' ? v.replace(/\*\*(.+?)\*\*/g, '$1').replace(/^#{1,6} /gm, '') : v;

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

  // Judge the same text again with the current way of asking and line. Show before and now side by side, and the agreement with your answer
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
  const word = x => { const g = prediction(x); return bandOf(x) + ' ' + name(choiceOf(g.truth || '-')) + ' ' + Math.round((g.p || 0) * 100) + '%'; };
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
  const fieldLabel = k => 'field-label flex items-center gap-[7px] px-[15px] pt-[11px] text-[10px] font-semibold tracking-[.03em] max-sm:px-3 max-sm:pt-2.5 ' + (k === 'request' ? 'text-request' : k === 'summary' ? 'text-accent' : 'text-muted');
  const fieldDot = k => 'h-[5px] w-[5px] rounded-full ' + (k === 'request' ? 'bg-request' : 'bg-accent');
  const body = k => 'message-body px-[15px] pt-2 pb-[15px] whitespace-pre-wrap max-sm:px-3 max-sm:pb-3 ' + (k === 'files' ? 'font-mono text-[12px] leading-[1.7]' : k === 'previous' ? 'text-[13px] leading-[1.9] text-fg' : 'text-[15px] leading-[1.9] max-sm:text-[14px]');
  const limit = 'text-limit mx-3 mt-1.5 mb-3 rounded-md bg-amberbg px-2.5 py-1.5 text-[11px] text-amber';
</script>

<article class="item {it.band} mb-5 rounded-[15px] border bg-surface shadow-card max-sm:rounded-xl max-sm:px-3.5 max-sm:py-4 {question ? 'card p-[26px]' : 'p-[22px]'} {it.band === 'ask' ? 'border-[color-mix(in_srgb,var(--color-amber)_45%,var(--color-line))]' : 'border-line'}" aria-busy={busy}>
  {#if question}<h3 class="card-question mt-0 mb-1.5 text-[18px] leading-[1.6] font-[650] max-sm:text-[16px]">{question}</h3>{/if}
  <div class="mb-5 flex items-center gap-2.5 text-[10px] text-muted max-sm:mb-3.5 max-sm:gap-[5px]">{#if !question}<span class="flex items-center gap-[7px] font-semibold tracking-[.04em] max-sm:text-[9px]"><span class="h-[9px] w-[7px] rounded-[2px] border border-muted" aria-hidden="true"></span>{t('conversationRecord')}</span>{/if}<span class="text-accent">{pos}</span>{#if it.project}<span class="project rounded-[5px] border border-line px-1.5 py-px text-[9px]">{it.project}</span>{/if}{#if it.at}<time class="ml-auto text-[10px] max-sm:text-[9px]" datetime={it.at}>{fmtDateTime(it.at)}</time>{/if}</div>
  {#if it.truth}
    <div class="rounded-lg bg-accentbg px-3 py-2.5 text-[12px] text-accent"><span aria-hidden="true">✓</span> {t('yourAnswer')(name(choiceOf(it.truth)), fmtDateTime(it.truth_at))}{#if it.redo_of}{' '}<span class="k">· {t('wasRedone')}</span>{/if}</div>
    {#if !it.legacy_planning}<div class="mt-[5px] mb-[15px] text-[10px] text-muted">{t('changeAnswer')}</div>{/if}
  {/if}
  {#if it.legacy_planning}<div class="audit legacy-planning"><strong>{t('legacyPlanning')}</strong>{#if it.record_question}<p>{it.record_question}</p>{/if}</div>{/if}
  {#if it.review}<div class="audit mb-[15px] rounded-[9px] border border-amber bg-amberbg px-3.5 py-2.5 text-[12px]">{it.review_reason === 'uncertain' ? t('reviewUncertain') : t('reviewRandom')}</div>{/if}
  {#if !question}<div class="mb-3 flex flex-wrap items-center gap-[9px] text-[11px] text-muted">
    <span>{it.band}</span><span>{it.model || ''}</span>
    {#if it.rung === 'context'}<span>{t('withContext')}</span>{/if}
    {#if it.audit}<span class="audit">{t('audit')}</span>{/if}
    {#if it.stale}<span>{t('changed')}</span>{/if}
  </div>{/if}
  <div class="text conversation mb-3.5 wrap-anywhere">
    {#if it.answerable}
      {#each inputs as [k, v]}
        {#if sp.id === 'needs-plan' && k === 'previous'}
          <details class="previous-context mt-1.5 mb-[18px] rounded-[9px] border border-dashed border-line text-[11px] text-muted [&[open]>summary]:border-b [&[open]>summary]:border-line" data-input={k}>
            <summary class="cursor-pointer px-[13px] py-[11px]">{inputLabel(k)}</summary>
            <div class={body(k)}>{plainText(it.original_inputs?.[k] ?? v)}
              {#if it.text_status?.[k]}<div class={limit}>{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
            </div>
          </details>
        {:else}
          <div class={fieldBox(k)} data-input={k}><div class={fieldLabel(k)}><span class={fieldDot(k)} aria-hidden="true"></span>{inputLabel(k)}</div><div class={body(k)}>{shown(k, v)}</div>
            {#if it.text_status?.[k]}<div class={limit}>{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
          </div>
        {/if}
      {/each}
    {:else}<span class="k">{t('noText')(it.inputs_hash)}</span>{/if}
  </div>
  {#if it.review}
    <div class="text followup wrap-anywhere {fieldBox('reply')}"><div class={fieldLabel('reply')}><span class={fieldDot('reply')} aria-hidden="true"></span>{t('reviewReply')}</div><div class={body('reply')}>{plainText(it.review.source_material.reply)}</div></div>
    <details class="review-evidence mb-[15px] text-[12px]"><summary>{t('reviewEvidence')}</summary><p>{it.review.truth}: {it.review.reason}</p></details>
  {:else if it.top}
    <div class="guess prediction flex items-center justify-between gap-[15px] border-t border-line pt-3.5 pb-[18px]"><div><span class="mb-1 block text-[9px] tracking-[.05em] text-muted">{t('predictionHeading')}</span><strong class="block text-[13px] font-[550] max-sm:text-[11px]">{t('aiGuess')(topName, (guess.p * 100).toFixed(0))}</strong><span class="mt-[3px] block text-[10px] text-muted">{bandWord}{#if it.audit} · {t('auditShort')}{/if}</span></div><div class="grid h-[46px] flex-[0_0_46px] place-items-center rounded-full bg-[conic-gradient(var(--color-accent)_var(--confidence),var(--color-line)_0)] p-[3px]" style="--confidence:{Math.round(guess.p * 100)}%" aria-hidden="true"><span class="grid h-10 w-10 grid-flow-col items-baseline justify-center rounded-full bg-surface text-[13px] leading-10 text-accent tabular-nums">{Math.round(guess.p * 100)}<small class="ml-px text-[8px]">%</small></span></div></div>
  {/if}
  {#if readonly && !it.legacy_planning}<p class="k readonly-note">{t('readonlyNote')}</p>{/if}
  {#if !it.legacy_planning && !readonly}<div class="answer-bar border-t border-line pt-[17px] {question ? 'sticky bottom-0 -mx-[26px] rounded-b-[15px] bg-surface px-[26px] pb-4 shadow-[0_-8px_12px_-10px_#0003] max-sm:-mx-3.5 max-sm:px-3.5' : ''}"><div class="answer-heading mb-3 flex items-baseline justify-between gap-3 max-lg:flex-wrap max-lg:gap-1"><span class="text-[13px] font-semibold">{question || t('yourDecision')}</span><small class="text-[10px] text-muted max-sm:text-[9px]">{it.truth ? t('changeAnswerShort') : t('chooseAnswer')}</small></div>
    <div class="btns flex flex-wrap items-center gap-2.5" role="group" aria-label={t('yourDecision')}>
      {#each it.choices as c, i}
        {@const top = !it.review && c.truth === guess.truth}
        {@const chosen = c.truth === it.truth}
        <button type="button" class="answer-choice flex min-h-[57px] flex-[1_1_calc(50%-10px)] items-center gap-3 rounded-[10px] px-[15px] py-3 text-left max-sm:min-h-[53px] max-sm:flex-[1_1_100%] max-sm:px-3 max-sm:py-2.5 {top ? 'top bg-accentbg' : ''} {chosen ? 'active border-accent' : top ? 'border-accent/55' : ''}" aria-pressed={chosen} data-truth={c.truth} title={c.description || ''} disabled={busy} onclick={() => answer(c.truth)}>
          <span data-num class="num grid h-6 flex-[0_0_24px] place-items-center rounded-md border border-line text-[10px] text-muted">{i + 1}</span><span class="flex-1 text-[14px] max-sm:text-[13px]"><span>{name(c)}</span>{#if top}<small class="mt-0.5 block text-[9px] text-accent">{t('sameAsAI')}</small>{/if}</span><span class="text-[13px] {chosen ? 'text-accent' : 'text-muted opacity-50'}" aria-hidden="true">{chosen ? '✓' : '↵'}</span>
        </button>
      {/each}
      {#if it.answerable && it.top && !it.review && sp.type !== 'noul'}
        <button type="button" class="weak partial-answer basis-full text-left" data-truth={'!' + guess.truth} disabled={busy} onclick={() => answer('!' + guess.truth)}>{t('notThat')(topName)}</button>
      {/if}
      {#if !it.truth}<button type="button" class="weak skip ml-auto border-transparent" disabled={busy} onclick={() => skipped.add(it.trace_id)}>{t('skip')} <span aria-hidden="true">→</span></button>{/if}
      {#if it.answerable}<button type="button" class="weak redo" title={t('redoHint')} disabled={busy} onclick={redo}>{t('redo')}</button>{/if}
    </div>
  </div>{/if}
  <div class="redo-out mt-2.5 text-[12px] text-amber empty:hidden" aria-live="polite">{redoRes ? redoLine(redoRes) : ''}</div>
</article>
