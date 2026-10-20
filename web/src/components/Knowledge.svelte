<script>
  // 知識の欄: 主張と下書きの一覧(証拠、支持するモデルの数、confirmable の印)。
  // 人がすること: 下書きを一般化して主張にする(promote)、confirmed / rejected にする(status)、下書きを消す(delete)。機械は status を変えない
  import { t } from '../lib/i18n.svelte.js';
  import { load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import Notebook from './Notebook.svelte';

  let { wiki } = $props();
  let busyPath = $state('');

  let claims = $derived((wiki.claims || []).filter(c => c.is_claim));
  let prose = $derived((wiki.claims || []).filter(c => !c.is_claim).length);

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

<Notebook {wiki} />

<section class="adv">
  <h2>{t('knowledge')} <span class="pill">{t('claimsCount')(claims.length, prose)}</span></h2>
  {#if !claims.length}<div class="empty">{t('noClaims')}</div>{/if}
  {#each claims as c (c.path)}
    <div class="item">
      <div class="meta">
        <span class="pill" class:calibrated={c.status === 'confirmed'} class:ready={c.status === 'draft'}>{c.status}</span>
        <span>{c.path}</span>
        {#if c.confirmable}<span class="audit">{t('confirmable')}</span>{/if}
      </div>
      <div class="text">
        <b>{c.claim}</b>
        {#if c.applies_to}<div class="k">{Object.entries(c.applies_to).map(([k, v]) => k + ': ' + v).join(' · ')}</div>{/if}
        {#each c.evidence || [] as e}
          <div class="k">{e.date + ' · ' + e.model + ' · ' + e.result + ' → ' + e.verdict}</div>
        {/each}
      </div>
      <div class="btns">
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
