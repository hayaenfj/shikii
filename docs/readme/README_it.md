[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · **Italiano** · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: trasformare il «probabilmente» di un'IA in un «si può agire» su cui far girare un'attività

### Uno strumento per lasciare che un'IA decida sì/no, quale o quanto, e per mantenere quella decisione affidabile nel lavoro di ogni giorno. Lei controlla alcune risposte e, ogni tanto, conferma «è ancora quello che intendevo?». Il resto lo fa la macchina.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-191%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Il problema](#il-problema) · [Cosa fa shikii](#cosa-fa-shikii) · [Cosa fa lei](#cosa-fa-lei) · [Installazione](#installazione) · [Le vie più brevi](#le-vie-più-brevi) · [Un giro completo](#un-giro-completo) · [Cosa c'è di nuovo](#cosa-cè-di-nuovo) · [Per saperne di più](#per-saperne-di-più)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: un modello le dà una probabilità, shikii le dice se può agire in base a quella" width="100%"></p>

## Il problema

Chieda a un'IA «questa richiesta riguarda la stessa pratica della precedente?» e risponde «probabilmente, 0,9». Per il lavoro vero non basta.

- Quel 0,9 è davvero giusto 9 volte su 10?
- Dal giorno in cui ha messo un modello nuovo, ha iniziato a mentire in silenzio?
- Se cambia un po' la formulazione, come si sposta la precisione?
- Le decisioni che ha preso da sola, senza nessuno a guardare: sono ancora giuste?

Se una persona deve controllare ogni risposta, usare l'IA aveva poco senso.

## Cosa fa shikii

1. **Raccoglie le sue correzioni e traccia la linea sopra la quale l'IA può agire da sola.** Dalle risposte che lei ha dato («questo era davvero vero») misura come la fiducia dell'IA si lega alla frequenza con cui ha ragione, e decide da dove in su ci si può fidare. Finché non esiste una linea, niente agisce. Quando esiste, ogni decisione automatica porta una ricevuta che dice su quante risposte controllate si regge la linea e quante volte erano giuste
2. **Prova altri modi di chiedere e tiene il migliore.** Sulle stesse correzioni dà un punteggio a domande alternative, descrizioni delle opzioni e disposizioni del prompt, e ne adotta una solo quando è davvero migliore. Lei conferma soltanto che il significato non è cambiato
3. **Le lascia cambiare modello senza ricominciare da capo.** Le correzioni vivono separate dal modello, quindi un modello nuovo le riusa e la macchina ritraccia la linea. Nessun lavoro umano
4. **Conserva ciò che ha imparato in un wiki.** «Chiedere così funziona» si accumula come affermazioni con i numeri misurati, da riprovare su un altro modello. Ciò che altre persone hanno misurato sui loro modelli si aggiunge alla stessa pagina

## Cosa fa lei

- **Scrive cosa vuole far decidere.** Risponde a poche domande e compare un file di decisione (`shikii new`, in modo interattivo o con `--type --question --option --input --pii`; il formato completo è nel [riferimento del file di decisione](../../skills/shikii/references/spec.md))
- **Controlla le risposte.** La pagina mostra le decisioni su cui l'IA era incerta, più 1 su 20 di quelle su cui ha agito, e lei preme il pulsante di ciò che era davvero vero. I tasti da 1 a 9 e Invio (uguale alla scelta dell'IA) vanno veloci. «Risposte date» permette di correggere una risposta precedente (vale la più recente). «Dettagli» aggiunge «chiedi di nuovo all'IA», che ridecide lo stesso testo con la formulazione e la linea di oggi e mostra prima e dopo; le registrazioni vengono aggiunte, mai cancellate
- **Ogni tanto conferma «è ancora quello che intendevo?».** Quando la macchina trova una formulazione con un punteggio migliore, lei guarda se il significato è cambiato e preme adotta o rifiuta

Tutto il resto è compito della macchina.

