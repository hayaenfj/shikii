<script>
  // 画面の全体: ヘッダー(かんたん表示 / くわしく、?、言語)、使い方の案内、タブ、判定の欄か知識の欄、toast。
  // 15 秒ごとに読み直す(画面が隠れている間は読まない)。キー操作: 1〜9 で先頭のカードの答えを選ぶ、Enter で AI と同じ、→ でスキップ
  import { onMount } from 'svelte';
  import { i18n, t } from './lib/i18n.svelte.js';
  import { ui, data, load, setAdvanced, setShowHelp } from './lib/state.svelte.js';
  import HowTo from './components/HowTo.svelte';
  import Tabs from './components/Tabs.svelte';
  import SpecPanel from './components/SpecPanel.svelte';
  import Knowledge from './components/Knowledge.svelte';
  import Toast from './components/Toast.svelte';
  import LangPicker from './components/LangPicker.svelte';
  import Projects from './components/Projects.svelte';

  let st = $derived(data.state);
  let hasWiki = $derived(!!(st && st.wiki));
  // 開くタブ: 保存値が知識で「くわしく」でないか、無くなった判定なら先頭の判定に戻す
  let activeTab = $derived.by(() => {
    if (!st || !st.specs.length) return null;
    const ids = st.specs.map(s => s.id);
    let tab = ui.tab;
    if (tab === 'wiki' && !(ui.advanced && st.wiki)) tab = null;
    if (tab !== 'wiki' && !ids.includes(tab)) tab = st.specs.find(s => s.unlabeled > 0)?.id || ids[0];
    return tab;
  });
  let activeSpec = $derived(st && activeTab !== 'wiki' ? st.specs.find(s => s.id === activeTab) : null);

  $effect(() => { document.body.classList.toggle('advanced', ui.advanced); });
  $effect(() => { document.documentElement.lang = i18n.lang; });

  function onKey(ev) {
    if (ev.target?.closest?.('input, select, textarea, button, a, summary, [contenteditable="true"]')) return;
    const first = document.querySelector('.item'); if (!first) return;
    if (/^[1-9]$/.test(ev.key)) { const b = first.querySelectorAll('.btns button')[Number(ev.key) - 1]; if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'Enter') { const b = first.querySelector('.btns button.top'); if (b) { ev.preventDefault(); b.click(); } }
    else if (ev.key === 'ArrowRight' || ev.key === 's') { const b = first.querySelector('.btns button.skip'); if (b) { ev.preventDefault(); b.click(); } }
  }

  onMount(() => {
    load().catch(e => { data.loadError = e.message; });
    const timer = setInterval(() => { if (!document.hidden) load().catch(() => {}); }, 15000);
    return () => clearInterval(timer);
  });
</script>

<svelte:document onkeydown={onKey} />

<header class="app-header">
  <div class="brand"><span class="brand-symbol" aria-hidden="true"><i></i><i></i><i></i></span><h1>shikii <small id="tagline">{t('tagline')}</small></h1></div>
  <div class="header-actions">
    {#if !st?.projects}<button id="help" type="button" title={t('helpTitle')} aria-label={t('helpTitle')} onclick={() => setShowHelp(!ui.showHelp)}>?</button>
    <button id="mode" type="button" onclick={() => setAdvanced(!ui.advanced)}>{t(ui.advanced ? 'modeSimple' : 'modeAdvanced')}</button>{/if}
    <LangPicker />
  </div>
</header>

<main id="root">
  {#if !st}
    <div class="empty loading-state" role="status">{data.loadError ? t('error')(data.loadError) : t('loading')}</div>
  {:else if st.projects}
    <Projects projects={st.projects} />
  {:else if !st.specs.length}
    <div class="empty">{t('noSpecs')}</div>
  {:else}
    <div class="workspace">
      <aside class="sidebar">
        <div class="project-heading"><a href="../../">{t('projectsTitle')} <span aria-hidden="true">↗</span></a><strong>{st.project_name || t('currentProject')}</strong></div>
        <Tabs specs={st.specs} {hasWiki} active={activeTab} />
      </aside>
      <div class="workspace-content">
        {#if ui.showHelp}<HowTo />{/if}
        {#if activeTab === 'wiki'}
          <Knowledge wiki={st.wiki} />
        {:else if activeSpec}
          {#key activeSpec.id}<SpecPanel sp={activeSpec} />{/key}
        {/if}
      </div>
    </div>
  {/if}
</main>

<Toast />
