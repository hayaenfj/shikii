import { mount, unmount, flushSync } from 'svelte';
import App from '../src/App.svelte';
import { data, ui, skipped } from '../src/lib/state.svelte.js';
import { i18n, t } from '../src/lib/i18n.svelte.js';
import { toastState } from '../src/lib/toast.svelte.js';

export async function checkAnswerTargetLifetime(target) {
  const realFetch = globalThis.fetch, state = data.state, saved = { ...ui }, lang = i18n.lang;
  const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync(); };
  const results = [];
  i18n.lang = 'en';
  try {
    for (const selection of ['', 'answer']) {
      for (const count of [1, 2]) {
        for (const calibrated of [false, true]) {
          const ids = ['needs-plan', 'done-as-asked'].slice(0, count), answered = new Set(), calls = [];
          let statusReads = 0;
          const item = id => ({ trace_id: 'synthetic-' + id, band: 'ask', top: 'yes', p: 0.6, inputs: { request: id }, answerable: true, waits_for_person: true, choices: [{ truth: 'yes', label: 'Yes' }, { truth: 'no', label: 'No' }] });
          const specs = () => ids.map(id => ({ id, type: 'noul', titles: {}, questions: {}, person_answers: true, records: 1, labels: answered.has(id) ? 1 : 0, unlabeled: answered.has(id) ? 0 : 1, waiting_for_person: answered.has(id) ? 0 : 1 }));
          globalThis.fetch = async (url, opts) => {
            let response = null;
            if (url === 'v1/status') { statusReads++; response = { specs: specs() }; }
            else if (url.startsWith('v1/queue')) {
              const id = new URL(url, 'http://localhost/').searchParams.get('spec');
              response = { items: answered.has(id) ? [] : [item(id)] };
            } else if (url === 'v1/label') {
              const body = JSON.parse(opts.body); calls.push(body); answered.add(body.spec);
              response = { truth: body.truth, calibrated };
            }
            return { ok: true, json: async () => response };
          };
          data.state = null; Object.assign(ui, { page: selection, tab: '', done: false, pinned: '' }); skipped.clear();
          let app;
          try {
            app = mount(App, { target }); await settle(); await settle();
            for (let i = 0; i < ids.length; i++) {
              target.querySelector('.item button[data-truth="yes"]')?.click(); await settle(); await settle();
              const correctCounts = data.state.specs.every((sp, j) => sp.labels === (j <= i ? 1 : 0) && sp.unlabeled === (j <= i ? 0 : 1) && sp.waiting_for_person === (j <= i ? 0 : 1));
              const next = i + 1 < ids.length;
              const screen = next ? target.querySelector('.item')?.textContent.includes(ids[i + 1]) && target.querySelector('[data-answer-left]')?.textContent.includes('1') : !!target.querySelector('[data-answer-empty]') && !target.querySelector('.item') && !target.querySelector('[data-answer-left]') && !target.querySelector('[data-page="answer"] .badge');
              const message = calibrated ? toastState.msg.startsWith(t('calibratedToast')) : toastState.msg === t('done')('yes');
              results.push(correctCounts && screen && message && calls.length === i + 1 && calls[i].spec === ids[i] && calls[i].trace_id === 'synthetic-' + ids[i] && (!calibrated || statusReads > i + 1));
            }
          } finally { if (app) unmount(app); }
        }
      }
    }
  } finally {
    globalThis.fetch = realFetch; data.state = state; Object.assign(ui, saved); i18n.lang = lang; skipped.clear();
  }
  const ok = results.every(Boolean);
  console.log((ok ? 'check-ui: ok' : 'check-ui: FAIL') + ' (answer target stays fixed through queue removal, next spec, final card and calibrated reload: ' + JSON.stringify(results) + ')');
  return ok;
}
