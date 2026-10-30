<script>
  import { untrack } from 'svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';

  let { article, action, oncancel, onsaved } = $props();
  // Keep this snapshot when a background reload supplies a newer article.
  const original = untrack(() => ({ ...article }));
  let draft = $state({ title: original.title, lesson: original.lesson, applies_to: original.applies_to, body: original.body || '', rationale: original.rationale || '', kind: original.kind || 'rule', scene: original.scene || '' });
  let busy = $state(false), error = $state(''), dialog = $state();
  const field = 'block w-full rounded-lg border border-line bg-bg px-3 py-2 text-[14px]';
  $effect(() => { if (dialog && !dialog.open) { if (dialog.showModal) dialog.showModal(); else dialog.setAttribute('open', ''); } });
  function cancel(event) { event?.preventDefault(); if (!busy) oncancel(); }
  function beforeUnload(event) { event.preventDefault(); event.returnValue = ''; }
  async function save(event) {
    event?.preventDefault();
    if (busy) return;
    busy = true; error = '';
    try {
      const page = await api.article({ id: original.id, revision: original.revision, action, ...(action === 'edit' ? draft : {}) });
      onsaved(page);
    } catch (e) { error = e.status === 409 ? t('articleConflict') : t('error')(e.message); }
    finally { busy = false; }
  }
</script>

<svelte:window onbeforeunload={beforeUnload} />

<dialog bind:this={dialog} oncancel={cancel} aria-labelledby="article-editor-title" class="m-auto max-h-[90dvh] w-[min(640px,calc(100%-24px))] overflow-y-auto rounded-[14px] border border-line bg-surface p-5 text-fg shadow-card backdrop:bg-black/40">
  <h2 id="article-editor-title" class="mt-0 text-[20px]">{action === 'edit' ? t('articleEdit') : t('articleDelete')}</h2>
  {#if action === 'edit'}
    <form onsubmit={save} data-article-form class="grid gap-3">
      <fieldset disabled={busy} class="m-0 grid min-w-0 gap-3 border-0 p-0">
        <label>{t('articleTitle')}<input name="title" class={field} required bind:value={draft.title}></label>
        <label>{t('articleSummary')}<textarea name="lesson" class={field} rows="3" required bind:value={draft.lesson}></textarea></label>
        <label>{t('appliesTo')}<textarea name="applies_to" class={field} rows="2" required bind:value={draft.applies_to}></textarea></label>
        <label>{t('bodyTitle')}<textarea name="body" class={field} rows="8" bind:value={draft.body}></textarea></label>
        <label>{t('articleRationale')}<textarea name="rationale" class={field} rows="3" bind:value={draft.rationale}></textarea></label>
        <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <label>{t('articleKind')}<select name="kind" class={field} bind:value={draft.kind}>{#each ['rule', 'howto', 'criteria', 'fact', 'history'] as kind}<option value={kind}>{t('kindName')(kind)}</option>{/each}</select></label>
          <label>{t('scenesTitle')}<input name="scene" class={field} maxlength="48" required bind:value={draft.scene}></label>
        </div>
      </fieldset>
      <p class="m-0 text-[13px] text-muted">{t('articleEditHow')}</p>
      {#if error}<p role="alert" data-article-error class="m-0 text-[14px]">{error}</p>{/if}
      <div class="flex gap-2"><button type="submit" disabled={busy} data-article-save>{busy ? t('articleSaving') : t('articleSave')}</button><button type="button" class="weak" disabled={busy} onclick={cancel} data-article-cancel>{t('articleCancel')}</button></div>
    </form>
  {:else}
    <p class="text-[16px] font-bold wrap-anywhere">{original.title}</p>
    <p class="text-[14px]">{t('articleDeleteHow')}</p>
    {#if error}<p role="alert" data-article-error class="text-[14px]">{error}</p>{/if}
    <div class="flex gap-2"><button type="button" disabled={busy} onclick={save} data-article-delete-confirm>{busy ? t('articleSaving') : t('articleDeleteConfirm')}</button><button type="button" class="weak" disabled={busy} onclick={cancel} data-article-cancel>{t('articleCancel')}</button></div>
  {/if}
</dialog>
