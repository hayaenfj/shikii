<script>
  import { untrack } from 'svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';

  let { article, action, oncancel, onsaved } = $props();
  // Keep this snapshot when a background reload supplies a newer article.
  const original = untrack(() => ({ ...article }));
  let draft = $state({ title: original.title, lesson: original.lesson, applies_to: original.applies_to, body: original.body || '', rationale: original.rationale || '', kind: original.kind || 'rule', scene: original.scene || '', passages: (original.passages || []).map(p => ({ ...p })) });
  let busy = $state(false), error = $state(''), dialog = $state();
  let stale = $state(false), comparison = $state(null);
  async function compare() {
    busy = true; error = '';
    try { comparison = await api.article({ id: original.id, revision: original.revision, action: 'reconcile', ...draft }); }
    catch (e) { error = t('error')(e.message); }
    finally { busy = false; }
  }
  async function saveComparison() {
    if (busy || comparison?.status !== 'ready') return;
    busy = true; error = '';
    const p = comparison.result;
    try { onsaved(await api.article({ id: p.id, revision: comparison.current.revision, action: 'edit', title: p.title, lesson: p.lesson, applies_to: p.applies_to, body: p.body || '', rationale: p.rationale || '', kind: p.kind, scene: p.scene, passages: p.passages || [] })); }
    catch (e) { comparison = null; stale = e.status === 409; error = stale ? t('articleConflict') : t('error')(e.message); }
    finally { busy = false; }
  }
  const field = 'block w-full rounded-lg border border-line bg-bg px-3 py-2 text-[14px]';
  $effect(() => { if (dialog && !dialog.open) { if (dialog.showModal) dialog.showModal(); else dialog.setAttribute('open', ''); } });
  function cancel(event) { event?.preventDefault(); if (!busy) oncancel(); }
  function beforeUnload(event) { event.preventDefault(); event.returnValue = ''; }
  async function save(event) {
    event?.preventDefault();
    if (busy || comparison) return;
    busy = true; error = '';
    try {
      const page = await api.article({ id: original.id, revision: original.revision, action, ...(action === 'edit' ? draft : {}) });
      onsaved(page);
    } catch (e) { stale = e.status === 409; error = stale ? t('articleConflict') : t('error')(e.message); }
    finally { busy = false; }
  }
</script>

<svelte:window onbeforeunload={beforeUnload} />

