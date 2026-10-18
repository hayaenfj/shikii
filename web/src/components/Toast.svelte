<script>
  // 画面の下の短い知らせ。出す側は lib/toast.svelte.js の toast() を呼ぶ
  import { toastState } from '../lib/toast.svelte.js';
</script>

<div id="toast" class:show={toastState.shown}>{toastState.msg}</div>
