<script>
  // 作業記録(.shikii/handoff/、0.8.1): 途中で書き換える作業ファイル(監査の報告、リリースの手順と記録、差分、画像)を読むだけで見せる。
  // フォルダごとに畳み、ファイルを押すと中身(文字は文字のまま、画像は画像)。書くのはエージェント(shikii handoff / MCP の handoff_*)
  import { t, fmtDateTime } from '../lib/i18n.svelte.js';
  import { api } from '../lib/api.js';

  let { files = [], error = false } = $props();
  let open = $state(new Set());
  let shown = $state(null); // { path, kind, text?, error? }
  let request = 0;

  // 先頭のフォルダでまとめる(フォルダの無いファイルは「直下」)。中は更新の新しい順(サーバーの順)
  let groups = $derived.by(() => {
    const m = new Map();
    for (const f of files) {
      const i = f.path.indexOf('/');
      const g = i > 0 ? f.path.slice(0, i) : '';
      if (!m.has(g)) m.set(g, []);
      m.get(g).push(f);
    }
    return [...m.entries()].map(([name, list]) => ({ name, list }));
  });
  const url = p => api.handoffFile(p);
  const size = n => n < 1024 ? n + ' B' : n < 1048576 ? (n / 1024).toFixed(1) + ' KB' : (n / 1048576).toFixed(1) + ' MB';
  function toggle(name) { const next = new Set(open); if (next.has(name)) next.delete(name); else next.add(name); open = next; }

  async function view(f) {
    const mine = ++request;
    if (f.kind !== 'text') { shown = { path: f.path, kind: f.kind }; return; }
    shown = { path: f.path, kind: 'text', text: null };
    try {
      const text = await api.handoffText(f.path);
      if (mine === request) shown = { path: f.path, kind: 'text', text };
    } catch (e) { if (mine === request) shown = { path: f.path, kind: 'text', error: e.message }; }
  }
</script>

<div class="handoff my-6 rounded-[14px] border border-line bg-surface p-5 max-sm:p-[15px]" data-handoff>
  <h3 class="mt-0 mb-2 text-[15px] font-bold">{t('handoffTitle')} <span class="pill">{t('handoffCount')(files.length)}</span></h3>
  <p class="k mt-0">{t('handoffHow')}</p>
  {#if error}<p class="audit">{t('handoffError')}</p>
  {:else if !files.length}<p class="k" data-handoff-empty>{t('handoffEmpty')}</p>
  {:else}
    <ul class="m-0 list-none p-0 [&_li]:my-[.15rem]">
      {#each groups as g (g.name)}
        <li data-handoff-group={g.name}>
          <button type="button" class="weak handoff-toggle font-semibold" aria-expanded={open.has(g.name)} onclick={() => toggle(g.name)}><span aria-hidden="true">{open.has(g.name) ? '▾' : '▸'}</span> {g.name || t('handoffRoot')} <span class="badge inline-block rounded-md bg-amberbg px-[7px] py-px text-[10px] text-amber tabular-nums max-sm:px-[5px] max-sm:text-[9px]">{g.list.length}</span></button>
          {#if open.has(g.name)}
            <ul class="mt-[.2rem] mb-[.6rem] list-none pl-[1.4rem]">
              {#each g.list as f (f.path)}
                <li><button type="button" class="weak text-left {shown?.path === f.path ? 'text-accent' : ''}" data-handoff-file={f.path} aria-pressed={shown?.path === f.path} onclick={() => view(f)}>{g.name ? f.path.slice(g.name.length + 1) : f.path}</button> <span class="k">{size(f.size)} · {fmtDateTime(f.modified)}</span></li>
              {/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
    {#if shown}
      <div class="mt-3 rounded-[11px] border border-line bg-surface2 p-[15px]" data-handoff-view={shown.path}>
        <div class="mb-2 flex flex-wrap items-center gap-2 text-[11px] text-muted"><strong class="text-fg wrap-anywhere">{shown.path}</strong><a href={url(shown.path)} target="_blank" rel="noopener">{t('handoffOpenRaw')}</a></div>
        {#if shown.kind === 'image'}<img class="max-w-full rounded-lg border border-line" src={url(shown.path)} alt={shown.path}>
        {:else if shown.kind === 'text'}
          {#if shown.error}<p class="audit">{t('error')(shown.error)}</p>
          {:else if shown.text === null}<p class="k" role="status">{t('loading')}</p>
          {:else}<pre class="m-0 max-h-[70vh] overflow-auto text-[12px] text-fg">{shown.text}</pre>{/if}
        {:else}<p class="k">{t('handoffOther')}</p>{/if}
      </div>
    {/if}
  {/if}
</div>
