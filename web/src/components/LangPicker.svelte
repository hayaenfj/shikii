<script>
  // 言語の選択。辞書(lib/i18n.js の locales)に登録した言語が自動で並ぶ。選んだ値は localStorage shikii.lang
  import { i18n, setLang, langOptions } from '../lib/i18n.svelte.js';
</script>

<select id="lang" aria-label="language" value={i18n.lang} onchange={e => setLang(e.target.value)}>
  {#each langOptions() as o (o.code)}
    <option value={o.code}>{o.name}</option>
  {/each}
</select>
