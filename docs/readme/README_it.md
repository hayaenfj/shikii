[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · **Italiano** · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: trasformare il «probabilmente» dell'IA in un «ci si può fidare» su cui far girare un'attività

### Uno strumento che lascia a un'IA decidere «sì o no», «quale» e «quanto», e la fa crescere con le sue correzioni. Lei deve solo rispondere, in una pagina, a cosa era davvero vero.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-380%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Il problema](#il-problema) · [Uso 1: misurare il pensiero dell'IA stessa](#uso-1-misurare-il-pensiero-dellia-stessa) · [Uso 2: affidarle una decisione di lavoro](#uso-2-affidarle-una-decisione-di-lavoro) · [Cosa fa lei nella pagina](#cosa-fa-lei-nella-pagina) · [Installazione](#installazione) · [Perché si può cambiare IA](#perché-si-può-cambiare-ia) · [Per saperne di più](#per-saperne-di-più)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Il problema

Chieda a un'IA «questa richiesta riguarda la stessa pratica della precedente?» e risponde «probabilmente, 0,9». Per il lavoro non basta. Quel 0,9 è davvero giusto nove volte su dieci? Dal giorno in cui è passato a un'IA più nuova, è diventato in silenzio una bugia? Le decisioni che prende mentre nessuno guarda sono ancora giuste? Se una persona deve controllare ogni volta, usare un'IA aveva poco senso.

shikii traccia una **linea** a partire dalle sue correzioni: sopra questa, l'IA può agire da sola. Finché non esiste una linea, non ci si fida di nulla. Quando esiste, ogni decisione automatica porta con sé le sue basi: su quante correzioni si regge la linea e quante volte aveva ragione. La linea vive insieme alla domanda e alle correzioni, non dentro l'IA, quindi cambiare IA non costa lavoro umano.

Ci sono due modi di usarlo: misurare il pensiero dell'IA che scrive il suo codice (il più leggero) e affidarle una decisione del suo lavoro.

## Uso 1: misurare il pensiero dell'IA stessa

Per chi usa Claude Code o Codex. L'agente che scrive codice prende delle decisioni quando riceve una richiesta e quando finisce; shikii le misura in silenzio.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Tutto qui. Installa il binario, aggiunge due hook a Claude Code e Codex, mette al loro posto le regole dell'agente (una skill) e avvia in background una piccola IA locale (Kev-4B) (se manca llama.cpp, stampa la riga per installarlo). Apra un terminale nuovo e continui a lavorare con l'agente come sempre (in Codex, esegua `/hooks` una volta per dare fiducia agli hook di shikii). Vengono misurate quattro cose.

| Quando | Domanda | Da dove viene la verità |
|---|---|---|
| Arriva una richiesta | Questa richiesta finirà per modificare dei file? | Automatica: l'albero di lavoro è cambiato prima della richiesta successiva |
| Arriva una richiesta | L'agente dovrebbe accordarsi con lei su un approccio prima di toccare qualcosa? | Lei, nella pagina |
| Il turno finisce | Le modifiche sono rimaste dentro ciò che ha chiesto? | Lei, nella pagina |
| Il turno finisce | Ha finito ciò che ha chiesto? | Lei, nella pagina |

Alla fine di un turno compare una riga, `shikii: 3 decisioni di questa sessione aspettano la sua risposta: http://127.0.0.1:8096/`, e lei la apre quando ha un minuto (anche la pagina viene avviata in background dall'hook); ogni risposta richiede pochi secondi. Dopo 50 risposte si traccia una linea e il comportamento dell'agente cambia: una richiesta che ha bisogno di un piano diventa una nota per l'agente, e un turno che sembra incompiuto o fuori dal richiesto diventa una riga per lei, ognuna con le sue basi, come «linea su 61 verità, precisione controllata da persone 0,96». Fino ad allora gli hook si limitano a registrare. Nessun hook ferma mai uno strumento.

Tutto vive in un unico posto, `~/.shikii/agent`, con una linea per domanda per tutti i suoi progetti (le domande riguardano il pensiero dell'agente, quindi la verità è la stessa in ogni repository). Ogni registrazione è contrassegnata con il repository da cui viene, e la pagina può filtrare per repository. Dettagli: [l'hook](../../integrations/claude-code.md) (giapponese).

## Uso 2: affidarle una decisione di lavoro

Per le decisioni che ripete al lavoro, come «questa richiesta è la stessa pratica?» o «questo messaggio è una richiesta di modifica?»: lasci decidere l'IA e la faccia crescere con le sue correzioni. Anche qui non c'è nessun comando da digitare. Lo dice all'agente.

> Voglio che shikii decida se una richiesta riguarda la stessa pratica di una precedente, e che cresca con le mie correzioni

L'agente segue le sue regole (la skill): crea `.shikii/` nel progetto (`shikii init`), fissa la domanda, le opzioni e gli input e scrive il file di decisione (`shikii new`; le mostra una volta la domanda e le opzioni, perché quella è la sua intenzione), chiama la decisione (`shikii decide`), avvia la pagina delle risposte (http://127.0.0.1:8095) e la avvisa. Finché non esiste una linea nessuna decisione è `act` (procedere). Man mano che lei risponde nella pagina, la linea si traccia da sola quando si raggiunge il numero fissato per quella decisione (20 in questo esempio), e da quel momento:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (si può agire. La linea si regge su 61 verità, 48 delle quali date da persone; di quelle date da persone che questa linea lascerebbe agire, 25, il 96% era giusto)
```

I valori di `band` sono solo quattro: `act` = procedere, `ask` = verificare con una persona, `discard` = non agire, `abstain` = input insufficiente. Non si agisce mai su una probabilità grezza.

- **A mano**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. È tutto nella [guida](../guide.md)
- **Da un agente**: `shikii init` scrive la configurazione MCP, così Claude Code, Cursor, OpenCode e Codex possono chiamare direttamente gli strumenti `shikii_decide` e `shikii_label` ([MCP](../../integrations/mcp.md))
- **Dal suo programma**: avvii `shikii serve` e chiami `POST /v1/decide` (decidere) e `POST /v1/label` (correggere). Il corpo è JSON e serve `-H 'Content-Type: application/json'` ([API HTTP](../guide.md#or-run-one-process-for-programs-and-people))
- **Decisioni pronte**: sei nella cartella `decisions/` di questo repository (tipo di richiesta, segnalazione di bug duplicata, stessa richiesta, …). Ne copi una con `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` e la riformuli ([elenco](../../decisions/README.md))
- **Ha già una tabella di decisioni passate**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` la trasforma in correzioni, così non deve rispondere alle prime 50 a mano
- **Docker**: ancora niente Go e niente modello? `docker compose up` avvia un'IA locale e shikii, con la pagina su http://127.0.0.1:8095 ([dettagli](../../integrations/docker.md))
- **L'esempio di qualcun altro**: [smistare le e-mail di richiesta](../examples/inquiry-route.md) (giapponese). Una persona al primo utilizzo ha fatto tutto il giro con il solo README; sono inclusi il file di decisione e i 22 input

## Cosa fa lei nella pagina

Nella pagina delle risposte (`shikii serve`) una persona fa tre cose. Tutto il resto è compito della macchina.

1. **Rispondere.** La pagina mostra le decisioni su cui l'IA era incerta, più 1 su 20 di quelle su cui ha agito, e lei preme il pulsante di ciò che era davvero vero. I tasti da 1 a 9 e Invio (uguale alla scelta dell'IA) vanno veloci. «Risposte date» permette di correggere una risposta precedente (vale la più recente)
2. **Scrivere una riformulazione.** Se pensa «chiedendo così funzionerebbe meglio», lo scriva nel pannello «Formulazione» con le sue parole. La macchina le dà un punteggio rispetto alla formulazione attuale sulle stesse correzioni (per le decisioni dell'agente stesso, in automatico ogni 50 risposte nuove) e dice «si può adottare» solo quando è davvero migliore. Può anche lasciare che un'IA proponga delle riformulazioni. Non apre mai un file
3. **Premere adotta o rifiuta.** Lei guarda se il significato è cambiato e preme. La macchina controlla i numeri; lei controlla il significato

«Dettagli» aggiunge lo stato della linea per ogni modello, «chiedi di nuovo all'IA» (ridecide lo stesso testo con la formulazione e la linea di oggi e mostra prima e dopo) e il pannello della conoscenza (affermazioni del tipo «chiedere così funziona», con le loro prove). Le registrazioni vengono aggiunte, mai cancellate; una decisione nuova prende il posto di quella vecchia.

## Installazione

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 o più recente; altrimenti Docker (sopra)
```

Cosa fa install.sh: installa i binari (`shikii` e `shikii-readout`), aggiunge due hook a Claude Code e Codex, mette le regole dell'agente (la skill) in `~/.claude/skills/shikii` e, se llama.cpp è presente, avvia in background l'IA locale (Kev-4B su :8080). Per usare invece Jev in hosting o OpenRouter, metta la chiave in una variabile d'ambiente ([collegamenti](../guide.md#what-you-can-connect)). `shikii doctor` le dice cosa è in esecuzione e cosa manca.

La preparazione per progetto (`shikii init`) è compito dell'agente (può anche eseguirla lei). Crea una sola cartella, `.shikii/` (dentro: `decisions/` = file di decisione, `wiki/` = conoscenza, `records/` e `labels/` = registrazioni e correzioni). Nella radice del progetto non compare nient'altro. `decisions/`, `wiki/` e `labels/` sono un patrimonio che sopravvive a un cambio di IA, quindi ne faccia il commit; `records/` continua a crescere, quindi farne il commit è facoltativo (`shikii gc` scarta quelle vecchie). Aggiunge anche una voce shikii alla configurazione dei suoi agenti (`.mcp.json` di Claude Code, `.cursor/mcp.json` di Cursor, `opencode.json` di OpenCode, `~/.codex/config.toml` di Codex) senza toccare le voci scritte da lei. Eseguirlo di nuovo non cambia nulla di ciò che esiste.

L'output dei comandi e i messaggi degli hook sono in inglese (se la lingua impostata in Claude Code è il giapponese, i messaggi degli hook sono mostrati in giapponese accanto all'inglese). La pagina ha il giapponese. I punti più fini, come usare solo un'IA in hosting, `AGENTS.md` rispetto a `CLAUDE.md` e Node per modificare la pagina stessa, sono nella [guida](../guide.md).

## Perché si può cambiare IA

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architettura di shikii: tre cose che fanno le persone, il patrimonio (domande, verità, ricevute), il ciclo che la macchina fa crescere, il wiki verificabile, il modello usa e getta" width="100%"></p>

- **La conoscenza si accumula insieme alla domanda e alle correzioni, non dentro l'IA.** Le correzioni sopravvivono all'IA, quindi una nuova può usarle così come sono e la linea viene ritracciata. Nelle nostre misurazioni, lasciare che la macchina scelga formulazione e disposizione e aggiunga esempi simili ha raggiunto la stessa precisione della messa a punto dei pesi del modello (LoRA) ([numeri](../bench.md))
- **Le decisioni automatiche portano una ricevuta.** Non «sicuro al 95%» ma «precisione 0,96 su 48 casi controllati da persone»
- **Il wiki non si legge, si misura.** «Chiedere così funziona» si accumula come affermazioni con i numeri, e può essere riprovato su un'IA nuova. Solo una persona decide che un'affermazione è confermata

## Per saperne di più

- [La guida](../guide.md): ogni comando, l'API HTTP, le domande frequenti · [il riferimento del file di decisione](../../skills/shikii/references/spec.md)
- [L'hook](../../integrations/claude-code.md) (giapponese): come viene misurato il pensiero dell'agente stesso, la tabella fascia → comportamento, il funzionamento interno
- [Architettura](../architecture.md) (giapponese): il quadro completo dietro il diagramma qui sopra
- [Decisioni pronte](../../decisions/README.md) · [il wiki su come chiedere](../../wiki/index.md) · [numeri misurati](../bench.md)
- [Collegamenti](../../integrations/mcp.md): MCP, l'adattatore per i normali modelli di chat, Docker

**Per chi contribuisce**: rompa un test di proposito e lo guardi fallire prima di sistemarlo. Ogni numero viene da uno script. Solo una persona conferma un'affermazione del wiki. Nessuna impostazione nuova: posti fissi e regole fisse al loro posto.

## Licenza

[Apache-2.0 OR MIT](../../LICENSE). Le licenze dei modelli sono a parte (Kev-4B e la famiglia Bonsai sono Apache 2.0, OpenJev è CC BY-NC 4.0, il Jev in hosting è un servizio di TypeSafe).
