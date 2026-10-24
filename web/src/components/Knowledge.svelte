<script>
  // 知識の欄(0.9.0): 正面は教訓(場面 → 種類 → 題、Lessons)と作業記録の 2 つのタブ。AI の内部の数(検索・見直し・振り返り)と
  // 前からの記事の木(Notebook)、以前の主張(手で確かめる物)は、畳んだ「AI の様子と詳しい記録」の中に置く(人が全部を確かめない)
  import { t } from '../lib/i18n.svelte.js';
  import { load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import Notebook from './Notebook.svelte';
  import Handoff from './Handoff.svelte';
  import Lessons from './Lessons.svelte';

  let { wiki } = $props();
  let busyPath = $state('');
  const readTab = () => { try { return localStorage.getItem('shikii.knowledgeTab') || 'lessons'; } catch { return 'lessons'; } };
  let tab = $state(readTab());
  function setTab(v) { tab = v; try { localStorage.setItem('shikii.knowledgeTab', v); } catch { /* 表示は動く */ } }

  let claims = $derived((wiki.claims || []).filter(c => c.is_claim));
  let prose = $derived((wiki.claims || []).filter(c => !c.is_claim).length);

  function evidenceText(e) {
    const present = value => typeof value === 'string' && value.trim();
    const details = [e?.date, e?.model, e?.result].filter(present).join(' · ');
    return [details, e?.verdict].filter(present).join(' → ');
  }

  async function act(c, action, status) {
    const body = { path: c.path, action, status };
    if (action === 'promote') {
      body.claim = prompt(t('promptClaim'), c.claim); if (!body.claim) return;
      body.slug = prompt(t('promptSlug'), c.path.split('/').pop().replace(/\.md$/, '')); if (!body.slug) return;
      const models = prompt(t('promptModels'), (c.applies_to && c.applies_to.models) || ''); if (models === null) return;
      body.applies_to = Object.assign({}, c.applies_to || {}, { models });
    }
    busyPath = c.path;
    try { await api.wikiClaim(body); toast(t('claimDone')); load(); }
    catch (e) { toast(t('error')(e.message)); }
    busyPath = '';
  }
</script>

<div class="mb-4 flex gap-2" role="tablist" data-knowledge-tabs>
  <button type="button" role="tab" aria-selected={tab === 'lessons'} class="rounded-lg border px-3 py-1 text-[13px] {tab === 'lessons' ? 'border-accent bg-accentbg text-accent' : 'border-line bg-transparent text-muted'}" data-knowledge-tab="lessons" onclick={() => setTab('lessons')}>{t('lessonsTab')}</button>
  <button type="button" role="tab" aria-selected={tab === 'handoff'} class="rounded-lg border px-3 py-1 text-[13px] {tab === 'handoff' ? 'border-accent bg-accentbg text-accent' : 'border-line bg-transparent text-muted'}" data-knowledge-tab="handoff" onclick={() => setTab('handoff')}>{t('handoffTitle')}</button>
</div>

{#if tab === 'handoff'}
  <Handoff files={wiki.handoff || []} error={!!wiki.handoff_error} />
{:else}
  <Lessons {wiki} />
  <details class="mt-6" data-ai-details>
    <summary class="cursor-pointer text-muted">{t('aiDetails')}</summary>
    <Notebook {wiki} />
<section data-claims>
  <h2 class="max-md:text-[23px]">{t('knowledge')} <span class="pill">{t('claimsCount')(claims.length, prose)}</span></h2>
  {#if !claims.length}<div class="empty-state">{t('noClaims')}</div>{/if}
  {#each claims as c (c.path)}
    <div class="item mb-5 rounded-[15px] border border-line bg-surface p-[22px] shadow-card max-sm:rounded-xl max-sm:px-3.5 max-sm:py-4">
      <div class="mb-3 flex flex-wrap items-center gap-[9px] text-[11px] text-muted">
        <span class="pill" class:calibrated={c.status === 'confirmed'} class:ready={c.status === 'draft'}>{c.status}</span>
        <span>{c.path}</span>
        {#if c.confirmable}<span class="audit">{t('confirmable')}</span>{/if}
      </div>
      <div class="text mb-3.5 wrap-anywhere">
        <b>{c.claim}</b>
        {#if c.applies_to}<div class="k">{Object.entries(c.applies_to).map(([k, v]) => k + ': ' + v).join(' · ')}</div>{/if}
        {#each c.evidence || [] as e}
          <div class="k">{evidenceText(e)}</div>
        {/each}
      </div>
      <div class="btns flex flex-wrap items-center gap-2.5">
        {#if c.status === 'draft'}
          <button type="button" disabled={busyPath === c.path} onclick={() => act(c, 'promote')}>{t('promote')}</button>
          <button type="button" class="weak" disabled={busyPath === c.path} onclick={() => act(c, 'delete')}>{t('deleteDraft')}</button>
        {:else}
          {#if c.status !== 'confirmed'}<button type="button" disabled={busyPath === c.path} onclick={() => act(c, 'status', 'confirmed')}>{t('confirm')}</button>{/if}
          {#if c.status !== 'rejected'}<button type="button" class="weak" disabled={busyPath === c.path} onclick={() => act(c, 'status', 'rejected')}>{t('reject')}</button>{/if}
        {/if}
      </div>
    </div>
  {/each}
</section>
  </details>
{/if}
