// 今の言語(反応する状態)と、画面の文を引く t()。言語を切り替えると t() を読んでいる所が全部描き直る(再読み込み無し)。
// 文中で言語を分岐しない: 文は全部 t(key) か t(key)(引数) を通す。日付・数は fmt* に lang を渡す
import { locales, resolveLang, translate, localized } from './i18n.js';
import { read, write } from './prefs.js';

export const i18n = $state({ lang: resolveLang(read('shikii.lang'), typeof navigator !== 'undefined' ? navigator.language : '') });

export function setLang(code) {
  if (!locales[code]) return;
  i18n.lang = code;
  write('shikii.lang', code);
}

// 画面の文。値が関数の鍵は t(key)(args) で使う
export function t(key) {
  return translate(i18n.lang, key);
}

// spec 側の表示(titles / questions / labels)。無ければ en、それも無ければ生の値
export function loc(byLang, raw) {
  return localized(i18n.lang, byLang, raw);
}

// 日時の表示。lang を渡して、その言語の書式にする
export function fmtDateTime(x) {
  return new Date(x).toLocaleString(i18n.lang);
}

// 言語の選択に並べる一覧(辞書を足せば自動で増える)
export function langOptions() {
  return Object.keys(locales).map(code => ({ code, name: translate(code, 'langName') }));
}
