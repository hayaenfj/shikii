<script>
  // 画面の全体: ヘッダー(?、言語)、使い方の案内、タブ、判定の欄か知識の欄、toast。見た目は Tailwind の class(色は app.css の token)
  // 15 秒ごとに読み直す(画面が隠れている間は読まない)。キー操作: 1〜9 で先頭のカードの答えを選ぶ、Enter で AI と同じ、→ でスキップ
  import { onMount, untrack } from 'svelte';
  import { i18n, t } from './lib/i18n.svelte.js';
  import { ui, data, load, selectedTab, setShowHelp } from './lib/state.svelte.js';
  import HowTo from './components/HowTo.svelte';
  import Tabs from './components/Tabs.svelte';
  import SpecPanel from './components/SpecPanel.svelte';
  import Knowledge from './components/Knowledge.svelte';
  import Toast from './components/Toast.svelte';
  import LangPicker from './components/LangPicker.svelte';
  import Projects from './components/Projects.svelte';
  import Summary from './components/Summary.svelte';

  let st = $derived(data.state);
  let hasWiki = $derived(!!st && !st.projects);
  let activeTab = $derived(selectedTab(st));
  let activeSpec = $derived(st && activeTab !== 'wiki' ? st.specs.find(s => s.id === activeTab) : null);

  $effect(() => { document.documentElement.lang = i18n.lang; });

  function onKey(ev) {
    // 判定の欄の未回答の列でだけ効く(知識の欄や回答済みで別のボタンを押さない。監査 R2)。入力欄やボタンに居る時は邪魔しない
    if (activeTab === 'wiki' || ui.done || ui.results || activeSpec?.person_answers === false) return;
    if (ev.target?.closest?.('input, select, textarea, button, a, summary, [contenteditable="true"]')) return;
    const first = document.querySelector('.item'); if (!first) return;
    if (/^[1-9]$/.test(ev.key)) { const b = first.querySelectorAll('.btns button')[Number(ev.key) - 1]; if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'Enter') { const b = first.querySelector('.btns button.top'); if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'ArrowRight' || ev.key === 's') { const b = first.querySelector('.btns button.skip'); if (b) { ev.preventDefault(); b.click(); } }
  }

  // 選択にだけ追従し、取得したデータの更新では読み直さない。
  $effect(() => {
    ui.tab; ui.done;
    untrack(() => { load(); });
  });

  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) load(); }, 15000);
    return () => clearInterval(timer);
  });
</script>

<svelte:document onkeydown={onKey} />

<header class="app-header mx-auto flex min-h-[84px] max-w-[1440px] items-center justify-between gap-6 border-b border-line max-md:min-h-[86px] max-sm:gap-2.5">
  <div class="flex items-center gap-[13px] max-sm:gap-[9px]"><span class="flex h-[42px] w-[42px] items-center gap-1 rounded-[13px] bg-accent p-2.5 max-sm:h-[34px] max-sm:w-[34px] max-sm:gap-[3px] max-sm:rounded-[10px] max-sm:p-[7px] [&>i]:block [&>i]:h-3 [&>i]:w-[5px] [&>i]:rounded-[3px] [&>i]:bg-bg [&>i:nth-child(2)]:h-[23px] [&>i:nth-child(3)]:h-[17px] max-sm:[&>i]:w-1" aria-hidden="true"><i></i><i></i><i></i></span><h1 class="m-0 text-[26px] leading-[1.15] font-[750] tracking-[-1px] max-sm:text-[22px]">shikii <small id="tagline" class="mt-[7px] block text-[11px] font-normal tracking-[.05em] text-muted max-sm:hidden">{t('tagline')}</small></h1></div>
  <div class="flex items-center gap-2 max-sm:gap-[5px]">
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
    <div class="workspace grid grid-cols-[238px_minmax(0,1fr)] items-start gap-9 pt-6 max-lg:grid-cols-[210px_minmax(0,1fr)] max-lg:gap-6 max-md:block max-md:pt-5">
      <aside class="sidebar sticky top-6 rounded-2xl border border-line bg-surface px-[13px] pt-[22px] pb-3 shadow-card max-lg:px-[9px] max-md:static max-md:mb-6 max-md:p-[15px] max-sm:px-2 max-sm:py-3">
        <div class="mb-[22px] border-b border-line px-2.5 pb-5 max-md:mb-3.5 max-md:flex max-md:items-center max-md:gap-3.5 max-md:px-0 max-md:pb-3"><a class="flex justify-between text-[11px] text-muted no-underline max-md:gap-2" href="../../">{t('projectsTitle')} <span aria-hidden="true">↗</span></a><strong class="mt-1 block text-[19px] wrap-anywhere max-md:m-0 max-md:text-[16px]">{st.project_name || t('currentProject')}</strong></div>
        <Tabs specs={st.specs} {hasWiki} active={activeTab} />
      </aside>
      <div class="workspace-content min-w-0 pb-6">
        {#if st.summary}<Summary summary={st.summary} specs={st.specs} model={st.model} />{/if}
        {#if data.loadError}<p class="audit" role="alert">{t('error')(data.loadError)}</p>{/if}
        {#if ui.showHelp}<HowTo />{/if}
        {#if activeTab === 'wiki'}
          {#if st.wiki}<Knowledge wiki={st.wiki} />
          {:else if !data.loadError}<div class="empty-state" role="status">{t('loading')}</div>{/if}
        {:else if activeSpec}
          {#if ui.done ? !!activeSpec.answered : !!activeSpec.queue}
            {#key activeSpec.id}<SpecPanel sp={activeSpec} />{/key}
          {:else if !data.loadError}<div class="empty-state" role="status">{t('loading')}</div>{/if}
        {/if}
      </div>
    </div>
  {/if}
</main>

<Toast />
