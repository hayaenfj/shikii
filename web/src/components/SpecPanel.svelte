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
  const kind = it => it.band === 'ask' ? 'ask' : it.audit ? 'audit' : 'other';
  let all = $derived(sp.queue.items);
  let counts = $derived({ all: all.length, ask: all.filter(i => kind(i) === 'ask').length, audit: all.filter(i => kind(i) === 'audit').length, other: all.filter(i => kind(i) === 'other').length });
  let matching = $derived(all.filter(it => ui.filter === 'all' || kind(it) === ui.filter));
  let visible = $derived(matching.filter(it => !skipped.has(it.trace_id)));
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
  <Chips {counts} labels={sp.labels} />

  {#if ui.done}
    {#if !sp.answered}
      <div class="empty">{t('loading')}</div>
    {:else}
      {#if !sp.answered.length}<div class="empty">{t('noneAnswered')}</div>{/if}
      {#each sp.answered.slice(0, ui.listSize) as it (it.trace_id)}
        <ItemCard {sp} {it} />
      {/each}
      {#if sp.answered.length > ui.listSize}
        <button type="button" class="weak" onclick={() => { ui.listSize += 30; }}>{t('showMore')(sp.answered.length - ui.listSize)}</button>
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
