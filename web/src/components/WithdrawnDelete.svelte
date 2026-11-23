<script>
  import { untrack } from 'svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';

  // The check before a person deletes a withdrawn article: it shows what goes and why it was withdrawn, and nothing is sent until the person
  // presses "delete" here a second time. Cancelling changes nothing.
  let { article, oncancel, ondeleted } = $props();
  // The revision shown here is the one sent, so a reload that brings a newer article does not change what the person agreed to delete.
  const shown = untrack(() => ({ id: article.id, revision: article.revision, title: article.title, reason: article.withdrawal?.reason || article.change_reason || '', work: article.withdrawal?.work || [], sources: (article.sources || []).length }));
  let busy = $state(false), error = $state(''), dialog = $state();
  $effect(() => { if (dialog && !dialog.open) { if (dialog.showModal) dialog.showModal(); else dialog.setAttribute('open', ''); } });
  function cancel(event) { event?.preventDefault(); if (!busy) oncancel(); }
  async function remove() {
    if (busy) return;
    busy = true; error = '';
    try { ondeleted(await api.article({ id: shown.id, revision: shown.revision, action: 'delete' })); }
    catch (e) { error = e.status === 409 ? t('withdrawnConflict') : t('error')(e.message); }
    finally { busy = false; }
  }
</script>

<dialog bind:this={dialog} oncancel={cancel} aria-labelledby="withdrawn-delete-title" data-withdrawn-delete-dialog class="m-auto max-h-[90dvh] w-[min(640px,calc(100%-24px))] overflow-y-auto rounded-[14px] border border-line bg-surface p-5 text-fg shadow-card backdrop:bg-black/40">
  <h2 id="withdrawn-delete-title" class="mt-0 text-[20px]">{t('withdrawnDeleteTitle')}</h2>
  <p class="text-[16px] font-bold wrap-anywhere" data-withdrawn-delete-article>{shown.title}</p>
  <div class="k">{t('withdrawnReason')}</div><p class="mt-0 whitespace-pre-wrap wrap-anywhere" data-withdrawn-delete-reason>{shown.reason}</p>
  <div class="k">{t('withdrawnWork')}</div><p class="mt-0 whitespace-pre-wrap wrap-anywhere" data-withdrawn-delete-work>{shown.work.length ? shown.work.join(' / ') : t('withdrawnWorkNone')}</p>
  <div class="k">{t('withdrawnSources')}</div><p class="mt-0" data-withdrawn-delete-sources>{t('withdrawnSourceCount')(shown.sources)}</p>
  <p class="text-[14px] font-bold" data-withdrawn-delete-warn>{t('withdrawnDeleteWarn')}</p>
  {#if error}<p role="alert" data-withdrawn-delete-error class="text-[14px]">{error}</p>{/if}
  <div class="flex gap-2"><button type="button" disabled={busy} onclick={remove} data-withdrawn-delete-confirm>{busy ? t('articleSaving') : t('withdrawnDeleteConfirm')}</button><button type="button" class="weak" disabled={busy} onclick={cancel} data-withdrawn-delete-cancel>{t('withdrawnDeleteCancel')}</button></div>
</dialog>
