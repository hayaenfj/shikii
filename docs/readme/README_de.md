[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · **Deutsch** · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: aus dem „wahrscheinlich“ einer KI ein „darf handeln“ machen, auf das Sie Ihr Geschäft bauen können

### Ein Werkzeug, mit dem eine KI Ja/Nein, „welches“ oder „wie viel“ entscheidet und diese Entscheidung im Arbeitsalltag verlässlich bleibt. Sie prüfen einige Antworten nach und bestätigen hin und wieder: „Ist das noch das, was ich gemeint habe?“. Den Rest macht die Maschine.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-191%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Das Problem](#das-problem) · [Was shikii tut](#was-shikii-tut) · [Was Sie tun](#was-sie-tun) · [Installation](#installation) · [Die kürzesten Wege hinein](#die-kürzesten-wege-hinein) · [Eine ganze Runde](#eine-ganze-runde) · [Was neu ist](#was-neu-ist) · [Mehr lesen](#mehr-lesen)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: ein Modell gibt Ihnen eine Wahrscheinlichkeit, shikii sagt Ihnen, ob Sie danach handeln dürfen" width="100%"></p>

## Das Problem

Fragen Sie eine KI „ist diese Anfrage dieselbe Sache wie die letzte?“, antwortet sie „wahrscheinlich, 0,9“. Für echte Arbeit reicht das nicht.

- Stimmt diese 0,9 wirklich in 9 von 10 Fällen?
- Hat die KI seit dem Tag, an dem Sie ein neues Modell eingesetzt haben, still und leise angefangen zu lügen?
- Wenn Sie die Formulierung ein wenig ändern, wie verändert sich die Treffsicherheit?
- Die Entscheidungen, die sie allein getroffen hat, ohne dass jemand zugesehen hat: sind die noch richtig?

Wenn ein Mensch jede Antwort nachprüfen muss, hatte der Einsatz der KI wenig Sinn.

## Was shikii tut

1. **Sammelt Ihre Korrekturen und zieht die Linie, oberhalb derer die KI allein handeln darf.** Aus den Antworten, die Sie gegeben haben („das war tatsächlich richtig“), misst es, wie die Zuversicht der KI mit ihrer Trefferquote zusammenhängt, und legt fest, ab wo man ihr vertrauen kann. Solange keine Linie existiert, handelt nichts. Sobald es eine gibt, trägt jede automatische Entscheidung einen Beleg, der sagt, auf wie vielen geprüften Antworten die Linie ruht und wie oft diese richtig waren
2. **Probiert andere Arten zu fragen aus und behält die bessere.** Auf denselben Korrekturen bewertet es andere Fragen, Beschreibungen der Optionen und Anordnungen des Prompts und übernimmt eine nur, wenn sie wirklich besser ist. Sie bestätigen nur, dass sich die Bedeutung nicht geändert hat
3. **Lässt Sie das Modell wechseln, ohne von vorn zu beginnen.** Die Korrekturen liegen getrennt vom Modell, also nutzt ein neues Modell sie weiter, und die Maschine zieht die Linie neu. Keine menschliche Arbeit
4. **Bewahrt das Gelernte in einem Wiki.** „So zu fragen funktioniert“ sammelt sich als Behauptungen mit den gemessenen Zahlen an, die sich auf einem anderen Modell erneut prüfen lassen. Was andere Leute auf ihren Modellen gemessen haben, kommt auf dieselbe Seite

## Was Sie tun

- **Schreiben Sie auf, was entschieden werden soll.** Beantworten Sie ein paar Fragen, und eine Entscheidungsdatei entsteht (`shikii new`, im Dialog oder mit `--type --question --option --input --pii`; das vollständige Format steht in [der Referenz zur Entscheidungsdatei](../../skills/shikii/references/spec.md))
- **Prüfen Sie Antworten nach.** Die Seite zeigt die Entscheidungen, bei denen die KI unsicher war, plus 1 von 20 derer, nach denen sie gehandelt hat, und Sie drücken den Knopf für das, was tatsächlich zutraf. Die Tasten 1 bis 9 und Enter (gleich wie die Wahl der KI) gehen schnell. „Beantwortet“ lässt Sie eine frühere Antwort korrigieren (die neueste zählt). „Details“ fügt „die KI noch einmal fragen“ hinzu, was denselben Text mit der heutigen Formulierung und Linie neu entscheidet und vorher und nachher zeigt; Aufzeichnungen werden angehängt, nie gelöscht
- **Bestätigen Sie hin und wieder: „Ist das noch das, was ich gemeint habe?“.** Wenn die Maschine eine Formulierung findet, die besser abschneidet, sehen Sie nach, ob sich die Bedeutung geändert hat, und drücken auf übernehmen oder ablehnen

Alles andere ist Aufgabe der Maschine.

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # braucht Go 1.27 oder neuer
cd <Projektroot> && shikii init
```

`shikii init` richtet einen Ordner ein, `.shikii/` (mit `decisions/` für Ihre Entscheidungsdateien, `wiki/` für das Wissen, `records/` und `labels/` für die Aufzeichnungen und Ihre Korrekturen; im Projektroot kommt nichts anderes hinzu), und in einem Zug die Anbindungen an die Agenten: den shikii-MCP-Server für Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) und Codex (`~/.codex/config.toml`), einen Abschnitt in `AGENTS.md` und die ausführlichen Regeln (den Skill) in `~/.claude/skills/shikii`. Ihre eigenen Einträge werden nie angefasst (ein vorhandener shikii-Eintrag bleibt, wie er ist). Nur der Skill-Ordner wird jedes Mal ersetzt, also bearbeiten Sie ihn nicht von Hand. `wiki/` erhält das gemeinsame Wissen (wie man jeden Fragetyp stellt, Formulierungsmuster, die funktioniert haben, die Eigenheiten jedes Modells); der Formulierungsverbesserer (`tune --propose`) liest es, und Seiten, die Sie bearbeitet haben, werden von einem späteren `init` nie überschrieben. Kein Go? Nutzen Sie Docker weiter unten.

Die Einträge zeigen auf ein lokales Modell (`shikii up`, Port 8080). Nur gehostet? Entfernen Sie `--local` von Hand aus jeder der vier Dateien. Konfigurationsdateien für Werkzeuge, die Sie nicht nutzen, werden ebenfalls angelegt; löschen Sie sie oder tragen Sie sie in `.gitignore` ein (ein zweites `shikii init` fasst nie an, was schon da ist). Committen Sie `.shikii/decisions/`, `.shikii/wiki/` und `.shikii/labels/` (Ihre Entscheidungsdateien, das Wissen und Ihre Korrekturen: die Werte, die einen Modellwechsel überdauern). `.shikii/records/` (jede Entscheidung) wächst immer weiter, also ist das Committen freigestellt (`shikii gc` wirft alte weg). Hat Codex bereits `mcp_servers`, wird nichts hinzugefügt, und die einzufügenden Zeilen werden ausgegeben. Ein Projekt mit nur einer `CLAUDE.md` bekommt keine `AGENTS.md` (OpenCode würde sonst aufhören, `CLAUDE.md` zu lesen).

Die Antwortseite selbst zu ändern (`web/`, Svelte) braucht Node 24 (`make web`). shikii zu benutzen nicht: die gebaute Seite ist committet und in die Binärdatei eingebettet.

## Die kürzesten Wege hinein

- **Docker**: kein Go, kein Modell? `docker compose up` startet ein lokales Modell (Kev-4B) und shikii, mit der Antwortseite unter http://127.0.0.1:8095 ([Details](../../integrations/docker.md))
- **Fertige Entscheidungen**: sechs im Ordner `decisions/` dieses Repos (Art der Anfrage, doppelter Fehlerbericht, dieselbe Anfrage, …). Kopieren Sie eine in Ihr `.shikii/decisions/` oder beginnen Sie damit über `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([Liste](../../decisions/README.md))
- **Sie haben schon eine Tabelle vergangener Entscheidungen**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` macht daraus Korrekturen, damit Sie die ersten 50 nicht von Hand beantworten
- **Aus Ihrem eigenen Programm**: starten Sie `shikii serve` und rufen Sie `POST /v1/decide` (entscheiden) und `POST /v1/label` (korrigieren) auf. Der Body ist JSON, und `-H 'Content-Type: application/json'` ist Pflicht (alle Endpunkte: [die lange Anleitung](../guide.md#or-run-one-process-for-programs-and-people))
- **Das Beispiel einer anderen Person**: [Anfrage-Mails weiterleiten](../examples/inquiry-route.md) (Japanisch). Jemand, der shikii zum ersten Mal nutzte, ist allein mit dem README eine ganze Runde gegangen; die Entscheidungsdatei und die 22 Eingaben sind enthalten
- **Das Denken der KI selbst messen** (Claude Code und Codex): `shikii hook install` fügt zwei Hooks hinzu, die shikii vor und nach jedem Durchgang laufen lassen. Vorher: wird diese Anfrage am Ende Dateien ändern, und sollte sich der Agent zuerst auf ein Vorgehen einigen? Nachher: lagen die Änderungen innerhalb dessen, was Sie verlangt haben, und hat er erledigt, was Sie verlangt haben? Die erste Wahrheit ergibt sich kostenlos daraus, ob sich der Arbeitsbaum geändert hat; den Rest beantworten Sie auf einer Seite. Nichts ändert sich, bis eine Linie existiert. Danach wird eine Anfrage, die einen Plan braucht, zu einem Hinweis an den Agenten, und ein Durchgang, der unfertig oder außerhalb des Verlangten aussieht, zu einer Zeile an Sie, mit der Zahl der Wahrheiten, auf denen die Linie ruht, und der von Menschen geprüften Präzision. Kein Hook hält je ein Werkzeug an ([Details](../../integrations/claude-code.md), Japanisch)
- **In CI**: `uses: hayaenfj/shikii@main` lässt `shikii check` bei jedem Pull Request laufen, sodass eine geänderte Formulierung oder eine aufgeweichte Linie den Build scheitern lässt

## Eine ganze Runde

```sh
shikii up                                                             # das lokale Modell starten (beim ersten Lauf wird es heruntergeladen)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (noch keine Linie, also: einen Menschen fragen)
shikii serve --local http://127.0.0.1:8080                                   # Entscheidungsdateien und Wissen werden aus .shikii/ gelesen
#   → antworten unter http://127.0.0.1:8095. Sobald eine Entscheidung die Zahl hat, die sie verlangt (eval.min_n, bei dieser 20), wird die Linie von selbst gezogen
#     Um es mit weniger Antworten zu probieren, senken Sie eval.min_n in der Entscheidungsdatei auf das, was Sie haben (eine mit --force geschriebene Linie wird von check markiert)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (darf handeln, mit dem Beleg: die Linie ruht auf 61 Korrekturen, 48 davon von Menschen. Von den menschlichen Antworten, die diese Linie auf die act-Seite legt, 25, waren 96 % richtig)
```

Es gibt nur vier Bänder. `act` = loslegen, `ask` = bei einem Menschen nachfragen, `discard` = nichts tun, `abstain` = Eingabe fehlt. Eine rohe Wahrscheinlichkeit lesen Sie nie.

## Was neu ist

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii-Architektur: drei Dinge, die Menschen tun, die Werte (Fragen, Wahrheiten, Belege), die Schleife, die die Maschine wachsen lässt, das überprüfbare Wiki, Wegwerf-Modelle" width="100%"></p>

- **Wissen sammelt sich in den Fragen und den Korrekturen an, nicht im Modell.** Das Modell ist also entbehrlich, und ein Wechsel braucht keine menschliche Arbeit. Gemessen: wenn die Maschine Formulierung und Anordnung wählt und ähnliche Beispiele hinzufügt, erreichte ein gewöhnliches Basismodell dieselbe Treffsicherheit wie Feinabstimmung (LoRA) ([Zahlen](../bench.md))
- **Automatische Entscheidungen kommen mit Belegen.** Nicht „95 % sicher“, sondern „Präzision 0,96 auf 48 von Menschen geprüften Antworten“
- **Das Wiki wird nicht gelesen, es wird gemessen.** „So zu fragen funktioniert“ sammelt sich als Behauptungen mit Zahlen an und wird auf neuen Modellen erneut geprüft. Nur ein Mensch erklärt eine Behauptung für bestätigt

## Mehr lesen

- [Die lange Anleitung](../guide.md): jeder Befehl, der vollständige Schnellstart, die HTTP-API, was Sie anschließen können, die gemessenen Ergebnisse und die FAQ · [die Referenz zur Entscheidungsdatei](../../skills/shikii/references/spec.md)
- [Entwurfsnotizen](../decision-ops-framework.md): was das Framework besitzt und was es ablehnt zu besitzen, und was überprüft wurde · [Architektur](../architecture.md) (Japanisch)
- [Fertige Entscheidungen](../../decisions/README.md) · [das Wiki, wie man fragt](../../wiki/index.md) · [Benchmark](../bench.md) · [Modellvergleich](../model-comparison.md) · [Betriebsprotokoll](../operations.md)
- [Anbindungen](../../integrations/mcp.md): MCP, der Adapter, der ein gewöhnliches Chat-Modell in ein Entscheidungsmodell verwandelt, Docker, Claude Code
- [Japanische Anleitung](../guide.ja.md) mit einer vollständigen Runde aus der Praxis

## Mitwirken

Issues und Pull Requests sind willkommen. Ein Test, der ein Verhalten absichert, wird zuerst fehlschlagend gezeigt: das Verhalten absichtlich brechen, den Test laufen lassen, festhalten, wie er fehlschlug, dann beheben und festhalten, dass er besteht. Jede Zahl in den Dokumenten stammt aus einem Skript. Nur Menschen ändern den Status einer Wiki-Behauptung (`confirmed` / `tentative` / `rejected`), mit dem Lauf, der der Beleg ist. Keine neuen Optionen: die Anordnung (`decisions/` `wiki/` `.shikii/` im Projektroot) und feste Regeln erledigen die Arbeit.

## Lizenz

[Apache 2.0](../../LICENSE). Modelllizenzen sind davon getrennt: Kev-4B und die Bonsai-Familie sind Apache 2.0, OpenJev ist CC BY-NC 4.0, das gehostete Jev ist ein Dienst von TypeSafe. shikii speichert Ihre Wahrscheinlichkeiten und Labels, nichts von den Modellen.
