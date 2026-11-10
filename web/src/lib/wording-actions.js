import { t } from './i18n.svelte.js';
import { load } from './state.svelte.js';
import { api } from './api.js';
import { toast } from './toast.svelte.js';

export async function reviewWording(context, keep) {
  try {
    await api.tuneReview({ ...context(), keep });
    toast(keep ? t('keptToast') : t('undoneToast'), 6000);
    load();
  } catch (e) { toast(t('error')(e.message)); }
}

export async function judgeWording(context, same) {
  try {
    const note = same ? '' : (prompt(t('notePrompt')) || '');
    const res = await api.tuneAdopt({ ...context(), meaning_same: same, note });
    toast(same ? t('adoptedToast')(res.path.split('/').pop()) : t('rejected'), 6000);
    load();
  } catch (e) { toast(t('error')(e.message)); }
}
