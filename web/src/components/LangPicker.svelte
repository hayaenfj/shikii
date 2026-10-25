<script>
  // The language picker. Languages registered in the dictionaries (locales in lib/i18n.js) appear automatically. The chosen value goes to localStorage shikii.lang
  import { i18n, setLang, langOptions, t } from '../lib/i18n.svelte.js';
</script>

<select id="lang" class="max-w-[115px] rounded-[9px] border border-line bg-transparent px-[11px] py-[7px] text-[12px] max-sm:px-[7px] max-sm:py-1.5 [&>option]:bg-surface" aria-label={t('langLabel')} value={i18n.lang} onchange={e => setLang(e.target.value)}>
  {#each langOptions() as o (o.code)}
    <option value={o.code}>{o.name}</option>
  {/each}
</select>
