[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · **Polski** · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: obsługa modeli decyzyjnych

### Jeden cykl życia dla każdego modelu decyzyjnego: define → decide → record → label → calibrate → check → drift.

**Model daje prawdopodobieństwo. shikii mówi, czy wolno na jego podstawie działać.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-189%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Notatki projektowe](../decision-ops-framework.md) · [Eksploatacja](../operations.md) · [Porównanie modeli](../model-comparison.md)**

[Szybki start](#szybki-start) · [Jak działa shikii](#jak-działa-shikii) · [Co można podłączyć](#co-można-podłączyć) · [Wyniki](#wyniki) · [FAQ](#faq) · [Dokumentacja](#dokumentacja)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii nigdy nie uruchamia modelu i nigdy nie czyta keychaina. Łączy się z endpointem modelu decyzyjnego, który już masz, prowadzi rejestr i odmawia **act**, dopóki prawdopodobieństwa dla *tego pytania na tym modelu* nie zostaną zweryfikowane na etykietach. Bloki kalibracji są generowane, nigdy pisane ręcznie.

---

Modele decyzyjne (TypeSafe Jev, llama.cpp Kev-4B i OpenJev, OpenRouter Decisions, …) odpowiadają na typowane pytania, `choice`, `score` i `noul` (tak/nie), samymi prawdopodobieństwami, bez generowania tekstu. Każdy dostawca dołącza to samo zastrzeżenie: *prawdopodobieństwa nie są skalibrowane na Twoich danych, a progi nie przenoszą się między modelami ani pytaniami.*

shikii zajmuje się tym, czego nikt nie dostarcza: **cyklem życia** decyzji. Plik spec zawiera pytanie, opcje, szablon stanu i progi. `decide` pyta model i zwraca strefę (`act` / `ask` / `discard` / `abstain`), nigdy surową liczbę. `label` zapisuje, co było prawdą. `calibrate` dopasowuje i waliduje krzyżowo, po czym zapisuje wynik z powrotem do spec pod kluczem złożonym z hasha pytania i nazwy modelu. `check` wywala CI, gdy zmieniło się brzmienie pytania albo model. `gc` utrzymuje store w małym rozmiarze.

W zestawie jest też `shikii-readout`, 500-liniowy adapter, który zamienia **dowolny czatowy GGUF na zwykłym llama.cpp** w model decyzyjny, odczytując logity liter opcji. W naszych pomiarach zwykły 27B model ternarny odczytywany w ten sposób dorównał trenowanym modelom decyzyjnym pod względem trafności, przy 0,6 sekundy na pytanie na laptopie.

> Nazwa jest japońska: *shikii* (閾) oznacza po japońsku próg, czyli granicę, która zamienia prawdopodobieństwo w działanie. CLI nazywa się `shikii`.

## Szybki start

Go 1.27 lub nowszy. Do pierwszego uruchomienia nie potrzeba niczego więcej.

### Uruchom cały cykl bez klucza i bez GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # fałszywy model: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # fałszywy model, którego prawdopodobieństwa wymagają kalibracji; zobacz, jak calibrate wybiera platt
```

### Skieruj go na prawdziwy model

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) lokalny model decyzyjny na llama.cpp (przy pierwszym uruchomieniu pobiera Kev-4B, ok. 2,5 GB)
make server                                       # llama-server na :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) hostowany TypeSafe Jev. Klucz pochodzi wyłącznie ze zmiennych środowiskowych
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

Odpowiedź to strefa, trace id i prawdopodobieństwa. Dopóki spec nie zawiera kalibracji dla tego modelu, strefa to co najwyżej `ask`.

### Zamknij pętlę

```sh
bin/shikii label <trace_id> change_request          # lub:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <nazwa modelu z odpowiedzi> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1, gdy zmieniło się brzmienie albo precision@act spadło poniżej act
```

### Użyj dowolnego GGUF jako modelu decyzyjnego

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # zwykły llama-server z flash attention i 8-bitowym KV, :8086
make readout                                               # shikii-readout przed nim, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Instalacja i aktualizacja

```sh
make install          # kopiuje bin/shikii do ~/.local/bin/shikii
git pull && make install
```

## Jak działa shikii

Każda decyzja to jeden plik YAML. Pytanie, opcje, szablon stanu, dokąd wolno wysyłać stan, progi oraz, generowane przez `calibrate --write`, bloki kalibracji.

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
  required: [message]            # brak danych wejściowych → abstain; model nie jest wywoływany
policy:
  send_to: [hosted, local]       # sesja z taint automatycznie pomija hosted
  pii: possible                  # rekordy zawierają hash danych wejściowych, nigdy tekst
thresholds:
  act: 0.75                      # ≥ act → działaj automatycznie, tylko jeśli skalibrowano
  ask: 0.60                      # ≥ ask → zapytaj człowieka; poniżej → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Kluczem kalibracji jest `(hash pytania + szablonu + opcji, id modelu zwrócone przez endpoint)`.** Zmień jedno słowo w pytaniu albo pozwól, by `jev-latest` przeskoczył na nową wersję, a kalibracja po prostu zniknie: strefa spada do `ask`, zamiast po cichu działać na nieaktualnych stałych. Układ promptu też należy do klucza: `shikii-readout` podaje swój układ w nazwie modelu (`…+state-last`), ponieważ przesunięcie stanu wewnątrz promptu zmieniło trafność modelu 27B o 5 punktów przy tych samych wagach.

`calibrate` porównuje trzy metody w 5-krotnej walidacji krzyżowej i zapisuje zwycięzcę. `identity` oznacza, że surowe prawdopodobieństwa już spełniają precision@act, a model jest po prostu oznaczany jako zweryfikowany. `platt` (σ((Δ−bias)/T)) służy do pytań tak/nie. `vector` (softmax((log p + b)/T)) służy do choice i score i naprawia nawyki poszczególnych opcji, takie jak „ucieka w *other*". Etykiety częściowe (`!option`, „to nie było *to*") liczą się do precision, ale nie do dopasowania. Etykiety wczytane z pliku są zapisywane trwale, więc `check` i `drift` działają potem offline.

Każda gałąź istotna dla bezpieczeństwa (routing taint, brak act bez kalibracji, unieważnianie nieaktualnego hasha, walidacja etykiet, wykrywanie inwersji, wybór metody przez CV, `gc` w trybie dry-run, …) ma zapisaną mutację, która zepsuła test, zanim poprawka została przyjęta. Jest 63 testów Go w 8 pakietach; demo działa w CI.

## Co można podłączyć

shikii mówi kształtem żądania System One (`POST /v1/systemone`), więc działa z wszystkim, co go obsługuje. Resztę pokrywa adapter.

| Cel | Co dostaje shikii | Wymagania i instrukcja |
|---|---|---|
| **TypeSafe Jev** (hostowany) | `choice` / `score` / `noul` z prawdopodobieństwami, id modelu w rodzaju `jev-1.13.0` | `TYPESAFE_API_KEY` w środowisku. Domyślny `--hosted` |
| **Modele decyzyjne llama.cpp** (Kev-4B, OpenJev, Clef, …) | Ten sam kształt, lokalnie, id modelu z `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 lub nowszy |
| **OpenRouter Decisions API** | Ten sam kształt przez OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Dowolny czatowy GGUF** przez `shikii-readout` | Odczyt logitów liter w kształcie System One, bez własnej kalibracji | Za nim zwykły `llama-server` 0.7.0. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** przez [BonJev](https://github.com/ChezRD/bonjev) | To samo, z własnym promptem BonJev i wbudowaną kalibracją | Build w Rust i fork Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Fałszywy model** (`bin/fake-systemone`) | Deterministyczne prawdopodobieństwa do dem i testów | Nic. `make fake` |
| **Hook Claude Code** (claude-jev) | Każdy prompt sklasyfikowany, każda tura oznaczona etykietą na podstawie drzewa roboczego | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint jest pełnoprawnym wejściem: przekaż `--tainted` (albo pozwól to zrobić hookowi) w sesji, która dotknęła danych produkcyjnych, a żądanie nigdy nie opuści maszyny. Jeśli nie skonfigurowano lokalnego endpointu, decyzja kończy się abstain.

## Wyniki

Te same spec, te same etykiety, cztery zbiory ewaluacyjne (intencja promptu po angielsku i japońsku, 62 żądania; łączenie tych samych przypadków na 32 japońskich parach). Szczegóły i pełna tabela są w [`docs/model-comparison.md`](../model-comparison.md).

| Model | Licencja | top-1 (całość / ja) | Brier tak/nie (surowy) | Wybrana kalibracja | Sekundy na pytanie |
|---|---|---|---|---|---|
| Kev-4B (trenowany model decyzyjny) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (trenowany) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B przez BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B przez `shikii-readout`, zwykły llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**Sformułowanie wygrywa z kalibracją.** W intencji promptu 19 z 21 błędów trafiało w *other*; przeredagowanie opcji podniosło top-1 z 0,68 do 0,90 jeszcze przed jakąkolwiek kalibracją. Kalibracja jest na „dobrą kolejność, za małą pewność". Macierz pomyłek drukowana przez `calibrate` pokazuje, z którym przypadkiem masz do czynienia.

**Modele już skalibrowane nie potrzebują Platta.** Tak/nie Kev-4B miało surowy Brier 0,046; Platt pogorszył go w walidacji krzyżowej, więc zapisano `identity`. Po raz pierwszy `calibrate` wybrał `platt` przy zwykłym LLM odczytywanym przez logity. To, czy kalibracja jest potrzebna, jest cechą *modelu wraz z odczytem*, a nie samego modelu.

**Zwykły GGUF ląduje obok trenowanego modelu decyzyjnego.** Czatowy model ternarny 27B, główna gałąź llama.cpp 0.7.0, jeden adapter, bez forka, bez trenowania, bez Rusta: to samo top-1 co najlepszy trenowany model na licencji Apache, japoński 1.000, 0,58 s na pytanie z flash attention i rozgrzanym prefiksem. Przesunięcie stanu sprzed opcji tuż przed odpowiedź podniosło wynik z 0,919 do 0,968 przy tych samych wagach, dlatego układ jest częścią klucza kalibracji.

## FAQ

**Czy shikii uruchamia modele?** Nie. Wysyła żądania System One do endpointu, który sam prowadzisz lub wynajmujesz, i przechowuje wszystko, co dzieje się po odpowiedzi. `shikii-readout` jest jedynym wyjątkiem z ducha: zamienia logity modelu czatowego na ten sam kształt, ale inferencję nadal wykonuje llama.cpp.

**Dlaczego moja decyzja nigdy nie dochodzi do `act`?** Ponieważ spec nie ma bloku kalibracji dla `(hash tego pytania, id tego modelu)`. Zbierz etykiety (`label` albo `--labeled file`), uruchom `calibrate --write`, a `status` pokaże `calibrated: identity|platt|vector`. Decyzja bez zweryfikowanej precision na progu act nie ma prawa działać. O to właśnie chodzi.

**Zmieniłem jedno słowo w pytaniu i kalibracja zniknęła.** Zgadza się. Hash się zmienił, a stałe były dopasowane do innego pytania. `status` pokazuje stare etykiety jako `stale hash`; uruchom `calibrate --write` ponownie, gdy będziesz mieć etykiety dla nowego brzmienia.

**Którego modelu użyć?** To zależy od licencji, szybkości i języka, a shikii nie decyduje za Ciebie. Tabela powyżej i [`docs/model-comparison.md`](../model-comparison.md) są argumentem: przy tym samym spec i tych samych etykietach możesz wybierać na podstawie liczb, łącznie z tym, czy model w ogóle potrzebuje kalibracji. `scripts/eval-model.sh <url> <name>` odtwarza tabelę dla nowego endpointu.

**Czy działa po japońsku?** Tak, i tam różnice są widoczne. Kev-4B uzyskuje 0,913 na japońskich promptach; modele 27B uzyskują 1,000. Łączenie tych samych przypadków na japońskich streszczeniach śledztw w pełni oddziela pozytywy od negatywów w każdym modelu 27B.

**Dokąd trafiają moje dane?** Tylko wyrenderowany stan trafia do endpointu wskazanego w `send_to`. Przy `pii: possible` (domyślnie) store przechowuje hash danych wejściowych, nigdy tekst. `--tainted` zostawia wszystko lokalnie. Klucze są czytane z `DECISIONS_API_KEY` lub `TYPESAFE_API_KEY` i z nigdzie indziej.

**Store ciągle rośnie.** `shikii gc` usuwa rekordy starsze niż 30 dni, które nigdy nie dostały etykiety. Rekordy z etykietą i same etykiety zostają na zawsze, bo są materiałem do kalibracji. Bez `--apply` to tylko dry run.

**Czy faworyzuje pierwszą opcję?** Zmierz, zanim zaczniesz poprawiać: `shikii bias <spec> --permutations 100` tasuje opcje i raportuje przeskoki oraz przesunięcie pozycji. Kev-4B: 0 przeskoków na 100, przesunięcie ≤ 0.024. Zwykłe LLM odczytywane przez logity mogą się różnić; to polecenie istnieje dla nich.

## Dokumentacja

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): notatki projektowe, co framework posiada, a czego odmawia posiadania, plan weryfikacji i to, co potwierdzono.
- [`docs/operations.md`](../operations.md): jak zatrzymać lub wycofać każdy ruchomy element, jak czytać awarię, czego jeszcze brakuje.
- [`docs/model-comparison.md`](../model-comparison.md): tabela porównawcza i sposób jej odtworzenia.
- [`docs/guide.ja.md`](../guide.ja.md): obszerny przewodnik po japońsku z każdym poleceniem i pełnym przykładem z realnego użycia.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): trzy połączenia, które uruchamiamy.
- [`examples/`](../../examples/) i [`testdata/`](../../testdata/README.md): spec bez kalibracji jako punkt wyjścia oraz syntetyczne zbiory z etykietami używane w testach.