<dialog bind:this={dialog} oncancel={cancel} aria-labelledby="article-editor-title" class="m-auto max-h-[90dvh] w-[min(640px,calc(100%-24px))] overflow-y-auto rounded-[14px] border border-line bg-surface p-5 text-fg shadow-card backdrop:bg-black/40">
  <h2 id="article-editor-title" class="mt-0 text-[20px]">{action === 'edit' ? t('articleEdit') : t('articleDelete')}</h2>
  {#if action === 'edit'}
    <form onsubmit={save} data-article-form class="grid gap-3">
      <fieldset disabled={busy || !!comparison} class="m-0 grid min-w-0 gap-3 border-0 p-0">
        <label>{t('articleTitle')}<input name="title" class={field} required bind:value={draft.title}></label>
        <label>{t('articleSummary')}<textarea name="lesson" class={field} rows="3" required bind:value={draft.lesson}></textarea></label>
        <label>{t('appliesTo')}<textarea name="applies_to" class={field} rows="2" required bind:value={draft.applies_to}></textarea></label>
        <label>{t('bodyTitle')}<textarea name="body" class={field} rows="8" bind:value={draft.body}></textarea></label>
        <label>{t('articleRationale')}<textarea name="rationale" class={field} rows="3" bind:value={draft.rationale}></textarea></label>
        <details data-passage-editor>
          <summary>{t('articlePassages')}</summary>
          <p class="text-[13px] text-muted">{t('passagesHow')}</p>
          {#each draft.passages as passage, i}
            <div class="mb-3 grid gap-2 rounded-lg border border-line p-3" data-passage-row>
              <label>{t('articlePassages')}<select class={field} bind:value={passage.role}>{#each ['current', 'history', 'recheck', 'counterexample'] as role}<option value={role}>{t('passageRole')(role)}</option>{/each}</select></label>
              <label>{t('passageQuote')}<textarea class={field} rows="2" required bind:value={passage.quote}></textarea></label>
              <label>{t('passageConditions')}<input class={field} required bind:value={passage.conditions}></label>
              <label>{t('passageReason')}<textarea class={field} rows="2" required bind:value={passage.reason}></textarea></label>
              <button type="button" class="weak justify-self-start" onclick={() => { draft.passages = draft.passages.filter((_, n) => n !== i); }}>{t('passageRemove')}</button>
            </div>
          {/each}
          <button type="button" class="weak" disabled={draft.passages.length >= 8} onclick={() => { draft.passages = [...draft.passages, { role: 'current', quote: '', conditions: '', reason: '' }]; }}>{t('passageAdd')}</button>
        </details>
        <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <label>{t('articleKind')}<select name="kind" class={field} bind:value={draft.kind}>{#each ['rule', 'howto', 'criteria', 'fact', 'history'] as kind}<option value={kind}>{t('kindName')(kind)}</option>{/each}</select></label>
          <label>{t('scenesTitle')}<input name="scene" class={field} maxlength="48" required bind:value={draft.scene}></label>
        </div>
      </fieldset>
      <p class="m-0 text-[13px] text-muted">{t('articleEditHow')}</p>
      {#if error}<p role="alert" data-article-error class="m-0 text-[14px]">{error}</p>{/if}
      {#if stale && !comparison}<button type="button" class="weak justify-self-start" disabled={busy} onclick={compare} data-article-compare>{t('articleCompare')}</button>{/if}
      {#if comparison}
        <section data-article-comparison class="grid gap-2 rounded-lg border border-line p-3">
          <h3 class="m-0">{t('articleCompare')}</h3>
          <p class="m-0 text-[13px]">{t('articleComparisonHow')}</p>
          {#if comparison.status === 'ready'}
            {#each ['title', 'lesson', 'applies_to', 'body', 'rationale', 'kind', 'scene'] as key}
              {#if (comparison.result[key] || '') !== (draft[key] || '') || (comparison.result[key] || '') !== (original[key] || '')}
                <details><summary>{t('articleField')(key)}</summary><p class="k">{t('articleMerged')}</p><pre class="whitespace-pre-wrap wrap-anywhere">{comparison.result[key] || ''}</pre></details>
              {/if}
            {/each}
            <button type="button" disabled={busy} onclick={saveComparison} data-article-merge-save>{t('articleMergeSave')}</button>
          {:else if comparison.status === 'conflict'}
            <p role="alert">{comparison.conflicts?.length ? t('articleMergeConflict') : t('articleMergeValidation')}</p>
            {#each comparison.conflicts || [] as conflict}
              <details><summary>{t('articleField')(conflict.field)}</summary>
                {#each [['articleBase', conflict.base], ['articleCurrent', conflict.current], ['articleProposed', conflict.proposed]] as pair}
                  <div class="k">{t(pair[0])}</div><pre class="whitespace-pre-wrap wrap-anywhere">{typeof pair[1] === 'string' ? pair[1] : JSON.stringify(pair[1], null, 2)}</pre>
                {/each}
              </details>
            {/each}
          {:else}<p>{t('articleMergeUnavailable')}</p>{/if}
          {#if comparison.reason}<details data-article-comparison-reason><summary>{t('articleComparisonReason')}</summary><p class="whitespace-pre-wrap wrap-anywhere">{comparison.reason}</p></details>{/if}
          <button type="button" class="weak" disabled={busy} onclick={() => { comparison = null; }}>{t('articleComparisonBack')}</button>
        </section>
      {/if}
      <div class="flex gap-2"><button type="submit" disabled={busy || !!comparison} data-article-save>{busy ? t('articleSaving') : t('articleSave')}</button><button type="button" class="weak" disabled={busy} onclick={cancel} data-article-cancel>{t('articleCancel')}</button></div>
    </form>
  {:else}
    <p class="text-[16px] font-bold wrap-anywhere">{original.title}</p>
    <p class="text-[14px]">{t('articleDeleteHow')}</p>
    {#if error}<p role="alert" data-article-error class="text-[14px]">{error}</p>{/if}
    <div class="flex gap-2"><button type="button" disabled={busy} onclick={save} data-article-delete-confirm>{busy ? t('articleSaving') : t('articleDeleteConfirm')}</button><button type="button" class="weak" disabled={busy} onclick={cancel} data-article-cancel>{t('articleCancel')}</button></div>
  {/if}
</dialog>
