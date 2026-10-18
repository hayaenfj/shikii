// 辞書の一覧と、言語に依らない純粋な関数。画面(Svelte)からも、鍵の揃いを検査する scripts/check-locales.mjs(Node)からも読む。
// 言語を足す: src/locales/<code>.js を 1 つ作り(鍵は en.js と同じ)、下の locales に 1 行足す。言語の選択には自動で並ぶ。
import en from '../locales/en.js';
import ja from '../locales/ja.js';

export const locales = { en, ja };

// 鍵が辞書に無い時に落ちる先。鍵の一覧の元でもある
export const fallback = 'en';

// 保存値 → ブラウザの言語の先頭 2 文字 → fallback。辞書に無い言語は fallback に落とす
export function resolveLang(saved, navigatorLanguage) {
  const cand = saved || (navigatorLanguage || fallback).slice(0, 2);
  return locales[cand] ? cand : fallback;
}

// 鍵 → 文(または文を返す関数)。その言語に無ければ fallback の辞書から。どこにも無ければ鍵そのまま(画面が空にならないように)
export function translate(lang, key) {
  const d = locales[lang] || locales[fallback];
  if (key in d) return d[key];
  if (key in locales[fallback]) return locales[fallback][key];
  return key;
}

// spec 側の表示用の訳(display.<lang> の titles / questions / labels)。lang → fallback → 生の値(id や label)の順に落ちる
export function localized(lang, byLang, raw) {
  if (byLang && byLang[lang]) return byLang[lang];
  if (byLang && byLang[fallback]) return byLang[fallback];
  return raw;
}
