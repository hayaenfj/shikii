<script>
  // 1 つの判定の欄: 題と人の言葉の質問、状態(くわしく: モデルごとの印)、人の言葉の進み具合のバー、絞り込み、
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

  // 人の言葉の状態と進み具合(かんたん表示の中心)
  let live = $derived(sp.keys.filter(k => k.state !== 'stale'));
  let calibration = $derived(live.some(k => k.hash === sp.calibration_result?.hash && k.model === sp.calibration_result?.model) ? sp.calibration_result : null);
  let calibrationSummary = $derived(calibration?.reason === 'quality' ? t('calibrationQuality')(calibration.human_truths) : calibration?.reason === 'insufficient' ? t('calibrationInsufficient')(calibration.human_truths, calibration.min_n) : calibration?.reason === 'written' ? t('calibrationWritten')(calibration.human_truths) : calibration?.reason === 'error' ? t('calibrationError') : t('calibrationDiagnostic'));
  let n = $derived(live.reduce((m, k) => Math.max(m, k.n), 0));
  let progress = $derived.by(() => {
    let plain = t('plainLearning')(n, sp.min_n), pct = Math.min(100, Math.round(100 * n / Math.max(1, sp.min_n)));
    if (live.some(k => k.state === 'calibrated')) { plain = t('plainAuto'); pct = 100; }
    else if (live.some(isHeld)) { plain = t('calibrationHeld'); }
    else if (live.some(k => k.state === 'ready')) { plain = t('plainReady'); pct = 100; }
    else if (sp.keys.length && !live.length) plain = t('plainStale');
    return { plain, pct };
  });

  // 件数が増えた時の見せ方: 種類ごとの数(押すと絞り込み)、1 件ずつ / 一覧、スキップ、ほかに待っている数
  const kind = it => it.audit ? 'audit' : it.band === 'ask' ? 'ask' : 'other';
  let all = $derived(sp.queue?.items || []);
  let counts = $derived({ all: ui.done ? (sp.unlabeled || 0) : all.length, ask: all.filter(i => kind(i) === 'ask').length, audit: all.filter(i => kind(i) === 'audit').length, other: all.filter(i => kind(i) === 'other').length });
  // 表示中の列から仕事の印を数える。回答済みでも、未回答の列を読む必要はない。
  let projectItems = $derived(ui.done ? (sp.answered || []) : all);
  let projects = $derived.by(() => {
    const m = new Map();
    for (const it of projectItems) if (it.project) m.set(it.project, (m.get(it.project) || 0) + 1);
    return [...m.entries()].sort((a, b) => b[1] - a[1]);
  });
  // この判定に無い仕事が選ばれたまま(別の判定で選んだ)なら絞らない。回答済みにも同じ絞り込みをかける
  let activeProject = $derived(projects.some(([name]) => name === ui.project) ? ui.project : '');
  let matching = $derived(all.filter(it => (ui.filter === 'all' || kind(it) === ui.filter) && (!activeProject || it.project === activeProject)));
  let answeredVisible = $derived((sp.answered || []).filter(it => !activeProject || it.project === activeProject));
  // URL で指された判定(ui.pinned)は先頭に(hook の「あとで答える → URL」から来た人が、その 1 件をすぐ答えられる)
  let visible = $derived.by(() => {
    const v = matching.filter(it => !skipped.has(it.trace_id));
    const i = v.findIndex(it => it.trace_id === ui.pinned);
    if (i > 0) v.unshift(v.splice(i, 1)[0]);
    return v;
  });
  let extra = $derived(Math.max(0, (sp.unlabeled || 0) - all.length));
</script>

