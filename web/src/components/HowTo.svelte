<script>
  // "How to use this page" (4 steps), shown first. Closing it with "Got it" hides it from next time (localStorage shikii.help). The ? at the top right shows it again
  import { t } from '../lib/i18n.svelte.js';
  import { setShowHelp } from '../lib/state.svelte.js';
</script>

<details class="howto mb-5 rounded-xl border border-line bg-surface px-4 text-[12px] text-muted max-md:mb-[22px] [&[open]>summary]:border-b [&[open]>summary]:border-line">
  <summary class="flex cursor-pointer list-none items-center gap-4 py-3 before:grid before:h-[23px] before:w-[23px] before:place-items-center before:rounded-full before:border before:border-line before:text-accent before:content-['?'] [&::-webkit-details-marker]:hidden"><b class="font-semibold text-fg">{t('howToTitle')}</b><span class="ml-auto text-[11px] max-sm:hidden">{t('howToShort')}</span></summary><div class="pt-2.5 pb-3.5">
  <ol class="mt-1 mb-3.5 pl-5 [&>li]:my-[5px]">
    {#each t('howTo') as step}
      <li>{step}</li>
    {/each}
  </ol>
  <button class="weak" type="button" onclick={() => setShowHelp(false)}>{t('gotIt')}</button>
</div></details>
