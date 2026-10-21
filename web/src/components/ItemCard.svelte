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
    return { request: t('inputRequest'), summary: t('inputSummary'), files: t('inputFiles'), previous: t('inputPrevious') }[key] || key;
  }
  let inputs = $derived.by(() => {
    const entries = Object.entries(it.inputs || {});
    if (!isHook()) return entries;
    const order = { request: 0, summary: 1, files: 2, previous: 3 };
    return entries.sort(([a], [b]) => (order[a] ?? 4) - (order[b] ?? 4));
  });

  async function answer(truth) {
    busy = true;
    try {
      const res = await api.label({ trace_id: it.trace_id, spec: sp.id, truth, source: it.audit ? 'audit' : 'ui' });
      if (it.truth) { sp.answered = null; }
      else { sp.queue.items = sp.queue.items.filter(x => x.trace_id !== it.trace_id); sp.unlabeled = Math.max(0, (sp.unlabeled || 1) - 1); sp.labels++; }
      if (res.calibrated) { toast(t('calibratedToast') + '\n' + (res.calibration || '').split('\n').slice(-2).join('\n'), 8000); load(); }
      else { toast(t('done')(res.truth)); }
      if (it.review) load();
    } catch (e) { toast(t('error')(e.message)); busy = false; }
  }

  // 今の聞き方と線で同じ本文をもう一度判定。前と今を並べ、あなたの答えとの一致を出す
  async function redo() {
    busy = true;
    try {
      const res = await api.redo({ trace_id: it.trace_id, spec: sp.id });
      redoRes = res;
      if (res.recorded) { sp.answered = null; load(); }
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

<div class="item {it.band}" class:card={ui.view === 'one'}>
  {#if pos}<div class="k">{pos}</div>{/if}
  {#if it.truth}
    <div class="guess">{t('yourAnswer')(name(choiceOf(it.truth)), fmtDateTime(it.truth_at))}{#if it.redo_of}{' '}<span class="k">· {t('wasRedone')}</span>{/if}</div>
    <div class="k">{t('changeAnswer')}</div>
  {/if}
  {#if it.review}
    <div class="audit">{it.review_reason === 'uncertain' ? t('reviewUncertain') : t('reviewRandom')}</div>
  {:else if it.top}
    <div class="guess">{bandWord} {t('aiGuess')(topName, (guess.p * 100).toFixed(0))}{#if it.audit}{' '}<span class="audit">{t('auditShort')}</span>{/if}</div>
  {/if}
  {#if it.project}<div class="meta"><span class="project">{it.project}</span></div>{/if}
  <div class="meta adv">
    <span>{it.band}</span><span>{fmtDateTime(it.at)}</span><span>{it.model || ''}</span>
    {#if it.rung === 'context'}<span>{t('withContext')}</span>{/if}
    {#if it.audit}<span class="audit">{t('audit')}</span>{/if}
    {#if it.stale}<span>{t('changed')}</span>{/if}
  </div>
  <div class="text">
    {#if it.answerable}
      {#each inputs as [k, v]}
        {#if sp.id === 'needs-plan' && k === 'previous'}
          <details class="previous-context" data-input={k}>
            <summary>{inputLabel(k)}</summary>
            <div>{it.original_inputs?.[k] ?? v}
              {#if it.text_status?.[k]}<div class="k text-limit">{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
            </div>
          </details>
        {:else}
          <div data-input={k}><span class="k">{inputLabel(k)}</span><br>{it.original_inputs?.[k] ?? v}
            {#if it.text_status?.[k]}<div class="k text-limit">{t(it.text_status[k] === 'legacy' ? 'legacyTextLimit' : it.text_status[k] === 'limited' ? 'originalTextLimit' : 'originalTextUnavailable')}</div>{/if}
          </div>
        {/if}
      {/each}
    {:else}
      <span class="k">{t('noText')(it.inputs_hash)}</span>
    {/if}
  </div>
  {#if it.review}
    <div class="text"><span class="k">{t('reviewReply')}</span><br>{it.review.source_material.reply}</div>
    <details class="adv review-evidence"><summary>{t('reviewEvidence')}</summary><p>{it.review.truth}: {it.review.reason}</p></details>
  {/if}
  <div class="btns">
    {#each it.choices as c, i}
      <button type="button" class:top={!it.review && c.truth === guess.truth} class:active={c.truth === it.truth} data-truth={c.truth} title={c.description || ''} disabled={busy} onclick={() => answer(c.truth)}>
        <span class="num">{i + 1}</span>{name(c)}{#if !it.review && c.truth === guess.truth}{' · ' + t('sameAsAI')}{/if}
      </button>
    {/each}
    {#if it.answerable && it.top && !it.review}
      <button type="button" class="weak" data-truth={'!' + guess.truth} disabled={busy} onclick={() => answer('!' + guess.truth)}>{t('notThat')(topName)}</button>
    {/if}
    {#if !it.truth}
      <button type="button" class="weak skip" disabled={busy} onclick={() => skipped.add(it.trace_id)}>{t('skip')}</button>
    {/if}
    {#if it.answerable}
      <button type="button" class="weak adv" title={t('redoHint')} disabled={busy} onclick={redo}>{t('redo')}</button>
    {/if}
  </div>
  <div class="redo-out">{redoRes ? redoLine(redoRes) : ''}</div>
</div>
