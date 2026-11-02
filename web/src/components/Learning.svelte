<script>
  import { t } from '../lib/i18n.svelte.js';
  let { learning } = $props();
</script>

{#if learning}
  <section class="my-3 grid gap-3 wrap-anywhere" data-article-learning aria-label={t('learningTitle')}>
    <h4 class="m-0">{t('learningTitle')}</h4>
    {#if learning.connections?.length}
      <div><div class="k">{t('learningConnections')}</div>
        {#each learning.connections as connection}
          <blockquote class="my-2 border-l-2 border-line pl-3">
            <p class="my-1 whitespace-pre-wrap">{connection.quote}</p>
            <p class="k my-1 whitespace-pre-wrap">{connection.conditions}</p>
            <p class="my-1 whitespace-pre-wrap">{connection.reason}</p>
            <div class="k">{t('learningRelation')(connection.relation)} · {connection.id} @ {connection.revision}</div>
          </blockquote>
        {/each}
      </div>
    {/if}
    {#if learning.probe}
      <div><div class="k">{t('learningProbe')}</div>
        <p class="my-1 whitespace-pre-wrap">{learning.probe.question}</p>
        <p class="my-1 whitespace-pre-wrap">{learning.probe.method}</p>
        <p class="k my-1 whitespace-pre-wrap">{t('learningReconsider')}: {learning.probe.reconsider_when}</p>
        {#each learning.probe.results || [] as result}
          <div class="my-2 rounded-lg border border-line p-2">
            <div class="k">{t('learningAssessment')(result.assessment)}</div>
            <p class="k my-1 whitespace-pre-wrap">{result.conditions}</p>
            <p class="my-1 whitespace-pre-wrap">{result.conclusion}</p>
            <div class="k">{t('learningSources')}: {result.sources.join(', ')}</div>
          </div>
        {/each}
      </div>
    {/if}
    {#if learning.practice}
      <div><div class="k">{t('learningPractice')}</div>
        <ol class="my-1 pl-5">{#each learning.practice.steps as step}
          <li class="my-2">
            <p class="k my-1 whitespace-pre-wrap">{step.condition}</p>
            <p class="my-1 whitespace-pre-wrap">{step.action}</p>
            <p class="my-1 whitespace-pre-wrap">{t('learningAvoid')}: {step.avoid}</p>
            <p class="my-1 whitespace-pre-wrap">{t('learningCheck')}: {step.check}</p>
            <div class="k">{t('learningSources')}: {step.sources.join(', ')}</div>
          </li>
        {/each}</ol>
      </div>
    {/if}
  </section>
{/if}
