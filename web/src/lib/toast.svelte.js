// A short notice at the bottom of the page. toast(text, milliseconds). When a new notice comes, the previous timer is dropped
export const toastState = $state({ msg: '', shown: false });
let timer;

export function toast(msg, ms = 4000) {
  toastState.msg = msg;
  toastState.shown = true;
  clearTimeout(timer);
  timer = setTimeout(() => { toastState.shown = false; }, ms);
}
