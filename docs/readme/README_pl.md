[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · **Polski** · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: zamień „prawdopodobnie” od AI w „można działać”, na którym da się prowadzić firmę

### Narzędzie, które pozwala AI decydować tak/nie, które lub ile, i utrzymuje tę decyzję godną zaufania w codziennej pracy. Ty sprawdzasz część odpowiedzi i od czasu do czasu potwierdzasz: „czy to nadal to, o co mi chodziło?”. Resztę robi maszyna.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-190%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Problem](#problem) · [Co robi shikii](#co-robi-shikii) · [Co robisz ty](#co-robisz-ty) · [Instalacja](#instalacja) · [Najkrótsze drogi](#najkrótsze-drogi) · [Jedno pełne okrążenie](#jedno-pełne-okrążenie) · [Co nowego](#co-nowego) · [Czytaj dalej](#czytaj-dalej)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: model podaje prawdopodobieństwo, shikii mówi, czy można na jego podstawie działać" width="100%"></p>

## Problem

Zapytaj AI „czy to zgłoszenie dotyczy tej samej sprawy, co poprzednie?”, a odpowie „prawdopodobnie, 0,9”. W prawdziwej pracy to za mało.

- Czy to 0,9 naprawdę trafia 9 razy na 10?
- Czy od dnia, w którym podmieniono model na nowy, nie zaczęło po cichu kłamać?
- Jeśli trochę zmienisz sformułowanie, jak zmieni się trafność?
- Decyzje, które podjęło samo, gdy nikt nie patrzył: czy nadal są słuszne?

Jeśli człowiek musi sprawdzać każdą odpowiedź, używanie AI miało mało sensu.

## Co robi shikii

1. **Zbiera twoje poprawki i wyznacza linię, powyżej której AI może działać samo.** Z odpowiedzi, które podałeś („tak było naprawdę”), mierzy, jak pewność AI ma się do tego, jak często ma rację, i ustala, od jakiego poziomu można mu zaufać. Dopóki nie ma linii, nic nie działa. Gdy już jest, każda automatyczna decyzja niesie pokwitowanie: na ilu sprawdzonych odpowiedziach opiera się linia i jak często były trafne
2. **Próbuje innych sposobów pytania i zachowuje lepszy.** Na tych samych poprawkach ocenia alternatywne pytania, opisy opcji i układy promptu, a przyjmuje nowy tylko wtedy, gdy jest naprawdę lepszy. Ty tylko potwierdzasz, że znaczenie się nie zmieniło
3. **Pozwala wymienić model bez zaczynania od zera.** Poprawki są przechowywane osobno od modelu, więc nowy model korzysta z nich ponownie, a linię maszyna wyznacza na nowo. Bez pracy człowieka
4. **Zapisuje to, czego się nauczyło, w wiki.** „Tak pytać działa” gromadzi się jako twierdzenia ze zmierzonymi liczbami, które można sprawdzić ponownie na innym modelu. To, co inni zmierzyli na swoich modelach, trafia na tę samą stronę

## Co robisz ty

- **Zapisz, co ma być rozstrzygane.** Odpowiedz na kilka pytań i powstaje plik decyzji (`shikii new`, interaktywnie albo z flagami `--type --question --option --input --pii`; pełny format jest w [opisie spec](../../skills/shikii/references/spec.md))
- **Sprawdzaj odpowiedzi.** Strona pokazuje decyzje, przy których AI miało wątpliwości, plus 1 na 20 tych, przy których działało, a ty naciskasz przycisk z tym, co było naprawdę. Klawisze od 1 do 9 i Enter (to samo, co wybrało AI) przyspieszają pracę. „Odpowiedziane” pozwala poprawić wcześniejszą odpowiedź (liczy się najnowsza). „Szczegóły” dodaje „zapytaj AI ponownie”: ten sam tekst jest rozstrzygany na nowo z dzisiejszym sformułowaniem i linią, a „przed” i „po” są pokazane obok siebie; zapisy są tylko dopisywane, nigdy usuwane
- **Od czasu do czasu potwierdź: „czy to nadal to, o co mi chodziło?”.** Gdy maszyna znajdzie sformułowanie, które wypada lepiej, patrzysz, czy znaczenie się zmieniło, i naciskasz „przyjmij” albo „odrzuć”

Cała reszta to zadanie maszyny.

## Instalacja

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # wymaga Go 1.27 lub nowszego
cd <katalog główny projektu> && shikii init
```

`shikii init` tworzy jeden folder `.shikii/` (w nim `decisions/` na twoje spec, `wiki/` na wiedzę, `records/` i `labels/` na zapisy i twoje poprawki; do katalogu głównego projektu nic więcej nie trafia) i za jednym razem ustawia integracje z agentami: serwer MCP shikii dla Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) i Codex (`~/.codex/config.toml`), sekcję w `AGENTS.md` oraz szczegółowe zasady (skill) w `~/.claude/skills/shikii`. Twoje własne wpisy nigdy nie są ruszane (istniejący wpis shikii zostaje bez zmian). Tylko folder skill jest zastępowany za każdym razem, więc nie poprawiaj go ręcznie. Do `wiki/` trafia wspólna wiedza (jak pytać dla każdego typu pytania, sformułowania, które się sprawdziły, nawyki każdego modelu); ulepszacz sformułowań (`tune --propose`) ją czyta, a stron, które poprawiłeś, późniejszy `init` nigdy nie nadpisuje. Nie masz Go? Użyj Dockera poniżej.

Wpisy wskazują na lokalny model (`shikii up`, port 8080). Tylko model w chmurze? Usuń `--local` ręcznie z każdego z czterech plików. Pliki konfiguracyjne powstają też dla narzędzi, których nie używasz; usuń je albo dodaj do `.gitignore` (ponowne `shikii init` nigdy nie rusza tego, co już jest). Commituj `.shikii/decisions/`, `.shikii/wiki/` i `.shikii/labels/` (twoje spec, wiedzę i twoje poprawki: dobra, które przetrwają zmianę modelu). `.shikii/records/` (każda decyzja) ciągle rośnie, więc commitowanie go jest opcjonalne (`shikii gc` usuwa stare). Jeśli Codex ma już `mcp_servers`, nic nie jest dodawane, a linie do wklejenia są wypisywane. Projekt, który ma tylko `CLAUDE.md`, nie dostaje `AGENTS.md` (OpenCode przestałby czytać `CLAUDE.md`).

Zmiana samej strony odpowiedzi (`web/`, Svelte) wymaga Node 24 (`make web`). Używanie shikii nie: zbudowana strona jest w commicie i wbudowana w plik wykonywalny.

## Najkrótsze drogi

- **Docker**: brak Go, brak modelu? `docker compose up` uruchamia lokalny model (Kev-4B) i shikii, ze stroną odpowiedzi pod http://127.0.0.1:8095 ([szczegóły](../../integrations/docker.md))
- **Gotowe decyzje**: sześć w `decisions/` tego repozytorium (typ zgłoszenia, zduplikowany raport o błędzie, to samo zapytanie, …). Skopiuj jedną do swojego `.shikii/decisions/` albo zacznij od niej: `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([lista](../../decisions/README.md))
- **Masz już tabelę dawnych decyzji**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` zamienia ją w poprawki, więc nie odpowiadasz na pierwsze 50 ręcznie
- **Z własnego programu**: uruchom `shikii serve` i wywołuj `POST /v1/decide` (rozstrzygnij) i `POST /v1/label` (popraw). Treść jest w JSON, a `-H 'Content-Type: application/json'` jest wymagany (wszystkie punkty końcowe: [długi przewodnik](../guide.md#or-run-one-process-for-programs-and-people))
- **Przykład kogoś innego**: [kierowanie maili z zapytaniami](../examples/inquiry-route.md) (po japońsku). Osoba używająca shikii po raz pierwszy przeszła całe okrążenie tylko z README; spec i 22 wejścia są dołączone
- **Mierz osąd samego AI** (Claude Code i Codex): `shikii hook install` dodaje hooki, które przepuszczają przez shikii własne decyzje agenta: co robi polecenie, czy edycja mieści się w tym, o co prosiłeś, czy prośba wymaga najpierw planu i co agent ostatecznie zrobi (ta ostatnia prawda jest za darmo, z tego, czy drzewo robocze się zmieniło). Nic się nie zmienia, dopóki nie ma linii. Potem ryzykowne polecenie (usuwanie, zmiana poza projektem, wysyłka na zewnątrz) prosi cię o potwierdzenie, podając, na ilu prawdach opiera się linia i jaką ma precyzję sprawdzoną przez ludzi; bezpieczne polecenia przechodzą po cichu ([szczegóły](../../integrations/claude-code.md), po japońsku)
- **W CI**: `uses: hayaenfj/shikii@main` uruchamia `shikii check` na każdym pull requeście, więc zmienione sformułowanie albo osłabiona linia wywala build

## Jedno pełne okrążenie

```sh
shikii up                                                             # uruchom lokalny model (pobierany przy pierwszym uruchomieniu)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (nie ma jeszcze linii, więc: zapytaj człowieka)
shikii serve --local http://127.0.0.1:8080                                   # spec i wiedza są czytane z .shikii/
#   → odpowiadaj pod http://127.0.0.1:8095. Gdy decyzja zebrała liczbę, o którą prosi (eval.min_n, tu 20), linia wyznacza się sama
#     Żeby spróbować z mniejszą liczbą odpowiedzi, obniż eval.min_n w spec do tego, co masz (linię zapisaną z --force check oznacza)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (można działać, z dowodem: linia opiera się na 61 poprawkach, 48 z nich od ludzi. Z odpowiedzi ludzi, które ta linia stawia po stronie act, czyli 25, 96% było trafnych)
```

Pasm są tylko cztery. `act` = działaj, `ask` = sprawdź z człowiekiem, `discard` = nic nie rób, `abstain` = brakuje danych wejściowych. Nigdy nie czytasz surowego prawdopodobieństwa.

## Co nowego

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architektura shikii: trzy rzeczy, które robią ludzie, dobra (pytania, prawdy, pokwitowania), pętla, którą rozwija maszyna, sprawdzalna wiki, modele jednorazowe" width="100%"></p>

- **Wiedza gromadzi się w pytaniach i poprawkach, nie w modelu.** Dlatego model jest jednorazowy, a jego wymiana nie wymaga pracy człowieka. Zmierzone: gdy maszyna sama wybierała sformułowanie i układ oraz dodawała podobne przykłady, zwykły model bazowy dorównał trafnością dostrajaniu (LoRA) ([liczby](../bench.md))
- **Automatyczne decyzje przychodzą z dowodem.** Nie „95% pewności”, lecz „precyzja 0,96 na 48 odpowiedziach sprawdzonych przez ludzi”
- **Wiki się nie czyta, wiki się mierzy.** „Tak pytać działa” gromadzi się jako twierdzenia z liczbami i jest sprawdzane ponownie na nowych modelach. Tylko człowiek decyduje, że twierdzenie jest potwierdzone

## Czytaj dalej

- [Długi przewodnik](../guide.md): każde polecenie, pełny szybki start, HTTP API, co można podłączyć, zmierzone wyniki i FAQ · [opis spec](../../skills/shikii/references/spec.md)
- [Notatki projektowe](../decision-ops-framework.md): za co framework odpowiada, za co odmawia odpowiadać i co zostało sprawdzone · [Architektura](../architecture.md) (po japońsku)
- [Gotowe decyzje](../../decisions/README.md) · [wiki o tym, jak pytać](../../wiki/index.md) · [benchmark](../bench.md) · [porównanie modeli](../model-comparison.md) · [dziennik operacyjny](../operations.md)
- [Połączenia](../../integrations/mcp.md): MCP, adapter, który zamienia zwykły model czatowy w model decyzyjny, Docker, Claude Code
- [Przewodnik po japońsku](../guide.ja.md) z pełnym okrążeniem z prawdziwego życia

## Współpraca

Issues i pull requesty są mile widziane. Test, który strzeże zachowania, najpierw pokazuje się jako niezdany: celowo zepsuj zachowanie, uruchom test, zapisz, jak nie przeszedł, potem napraw i zapisz, że przechodzi. Każda liczba w dokumentacji pochodzi ze skryptu. Tylko ludzie zmieniają status twierdzenia w wiki (`confirmed` / `tentative` / `rejected`), wraz z uruchomieniem, które jest dowodem. Żadnych nowych opcji: układ (`decisions/` `wiki/` `.shikii/` w katalogu głównym projektu) i stałe zasady wykonują pracę.

## Licencja

[Apache 2.0](../../LICENSE). Licencje modeli są osobne: Kev-4B i rodzina Bonsai to Apache 2.0, OpenJev to CC BY-NC 4.0, hostowany Jev to usługa TypeSafe. shikii przechowuje twoje prawdopodobieństwa i etykiety, nic z modeli.
