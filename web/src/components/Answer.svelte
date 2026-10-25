<script>
  // 答える画面(0.9.1): 人が答える問いの待ちを 1 列にまとめ、先頭の 1 件を札で出す。問いは札の先頭とボタンの上に出す。
  // AI の内側の数(学習の進み・線・仮採点・帯)はここに出さない(AI の様子へ)。聞き方は、AI が変えた時(このままでよいか)と、AI が意味を決めきれなかった時だけここで聞く(0.9.2)
  import { t, loc } from '../lib/i18n.svelte.js';
  import { ui, skipped, personSpecs, setPage, wordingNeedsPerson } from '../lib/state.svelte.js';
  import ItemCard from './ItemCard.svelte';
  import Wording from './Wording.svelte';

  let { st } = $props();
  let specs = $derived(personSpecs(st));
  let rows = $derived(specs.flatMap(sp => (sp.queue?.items || []).filter(it => it.waits_for_person !== false).map(it => ({ sp, it }))));
  // URL で指された判定(ui.pinned)は先頭に(hook の「あとで答える → URL」から来た人が、その 1 件をすぐ答えられる)
  let visible = $derived.by(() => {
    const v = rows.filter(r => !skipped.has(r.it.trace_id));
    const i = v.findIndex(r => r.it.trace_id === ui.pinned);
    if (i > 0) v.unshift(v.splice(i, 1)[0]);
    return v;
  });
  let waiting = $derived(specs.reduce((n, s) => n + (s.waiting_for_person || 0), 0));
  let loading = $derived(specs.some(s => s.waiting_for_person > 0 && !s.queue));
  let wording = $derived(specs.filter(s => wordingNeedsPerson(s.tune?.latest)));
  const question = sp => loc(sp.questions, sp.question || '') || loc(sp.titles, sp.id);
</script>

<section data-answer>
  <h2 class="mb-1 flex items-baseline gap-3 text-[25px] max-md:text-[23px]">{t('answerTitle')}{#if waiting}<span class="text-[14px] font-normal text-muted" data-answer-left>{t('answerLeft')(Math.max(waiting, visible.length))}</span>{/if}</h2>
  <p class="k mt-0 mb-5">{t('answerHow')}</p>

  {#if loading}
    <div class="empty-state" role="status">{t('loading')}</div>
  {:else if !visible.length}
    <div class="empty-state" data-answer-empty><span class="grid h-[42px] w-[42px] place-items-center rounded-full bg-accentbg text-[20px] text-accent" aria-hidden="true">✓</span>
      <p>{t('answerEmpty')}</p>
      {#if rows.length}<button type="button" class="weak" onclick={() => skipped.clear()}>{t('unskip')(rows.length)}</button>{/if}
      <button type="button" class="weak" onclick={() => setPage('wiki')}>{t('answerToKnowledge')}</button>
    </div>
  {:else}
    {#key visible[0].it.trace_id}
      <ItemCard sp={visible[0].sp} it={visible[0].it} question={question(visible[0].sp)} pos={t('position')(1, visible.length)} />
    {/key}
  {/if}

  {#each wording as sp (sp.id)}
    <div class="mt-6" data-answer-wording={sp.id}>
      <h3 class="mt-0 mb-1 text-[15px]">{(sp.tune.latest.adopted ? t('answerWordingAdopted') : t('answerWording'))(loc(sp.titles, sp.id))}</h3>
      <Wording {sp} />
    </div>
  {/each}
</section>
