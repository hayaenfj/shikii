[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · **Deutsch** · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: aus dem „wahrscheinlich“ der KI ein „darf handeln“ machen, auf das Sie Ihr Geschäft bauen können

### Ein Werkzeug, mit dem eine KI „ja oder nein“, „welches“ und „wie viel“ entscheidet, und das aus Ihren Korrekturen wächst. Sie tun nur eines: auf einer Seite beantworten, was tatsächlich zutraf.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-275%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Das Problem](#das-problem) · [Verwendung 1: das Denken der KI selbst messen](#verwendung-1-das-denken-der-ki-selbst-messen) · [Verwendung 2: ihr eine Geschäftsentscheidung übergeben](#verwendung-2-ihr-eine-geschäftsentscheidung-übergeben) · [Was Sie auf der Seite tun](#was-sie-auf-der-seite-tun) · [Installation](#installation) · [Warum Sie die KI wechseln können](#warum-sie-die-ki-wechseln-können) · [Mehr lesen](#mehr-lesen)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Das Problem

Fragen Sie eine KI „ist diese Anfrage dieselbe Sache wie die letzte?“, antwortet sie „wahrscheinlich, 0,9“. Für die Arbeit reicht das nicht. Stimmt diese 0,9 wirklich in neun von zehn Fällen? Ist sie an dem Tag, an dem Sie auf eine neuere KI gewechselt haben, still und leise zur Lüge geworden? Sind die Entscheidungen, die sie trifft, während niemand zusieht, noch richtig? Wenn ein Mensch jedes Mal nachprüfen muss, hatte der Einsatz der KI wenig Sinn.

shikii zieht aus Ihren Korrekturen eine **Linie**: oberhalb davon darf die KI allein handeln. Solange keine Linie existiert, wird nichts vertraut. Sobald es eine gibt, trägt jede automatische Entscheidung ihre Begründung: auf wie vielen Korrekturen die Linie ruht und wie oft sie richtig lag. Die Linie lebt bei der Frage und den Korrekturen, nicht in der KI, darum kostet ein Wechsel der KI keine menschliche Arbeit.

Es gibt zwei Arten, es zu benutzen: das Denken der KI messen, die Ihren Code schreibt (die leichteste), und ihr eine Entscheidung aus Ihrem Geschäft übergeben.

## Verwendung 1: das Denken der KI selbst messen

Für Leute, die Claude Code oder Codex benutzen. Der Coding-Agent trifft Urteile, wenn er eine Anfrage erhält und wenn er fertig ist; shikii misst sie im Stillen.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Das ist alles. Es installiert die Binärdatei, fügt Claude Code und Codex zwei Hooks hinzu, legt die Regeln des Agenten (einen Skill) ab und startet im Hintergrund eine kleine lokale KI (Kev-4B) (fehlt llama.cpp, gibt es die eine Zeile aus, mit der man es installiert). Öffnen Sie ein neues Terminal und arbeiten Sie wie gewohnt mit dem Agenten weiter (in Codex einmal `/hooks` ausführen, um den Hooks von shikii zu vertrauen). Vier Dinge werden gemessen.

| Wann | Frage | Woher die Wahrheit kommt |
|---|---|---|
| Eine Anfrage kommt an | Wird diese Anfrage am Ende Dateien ändern? | Automatisch: hat sich der Arbeitsbaum vor der nächsten Anfrage geändert |
| Eine Anfrage kommt an | Sollte sich der Agent mit Ihnen auf ein Vorgehen einigen, bevor er etwas anfasst? | Sie, auf der Seite |
| Der Durchgang endet | Lagen die Änderungen innerhalb dessen, was Sie verlangt haben? | Sie, auf der Seite |
| Der Durchgang endet | Hat er erledigt, was Sie verlangt haben? | Sie, auf der Seite |

Am Ende eines Durchgangs erscheint eine Zeile, `shikii: 3 Entscheidungen dieser Sitzung warten auf Ihre Antwort: http://127.0.0.1:8096/`, und Sie öffnen sie, wenn Sie eine Minute haben (der Hook startet die Seite ebenfalls im Hintergrund); jede Antwort dauert Sekunden. Nach 50 Antworten wird eine Linie gezogen, und das Verhalten des Agenten ändert sich: eine Anfrage, die einen Plan braucht, wird zu einem Hinweis an den Agenten, und ein Durchgang, der unfertig oder außerhalb des Verlangten aussieht, zu einer Zeile an Sie, jeweils mit einer Begründung wie „Linie auf 61 Wahrheiten, von Menschen geprüfte Präzision 0,96“. Bis dahin zeichnen die Hooks nur auf. Kein Hook hält je ein Werkzeug an.

Alles liegt an einem Ort, `~/.shikii/agent`, mit einer Linie pro Frage über alle Ihre Projekte hinweg (die Fragen betreffen das Denken des Agenten, darum ist die Wahrheit in jedem Repository dieselbe). Jede Aufzeichnung ist mit dem Repository markiert, aus dem sie stammt, und die Seite kann danach filtern. Details: [der Hook](../../integrations/claude-code.md) (Japanisch).

## Verwendung 2: ihr eine Geschäftsentscheidung übergeben

Für Entscheidungen, die Sie bei der Arbeit wiederholen, etwa „ist diese Anfrage dieselbe Sache?“ oder „ist diese Nachricht eine Änderungsanfrage?“: lassen Sie die KI entscheiden und lassen Sie sie aus Ihren Korrekturen wachsen. Auch hier gibt es keinen Befehl zu tippen. Sie sagen es dem Agenten.

> Ich möchte, dass shikii entscheidet, ob eine Anfrage dieselbe Sache ist wie eine frühere, und dass es aus meinen Korrekturen wächst

Der Agent folgt seinen Regeln (dem Skill): er legt `.shikii/` im Projekt an (`shikii init`), klärt die Frage, die Optionen und die Eingaben und schreibt die Entscheidungsdatei (`shikii new`; er zeigt Ihnen die Frage und die Optionen einmal, denn das ist Ihre Absicht), ruft die Entscheidung auf (`shikii decide`), startet die Antwortseite (http://127.0.0.1:8095) und sagt es Ihnen. Solange keine Linie existiert, ist keine Entscheidung `act` (loslegen). Während Sie auf der Seite antworten, wird die Linie automatisch gezogen, sobald die Zahl erreicht ist, die die Entscheidung selbst verlangt (in diesem Beispiel 20), und von da an:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (darf handeln. Die Linie ruht auf 61 Wahrheiten, 48 davon von Menschen beantwortet; von den menschlich beantworteten, die diese Linie handeln ließe, 25, waren 96 % richtig)
```

Es gibt nur vier Werte für `band`: `act` = loslegen, `ask` = bei einem Menschen nachfragen, `discard` = nicht handeln, `abstain` = Eingabe reicht nicht. Nach rohen Wahrscheinlichkeiten handeln Sie nie.

- **Von Hand**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Alles davon steht in [der Anleitung](../guide.md)
- **Aus einem Agenten**: `shikii init` schreibt die MCP-Einrichtung, sodass Claude Code, Cursor, OpenCode und Codex die Werkzeuge `shikii_decide` und `shikii_label` direkt aufrufen können ([MCP](../../integrations/mcp.md))
- **Aus Ihrem eigenen Programm**: starten Sie `shikii serve` und rufen Sie `POST /v1/decide` (entscheiden) und `POST /v1/label` (korrigieren) auf. Der Body ist JSON und braucht `-H 'Content-Type: application/json'` ([HTTP-API](../guide.md#or-run-one-process-for-programs-and-people))
- **Fertige Entscheidungen**: sechs im Ordner `decisions/` dieses Repos (Art der Anfrage, doppelter Fehlerbericht, dieselbe Anfrage, …). Kopieren Sie eine mit `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` und formulieren Sie sie um ([Liste](../../decisions/README.md))
- **Sie haben schon eine Tabelle vergangener Entscheidungen**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` macht daraus Korrekturen, damit Sie die ersten 50 nicht von Hand beantworten müssen
- **Docker**: noch kein Go und kein Modell? `docker compose up` startet eine lokale KI und shikii, mit der Seite unter http://127.0.0.1:8095 ([Details](../../integrations/docker.md))
- **Das Beispiel einer anderen Person**: [Anfrage-Mails weiterleiten](../examples/inquiry-route.md) (Japanisch). Jemand, der shikii zum ersten Mal nutzte, ist allein mit dem README eine ganze Runde gegangen; Entscheidungsdatei und 22 Eingaben sind enthalten

## Was Sie auf der Seite tun

Auf der Antwortseite (`shikii serve`) tut ein Mensch drei Dinge. Alles andere ist Aufgabe der Maschine.

1. **Antworten.** Die Seite zeigt die Entscheidungen, bei denen die KI unsicher war, plus 1 von 20 derer, nach denen sie gehandelt hat, und Sie drücken den Knopf für das, was tatsächlich zutraf. Die Tasten 1 bis 9 und Enter (gleich wie die Wahl der KI) gehen schnell. „Beantwortet“ lässt Sie eine frühere Antwort korrigieren (die neueste zählt)
2. **Eine Umformulierung schreiben.** Wenn Sie denken „so gefragt würde es besser gehen“, tippen Sie es im Feld „Formulierung“ in Ihren eigenen Worten ein. Die Maschine bewertet es auf denselben Korrekturen gegen die aktuelle Formulierung (bei den eigenen Entscheidungen des Agenten automatisch alle 50 neuen Antworten) und sagt nur dann „kann übernommen werden“, wenn es wirklich besser ist. Sie können auch eine KI Umformulierungen vorschlagen lassen. Sie öffnen nie eine Datei
3. **Übernehmen oder ablehnen drücken.** Sie sehen nach, ob sich die Bedeutung geändert hat, und drücken. Die Maschine prüft die Zahlen; Sie prüfen die Bedeutung

„Details“ fügt den Zustand der Linie pro Modell hinzu, „die KI noch einmal fragen“ (denselben Text mit der heutigen Formulierung und Linie neu entscheiden und vorher und nachher sehen) und das Wissensfeld (Behauptungen der Form „so zu fragen funktioniert“, mit ihren Belegen). Aufzeichnungen werden angehängt, nie gelöscht; eine neue Entscheidung tritt an die Stelle der alten.

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 oder neuer; sonst Docker (oben)
```

Was install.sh tut: es installiert die Binärdateien (`shikii` und `shikii-readout`), fügt Claude Code und Codex zwei Hooks hinzu, legt die Regeln des Agenten (den Skill) in `~/.claude/skills/shikii` ab und startet, wenn llama.cpp vorhanden ist, die lokale KI (Kev-4B auf :8080) im Hintergrund. Um stattdessen das gehostete Jev oder OpenRouter zu nutzen, legen Sie den Schlüssel in eine Umgebungsvariable ([Anbindungen](../guide.md#what-you-can-connect)). `shikii doctor` sagt Ihnen, was läuft und was fehlt.

Die Einrichtung pro Projekt (`shikii init`) ist Aufgabe des Agenten (Sie dürfen sie auch selbst ausführen). Sie legt einen Ordner an, `.shikii/` (darin: `decisions/` = Entscheidungsdateien, `wiki/` = Wissen, `records/` und `labels/` = Aufzeichnungen und Korrekturen). Im Projektroot kommt nichts anderes hinzu. `decisions/`, `wiki/` und `labels/` sind Werte, die einen Wechsel der KI überdauern, also committen Sie sie; `records/` wächst immer weiter, darum ist das Committen freigestellt (`shikii gc` wirft alte weg). Sie fügt außerdem der Einrichtung Ihrer Agenten einen shikii-Eintrag hinzu (`.mcp.json` von Claude Code, `.cursor/mcp.json` von Cursor, `opencode.json` von OpenCode, `~/.codex/config.toml` von Codex), ohne Einträge anzufassen, die Sie geschrieben haben. Ein zweiter Lauf ändert nichts, was schon da ist.

Befehlsausgaben und Hook-Meldungen sind auf Englisch (steht die Spracheinstellung von Claude Code auf Japanisch, werden die Hook-Meldungen neben dem Englischen auf Japanisch gezeigt). Die Seite hat Japanisch. Die feineren Punkte, etwa nur eine gehostete KI zu nutzen, `AGENTS.md` gegenüber `CLAUDE.md` und Node zum Ändern der Seite selbst, stehen in [der Anleitung](../guide.md).

## Warum Sie die KI wechseln können

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii-Architektur: drei Dinge, die Menschen tun, die Werte (Fragen, Wahrheiten, Belege), die Schleife, die die Maschine wachsen lässt, das überprüfbare Wiki, das Wegwerf-Modell" width="100%"></p>

- **Wissen sammelt sich bei der Frage und den Korrekturen an, nicht in der KI.** Die Korrekturen überdauern die KI, also kann eine neue sie unverändert nutzen, und die Linie wird neu gezogen. In unseren Messungen erreichte es dieselbe Treffsicherheit wie die Feinabstimmung der Modellgewichte (LoRA), wenn die Maschine Formulierung und Anordnung wählt und ähnliche Beispiele hinzufügt ([Zahlen](../bench.md))
- **Automatische Entscheidungen tragen einen Beleg.** Nicht „95 % sicher“, sondern „Präzision 0,96 auf 48 von Menschen geprüften Fällen“
- **Das Wiki wird nicht gelesen, es wird gemessen.** „So zu fragen funktioniert“ sammelt sich als Behauptungen mit Zahlen an und kann auf einer neuen KI erneut geprüft werden. Nur ein Mensch erklärt eine Behauptung für bestätigt

## Mehr lesen

- [Die Anleitung](../guide.md): jeder Befehl, die HTTP-API, FAQ · [die Referenz zur Entscheidungsdatei](../../skills/shikii/references/spec.md)
- [Der Hook](../../integrations/claude-code.md) (Japanisch): wie das Denken des Agenten selbst gemessen wird, die Tabelle Band → Verhalten, das Innere
- [Architektur](../architecture.md) (Japanisch): das ganze Bild hinter dem Diagramm oben
- [Fertige Entscheidungen](../../decisions/README.md) · [das Wiki, wie man fragt](../../wiki/index.md) · [gemessene Zahlen](../bench.md)
- [Anbindungen](../../integrations/mcp.md): MCP, der Adapter für gewöhnliche Chat-Modelle, Docker

**Für Mitwirkende**: einen Test absichtlich brechen und ihn fehlschlagen sehen, bevor Sie ihn beheben. Jede Zahl stammt aus einem Skript. Nur ein Mensch bestätigt eine Wiki-Behauptung. Keine Einstellungen hinzufügen; stattdessen feste Orte und feste Regeln.

## Lizenz

[Apache-2.0 OR MIT](../../LICENSE). Modelllizenzen sind davon getrennt (Kev-4B und die Bonsai-Familie sind Apache 2.0, OpenJev ist CC BY-NC 4.0, das gehostete Jev ist ein Dienst von TypeSafe).
