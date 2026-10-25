<script>
  // The whole page (0.9.1): the header (project, the switch between the 3 screens, ?, language), the how-to guide, the open screen, and the toast. The look is Tailwind classes (colors are tokens in app.css)
  // There are only 3 screens: "Answer" (decisions a person answers, in one list), "Knowledge" and "AI status". The per-question menu on the left and the summary band at the top, used up to 0.8, were dropped
  // Reloads every 15 seconds (not while the page is hidden). Keys (Answer screen only): 1 to 9 choose the card's answer, Enter means the same as the AI, → skips
  import { onMount, untrack } from 'svelte';
  import { i18n, t } from './lib/i18n.svelte.js';
  import { ui, data, load, selectedPage, setPage, setShowHelp, personSpecs } from './lib/state.svelte.js';
  import HowTo from './components/HowTo.svelte';
  import Answer from './components/Answer.svelte';
  import AiView from './components/AiView.svelte';
  import Knowledge from './components/Knowledge.svelte';
  import Toast from './components/Toast.svelte';
  import LangPicker from './components/LangPicker.svelte';
  import Projects from './components/Projects.svelte';

  let st = $derived(data.state);
  let hasWiki = $derived(!!st && !st.projects);
  let page = $derived(selectedPage(st));
  let waits = $derived(personSpecs(st).reduce((n, s) => n + (s.waiting_for_person || 0), 0));

  $effect(() => { document.documentElement.lang = i18n.lang; });

  function onKey(ev) {
    // Only works on the first card of the Answer screen (so it does not press other buttons in Knowledge or AI status; audit R2). Does not interfere while in an input field or on a button
    if (page !== 'answer') return;
    if (ev.target?.closest?.('input, select, textarea, button, a, summary, [contenteditable="true"]')) return;
    const first = document.querySelector('.item'); if (!first) return;
    if (/^[1-9]$/.test(ev.key)) { const b = first.querySelectorAll('.btns button')[Number(ev.key) - 1]; if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'Enter') { const b = first.querySelector('.btns button.top'); if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'ArrowRight' || ev.key === 's') { const b = first.querySelector('.btns button.skip'); if (b) { ev.preventDefault(); b.click(); } }
  }

  // Follows only the selection; updates of fetched data do not trigger a reload.
  $effect(() => {
    ui.page; ui.tab; ui.done;
    untrack(() => { load(); });
  });

  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) load(); }, 15000);
    return () => clearInterval(timer);
  });

  // The switch at the top (0.9.1): the 3 are grouped in one frame, and the chosen screen is filled dark green with white text. Screens not chosen are still readable in the normal text color
  const pageBtn = on => 'flex items-center gap-2 rounded-[9px] border-0 px-4 py-2 text-[14px] font-[650] max-sm:flex-1 max-sm:justify-center max-sm:px-2.5 max-sm:py-1.5 max-sm:text-[13px] ' + (on ? 'active bg-accent text-bg shadow-[0_1px_3px_#0002]' : 'bg-transparent text-fg hover:bg-surface');
</script>

<svelte:document onkeydown={onKey} />

<header class="app-header mx-auto flex min-h-[72px] max-w-[1440px] flex-wrap items-center gap-x-6 gap-y-2 border-b border-line py-2 max-sm:gap-x-2.5">
  <div class="flex items-center gap-[11px]"><span class="flex h-[36px] w-[36px] items-center gap-1 rounded-[11px] bg-accent p-2 max-sm:h-[30px] max-sm:w-[30px] max-sm:gap-[3px] max-sm:rounded-[9px] max-sm:p-[6px] [&>i]:block [&>i]:h-2.5 [&>i]:w-1 [&>i]:rounded-[3px] [&>i]:bg-bg [&>i:nth-child(2)]:h-[19px] [&>i:nth-child(3)]:h-[14px]" aria-hidden="true"><i></i><i></i><i></i></span><h1 class="m-0 text-[22px] leading-[1.15] font-[750] tracking-[-.5px] max-sm:text-[19px]">shikii{#if st?.project_name}<a class="ml-2.5 text-[15px] font-normal tracking-normal text-muted no-underline" href="../../" title={t('projectsTitle')}>{st.project_name}</a>{/if}</h1></div>
  {#if hasWiki && st.specs?.length}
    <nav id="tabs" class="flex gap-1 rounded-xl border border-line bg-surface2 p-1 max-md:order-last max-md:w-full" aria-label={t('pagesNav')}>
      <button type="button" data-page="answer" aria-current={page === 'answer' ? 'page' : undefined} class={pageBtn(page === 'answer')} onclick={() => setPage('answer')}>{t('pageAnswer')}{#if waits}<span class="badge rounded-md bg-amber px-[7px] py-px text-[12px] text-bg tabular-nums">{waits}</span>{/if}</button>
      <button type="button" data-page="wiki" aria-current={page === 'wiki' ? 'page' : undefined} class={pageBtn(page === 'wiki')} onclick={() => setPage('wiki')}>{t('pageKnowledge')}</button>
      <button type="button" data-page="ai" aria-current={page === 'ai' ? 'page' : undefined} class={pageBtn(page === 'ai')} onclick={() => setPage('ai')}>{t('pageAI')}</button>
    </nav>
  {/if}
  <div class="ml-auto flex items-center gap-2 max-sm:gap-[5px]">
    {#if !st?.projects}<button id="help" class="w-[33px] rounded-full border border-line bg-transparent p-1.5 text-[12px] max-sm:w-7 max-sm:p-1" type="button" title={t('helpTitle')} aria-label={t('helpTitle')} onclick={() => setShowHelp(!ui.showHelp)}>?</button>{/if}
    <LangPicker />
  </div>
</header>

<main id="root" class="mx-auto max-w-[1440px]">
  {#if !st}
    <div class="empty-state mt-[60px]" role="status">{data.loadError ? t('error')(data.loadError) : t('loading')}</div>
  {:else if st.projects}
    <Projects projects={st.projects} />
  {:else if !st.specs.length}
    <div class="empty-state">{t('noSpecs')}</div>
  {:else}
    <div class="workspace-content min-w-0 pt-6 pb-6 max-md:pt-5">
      {#if data.loadError}<p class="audit" role="alert">{t('error')(data.loadError)}</p>{/if}
      {#if ui.showHelp}<HowTo {page} />{/if}
      {#if page === 'wiki'}
        {#if st.wiki}<Knowledge wiki={st.wiki} />
        {:else if !data.loadError}<div class="empty-state" role="status">{t('loading')}</div>{/if}
      {:else if page === 'ai'}
        <AiView {st} />
      {:else}
        <Answer {st} />
      {/if}
    </div>
  {/if}
</main>

<Toast />
