<script>
  import { t } from '../lib/i18n.svelte.js';
  let { projects = [] } = $props();
</script>

<section class="projects mx-auto my-[55px] max-w-[1080px] max-sm:mt-8">
  <div class="mb-1.5 flex items-center gap-[9px] text-[12px] font-[650] tracking-[.08em] text-muted">shikii / {t('workspaceTitle')}</div><h2 class="my-2.5 text-[32px]">{t('projectsTitle')}</h2>
  <p class="text-[14px] text-muted">{t('projectsHow')}</p>
  <div class="mt-8 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-5 max-sm:grid-cols-[minmax(0,1fr)]">
  {#each projects as project}
    <a class="project-entry relative block rounded-[15px] border border-line bg-surface p-6 text-fg no-underline shadow-card transition-[border-color,translate] duration-[160ms] hover:-translate-y-[3px] hover:border-accent" href={project.url}>
      <span class="mb-[25px] grid h-[42px] w-[42px] place-items-center rounded-[11px] bg-accentbg text-[27px] text-accent" aria-hidden="true">▱</span><span class="absolute top-[26px] right-6 text-[20px] text-muted" aria-hidden="true">↗</span>
      <strong class="block text-[20px]">{project.name}</strong>
      <small class="mt-1.5 block text-[12px] text-muted wrap-anywhere">{project.path}</small><span class="mt-[25px] flex items-center justify-between border-t border-line pt-[15px] text-[12px] text-accent">{t('openProject')} <span aria-hidden="true">→</span></span>
    </a>
  {:else}
    <p class="empty-state">{t('projectsEmpty')}</p>
  {/each}
  </div>
</section>
