<script>
  // Settings: what a person chooses for this project. Today one setting, the AI this project's decisions use (the same choice as
  // shikii init --model, kept in .shikii/project.json). It comes before SHIKII_MODEL and before TYPESAFE_API_KEY: a project set to
  // the local model is never sent to Jev, even with a key.
  import { t } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';
  import { load } from '../lib/state.svelte.js';
  import { toast } from '../lib/toast.svelte.js';
  // This project's choice of model (the same as shikii init --model): '' follows SHIKII_MODEL, 'hosted' is Jev, 'local' the local model
  let saving = $state(false);
  async function choose(choice) {
    if (saving || (model?.choice || '') === choice) return;
    saving = true;
    try { await api.setModel(choice); toast(t('modelSaved')); load(); } catch (e) { toast(t('modelFailed')(e.message)); } finally { saving = false; }
  }
  const choiceButton = on => 'rounded-lg border px-3 py-1.5 text-[13px] ' + (on ? 'border-accent bg-accentbg font-semibold text-fg' : 'border-line bg-surface hover:bg-surface2');
  let { st } = $props();
  let model = $derived(st.model);
  const head = 'section-head mt-0 mb-3 flex items-center gap-2 rounded-lg bg-surface2 px-2.5 py-2 text-[13px] font-bold tracking-[.02em] text-fg before:block before:h-3.5 before:w-1 before:rounded-sm before:bg-accent';
</script>

<section data-settings>
  <h2 class="mb-1 text-[25px] max-md:text-[23px]">{t('settingsTitle')}</h2>
  <p class="page-purpose mt-0 mb-5 text-[14px] text-muted">{t('settingsHow')}</p>
  <section class="rounded-[14px] border border-line bg-surface p-4 text-[13px] max-sm:p-3 [&_p]:m-0 [&_p]:mb-1.5">
    <h3 class={head}>{t('modelChoiceLabel')}</h3>
    <p>{t('summaryModel')(model?.name || '', model?.endpoint || '')}</p>
    <div class="my-2" role="radiogroup" aria-label={t('modelChoiceLabel')} data-model-choice>
      <p class="text-muted">{t('modelChoiceWins')}</p>
      <div class="flex flex-wrap gap-2">
        {#each ['', 'hosted', 'local'] as c (c)}
          <button type="button" role="radio" aria-checked={(model?.choice || '') === c} data-model={c || 'default'} class={choiceButton((model?.choice || '') === c)} disabled={saving} onclick={() => choose(c)}>{t(c === 'hosted' ? 'modelChoiceHosted' : c === 'local' ? 'modelChoiceLocal' : 'modelChoiceDefault')}</button>
        {/each}
      </div>
      {#if model?.choice === 'hosted' && !model?.has_key}<p class="mt-1 text-amber" data-model-no-key>{t('modelNoKey')}</p>{/if}
      {#if model?.choice === 'local'}<p class="mt-1 text-muted">{t('modelLocalNote')}</p>{/if}
    </div>
  </section>
</section>
