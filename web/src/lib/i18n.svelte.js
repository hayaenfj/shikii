// The current language (reactive state) and t(), which looks up the page's text. Switching the language redraws everything that reads t() (no reload).
// Do not branch on language inside text: all text goes through t(key) or t(key)(args). Dates and numbers pass lang to fmt*
import { locales, resolveLang, translate, localized, personQuestion, personTitle } from './i18n.js';
import { read, write } from './prefs.js';

export const i18n = $state({ lang: resolveLang(read('shikii.lang'), typeof navigator !== 'undefined' ? navigator.language : '') });

export function setLang(code) {
  if (!locales[code]) return;
  i18n.lang = code;
  write('shikii.lang', code);
}

// The page's text. Keys whose value is a function are used as t(key)(args)
export function t(key) {
  return translate(i18n.lang, key);
}

// The spec's display (titles / questions / labels). If missing, en; if that is missing too, the raw value
export function loc(byLang, raw) {
  return localized(i18n.lang, byLang, raw);
}

// Showing dates and times. Pass lang to use that language's format
export function fmtDateTime(x) {
  return new Date(x).toLocaleString(i18n.lang);
}

// What the page asks a person and the short title (the pure versions are in i18n.js)
export function askOf(sp) { return personQuestion(i18n.lang, sp); }
export function titleOf(sp) { return personTitle(i18n.lang, sp); }

// The list shown in the language picker (grows automatically when a dictionary is added)
export function langOptions() {
  return Object.keys(locales).map(code => ({ code, name: translate(code, 'langName') }));
}
