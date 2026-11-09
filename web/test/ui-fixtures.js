import Knowledge from '../src/components/Knowledge.svelte';
import Projects from '../src/components/Projects.svelte';
import { load, data } from '../src/lib/state.svelte.js';
import { mount, unmount, flushSync } from 'svelte';
import LangPicker from '../src/components/LangPicker.svelte';
import SpecPanel from '../src/components/SpecPanel.svelte';
import Wording from '../src/components/Wording.svelte';
import Notebook from '../src/components/Notebook.svelte';
import Handoff from '../src/components/Handoff.svelte';
import Lessons from '../src/components/Lessons.svelte';
import ItemCard from '../src/components/ItemCard.svelte';
import Answer from '../src/components/Answer.svelte';
import WordingAsk from '../src/components/WordingAsk.svelte';
import AiView from '../src/components/AiView.svelte';
import App from '../src/App.svelte';
import { i18n, t } from '../src/lib/i18n.svelte.js';
import { ui, skipped, wordingNeedsPerson } from '../src/lib/state.svelte.js';
import Toast from '../src/components/Toast.svelte';
import { toastState } from '../src/lib/toast.svelte.js';




export function loadingSpecs() {
  return ['needs-plan', 'done-as-asked', 'prompt-kind', 'changes-in-scope'].map(id => ({ id, type: 'noul', titles: {}, questions: {}, keys: [], min_n: 50, records: 2, labels: 0, unlabeled: 1, waiting_for_person: 1, person_answers: true }));
}


// every question a person answers has its way of asking fetched exactly once per load (one request per question, not per card)
export const tunesOnce = (calls, ids) => ids.every(id => calls.filter(u => u === 'v1/tune?spec=' + id).length === 1) && calls.filter(u => u.startsWith('v1/tune')).length === ids.length;


// Audit B (0.8.1, Tailwind): do not stack utilities on the same element that set the same property to different values under the same variant.
// Tailwind v4 generates utilities in "a fixed order per property → name order", and the order in the class attribute does not decide which wins
// (e.g. bg-transparent is generated after bg-accentbg and bg-surface, so even when added conditionally, transparent always wins).
// Builds a table of class → property from the utilities layer of the shipped CSS (dist/assets/index.css), and looks at every classList of the rendered elements.
// Different variants (max-sm: / hover: / [&>span]: …) are meant to override by width or state, so they are not checked. Values derived from --tw-* are not checked either
export function utilityTable(css) {
  const table = new Map();
  const at = css.indexOf('@layer utilities {');
  if (at < 0) return table;
  const re = /\n\s*(\.[^{}]*?)\s*\{\n([^{}]*?)\n\s*\}/g;
  let m;
  while ((m = re.exec(css.slice(at)))) {
    const decls = m[2].split('\n').map(l => l.trim().replace(/;$/, '')).filter(l => l.includes(':') && !l.includes('var(--tw-'));
    for (const sel of m[1].split(/,\s*(?=\.)/)) {
      let i = 1, name = '';
      while (i < sel.length) {
        const c = sel[i];
        if (c === '\\') { name += sel[i + 1]; i += 2; continue; }
        if (/[\s.:>[,]/.test(c)) break;
        name += c; i++;
      }
      if (sel.slice(i).trim() && !name.includes(':')) continue; // compound rules such as .pill.calibrated and .empty-state p
      const props = table.get(name) || new Map();
      for (const d of decls) { const k = d.indexOf(':'); props.set(d.slice(0, k).trim(), d.slice(k + 1).trim()); }
      table.set(name, props);
    }
  }
  return table;
}

export function variantPrefix(name) {
  let depth = 0, last = -1;
  for (let i = 0; i < name.length; i++) { const c = name[i]; if (c === '[' || c === '(') depth++; else if (c === ']' || c === ')') depth--; else if (c === ':' && depth === 0) last = i; }
  return last < 0 ? '' : name.slice(0, last);
}

export function collectConflicts(target, table, out) {
  for (const el of target.querySelectorAll('[class]')) {
    const names = [...el.classList].filter(n => table.has(n));
    for (let a = 0; a < names.length; a++) for (let b = a + 1; b < names.length; b++) {
      if (variantPrefix(names[a]) !== variantPrefix(names[b])) continue;
      for (const [prop, va] of table.get(names[a])) {
        const vb = table.get(names[b]).get(prop);
        if (vb !== undefined && vb !== va) out.add(el.tagName.toLowerCase() + ': ' + names[a] + ' and ' + names[b] + ' (' + prop + ')');
      }
    }
  }
}


// ---- audit 0.9.2 (ui) ----

// The utilities of the shipped CSS with the media query each one is under, in CSS order: [{ name, media: [..], decls: Map }]. Only rules for one class (what the markup's classes produce)
export function mediaUtilities(css) {
  const out = [];
  const at = css.indexOf('@layer utilities {');
  if (at < 0) return out;
  const stack = [];
  let i = at + '@layer utilities {'.length, head = '';
  for (let depth = 1; i < css.length && depth > 0; i++) {
    const c = css[i];
    if (c === '{') {
      const sel = head.trim(); head = '';
      if (sel.startsWith('@')) { stack.push(sel); depth++; continue; }
      const end = css.indexOf('}', i);
      const body = css.slice(i + 1, end);
      if (body.includes('{')) { stack.push(sel); depth++; continue; } // nested (e.g. &:hover) — not one class
      const decls = new Map();
      for (const d of body.split(';')) { const k = d.indexOf(':'); if (k > 0) decls.set(d.slice(0, k).trim(), d.slice(k + 1).trim()); }
      for (const one of sel.split(/,\s*/)) {
        const m = one.match(/^\.((?:\\.|[^\s.:>[,\\])+)$/);
        if (m) out.push({ name: m[1].replace(/\\(.)/g, '$1'), media: stack.filter(s => s.startsWith('@media')), decls });
      }
      i = end;
    } else if (c === '}') { depth--; stack.pop(); head = ''; }
    else head += c;
  }
  return out;
}

// whether a media query holds at a width (light colors, a pointer without hover, motion allowed)
export function mediaHolds(q, width) {
  let m;
  if ((m = q.match(/^@media not all and \(min-width: (\d+)px\)$/))) return width < Number(m[1]);
  if ((m = q.match(/^@media \(min-width: (\d+)px\)$/))) return width >= Number(m[1]);
  return false;
}

// The properties the utilities on an element give it at a width (later rules win, as in the shipped CSS)
export function styleAt(rules, el, width) {
  const style = new Map(), names = new Set(el.classList);
  for (const r of rules) if (names.has(r.name) && r.media.every(q => mediaHolds(q, width))) for (const [k, v] of r.decls) style.set(k, v);
  return style;
}

export const shownAt = (rules, el, width) => { for (let e = el; e && e.nodeType === 1; e = e.parentElement) if (styleAt(rules, e, width).get('display') === 'none') return false; return true; };
