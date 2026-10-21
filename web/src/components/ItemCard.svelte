<script>
  // 1 件のカード: AI の予想と自信、本文、答えのボタン(番号付き。AI の予想は「AI と同じ」)、「〜ではない」(部分正解 !top)、
  // スキップ、「AI にもう一度聞く」(くわしく)。回答済みのカードでは別のボタンで答えを直せる(いちばん新しい答えが使われる)。
  // キー操作(App.svelte)は .item の先頭のカードの .btns button を押すので、ここの class(top / skip)は変えない
  import { t, loc, fmtDateTime } from '../lib/i18n.svelte.js';
  import { ui, skipped, load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp, it, pos = '' } = $props();
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
</script>

<article class="item {it.band}" class:card={ui.view === 'one'} aria-busy={busy}>
  <div class="card-topline"><span class="card-kind"><span class="record-mark" aria-hidden="true"></span>{t('conversationRecord')}</span><span class="card-position">{pos}</span>{#if it.project}<span class="project">{it.project}</span>{/if}{#if it.at}<time datetime={it.at}>{fmtDateTime(it.at)}</time>{/if}</div>
  {#if it.truth}
    <div class="your-answer"><span aria-hidden="true">✓</span> {t('yourAnswer')(name(choiceOf(it.truth)), fmtDateTime(it.truth_at))}{#if it.redo_of}{' '}<span class="k">· {t('wasRedone')}</span>{/if}</div>
    {#if !it.legacy_planning}<div class="k change-answer">{t('changeAnswer')}</div>{/if}
  {/if}
  {#if it.legacy_planning}<div class="audit legacy-planning"><strong>{t('legacyPlanning')}</strong>{#if it.record_question}<p>{it.record_question}</p>{/if}</div>{/if}
  {#if it.review}<div class="audit review-banner">{it.review_reason === 'uncertain' ? t('reviewUncertain') : t('reviewRandom')}</div>{/if}
  <div class="meta adv">
    <span>{it.band}</span><span>{it.model || ''}</span>
    {#if it.rung === 'context'}<span>{t('withContext')}</span>{/if}
    {#if it.audit}<span class="audit">{t('audit')}</span>{/if}
    {#if it.stale}<span>{t('changed')}</span>{/if}
  </div>
  <div class="text conversation">
    {#if it.answerable}
      {#each inputs as [k, v]}
        {#if sp.id === 'needs-plan' && k === 'previous'}
          <details class="previous-context" data-input={k}>
            <summary>{inputLabel(k)}</summary>
            <div class="message-body">{it.original_inputs?.[k] ?? v}
              {#if it.text_status?.[k]}<div class="k text-limit">{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
            </div>
          </details>
        {:else}
          <div class="input-field" data-input={k}><div class="field-label"><span class="field-dot" aria-hidden="true"></span>{inputLabel(k)}</div><div class="message-body">{it.original_inputs?.[k] ?? v}</div>
            {#if it.text_status?.[k]}<div class="k text-limit">{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
          </div>
        {/if}
      {/each}
    {:else}<span class="k">{t('noText')(it.inputs_hash)}</span>{/if}
  </div>
  {#if it.review}
    <div class="text input-field followup"><div class="field-label"><span class="field-dot" aria-hidden="true"></span>{t('reviewReply')}</div><div class="message-body">{it.review.source_material.reply}</div></div>
    <details class="adv review-evidence"><summary>{t('reviewEvidence')}</summary><p>{it.review.truth}: {it.review.reason}</p></details>
  {:else if it.top}
    <div class="guess prediction"><div><span class="prediction-label">{t('predictionHeading')}</span><strong>{t('aiGuess')(topName, (guess.p * 100).toFixed(0))}</strong><span class="prediction-band">{bandWord}{#if it.audit} · {t('auditShort')}{/if}</span></div><div class="confidence-ring" style="--confidence:{Math.round(guess.p * 100)}%" aria-hidden="true"><span>{Math.round(guess.p * 100)}<small>%</small></span></div></div>
  {/if}
  {#if !it.legacy_planning}<div class="answer-area"><div class="answer-heading"><span>{t('yourDecision')}</span><small>{it.truth ? t('changeAnswerShort') : t('chooseAnswer')}</small></div>
    <div class="btns" role="group" aria-label={t('yourDecision')}>
      {#each it.choices as c, i}
        <button type="button" class="answer-choice" class:top={!it.review && c.truth === guess.truth} class:active={c.truth === it.truth} aria-pressed={c.truth === it.truth} data-truth={c.truth} title={c.description || ''} disabled={busy} onclick={() => answer(c.truth)}>
          <span class="num">{i + 1}</span><span class="choice-content"><span>{name(c)}</span>{#if !it.review && c.truth === guess.truth}<small>{t('sameAsAI')}</small>{/if}</span><span class="choice-check" aria-hidden="true">{c.truth === it.truth ? '✓' : '↵'}</span>
        </button>
      {/each}
      {#if it.answerable && it.top && !it.review && sp.type !== 'noul'}
        <button type="button" class="weak partial-answer" data-truth={'!' + guess.truth} disabled={busy} onclick={() => answer('!' + guess.truth)}>{t('notThat')(topName)}</button>
      {/if}
      {#if !it.truth}<button type="button" class="weak skip" disabled={busy} onclick={() => skipped.add(it.trace_id)}>{t('skip')} <span aria-hidden="true">→</span></button>{/if}
      {#if it.answerable}<button type="button" class="weak adv" title={t('redoHint')} disabled={busy} onclick={redo}>{t('redo')}</button>{/if}
    </div>
  </div>{/if}
  <div class="redo-out" aria-live="polite">{redoRes ? redoLine(redoRes) : ''}</div>
</article>
