<script>
  import { t } from '../lib/i18n.svelte.js';
  let { projects = [] } = $props();
</script>

<section class="projects">
  <h2>{t('projectsTitle')}</h2>
  <p>{t('projectsHow')}</p>
  {#each projects as project}
    <a class="project-entry" href={project.url}>
      <strong>{project.name}</strong>
      <small>{project.path}</small>
    </a>
  {:else}
    <p class="empty">{t('projectsEmpty')}</p>
  {/each}
</section>

<style>
  .projects { max-width: 800px; margin: 24px auto; }
  .project-entry { display: block; padding: 18px; margin: 12px 0; border: 1px solid var(--border, #ddd); border-radius: 8px; text-decoration: none; color: inherit; }
  .project-entry:hover { background: var(--bg2, #f5f5f5); }
  small { display: block; overflow-wrap: anywhere; margin-top: 6px; opacity: .7; }
</style>
