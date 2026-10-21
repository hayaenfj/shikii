<script>
  import { t } from '../lib/i18n.svelte.js';
  let { projects = [] } = $props();
</script>

<section class="projects">
  <div class="panel-eyebrow">shikii / {t('workspaceTitle')}</div><h2>{t('projectsTitle')}</h2>
  <p>{t('projectsHow')}</p>
  <div class="project-grid">
  {#each projects as project}
    <a class="project-entry" href={project.url}>
      <span class="project-folder" aria-hidden="true">▱</span><span class="project-arrow" aria-hidden="true">↗</span>
      <strong>{project.name}</strong>
      <small>{project.path}</small><span class="project-open">{t('openProject')} <span aria-hidden="true">→</span></span>
    </a>
  {:else}
    <p class="empty">{t('projectsEmpty')}</p>
  {/each}
  </div>
</section>
