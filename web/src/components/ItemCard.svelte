<script>
  // 1 件のカード: AI の予想と自信、本文、答えのボタン(番号付き。AI の予想は「AI と同じ」)、「〜ではない」(部分正解 !top)、
  // スキップ、「AI にもう一度聞く」(redo)。回答済みのカードでは別のボタンで答えを直せる(いちばん新しい答えが使われる)。
  // キー操作(App.svelte)は .item の先頭のカードの .btns button を押すので、ここの class(top / skip)は変えない
  import { t, loc, fmtDateTime } from '../lib/i18n.svelte.js';
  import { ui, skipped, load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp, it, pos = '', readonly = false } = $props(); // readonly: 人が答えない判定(AI が答え、正解は結果から)。ボタンを出さない(0.8.0)
  let busy = $state(false);
  let redoRes = $state(null); // やり直しの結果(前・今・答えとの一致)。文は描く時に組むので、言語を切り替えても変わる

  const name = c => loc(c.labels, c.label);
  const choiceOf = truth => it.choices.find(c => c.truth === truth) || { label: truth };
  // noulのpは「はい」の確率で、topは常にyes。画面の予想は確率の高い側にする。
  const prediction = x => sp.type === 'noul' && x.top === 'yes' && x.p < 0.5 ? { truth: 'no', p: 1 - x.p } : { truth: x.top, p: x.p };
  let guess = $derived(prediction(it));
  let topName = $derived(guess.truth ? name(choiceOf(guess.truth)) : '');
  let bandWord = $derived(it.band === 'ask' ? t('bandAsk') : it.band === 'act' ? t('bandAct') : t('bandDiscard'));

  const isHook = () => ['prompt-kind', 'needs-plan', 'changes-in-scope', 'done-as-asked'].includes(sp.id);
  function inputLabel(key) {
    if (!isHook()) return key;
    return { request: t('inputRequest'), agreed: t('inputAgreed'), open: t('inputOpen'), summary: t('inputSummary'), files: t('inputFiles'), previous: t('inputPrevious') }[key] || key;
  }
  // 着手前の整理の先頭に付く前置きはモデルへ渡す入力の一部。画面では見出しが同じことを言うので外す(先頭だけ)
  const planningPrefix = 'AIによる整理（実行許可や人の正解ではない）: ';
  const shown = (k, v) => {
    const text = it.original_inputs?.[k] ?? v;
    return sp.id === 'needs-plan' && (k === 'agreed' || k === 'open') && typeof text === 'string' && text.startsWith(planningPrefix) ? text.slice(planningPrefix.length) : text;
  };
  let inputs = $derived.by(() => {
    const entries = Object.entries(it.inputs || {});
    if (!isHook()) return entries;
    const order = { request: 0, agreed: 1, open: 2, summary: 3, files: 4, previous: 5 };
    return entries.sort(([a], [b]) => (order[a] ?? 4) - (order[b] ?? 4));
  });

  async function answer(truth) {
    busy = true;
    try {
      const res = await api.label({ trace_id: it.trace_id, spec: sp.id, truth, source: it.audit ? 'audit' : 'ui' });
      if (!it.truth) { sp.queue.items = sp.queue.items.filter(x => x.trace_id !== it.trace_id); sp.unlabeled = Math.max(0, (sp.unlabeled || 1) - 1); sp.labels++; }
      if (res.calibrated) { toast(t('calibratedToast') + '\n' + (res.calibration || '').split('\n').slice(-2).join('\n'), 8000); }
      else { toast(t('done')(res.truth)); }
      if (it.truth || it.review || res.calibrated) await load();
    } catch (e) { toast(t('error')(e.message)); }
    finally { busy = false; }
  }

  // 今の聞き方と線で同じ本文をもう一度判定。前と今を並べ、あなたの答えとの一致を出す
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
  // 見た目(Tailwind): 入力の欄は種類で色が変わる(request は琥珀、summary は緑)
  const fieldBox = k => 'input-field mb-3.5 overflow-hidden rounded-[11px] border border-line ' + (k === 'request' ? 'border-l-[3px] border-l-request bg-requestbg' : k === 'summary' ? 'border-l-[3px] border-l-accent bg-surface2' : 'bg-surface2');
  const fieldLabel = k => 'field-label flex items-center gap-[7px] px-[15px] pt-[11px] text-[10px] font-semibold tracking-[.03em] max-sm:px-3 max-sm:pt-2.5 ' + (k === 'request' ? 'text-request' : k === 'summary' ? 'text-accent' : 'text-muted');
  const fieldDot = k => 'h-[5px] w-[5px] rounded-full ' + (k === 'request' ? 'bg-request' : 'bg-accent');
  const body = k => 'message-body px-[15px] pt-2 pb-[15px] whitespace-pre-wrap max-sm:px-3 max-sm:pb-3 ' + (k === 'files' ? 'font-mono text-[12px] leading-[1.7]' : 'text-[15px] leading-[1.9] max-sm:text-[14px]');
  const limit = 'text-limit mx-3 mt-1.5 mb-3 rounded-md bg-amberbg px-2.5 py-1.5 text-[11px] text-amber';
</script>