<section class="decision-panel">
  <div class="panel-eyebrow">{t('workspaceTitle')} <span aria-hidden="true">/</span> {t('decisionNav')}</div>
  <h2>
    {#if title}{title} <span class="k adv">{sp.id}</span>{:else}{sp.id}{/if}
    <span class="adv">
      <span class="pill">{sp.type}</span>
      {#each sp.keys as k}<span class="pill {k.state}">{pillText(k)}</span> {/each}
      <span class="pill">{t('records')(sp.records)} · {t('answered')(sp.labels)}</span>
      {#if sp.funnel && sp.records > 0}<span class="pill">{t('toPerson')(sp.funnel.human, sp.funnel.human_per_thousand)}</span>{/if}
    </span>
  </h2>
  <div class="overview-stats">
    <div class="stat-tile"><span>{t('pendingCount')}</span><strong>{sp.unlabeled || 0}<small>{t('countUnit')}</small></strong><span class="stat-dot pending-dot" aria-hidden="true"></span></div>
    <div class="stat-tile"><span>{t('checkedCount')}</span><strong>{sp.labels || 0}<small>{t('countUnit')}</small></strong><span class="stat-dot checked-dot" aria-hidden="true"></span></div>
    <div class="stat-tile"><span>{t('recordCount')}</span><strong>{sp.records || 0}<small>{t('countUnit')}</small></strong><span class="stat-dot record-dot" aria-hidden="true"></span></div>
  </div>
  <div class="question-box"><div class="question-icon" aria-hidden="true">?</div><div><div class="eyebrow">{t('questionHeading')}</div>
  {#if question}<div class="q">{question}</div>{/if}
  {#if sp.id === 'needs-plan'}<p class="decision-phase">{t('beforeWork')}</p>
  {:else if sp.id === 'done-as-asked' || sp.id === 'changes-in-scope'}<p class="decision-phase">{t('afterWork')}</p>{/if}
  </div></div>
  <div class="status"><div class="learning-line"><span class="learning-icon" aria-hidden="true">◒</span><span>{progress.plain}</span><span class="learning-number">{Math.min(n, sp.min_n)} / {sp.min_n}</span></div><div class="bar" role="progressbar" aria-label={t('learningProgress')} aria-valuenow={progress.pct} aria-valuemin="0" aria-valuemax="100"><div style="width:{progress.pct}%"></div></div></div>
  {#if sp.last_calibration}
    <div class="calibration-report">
      <strong>{t('last')}</strong>{#if calibration?.at}<time>{calibration.at}</time>{/if}
      <p class="calibration-summary">{calibrationSummary}</p>
      <details class="adv"><summary>{t('calibrationDetails')}</summary><pre>{sp.last_calibration}</pre></details>
    </div>
  {/if}
  {#if sp.review_quality?.scored}<p class="k review-quality">{t('reviewQuality')(sp.review_quality)}</p>{/if}
  {#if sp.review_pending}<p class="k">{t('reviewPending')(sp.review_pending)}</p>{/if}
  <Chips {counts} labels={sp.labels} />
  {#if projects.length > 1}
    <div class="chips projects">
      <span class="k">{t('byProject')}</span>
      <button type="button" class:active={!activeProject} onclick={() => { ui.project = ''; }}>{t('chip_all')} {projectItems.length}</button>
      {#each projects as [name, n]}
        <button type="button" class:active={activeProject === name} onclick={() => { ui.project = ui.project === name ? '' : name; }}>{name} {n}</button>
      {/each}
    </div>
  {/if}

  {#if ui.done}
    {#if !sp.answered}
      <div class="empty">{t('loading')}</div>
    {:else}
      {#if !answeredVisible.length}<div class="empty"><span class="empty-icon" aria-hidden="true">✓</span><p>{t('noneAnswered')}</p></div>{/if}
      {#each answeredVisible.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} />
      {/each}
      {#if answeredVisible.length > ui.listSize}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(answeredVisible.length - ui.listSize)}</button>
      {/if}
    {/if}
  {:else}
    {#if !visible.length}
      <div class="empty"><span class="empty-icon" aria-hidden="true">✓</span>
        <p>{all.length ? t('emptyFiltered') : t('empty')}</p>
        {#if matching.length}<button type="button" class="weak" onclick={() => skipped.clear()}>{t('unskip')(matching.length)}</button>{/if}
        {#if extra}<div>{t('more')(extra)}</div>{/if}
      </div>
    {:else if ui.view === 'one'}
      <div class="todo">{t('todo')}</div>
      <div class="keys">{t('keysHint')}</div>
      {#key visible[0].trace_id}
        <ItemCard {sp} it={visible[0]} pos={t('position')(1, visible.length) + (extra ? ' · ' + t('more')(extra) : '')} />
      {/key}
    {:else}
      <div class="keys">{t('keysHint')}</div>
      {#each visible.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} />
      {/each}
      {#if visible.length > ui.listSize || extra}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(Math.max(0, visible.length - ui.listSize) + extra)}</button>
      {/if}
    {/if}
    {#if ui.advanced}<Wording {sp} />{/if}
  {/if}
</section>
