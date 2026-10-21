<script>
  // 最初に出る「この画面の使い方」(4 手順)。「わかった」で閉じると次回から出ない(localStorage shikii.help)。右上の ? で再表示
  import { t } from '../lib/i18n.svelte.js';
  import { setShowHelp } from '../lib/state.svelte.js';
</script>

<details class="howto">
  <summary><b>{t('howToTitle')}</b><span>{t('howToShort')}</span></summary><div class="howto-body">
  <ol>
    {#each t('howTo') as step}
      <li>{step}</li>
    {/each}
  </ol>
  <button class="weak" type="button" onclick={() => setShowHelp(false)}>{t('gotIt')}</button>
</div></details>