<article class="item {it.band} mb-5 rounded-[15px] border bg-surface shadow-card max-sm:rounded-xl max-sm:px-3.5 max-sm:py-4 {ui.view === 'one' ? 'card p-[26px]' : 'p-[22px]'} {it.band === 'ask' ? 'border-[color-mix(in_srgb,var(--color-amber)_45%,var(--color-line))]' : 'border-line'}" aria-busy={busy}>
  <div class="mb-5 flex items-center gap-2.5 text-[10px] text-muted max-sm:mb-3.5 max-sm:gap-[5px]"><span class="flex items-center gap-[7px] font-semibold tracking-[.04em] max-sm:text-[9px]"><span class="h-[9px] w-[7px] rounded-[2px] border border-muted" aria-hidden="true"></span>{t('conversationRecord')}</span><span class="text-accent">{pos}</span>{#if it.project}<span class="project rounded-[5px] border border-line px-1.5 py-px text-[9px]">{it.project}</span>{/if}{#if it.at}<time class="ml-auto text-[10px] max-sm:text-[9px]" datetime={it.at}>{fmtDateTime(it.at)}</time>{/if}</div>
  {#if it.truth}
    <div class="rounded-lg bg-accentbg px-3 py-2.5 text-[12px] text-accent"><span aria-hidden="true">✓</span> {t('yourAnswer')(name(choiceOf(it.truth)), fmtDateTime(it.truth_at))}{#if it.redo_of}{' '}<span class="k">· {t('wasRedone')}</span>{/if}</div>
    {#if !it.legacy_planning}<div class="mt-[5px] mb-[15px] text-[10px] text-muted">{t('changeAnswer')}</div>{/if}
  {/if}
  {#if it.legacy_planning}<div class="audit legacy-planning"><strong>{t('legacyPlanning')}</strong>{#if it.record_question}<p>{it.record_question}</p>{/if}</div>{/if}
  {#if it.review}<div class="audit mb-[15px] rounded-[9px] border border-amber bg-amberbg px-3.5 py-2.5 text-[12px]">{it.review_reason === 'uncertain' ? t('reviewUncertain') : t('reviewRandom')}</div>{/if}
  <div class="mb-3 flex flex-wrap items-center gap-[9px] text-[11px] text-muted">
    <span>{it.band}</span><span>{it.model || ''}</span>
    {#if it.rung === 'context'}<span>{t('withContext')}</span>{/if}
    {#if it.audit}<span class="audit">{t('audit')}</span>{/if}
    {#if it.stale}<span>{t('changed')}</span>{/if}
  </div>
  <div class="text conversation mb-3.5 wrap-anywhere">
    {#if it.answerable}
      {#each inputs as [k, v]}
        {#if sp.id === 'needs-plan' && k === 'previous'}
          <details class="previous-context mt-1.5 mb-[18px] rounded-[9px] border border-dashed border-line text-[11px] text-muted [&[open]>summary]:border-b [&[open]>summary]:border-line" data-input={k}>
            <summary class="cursor-pointer px-[13px] py-[11px]">{inputLabel(k)}</summary>
            <div class="{body(k)} text-[13px] text-fg">{it.original_inputs?.[k] ?? v}
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
    <div class="text followup {fieldBox('reply')}"><div class={fieldLabel('reply')}><span class={fieldDot('reply')} aria-hidden="true"></span>{t('reviewReply')}</div><div class={body('reply')}>{it.review.source_material.reply}</div></div>
    <details class="review-evidence mb-[15px] text-[12px]"><summary>{t('reviewEvidence')}</summary><p>{it.review.truth}: {it.review.reason}</p></details>
  {:else if it.top}
    <div class="guess prediction flex items-center justify-between gap-[15px] border-t border-line pt-3.5 pb-[18px]"><div><span class="mb-1 block text-[9px] tracking-[.05em] text-muted">{t('predictionHeading')}</span><strong class="block text-[13px] font-[550] max-sm:text-[11px]">{t('aiGuess')(topName, (guess.p * 100).toFixed(0))}</strong><span class="mt-[3px] block text-[10px] text-muted">{bandWord}{#if it.audit} · {t('auditShort')}{/if}</span></div><div class="grid h-[46px] flex-[0_0_46px] place-items-center rounded-full bg-[conic-gradient(var(--color-accent)_var(--confidence),var(--color-line)_0)] p-[3px]" style="--confidence:{Math.round(guess.p * 100)}%" aria-hidden="true"><span class="grid h-10 w-10 grid-flow-col items-baseline justify-center rounded-full bg-surface text-[13px] leading-10 text-accent tabular-nums">{Math.round(guess.p * 100)}<small class="ml-px text-[8px]">%</small></span></div></div>
  {/if}
  {#if readonly && !it.legacy_planning}<p class="k readonly-note">{t('readonlyNote')}</p>{/if}
  {#if !it.legacy_planning && !readonly}<div class="border-t border-line pt-[17px]"><div class="answer-heading mb-3 flex items-baseline justify-between gap-3 max-lg:flex-wrap max-lg:gap-1"><span class="text-[13px] font-semibold">{t('yourDecision')}</span><small class="text-[10px] text-muted max-sm:text-[9px]">{it.truth ? t('changeAnswerShort') : t('chooseAnswer')}</small></div>
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
