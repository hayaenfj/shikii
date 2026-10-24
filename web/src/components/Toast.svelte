<script>
  // 画面の下の短い知らせ。出す側は lib/toast.svelte.js の toast() を呼ぶ
  import { toastState } from '../lib/toast.svelte.js';
</script>

<div id="toast" class="fixed bottom-6 left-1/2 z-10 max-w-[90vw] -translate-x-1/2 rounded-xl bg-fg px-5 py-3 text-[13px] whitespace-pre-wrap text-bg shadow-[0_10px_40px_#0002] {toastState.shown ? 'show block' : 'hidden'}">{toastState.msg}</div>
