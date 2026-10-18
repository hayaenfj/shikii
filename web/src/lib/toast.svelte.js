// 画面の下に出る短い知らせ。toast(文, ミリ秒)。新しい知らせが来たら前のタイマーは捨てる
export const toastState = $state({ msg: '', shown: false });
let timer;

export function toast(msg, ms = 4000) {
  toastState.msg = msg;
  toastState.shown = true;
  clearTimeout(timer);
  timer = setTimeout(() => { toastState.shown = false; }, ms);
}