## Installazione

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # serve Go 1.27 o più recente
cd <radice del progetto> && shikii init
```

`shikii init` prepara una cartella, `.shikii/` (con `decisions/` per i suoi file di decisione, `wiki/` per la conoscenza, `records/` e `labels/` per le registrazioni e le sue correzioni; nient'altro viene aggiunto alla radice del progetto), e in un colpo solo i collegamenti con gli agenti: il server MCP di shikii per Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) e Codex (`~/.codex/config.toml`), una sezione in `AGENTS.md` e le regole dettagliate (la skill) in `~/.claude/skills/shikii`. Le sue voci non vengono mai toccate (una voce shikii già presente resta com'è). Solo la cartella della skill viene sostituita ogni volta, quindi non la modifichi a mano. `wiki/` riceve la conoscenza condivisa (come porre ogni tipo di domanda, formulazioni che hanno funzionato, le abitudini di ogni modello); il miglioratore di formulazione (`tune --propose`) la legge, e le pagine che lei ha modificato non vengono mai sovrascritte da un `init` successivo. Niente Go? Usi Docker, qui sotto.

Le voci puntano a un modello locale (`shikii up`, porta 8080). Solo in hosting? Tolga `--local` a mano da ciascuno dei quattro file. Vengono creati anche i file di configurazione per strumenti che non usa; li cancelli o li aggiunga a `.gitignore` (un secondo `shikii init` non tocca mai ciò che esiste). Faccia il commit di `.shikii/decisions/`, `.shikii/wiki/` e `.shikii/labels/` (i suoi file di decisione, la conoscenza e le sue correzioni: il patrimonio che sopravvive a un cambio di modello). `.shikii/records/` (ogni decisione) continua a crescere, quindi farne il commit è facoltativo (`shikii gc` scarta quelle vecchie). Se Codex ha già `mcp_servers`, non viene aggiunto nulla e vengono stampate le righe da incollare. Un progetto con solo un `CLAUDE.md` non riceve `AGENTS.md` (OpenCode smetterebbe di leggere `CLAUDE.md`).

Modificare la pagina delle risposte stessa (`web/`, Svelte) richiede Node 24 (`make web`). Usare shikii no: la pagina compilata è nel repository e incorporata nel binario.

## Le vie più brevi

- **Docker**: niente Go, niente modello? `docker compose up` avvia un modello locale (Kev-4B) e shikii, con la pagina delle risposte su http://127.0.0.1:8095 ([dettagli](../../integrations/docker.md))
- **Decisioni pronte**: sei nella cartella `decisions/` di questo repository (tipo di richiesta, segnalazione di bug duplicata, stessa richiesta, …). Ne copi una nel suo `.shikii/decisions/`, oppure parta da lì con `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([elenco](../../decisions/README.md))
- **Ha già una tabella di decisioni passate**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` la trasforma in correzioni, così non risponde alle prime 50 a mano
- **Dal suo programma**: avvii `shikii serve` e chiami `POST /v1/decide` (decidere) e `POST /v1/label` (correggere). Il corpo è JSON e `-H 'Content-Type: application/json'` è obbligatorio (tutti gli endpoint: [la guida lunga](../guide.md#or-run-one-process-for-programs-and-people))
- **L'esempio di qualcun altro**: [smistare le e-mail di richiesta](../examples/inquiry-route.md) (giapponese). Una persona che usava shikii per la prima volta ha fatto tutto il giro con il solo README; sono inclusi il file di decisione e i 22 input
- **Misurare il giudizio dell'IA stessa** (Claude Code e Codex): `shikii hook install` aggiunge hook che fanno passare da shikii le decisioni dell'agente stesso: cosa fa un comando, se una modifica rientra in ciò che lei ha chiesto, se una richiesta ha bisogno prima di un piano, e cosa finirà per fare l'agente (quest'ultima verità arriva gratis dal fatto che l'albero di lavoro sia cambiato o no). Niente cambia finché non esiste una linea. Poi un comando rischioso (cancellare, modificare fuori dal progetto, inviare all'esterno) le chiede conferma, con quante verità reggono la linea e la precisione controllata da persone; i comandi sicuri passano in silenzio ([dettagli](../../integrations/claude-code.md), giapponese)
- **In CI**: `uses: hayaenfj/shikii@main` esegue `shikii check` su ogni pull request, così una formulazione cambiata o una linea indebolita fa fallire la build

## Un giro completo

```sh
shikii up                                                             # avviare il modello locale (scaricato alla prima esecuzione)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (ancora nessuna linea, quindi: chiedere a una persona)
shikii serve --local http://127.0.0.1:8080                                   # i file di decisione e la conoscenza si leggono da .shikii/
#   → risponda su http://127.0.0.1:8095. Quando una decisione ha il numero che chiede (eval.min_n, 20 per questa), la linea si traccia da sola
#     Per provare con meno risposte, abbassi eval.min_n nel file di decisione a quante ne ha (una linea scritta con --force viene segnalata da check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (si può agire, con la prova: la linea si regge su 61 correzioni, 48 delle quali di persone. Delle risposte umane che questa linea mette dal lato act, 25, il 96% era giusto)
```

Ci sono solo quattro fasce. `act` = procedere, `ask` = verificare con una persona, `discard` = non fare nulla, `abstain` = manca l'input. Non legge mai una probabilità grezza.

## Cosa c'è di nuovo

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architettura di shikii: tre cose che fanno le persone, il patrimonio (domande, verità, ricevute), il ciclo che la macchina fa crescere, il wiki verificabile, modelli usa e getta" width="100%"></p>

- **La conoscenza si accumula nelle domande e nelle correzioni, non nel modello.** Quindi il modello è usa e getta, e cambiarlo non richiede lavoro umano. Misurato: con la macchina che sceglie formulazione e disposizione e aggiunge esempi simili, un modello base ordinario ha raggiunto la stessa precisione della messa a punto (LoRA) ([numeri](../bench.md))
- **Le decisioni automatiche arrivano con le prove.** Non «sicuro al 95%» ma «precisione 0,96 su 48 risposte controllate da persone»
- **Il wiki non si legge, si misura.** «Chiedere così funziona» si accumula come affermazioni con numeri e viene riprovato sui modelli nuovi. Solo una persona decide che un'affermazione è confermata

## Per saperne di più

- [La guida lunga](../guide.md): ogni comando, l'avvio rapido completo, l'API HTTP, cosa si può collegare, i risultati misurati e le domande frequenti · [il riferimento del file di decisione](../../skills/shikii/references/spec.md)
- [Note di progettazione](../decision-ops-framework.md): di cosa si fa carico il framework e di cosa rifiuta di farsi carico, e cosa è stato verificato · [Architettura](../architecture.md) (giapponese)
- [Decisioni pronte](../../decisions/README.md) · [il wiki su come chiedere](../../wiki/index.md) · [benchmark](../bench.md) · [confronto tra modelli](../model-comparison.md) · [diario operativo](../operations.md)
- [Collegamenti](../../integrations/mcp.md): MCP, l'adattatore che trasforma un normale modello di chat in un modello di decisione, Docker, Claude Code
- [Guida in giapponese](../guide.ja.md) con un giro completo su un caso reale

## Contribuire

Issue e pull request sono benvenuti. Un test che protegge un comportamento viene prima mostrato mentre fallisce: rompa il comportamento di proposito, esegua il test, annoti come è fallito, poi lo sistemi e annoti che passa. Ogni numero nella documentazione viene da uno script. Solo le persone cambiano lo stato di un'affermazione del wiki (`confirmed` / `tentative` / `rejected`), con l'esecuzione che ne è la prova. Nessuna opzione nuova: la disposizione (`decisions/` `wiki/` `.shikii/` nella radice del progetto) e regole fisse fanno il lavoro.

## Licenza

[Apache 2.0](../../LICENSE). Le licenze dei modelli sono a parte: Kev-4B e la famiglia Bonsai sono Apache 2.0, OpenJev è CC BY-NC 4.0, il Jev in hosting è un servizio di TypeSafe. shikii conserva le sue probabilità e le sue etichette, nulla dei modelli.
