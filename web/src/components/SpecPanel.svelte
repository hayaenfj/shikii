<script>
  // 1 つの判定の欄: 題と人の言葉の質問、状態(くわしく: モデルごとの印)、人の言葉の進み具合のバー、絞り込み、
  // 未回答のカード(1 件ずつ / 一覧、スキップ、ほかに待っている数)または回答済み(新しい順、答えの直し、AI にもう一度聞く)、最後の自動較正、聞き方の欄
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, skipped } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import Chips from './Chips.svelte';
  import ItemCard from './ItemCard.svelte';
  import Wording from './Wording.svelte';

  let { sp } = $props();

  let title = $derived(loc(sp.titles, ''));
  let question = $derived(loc(sp.questions, sp.question || ''));

  // モデルごとの印の文。状態の鍵(collecting / ready)は辞書の鍵と同じ
  const pillText = k => k.model + (k.rung === 'context' ? ' · ' + t('contextRung') : '') + ' · ' + (k.state === 'calibrated' ? t('calibrated')(k.method) : k.state === 'stale' ? t('stale') : t(k.state)(k.n, sp.min_n));

  // 人の言葉の状態と進み具合(かんたん表示の中心)
  let live = $derived(sp.keys.filter(k => k.state !== 'stale'));
  let n = $derived(live.reduce((m, k) => Math.max(m, k.n), 0));
  let progress = $derived.by(() => {
    let plain = t('plainLearning')(n, sp.min_n), pct = Math.min(100, Math.round(100 * n / Math.max(1, sp.min_n)));
    if (live.some(k => k.state === 'calibrated')) { plain = t('plainAuto'); pct = 100; }
    else if (live.some(k => k.state === 'ready')) { plain = t('plainReady'); pct = 100; }
    else if (sp.keys.length && !live.length) plain = t('plainStale');
    return { plain, pct };
  });

  // 件数が増えた時の見せ方: 種類ごとの数(押すと絞り込み)、1 件ずつ / 一覧、スキップ、ほかに待っている数
  const kind = it => it.audit ? 'audit' : it.band === 'ask' ? 'ask' : 'other';
  let all = $derived(sp.queue.items);
  let counts = $derived({ all: all.length, ask: all.filter(i => kind(i) === 'ask').length, audit: all.filter(i => kind(i) === 'audit').length, other: all.filter(i => kind(i) === 'other').length });
  // 仕事(project の印)ごとの数。印の付く記録(hook)が 2 つ以上の仕事にまたがる時だけ絞り込みを出す
  let projects = $derived.by(() => {
    const m = new Map();
    for (const it of all) if (it.project) m.set(it.project, (m.get(it.project) || 0) + 1);
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

  // 回答済み: 開いた時に読む(新しい順)。答えを直した・もう一度聞いた後は null に戻るので読み直す
  $effect(() => {
    if (ui.done && !sp.answered) {
      api.answered(sp.id).then(r => { sp.answered = r.items; }).catch(e => toast(t('error')(e.message)));
    }
  });
</script>

<section>
  <h2>
    {#if title}{title} <span class="k adv">{sp.id}</span>{:else}{sp.id}{/if}
    <span class="adv">
      <span class="pill">{sp.type}</span>
      {#each sp.keys as k}<span class="pill {k.state}">{pillText(k)}</span> {/each}
      <span class="pill">{t('records')(sp.records)} · {t('answered')(sp.labels)}</span>
      {#if sp.funnel && sp.records > 0}<span class="pill">{t('toPerson')(sp.funnel.human, sp.funnel.human_per_thousand)}</span>{/if}
    </span>
  </h2>
  {#if question}<div class="q">{question}</div>{/if}
  <div class="status">{progress.plain}<div class="bar"><div style="width:{progress.pct}%"></div></div></div>
  {#if sp.review_quality?.scored}<p class="k review-quality">{t('reviewQuality')(sp.review_quality)}</p>{/if}
  {#if sp.review_pending}<p class="k">{t('reviewPending')(sp.review_pending)}</p>{/if}
  <Chips {counts} labels={sp.labels} />
  {#if projects.length > 1}
    <div class="chips projects">
      <span class="k">{t('byProject')}</span>
      <button type="button" class:active={!activeProject} onclick={() => { ui.project = ''; }}>{t('chip_all')} {all.length}</button>
      {#each projects as [name, n]}
        <button type="button" class:active={activeProject === name} onclick={() => { ui.project = ui.project === name ? '' : name; }}>{name} {n}</button>
      {/each}
    </div>
  {/if}

  {#if ui.done}
    {#if !sp.answered}
      <div class="empty">{t('loading')}</div>
    {:else}
      {#if !answeredVisible.length}<div class="empty">{t('noneAnswered')}</div>{/if}
      {#each answeredVisible.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} />
      {/each}
      {#if answeredVisible.length > ui.listSize}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(answeredVisible.length - ui.listSize)}</button>
      {/if}
    {/if}
  {:else}
    {#if !visible.length}
      <div class="empty">
        {all.length ? t('emptyFiltered') : t('empty')}
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
    {#if sp.last_calibration}<pre class="adv">{t('last')}
{sp.last_calibration}</pre>{/if}
    <Wording {sp} />
  {/if}
</section>
