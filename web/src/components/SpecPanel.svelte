<script>
  // 1 つの判定の欄: 題と人の言葉の質問、状態(モデルごとの印)、人の言葉の進み具合のバー、絞り込み、
  // 未回答のカード(1 件ずつ / 一覧、スキップ、ほかに待っている数)または回答済み(新しい順、答えの直し、AI にもう一度聞く)、最後の自動較正、聞き方の欄
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
  const pillText = k => k.model + (k.rung === 'context' ? ' · ' + t('contextRung') : '') + ' · ' + (isHeld(k) ? t('calibrationHeld') : k.state === 'calibrated' ? t('calibrated')(k.method) : k.state === 'stale' ? t('stale') : t(k.state)(k.n, sp.min_n));

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

  // 件数が増えた時の見せ方: 種類ごとの数(押すと絞り込み)、1 件ずつ / 一覧、スキップ、ほかに待っている数
  // 人が答えない判定(wiki の矛盾・差し込み先): AI が答え、正解は振り返りの結果から付く。列は「振り返りで確定する」と「確定済み」だけ(0.8.0)
  let machineOnly = $derived(sp.person_answers === false);
  // 列(lane): you = 未回答(あなたが答える)、results = 次の依頼・振り返りで確定する(人は答えない)、done = 回答済み / 確定済み
  let lane = $derived(machineOnly ? (ui.done ? 'done' : 'results') : ui.done ? 'done' : ui.results ? 'results' : 'you');
  const forYou = it => !machineOnly && it.waits_for_person !== false;
  const kind = it => it.audit ? 'audit' : it.band === 'ask' ? 'ask' : 'all';
  let queued = $derived(sp.queue?.items || []);
  let all = $derived(queued.filter(it => lane === 'results' ? !forYou(it) : forYou(it)));
  let resultsCount = $derived(queued.filter(it => !forYou(it)).length + Math.max(0, (sp.awaiting_results || 0) - queued.filter(it => !forYou(it)).length));
  // 未回答(あなた)の数: 列を読む前(回答済みを見ている時)は status の人待ちの数
  let counts = $derived({ all: sp.queue ? queued.filter(forYou).length : (sp.waiting_for_person || 0), ask: queued.filter(it => forYou(it) && kind(it) === 'ask').length, audit: queued.filter(it => forYou(it) && kind(it) === 'audit').length });
  // 表示中の列から仕事の印を数える。回答済みでも、未回答の列を読む必要はない。
  let projectItems = $derived(ui.done ? (sp.answered || []) : all);
  let projects = $derived.by(() => {
    const m = new Map();
    for (const it of projectItems) if (it.project) m.set(it.project, (m.get(it.project) || 0) + 1);
    return [...m.entries()].sort((a, b) => b[1] - a[1]);
  });
  // この判定に無い仕事が選ばれたまま(別の判定で選んだ)なら絞らない。回答済みにも同じ絞り込みをかける
  let activeProject = $derived(projects.some(([name]) => name === ui.project) ? ui.project : '');
  let matching = $derived(all.filter(it => (lane !== 'you' || ui.filter === 'all' || kind(it) === ui.filter) && (!activeProject || it.project === activeProject)));
  let answeredVisible = $derived((sp.answered || []).filter(it => !activeProject || it.project === activeProject));
  // URL で指された判定(ui.pinned)は先頭に(hook の「あとで答える → URL」から来た人が、その 1 件をすぐ答えられる)
  let visible = $derived.by(() => {
    const v = matching.filter(it => !skipped.has(it.trace_id));
    const i = v.findIndex(it => it.trace_id === ui.pinned);
    if (i > 0) v.unshift(v.splice(i, 1)[0]);
    return v;
  });
  let extra = $derived(lane === 'you' ? Math.max(0, (sp.waiting_for_person || 0) - all.length) : 0);
  // 見た目(Tailwind)。何度も出る物だけここで名前を付ける
  const chip = on => 'flex items-center gap-1.5 rounded-[7px] px-2.5 py-[5px] text-[11px] [&>span]:text-[10px] [&>span]:opacity-80 ' + (on ? 'active border-accent/40 bg-accentbg text-accent' : 'bg-transparent text-muted');
  const phase = 'decision-phase mt-[9px] mb-0 text-[12px] text-muted max-sm:text-[11px]';
</script>

