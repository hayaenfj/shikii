<script>
  // The Answer screen (0.9.1): gathers the questions waiting for a person into one list and shows the first item as a card.
  // 0.9.2: the card has a short title on top and the question once, above the buttons. The AI's internal numbers (learning progress, provisional scores) are not shown here (they go to the AI page).
  // About ways of asking, only the one question for the person is shown here (WordingAsk), and only when wordingNeedsPerson says there is something to do
  import { t } from '../lib/i18n.svelte.js';
  import { askOf, titleOf } from '../lib/i18n.svelte.js';
  import { ui, skipped, personSpecs, wordingNeedsPerson } from '../lib/state.svelte.js';
  import ItemCard from './ItemCard.svelte';
  import WordingAsk from './WordingAsk.svelte';

  let { st } = $props();
  let specs = $derived(personSpecs(st));
  let rows = $derived(specs.flatMap(sp => (sp.queue?.items || []).filter(it => it.waits_for_person !== false).map(it => ({ sp, it }))));
  // The decision named in the URL (ui.pinned) goes first (so a person coming from the hook's "answer later → URL" can answer that item right away)
  let visible = $derived.by(() => {
    const v = rows.filter(r => !skipped.has(r.it.trace_id));
    const i = v.findIndex(r => r.it.trace_id === ui.pinned);
    if (i > 0) v.unshift(v.splice(i, 1)[0]);
    return v;
  });
  let waiting = $derived(specs.reduce((n, s) => n + (s.waiting_for_person || 0), 0));
  let loading = $derived(specs.some(s => s.waiting_for_person > 0 && !s.queue));
  let wording = $derived(specs.filter(s => wordingNeedsPerson(s.tune?.latest)));
</script>

<section data-answer>
  <h2 class="mb-1 flex items-baseline gap-3 text-[25px] max-md:text-[23px]">{t('answerTitle')}{#if waiting}<span class="text-[14px] font-normal text-muted" data-answer-left>{t('answerLeft')(Math.max(waiting, visible.length))}</span>{/if}</h2>
  {#if visible.length || wording.length}<p class="page-purpose mt-0 mb-5 text-[14px] text-muted">{t('answerHow')}</p>{/if}

  {#if loading}
    <div class="empty-state" role="status">{t('loading')}</div>
  {:else if visible.length}
    {#key visible[0].it.trace_id}
      <ItemCard sp={visible[0].sp} it={visible[0].it} question={askOf(visible[0].sp)} title={titleOf(visible[0].sp)} />
    {/key}
  {:else if !wording.length}
    <div class="empty-state" data-answer-empty><span class="grid h-[42px] w-[42px] place-items-center rounded-full bg-accentbg text-[20px] text-accent" aria-hidden="true">✓</span>
      <p class="text-[16px] text-fg">{t('answerEmpty')}</p>
      {#if rows.length}<button type="button" class="weak" onclick={() => skipped.clear()}>{t('unskip')(rows.length)}</button>{/if}
    </div>
  {/if}

  {#each wording as sp (sp.id)}
    <div class="mt-6"><WordingAsk {sp} /></div>
  {/each}
</section>