Polecenia:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, ręcznie pisane bloki, precision@act poniżej act → exit 1
shikii drift     <spec> --model M [--window N]            # ostatnie etykiety wobec Briera i precision zapisanych przy kalibracji
shikii bias      <spec> [--rows K] [--permutations N]     # czy odpowiedź zależy od kolejności opcji?
shikii status    [spec...] [--store DIR]                  # inwentarz: rekordy, etykiety, źródła, stan dla każdej pary (model, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # usuń stare rekordy bez etykiety; domyślnie dry run
shikii hash      <spec>
```

## Współtworzenie

Zgłoszenia i pull requesty są mile widziane. Jedna zasada: test chroniący jakieś zachowanie trzeba najpierw pokazać w stanie czerwonym. Zepsuj to zachowanie celowo, uruchom test, zapisz, jak się wywalił, potem napraw i zapisz, że przechodzi. Dokumentacja wylicza te mutacje dla każdej gałęzi istotnej dla bezpieczeństwa; utrzymuj tę listę uczciwą.

## Licencja

[Apache 2.0](../../LICENSE). Licencje modeli są osobne: Kev-4B i rodzina Bonsai są na Apache 2.0, OpenJev na CC BY-NC 4.0, hostowany Jev to usługa TypeSafe. shikii przechowuje Twoje prawdopodobieństwa i etykiety, nic z modeli.
