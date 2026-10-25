// The list of dictionaries, and pure functions that do not depend on the language. Read both by the page (Svelte) and by scripts/check-locales.mjs (Node), which checks that the keys line up.
// To add a language: create one src/locales/<code>.js (with the same keys as en.js) and add one line to locales below. It appears in the language picker automatically.
import en from '../locales/en.js';
import ja from '../locales/ja.js';

export const locales = { en, ja };

// Where it falls back when a key is not in a dictionary. Also the source of the list of keys
export const fallback = 'en';

// Saved value → the first 2 letters of the browser's language → fallback. A language with no dictionary falls back
export function resolveLang(saved, navigatorLanguage) {
  const cand = saved || (navigatorLanguage || fallback).slice(0, 2);
  return locales[cand] ? cand : fallback;
}

// Key → text (or a function that returns text). If the language lacks it, from the fallback dictionary. If nowhere, the key itself (so the page is never blank)
export function translate(lang, key) {
  const d = locales[lang] || locales[fallback];
  if (key in d) return d[key];
  if (key in locales[fallback]) return locales[fallback][key];
  return key;
}

// The spec's translations for display (titles / questions / labels in display.<lang>). Falls back in the order lang → fallback → the raw value (id or label)
export function localized(lang, byLang, raw) {
  if (byLang && byLang[lang]) return byLang[lang];
  if (byLang && byLang[fallback]) return byLang[fallback];
  return raw;
}

// What the page asks a person (0.9.2): the display question in the language, then the display title (in the language, en, or any).
// The question: field is the prompt for the decision model, so it is shown only when the spec has no display at all (then it is the only text anyone wrote)
const firstOf = o => Object.values(o || {}).find(Boolean);
export function personQuestion(lang, sp) {
  const q = sp.questions || {}, ti = sp.titles || {};
  return q[lang] || ti[lang] || q[fallback] || ti[fallback] || firstOf(ti) || firstOf(q) || sp.question || sp.id;
}
// The short title of a question ('' when the spec has none)
export function personTitle(lang, sp) {
  const ti = sp.titles || {};
  return ti[lang] || ti[fallback] || firstOf(ti) || '';
}
