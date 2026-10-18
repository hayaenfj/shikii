[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · **Italiano** · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Gestire i modelli decisionali

### Un solo ciclo di vita per ogni modello decisionale: define → decide → record → label → calibrate → check → drift.

**Un modello ti dà una probabilità. shikii ti dice se puoi agire su di essa.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-161%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Note di progetto](../decision-ops-framework.md) · [Operatività](../operations.md) · [Confronto dei modelli](../model-comparison.md)**

[Avvio rapido](#avvio-rapido) · [Come funziona shikii](#come-funziona-shikii) · [Cosa puoi collegare](#cosa-puoi-collegare) · [Risultati](#risultati) · [FAQ](#faq) · [Documentazione](#documentazione)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii non esegue mai un modello e non legge mai un portachiavi. Parla con un endpoint di modello decisionale che hai già, conserva i record e si rifiuta di **agire** finché le probabilità per *questa domanda su questo modello* non sono state verificate con le etichette. I blocchi di calibrazione sono generati, mai scritti a mano.

---

I modelli decisionali (TypeSafe Jev, Kev-4B e OpenJev su llama.cpp, OpenRouter Decisions, …) rispondono a domande tipizzate, `choice`, `score` e `noul` (sì/no), con probabilità e senza testo generato. Ogni fornitore accompagna i modelli con lo stesso avvertimento: *le probabilità non sono calibrate sui tuoi dati e le soglie non valgono da un modello o da una domanda all'altra.*

shikii si occupa della parte che nessuno fornisce: il **ciclo di vita** di una decisione. Un file spec contiene la domanda, le opzioni, il template dello stato e le soglie. `decide` interroga un modello e restituisce una fascia (`act` / `ask` / `discard` / `abstain`), mai un numero grezzo. `label` registra ciò che era vero. `calibrate` esegue il fit e la cross-validation, poi riscrive il risultato nella spec sotto una chiave composta dall'hash della domanda e dal nome del modello. `check` fa fallire la CI quando la formulazione o il modello sono cambiati. `gc` mantiene lo store di dimensioni contenute.

Include anche `shikii-readout`, un adattatore di 500 righe che trasforma **qualsiasi GGUF di chat su un llama.cpp standard** in un modello decisionale, leggendo i logit delle lettere delle opzioni. Nelle nostre misurazioni un comune modello ternario da 27B letto in questo modo ha eguagliato in accuratezza i modelli decisionali addestrati, a 0.6 secondi a domanda su un laptop.

> Il nome è giapponese: *shikii* (閾) significa soglia, la linea che trasforma una probabilità in un'azione. La CLI si chiama `shikii`.

## Avvio rapido

Go 1.24 o successivo. Per la prima esecuzione non serve nient'altro.

### Esegui l'intero ciclo senza chiave né GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # modello finto: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # un modello finto le cui probabilità richiedono calibrazione; guarda calibrate scegliere platt
```

### Collegalo a un modello reale

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) modello decisionale locale su llama.cpp (al primo avvio scarica Kev-4B, circa 2.5 GB)
make server                                       # llama-server su :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev ospitato. La chiave viene letta solo dall'ambiente
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

La risposta è una fascia, un trace id e le probabilità. Finché la spec non contiene una calibrazione per questo modello, la fascia è al massimo `ask`.

### Chiudi il ciclo

```sh
bin/shikii label <trace_id> change_request          # oppure:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <nome del modello nella risposta> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 se la formulazione è cambiata o se precision@act è scesa sotto act
```

### Usa qualsiasi GGUF come modello decisionale

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server standard con flash attention e KV a 8 bit, :8086
make readout                                               # shikii-readout davanti ad esso, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Installazione e aggiornamento

```sh
make install          # copia bin/shikii in ~/.local/bin/shikii
git pull && make install
```

## Come funziona shikii

Ogni decisione è un singolo file YAML. La domanda, le opzioni, il template dello stato, i luoghi in cui lo stato può essere inviato, le soglie e, generati da `calibrate --write`, i blocchi di calibrazione.

```yaml
id: prompt-intent
type: choice
question: What is the user asking the assistant to do?
options:
  - id: change_request
    description: Change something in the repository - edit, fix, add, remove, rename...
  - id: question
    description: Explain, investigate, compare or give an opinion, without changing anything.
state:
  template: "{{ message }}"
  required: [message]            # input mancante → abstain; il modello non viene mai chiamato
policy:
  send_to: [hosted, local]       # una sessione tainted salta automaticamente hosted
  pii: possible                  # i record conservano l'hash degli input, mai il testo
thresholds:
  act: 0.75                      # ≥ act → act in automatico, solo se calibrato
  ask: 0.60                      # ≥ ask → chiedi a una persona; sotto → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**La chiave di calibrazione è `(hash di domanda + template + opzioni, id del modello come restituito dall'endpoint)`.** Modifica una sola parola della domanda, oppure lascia che `jev-latest` passi a una nuova versione, e la calibrazione semplicemente sparisce: la fascia scende a `ask` invece di agire in silenzio su costanti obsolete. Anche la struttura del prompt fa parte della chiave: `shikii-readout` riporta la propria struttura nel nome del modello (`…+state-last`), perché spostare lo stato all'interno del prompt ha cambiato di 5 punti l'accuratezza di un modello da 27B a parità di pesi.

`calibrate` confronta tre metodi con la cross-validation a 5 fold e scrive il vincitore. `identity` significa che le probabilità grezze soddisfano già precision@act e il modello viene semplicemente marcato come verificato. `platt` (σ((Δ−bias)/T)) è per le domande sì/no. `vector` (softmax((log p + b)/T)) è per choice e score, e corregge le abitudini legate alle singole opzioni, come «ripiegare su *other*». Le etichette parziali (`!option`, «non era *quella*») contano per la precision ma non per il fit. Le etichette ottenute da un file vengono rese persistenti, quindi `check` e `drift` funzionano poi offline.

Ogni ramo rilevante per la sicurezza (instradamento dei taint, nessun act senza calibrazione, invalidazione dell'hash obsoleto, validazione delle etichette, rilevamento delle inversioni, scelta del metodo basata sulla CV, `gc` in dry run, …) ha una mutazione registrata che ha fatto fallire un test prima che la correzione venisse accettata. Ci sono 63 test Go in 8 package; la demo gira in CI.

## Cosa puoi collegare

shikii parla il formato di richiesta System One (`POST /v1/systemone`), quindi funziona con qualsiasi cosa lo esponga. L'adattatore copre tutto il resto.

| Destinazione | Cosa riceve shikii | Requisiti e guida |
|---|---|---|
| **TypeSafe Jev** (ospitato) | `choice` / `score` / `noul` con probabilità, id del modello come `jev-1.13.0` | `TYPESAFE_API_KEY` nell'ambiente. `--hosted` predefinito |
| **Modelli decisionali su llama.cpp** (Kev-4B, OpenJev, Clef, …) | Stesso formato, in locale, id del modello da `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 o successivo |
| **OpenRouter Decisions API** | Stesso formato tramite OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Qualsiasi GGUF di chat** tramite `shikii-readout` | Lettura dei logit delle lettere in formato System One, senza calibrazione propria | `llama-server` standard 0.7.0 dietro di esso. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** tramite [BonJev](https://github.com/ChezRD/bonjev) | Lo stesso, con il prompt di BonJev e la sua calibrazione integrata | Build Rust e fork di Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Modello finto** (`bin/fake-systemone`) | Probabilità deterministiche per demo e test | Niente. `make fake` |
| **Hook di Claude Code** (claude-jev) | Ogni prompt classificato, ogni turno etichettato a partire dal working tree | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Il taint è un input di prima classe: passa `--tainted` (o lascia che lo faccia il tuo hook) in una sessione che ha toccato dati di produzione e la richiesta non lascerà mai la macchina. Se non è configurato alcun endpoint locale, la decisione fa abstain.

## Risultati

Stesse spec, stesse etichette, quattro set di valutazione (intento dei prompt in inglese e giapponese, 62 richieste; collegamento di casi identici su 32 coppie giapponesi). I dettagli e la tabella completa sono in [`docs/model-comparison.md`](../model-comparison.md).

| Modello | Licenza | top-1 (tutti / ja) | Brier sì/no (grezzo) | Calibrazione scelta | Secondi per domanda |
|---|---|---|---|---|---|
| Kev-4B (modello decisionale addestrato) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (addestrato) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B tramite BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B tramite `shikii-readout`, llama.cpp standard** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**La formulazione batte la calibrazione.** Sull'intento dei prompt, 19 errori su 21 sono finiti su *other*; riformulare le opzioni ha portato il top-1 da 0.68 a 0.90 prima di qualsiasi calibrazione. La calibrazione serve per il caso «ordine giusto, confidenza insufficiente». La matrice di confusione stampata da `calibrate` ti dice in quale dei due casi ti trovi.

**I modelli già calibrati non hanno bisogno di Platt.** Il sì/no di Kev-4B aveva un Brier di 0.046 grezzo; Platt lo ha peggiorato in cross-validation, quindi è stato scritto `identity`. La prima volta che `calibrate` ha scelto `platt` è stata con un LLM comune letto tramite logit. Se la calibrazione serva o no è una proprietà di *modello più lettura*, non del solo modello.

**Un GGUF comune si piazza accanto a un modello decisionale addestrato.** Un modello di chat ternario da 27B, llama.cpp mainline 0.7.0, un adattatore, nessun fork, nessun addestramento, nessun Rust: stesso top-1 del miglior modello addestrato con licenza Apache, giapponese 1.000, 0.58 s a domanda con flash attention e prefisso già caldo. Spostare lo stato da prima delle opzioni a subito prima della risposta lo ha portato da 0.919 a 0.968 a parità di pesi, ed è per questo che la struttura fa parte della chiave di calibrazione.

## FAQ

**shikii esegue modelli?** No. Invia richieste System One a un endpoint che gestisci o noleggi, e conserva tutto ciò che accade dopo la risposta. `shikii-readout` è l'unica eccezione nello spirito: converte i logit di un modello di chat nello stesso formato, ma l'inferenza resta quella di llama.cpp.

**Perché la mia decisione non arriva mai ad `act`?** Perché la spec non ha un blocco di calibrazione per `(hash di questa domanda, id di questo modello)`. Raccogli etichette (`label`, oppure `--labeled file`), esegui `calibrate --write`, e `status` mostra `calibrated: identity|platt|vector`. Una decisione senza precision verificata alla soglia act non può agire. È proprio questo il punto.

**Ho cambiato una parola nella domanda e la calibrazione è sparita.** Corretto. L'hash è cambiato, le costanti erano state adattate a un'altra domanda. `status` mostra le vecchie etichette come `stale hash`; riesegui `calibrate --write` quando hai etichette per la nuova formulazione.

**Quale modello dovrei usare?** Dipende da licenza, velocità e lingua, e shikii non decide per te. La tabella qui sopra e [`docs/model-comparison.md`](../model-comparison.md) sono l'argomentazione: con la stessa spec e le stesse etichette puoi scegliere in base ai numeri, compreso il fatto che un modello abbia o meno bisogno di calibrazione. `scripts/eval-model.sh <url> <name>` riproduce la tabella per un nuovo endpoint.

**Funziona in giapponese?** Sì, ed è lì che le differenze si vedono. Kev-4B ottiene 0.913 sui prompt in giapponese; i modelli da 27B ottengono 1.000. Il collegamento di casi identici sui riepiloghi di indagine in giapponese separa completamente i positivi dai negativi su ogni modello da 27B.

**Dove vanno i miei dati?** All'endpoint indicato in `send_to` va solo lo stato renderizzato. Con `pii: possible` (il valore predefinito) lo store conserva un hash degli input, mai il testo. `--tainted` mantiene tutto in locale. Le chiavi vengono lette da `DECISIONS_API_KEY` o `TYPESAFE_API_KEY` e da nessun altro posto.

**Lo store continua a crescere.** `shikii gc` elimina i record più vecchi di 30 giorni che non hanno mai ricevuto un'etichetta. I record etichettati e le etichette restano per sempre, perché sono il materiale di calibrazione. È un dry run a meno che tu non passi `--apply`.

**C'è una propensione verso la prima opzione?** Misura prima di correggere: `shikii bias <spec> --permutations 100` rimescola le opzioni e riporta i cambi di risposta e lo spostamento di posizione. Kev-4B: 0 cambi su 100, spostamento ≤ 0.024. Gli LLM comuni letti tramite logit possono comportarsi diversamente; il comando esiste per loro.

## Documentazione

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): note di progetto, ciò che il framework gestisce e ciò che rifiuta di gestire, il piano di verifica e cosa è stato confermato.
- [`docs/operations.md`](../operations.md): come fermare o riportare indietro ogni parte in movimento, come leggere un guasto, cosa manca ancora.
- [`docs/model-comparison.md`](../model-comparison.md): la tabella di confronto e come riprodurla.
- [`docs/guide.ja.md`](../guide.ja.md): la guida estesa in giapponese, con ogni comando e un ciclo completo in un caso reale.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): le tre connessioni che usiamo.
- [`examples/`](../../examples/) e [`testdata/`](../../testdata/README.md): una spec senza calibrazione da cui partire e i set sintetici etichettati usati nei test.

I comandi:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, blocchi scritti a mano, precision@act sotto act → exit 1
shikii drift     <spec> --model M [--window N]            # etichette recenti a confronto con Brier e precision registrati alla calibrazione
shikii bias      <spec> [--rows K] [--permutations N]     # la risposta dipende dall'ordine delle opzioni?
shikii status    [spec...] [--store DIR]                  # inventario: record, etichette, fonti, stato per (modello, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # elimina i vecchi record senza etichetta; dry run per impostazione predefinita
shikii hash      <spec>
```

## Contribuire

Issue e pull request sono benvenute. Una sola regola: un test che protegge un comportamento deve prima essere mostrato mentre fallisce. Rompi il comportamento di proposito, esegui il test, registra come è fallito, poi correggi e registra che passa. La documentazione elenca quelle mutazioni per ogni ramo rilevante per la sicurezza; tieni onesto l'elenco.

## Licenza

[Apache 2.0](../../LICENSE). Le licenze dei modelli sono separate: Kev-4B e la famiglia Bonsai sono Apache 2.0, OpenJev è CC BY-NC 4.0, Jev ospitato è un servizio di TypeSafe. shikii conserva le tue probabilità e le tue etichette, niente dei modelli.
