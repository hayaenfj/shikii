<script>
  // 画面の全体(0.9.1): ヘッダー(プロジェクト、3 つの画面の切替、?、言語)、使い方の案内、開いた画面、toast。見た目は Tailwind の class(色は app.css の token)
  // 画面は「答える」(人が答える判定を 1 列に)・「知識」・「AI の様子」の 3 つだけ。0.8 までの左の問いごとのメニューと先頭の要約の帯はやめた
  // 15 秒ごとに読み直す(画面が隠れている間は読まない)。キー操作(答える画面だけ): 1〜9 で札の答えを選ぶ、Enter で AI と同じ、→ でスキップ
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
    // 答える画面の先頭の札でだけ効く(知識や AI の様子で別のボタンを押さない。監査 R2)。入力欄やボタンに居る時は邪魔しない
    if (page !== 'answer') return;
    if (ev.target?.closest?.('input, select, textarea, button, a, summary, [contenteditable="true"]')) return;
    const first = document.querySelector('.item'); if (!first) return;
    if (/^[1-9]$/.test(ev.key)) { const b = first.querySelectorAll('.btns button')[Number(ev.key) - 1]; if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'Enter') { const b = first.querySelector('.btns button.top'); if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'ArrowRight' || ev.key === 's') { const b = first.querySelector('.btns button.skip'); if (b) { ev.preventDefault(); b.click(); } }
  }

  // 選択にだけ追従し、取得したデータの更新では読み直さない。
  $effect(() => {
    ui.page; ui.tab; ui.done;
    untrack(() => { load(); });
  });

  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) load(); }, 15000);
    return () => clearInterval(timer);
  });

  const pageBtn = on => 'flex items-center gap-2 rounded-[9px] border px-3.5 py-2 text-[14px] max-sm:px-2.5 max-sm:py-1.5 max-sm:text-[13px] ' + (on ? 'active border-accent/30 bg-accentbg font-[650] text-accent' : 'border-transparent bg-transparent text-muted hover:bg-surface2');
</script>

<svelte:document onkeydown={onKey} />

<header class="app-header mx-auto flex min-h-[72px] max-w-[1440px] flex-wrap items-center gap-x-6 gap-y-2 border-b border-line py-2 max-sm:gap-x-2.5">
  <div class="flex items-center gap-[11px]"><span class="flex h-[36px] w-[36px] items-center gap-1 rounded-[11px] bg-accent p-2 max-sm:h-[30px] max-sm:w-[30px] max-sm:gap-[3px] max-sm:rounded-[9px] max-sm:p-[6px] [&>i]:block [&>i]:h-2.5 [&>i]:w-1 [&>i]:rounded-[3px] [&>i]:bg-bg [&>i:nth-child(2)]:h-[19px] [&>i:nth-child(3)]:h-[14px]" aria-hidden="true"><i></i><i></i><i></i></span><h1 class="m-0 text-[22px] leading-[1.15] font-[750] tracking-[-.5px] max-sm:text-[19px]">shikii{#if st?.project_name}<a class="ml-2.5 text-[15px] font-normal tracking-normal text-muted no-underline" href="../../" title={t('projectsTitle')}>{st.project_name}</a>{/if}</h1></div>
  {#if hasWiki && st.specs?.length}
    <nav id="tabs" class="flex gap-1 max-md:order-last max-md:w-full" aria-label={t('pagesNav')}>
      <button type="button" data-page="answer" aria-current={page === 'answer' ? 'page' : undefined} class={pageBtn(page === 'answer')} onclick={() => setPage('answer')}>{t('pageAnswer')}{#if waits}<span class="badge rounded-md bg-amberbg px-[7px] py-px text-[11px] text-amber tabular-nums">{waits}</span>{/if}</button>
      <button type="button" data-page="wiki" aria-current={page === 'wiki' ? 'page' : undefined} class={pageBtn(page === 'wiki')} onclick={() => setPage('wiki')}>{t('pageKnowledge')}</button>
      <button type="button" data-page="ai" aria-current={page === 'ai' ? 'page' : undefined} class={pageBtn(page === 'ai')} onclick={() => setPage('ai')}>{t('pageAI')}</button>
    </nav>
  {/if}
  <div class="ml-auto flex items-center gap-2 max-sm:gap-[5px]">
    {#if !st?.projects}<button id="help" class="w-[33px] rounded-full border border-line bg-transparent p-1.5 text-[12px] max-sm:w-7 max-sm:p-1 max-sm:text-[11px]" type="button" title={t('helpTitle')} aria-label={t('helpTitle')} onclick={() => setShowHelp(!ui.showHelp)}>?</button>{/if}
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
      {#if ui.showHelp}<HowTo />{/if}
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