{#snippet tile(label, n, dot)}
  <div class="stat-tile relative rounded-[13px] border border-line bg-surface px-5 py-[13px] shadow-card max-lg:p-[15px] max-sm:rounded-[10px] max-sm:px-2.5 max-sm:py-3"><span class="block text-[11px] text-muted max-sm:text-[9px]">{label}</span><strong class="mt-1 block text-[29px] leading-[1.3] font-semibold tracking-[-.03em] tabular-nums max-sm:text-[25px]">{n}<small class="ml-1.5 text-[11px] font-normal text-muted">{t('countUnit')}</small></strong><span class="absolute top-[22px] right-5 h-[7px] w-[7px] rounded-full {dot} max-lg:right-[15px] max-sm:hidden" aria-hidden="true"></span></div>
{/snippet}

<section class="decision-panel">
  <div class="mb-1.5 flex items-center gap-[9px] text-[10px] font-[650] tracking-[.08em] text-muted">{t('workspaceTitle')} <span class="opacity-50" aria-hidden="true">/</span> {t('decisionNav')}</div>
  <h2 class="max-md:text-[23px]">
    {#if title}{title} <span class="k font-normal">{sp.id}</span>{:else}{sp.id}{/if}
    <span>
      <span class="pill">{sp.type}</span>
      {#each sp.keys as k}<span class="pill {k.state}">{pillText(k)}</span> {/each}
      <span class="pill">{t('records')(sp.records)} · {t('answered')(sp.labels)}</span>
      {#if sp.funnel && sp.records > 0}<span class="pill">{t('toPerson')(sp.funnel.human, sp.funnel.human_per_thousand)}</span>{/if}
    </span>
  </h2>
  <div class="mb-[18px] grid grid-cols-3 gap-3.5 max-md:gap-2.5 max-sm:gap-[7px]">
    {#if !machineOnly}{@render tile(t('pendingCount'), sp.waiting_for_person || 0, 'bg-amber')}{/if}
    {#if machineOnly || sp.awaiting_results}{@render tile(t('awaitingResults'), sp.awaiting_results || 0, 'bg-blue')}{/if}
    {@render tile(t('checkedCount'), sp.labels || 0, 'bg-accent')}
    {@render tile(t('recordCount'), sp.records || 0, 'bg-blue')}
  </div>
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
  {#if projects.length > 1}
    <div class="chips projects mb-4 flex flex-wrap items-center gap-[7px]">
      <span class="k">{t('byProject')}</span>
      <button type="button" class={chip(!activeProject)} onclick={() => { ui.project = ''; }}>{t('chip_all')} {projectItems.length}</button>
      {#each projects as [name, n]}
        <button type="button" class={chip(activeProject === name)} onclick={() => { ui.project = ui.project === name ? '' : name; }}>{name} {n}</button>
      {/each}
    </div>
  {/if}

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
        <p>{lane === 'results' ? t('emptyResults') : all.length ? t('emptyFiltered') : t('empty')}</p>
        {#if matching.length}<button type="button" class="weak" onclick={() => skipped.clear()}>{t('unskip')(matching.length)}</button>{/if}
        {#if extra}<div>{t('more')(extra)}</div>{/if}
      </div>
    {:else if ui.view === 'one'}
      {#if lane === 'you'}<div class="mt-4 mb-0.5 text-[12px]">{t('todo')}</div><div class="mt-1.5 mb-4 text-[10px] text-muted">{t('keysHint')}</div>{:else}<div class="lane-results mt-4 mb-0.5 text-[12px]">{t(machineOnly ? 'machineOnlyLane' : sp.truth_source === 'person' ? 'settledByAILane' : 'resultsLane')}</div>{/if}
      {#key visible[0].trace_id}
        <ItemCard {sp} it={visible[0]} readonly={lane === 'results' && (machineOnly || sp.truth_source !== 'person')} pos={t('position')(1, visible.length) + (extra ? ' · ' + t('more')(extra) : '')} />
      {/key}
    {:else}
      {#if lane === 'you'}<div class="mt-1.5 mb-4 text-[10px] text-muted">{t('keysHint')}</div>{:else}<div class="lane-results mt-4 mb-0.5 text-[12px]">{t(machineOnly ? 'machineOnlyLane' : sp.truth_source === 'person' ? 'settledByAILane' : 'resultsLane')}</div>{/if}
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
