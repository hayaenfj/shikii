<script>
  // AI の様子で開く 1 つの問いの欄(0.9.1): 題と人の言葉の質問、進み具合の 1 行とバー、最後の自動較正、列の切替、
  // 未回答のカード(一覧)または回答済み(新しい順、答えの直し、AI にもう一度聞く)、聞き方の欄。
  // モデルごとの印・数字のカード・絞り込み・表示の切替は 0.9.1 で外した(人が答えるのは「答える」画面で、ここは見直す所)
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, skipped } from '../lib/state.svelte.js';
  import Chips from './Chips.svelte';
  import ItemCard from './ItemCard.svelte';
  import Wording from './Wording.svelte';

  let { sp } = $props();

  let title = $derived(loc(sp.titles, ''));
  let question = $derived(loc(sp.questions, sp.question || ''));

  // モデルごとの印の文。状態の鍵(collecting / ready)は辞書の鍵と同じ
  const isHeld = k => k.state !== 'calibrated' && k.state !== 'stale' && sp.calibration_result?.hash === k.hash && sp.calibration_result?.model === k.model && ['quality', 'insufficient', 'error'].includes(sp.calibration_result?.reason);

  // 人の言葉の状態と進み具合
  let live = $derived(sp.keys.filter(k => k.state !== 'stale'));
  let calibration = $derived(live.some(k => k.hash === sp.calibration_result?.hash && k.model === sp.calibration_result?.model) ? sp.calibration_result : null);
  let calibrationSummary = $derived(calibration?.reason === 'quality' ? t('calibrationQuality')(calibration.human_truths) : calibration?.reason === 'insufficient' ? t('calibrationInsufficient')(calibration.human_truths, calibration.min_n) : calibration?.reason === 'written' ? t('calibrationWritten')(calibration.human_truths) : calibration?.reason === 'error' ? t('calibrationError') : t('calibrationDiagnostic'));
  let n = $derived(live.reduce((m, k) => Math.max(m, k.n), 0));
  let progress = $derived.by(() => {
    let plain = t('plainLearning')(n, sp.min_n), pct = Math.min(100, Math.round(100 * n / Math.max(1, sp.min_n)));
    if (live.some(k => k.state === 'calibrated')) { plain = t('plainAuto'); pct = 100; }
    else if (live.some(isHeld)) { plain = t('calibrationHeld'); }
    else if (live.some(k => k.state === 'ready')) { plain = t('plainReady'); pct = 100; }
    else if (live.some(k => k.state === 'not_separable')) { plain = t('plainNotSeparable'); pct = 100; }
    else if (live.some(k => k.state === 'one_answer')) { plain = t('plainOneAnswer'); pct = 100; }
    else if (sp.keys.length && !live.length) plain = t('plainStale');
    return { plain, pct };
  });

  // 人が答えない判定(wiki の矛盾・差し込み先): AI が答え、正解は振り返りの結果から付く。列は「振り返りで確定する」と「確定済み」だけ(0.8.0)
  let machineOnly = $derived(sp.person_answers === false);
  // 列(lane): you = 未回答(あなたが答える)、results = 次の依頼・振り返りで確定する(人は答えない)、done = 回答済み / 確定済み
  let lane = $derived(machineOnly ? (ui.done ? 'done' : 'results') : ui.done ? 'done' : ui.results ? 'results' : 'you');
  const forYou = it => !machineOnly && it.waits_for_person !== false;
  let queued = $derived(sp.queue?.items || []);
  let all = $derived(queued.filter(it => lane === 'results' ? !forYou(it) : forYou(it)));
  let resultsCount = $derived(queued.filter(it => !forYou(it)).length + Math.max(0, (sp.awaiting_results || 0) - queued.filter(it => !forYou(it)).length));
  // 未回答(あなた)の数: 列を読む前(回答済みを見ている時)は status の人待ちの数
  let counts = $derived({ all: sp.queue ? queued.filter(forYou).length : (sp.waiting_for_person || 0) });
  let answeredVisible = $derived(sp.answered || []);
  let visible = $derived(all.filter(it => !skipped.has(it.trace_id)));
  let extra = $derived(lane === 'you' ? Math.max(0, (sp.waiting_for_person || 0) - all.length) : 0);
  // 見た目(Tailwind)。何度も出る物だけここで名前を付ける
  const phase = 'decision-phase mt-[9px] mb-0 text-[12px] text-muted max-sm:text-[11px]';
</script>

