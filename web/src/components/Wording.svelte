<script>
  // 聞き方の欄(くわしく): 最新の tune の結果、候補の表、勝った候補と今の文言の並べ比べ、人の「意味は同じ / 違う」、候補を測るボタン。
  // 数字は機械が確かめる。意味が同じかは人が確かめる(採用・却下は人だけ)
  import { t } from '../lib/i18n.svelte.js';
  import { load } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { sp } = $props();
  let tu = $derived(sp.tune);
  let latest = $derived(tu && tu.latest);
  let showDiff = $derived(!!(latest && latest.adoptable && !latest.adopted && latest.winner_wording));

  async function run(propose) {
    try { await api.tuneRun({ spec: sp.id, propose }); toast(t('running')); load(); }
    catch (e) { toast(t('error')(e.message)); }
  }

  async function judge(same) {
    try {
      const note = same ? '' : (prompt(t('notePrompt')) || '');
      const res = await api.tuneAdopt({ spec: sp.id, run: latest.run, meaning_same: same, note });
      toast(same ? t('adoptedToast')(res.path.split('/').pop()) : t('rejected'), 6000);
      load();
    } catch (e) { toast(t('error')(e.message)); }
  }
</script>

{#snippet options(opts, base)}
  {#if opts}
    <div class="opts">
      {#each Object.entries(opts) as [id, d]}
        <div class:changed={base && base[id] !== d}><span class="k">{id}</span> {d}</div>
      {/each}
    </div>
  {/if}
{/snippet}

{#snippet steps(list)}
  {#if list && list.length}
    <div class="opts">
      {#each list as s, i}
        <div class="changed"><span class="k">{i + 1}.</span> {s.ask}{#if s.yes}{' '}<span class="k">{t('yesTo')}</span>{s.yes}{/if}{#if s.no}{' '}<span class="k">{t('noTo')}</span>{s.no}{/if}</div>
      {/each}
    </div>
  {/if}
{/snippet}

<details class="wording adv" open={showDiff}>
  <summary>
    <b>{t('wording')}</b>
    {#if tu && tu.running}<span class="pill ready">{t('running')}</span>
    {:else if latest && latest.adopted}<span class="pill calibrated">{t('adoptedLine')(latest.adopted_path.split('/').pop())}</span>
    {:else if latest && latest.adoptable}<span class="pill ready">{t('winnerLine')(latest.winner)}</span>
    {:else if latest}<span class="pill">{t('keptLine')}</span>
    {:else}<span class="pill">{t('noRun')}</span>{/if}
  </summary>
  <div class="wbody">
    {#if latest}
      <div class="meta"><span>{latest.model}</span><span>{t('truthsCount')(latest.truths)}</span><span>{latest.at.slice(0, 16).replace('T', ' ')}</span></div>
      <table class="cands">
        <tbody>
          <tr>{#each t('tableHead') as h}<th>{h}</th>{/each}<th></th></tr>
          {#each latest.candidates as c}
            <tr class:win={c.name === latest.winner && latest.winner !== 'current'}>
              <td>{c.name}</td><td>{c.top1.toFixed(3)}</td><td>{c.brier.toFixed(4)}</td><td>{c.prec_at_act.toFixed(3)}</td><td>{c.rec_at_act.toFixed(3)}</td><td class="k">{c.change}</td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if latest.have_holdout}<div class="k">{t('holdoutLine')(latest.holdout.toFixed(3), latest.candidates[0].top1.toFixed(3))}</div>{/if}
      <div class="k">{latest.decision}</div>
      {#if showDiff}
        {@const w = latest.winner_wording}
        {@const cur = latest.current_wording || {}}
        <div class="diff">
          <div><div class="k">{t('current')}</div><div class="text">{cur.question || ''}</div>{@render options(cur.options)}</div>
          <div><div class="k">{latest.winner}</div><div class="text">{w.question || ''}</div>{@render options(w.options, cur.options)}{@render steps(w.steps)}</div>
        </div>
        <div class="k">{t('checkHint')}</div>
        <div class="btns"><button type="button" class="top" onclick={() => judge(true)}>{t('sameMeaning')}</button><button type="button" class="weak" onclick={() => judge(false)}>{t('changedMeaning')}</button></div>
      {/if}
    {/if}
    {#if tu && tu.error}<div class="k">{t('error')(tu.error)}</div>{/if}
    {#if tu && !tu.running}
      <div class="btns">
        <button type="button" onclick={() => run(0)}>{t('runTune')}</button>
        {#if tu.proposer}<button type="button" onclick={() => run(3)}>{t('runPropose')(3)}</button>{/if}
      </div>
    {/if}
  </div>
</details>