<section class="decision-panel">
  <h2 class="mb-1 text-[20px] max-md:text-[18px]">{title || sp.id} <span class="k font-normal">{sp.id}</span></h2>
  <div class="mb-3.5 flex items-start gap-3.5 rounded-[13px] border border-accent/25 bg-accentbg px-5 py-4 max-sm:gap-2.5 max-sm:px-[13px]"><div class="grid h-[27px] flex-[0_0_27px] place-items-center rounded-[9px] bg-surface text-[14px] font-semibold text-accent max-sm:h-[23px] max-sm:flex-[0_0_23px]" aria-hidden="true">?</div><div><div class="text-[10px] font-[650] tracking-[.08em] text-accent">{t('questionHeading')}</div>
  {#if question}<div class="mt-[5px] text-[16px] leading-[1.75] font-[550] max-sm:text-[14px]">{question}</div>{/if}
  {#if machineOnly}<p class="{phase} machine-only">{t('machineOnly')(sp.awaiting_results || 0, sp.undecided || 0)}</p>
  {:else if sp.id === 'needs-plan'}<p class={phase}>{t('beforeWork')}</p>
  {:else if sp.id === 'done-as-asked' || sp.id === 'changes-in-scope'}<p class={phase}>{t('afterWork')}</p>{/if}
  </div></div>
  <div class="mb-4 px-0.5"><div class="learning-line flex items-center gap-2 text-[11px] text-muted max-sm:text-[10px]"><span class="text-[16px] text-accent" aria-hidden="true">◒</span><span>{progress.plain}</span><span class="ml-auto whitespace-nowrap tabular-nums max-sm:hidden">{Math.min(n, sp.min_n)} / {sp.min_n}</span></div><div class="mt-[9px] h-1 overflow-hidden rounded-[3px] bg-line" role="progressbar" aria-label={t('learningProgress')} aria-valuenow={progress.pct} aria-valuemin="0" aria-valuemax="100"><div class="h-full rounded-[3px] bg-accent" style="width:{progress.pct}%"></div></div></div>
  {#if sp.planning_context && sp.legacy_planning_records}<p class="k">{t('planningLearning')(sp.legacy_planning_records)}</p>{/if}
  {#if sp.last_calibration}
    <div class="calibration-report my-4 rounded-[11px] border border-line bg-surface px-[18px] py-4 text-[12px] [&_summary]:cursor-pointer [&_summary]:text-muted">
      <strong>{t('last')}</strong>{#if calibration?.at}<time class="mt-1 block text-[10px] text-muted">{calibration.at}</time>{/if}
      <p class="calibration-summary my-2.5 leading-[1.8]">{calibrationSummary}</p>
      <details><summary>{t('calibrationDetails')}</summary><pre>{sp.last_calibration}</pre></details>
    </div>
  {/if}
  {#if sp.review_quality?.scored}<p class="k review-quality">{t('reviewQuality')(sp.review_quality)}</p>{/if}
  {#if sp.review_pending}<p class="k">{t('reviewPending')(sp.review_pending)}</p>{/if}
  <Chips {counts} labels={sp.labels} {machineOnly} {resultsCount} settledByAI={!machineOnly && sp.truth_source === 'person'} />

  {#if ui.done}
    {#if !sp.answered}
      <div class="empty-state">{t('loading')}</div>
    {:else}
      {#if !answeredVisible.length}<div class="empty-state"><span class="grid h-[42px] w-[42px] place-items-center rounded-full bg-accentbg text-[20px] text-accent" aria-hidden="true">✓</span><p>{t('noneAnswered')}</p></div>{/if}
      {#each answeredVisible.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} />
      {/each}
      {#if answeredVisible.length > ui.listSize}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(answeredVisible.length - ui.listSize)}</button>
      {/if}
    {/if}
  {:else}
    {#if !visible.length}
      <div class="empty-state"><span class="grid h-[42px] w-[42px] place-items-center rounded-full bg-accentbg text-[20px] text-accent" aria-hidden="true">✓</span>
        <p>{lane === 'results' ? t('emptyResults') : t('empty')}</p>
        {#if all.length}<button type="button" class="weak" onclick={() => skipped.clear()}>{t('unskip')(all.length)}</button>{/if}
        {#if extra}<div>{t('more')(extra)}</div>{/if}
      </div>
    {:else}
      {#if lane !== 'you'}<div class="lane-results mt-4 mb-0.5 text-[12px]">{t(machineOnly ? 'machineOnlyLane' : sp.truth_source === 'person' ? 'settledByAILane' : 'resultsLane')}</div>{/if}
      {#each visible.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} readonly={lane === 'results' && (machineOnly || sp.truth_source !== 'person')} />
      {/each}
      {#if visible.length > ui.listSize || extra}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(Math.max(0, visible.length - ui.listSize) + extra)}</button>
      {/if}
    {/if}
    <Wording {sp} />
  {/if}
</section>
