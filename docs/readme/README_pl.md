[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · **Polski** · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: zamień „prawdopodobnie” od AI w „można działać”, na którym da się prowadzić firmę

### Narzędzie, które pozwala AI decydować „tak czy nie”, „które” i „ile”, i rozwija je z twoich poprawek. Ty robisz tylko jedno: odpowiadasz na stronie, jak było naprawdę.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-381%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Problem](#problem) · [Zastosowanie 1: zmierz myślenie samego AI](#zastosowanie-1-zmierz-myślenie-samego-ai) · [Zastosowanie 2: powierz mu decyzję biznesową](#zastosowanie-2-powierz-mu-decyzję-biznesową) · [Co robisz na stronie](#co-robisz-na-stronie) · [Instalacja](#instalacja) · [Dlaczego można wymienić AI](#dlaczego-można-wymienić-ai) · [Czytaj dalej](#czytaj-dalej)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Problem

Zapytaj AI „czy to zgłoszenie dotyczy tej samej sprawy, co poprzednie?”, a odpowie „prawdopodobnie, 0,9”. W pracy to za mało. Czy to 0,9 naprawdę trafia dziewięć razy na dziesięć? Czy od dnia, w którym przeszedłeś na nowsze AI, nie stało się po cichu kłamstwem? Czy decyzje, które podejmuje, gdy nikt nie patrzy, nadal są słuszne? Jeśli człowiek musi sprawdzać za każdym razem, używanie AI miało mało sensu.

shikii wyznacza z twoich poprawek **linię**: powyżej niej AI może działać samo. Dopóki nie ma linii, niczemu się nie ufa. Gdy już jest, każda automatyczna decyzja niesie swoje uzasadnienie: na ilu poprawkach opiera się linia i jak często miała rację. Linia mieszka przy pytaniu i poprawkach, nie wewnątrz AI, więc wymiana AI nie kosztuje ani chwili pracy człowieka.

Są dwa sposoby użycia: mierzenie myślenia AI, które pisze twój kod (najlżejszy), i powierzenie mu decyzji z twojej firmy.

## Zastosowanie 1: zmierz myślenie samego AI

Dla osób używających Claude Code lub Codex. Agent piszący kod podejmuje osądy, gdy otrzymuje prośbę i gdy kończy pracę; shikii mierzy je po cichu.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

To wszystko. Instaluje plik wykonywalny, dodaje dwa hooki do Claude Code i Codex, umieszcza zasady agenta (skill) i uruchamia w tle małe lokalne AI (Kev-4B) (jeśli brakuje llama.cpp, wypisuje jedną linijkę, jak je zainstalować). Otwórz nowy terminal i pracuj z agentem jak zwykle (w Codex uruchom raz `/hooks`, aby zaufać hookom shikii). Mierzone są cztery rzeczy.

| Kiedy | Pytanie | Skąd pochodzi prawda |
|---|---|---|
| Przychodzi prośba | Czy ta prośba skończy się zmianą plików? | Automatycznie: czy drzewo robocze zmieniło się przed następną prośbą |
| Przychodzi prośba | Czy agent powinien uzgodnić z tobą podejście, zanim czegokolwiek dotknie? | Ty, na stronie |
| Tura się kończy | Czy zmiany mieściły się w tym, o co prosiłeś? | Ty, na stronie |
| Tura się kończy | Czy skończył to, o co prosiłeś? | Ty, na stronie |

Na końcu tury pojawia się jedna linijka, `shikii: 3 decyzje z tej sesji czekają na twoją odpowiedź: http://127.0.0.1:8096/`, a ty otwierasz ją, gdy masz chwilę (hook uruchamia stronę w tle); każda odpowiedź zajmuje sekundy. Po 50 odpowiedziach wyznaczana jest linia i zachowanie agenta się zmienia: prośba, która wymaga planu, staje się notatką dla agenta, a tura, która wygląda na niedokończoną albo wychodzącą poza zakres, staje się jedną linijką dla ciebie, każda z uzasadnieniem takim jak „linia na 61 prawdach, precyzja sprawdzona przez ludzi 0,96”. Do tego czasu hooki tylko zapisują. Żaden hook nigdy nie zatrzymuje narzędzia.

Wszystko mieszka w jednym miejscu, `~/.shikii/agent`, z jedną linią na pytanie dla wszystkich twoich projektów (pytania dotyczą myślenia agenta, więc prawda jest taka sama w każdym repozytorium). Każdy zapis jest oznaczony repozytorium, z którego pochodzi, a strona potrafi po nim filtrować. Szczegóły: [hook](../../integrations/claude-code.md) (po japońsku).

## Zastosowanie 2: powierz mu decyzję biznesową

Dla decyzji, które powtarzasz w pracy, takich jak „czy to zgłoszenie dotyczy tej samej sprawy?” albo „czy ta wiadomość to prośba o zmianę?”: pozwól AI decydować i rozwijaj je z twoich poprawek. Tu też nie ma polecenia do wpisania. Mówisz agentowi.

> Chcę, żeby shikii rozstrzygało, czy zgłoszenie dotyczy tej samej sprawy, co wcześniejsze, i żeby rozwijało się z moich poprawek

Agent postępuje według swoich zasad (skill): tworzy w projekcie `.shikii/` (`shikii init`), ustala pytanie, opcje i dane wejściowe i zapisuje plik decyzji (`shikii new`; pokazuje ci pytanie i opcje raz, bo to jest twoja intencja), wywołuje decyzję (`shikii decide`), uruchamia stronę odpowiedzi (http://127.0.0.1:8095) i daje ci znać. Dopóki nie ma linii, żadna decyzja nie jest `act` (działaj). Gdy odpowiadasz na stronie, linia wyznacza się sama, kiedy decyzja osiągnie swoją własną liczbę (w tym przykładzie 20), a od tej chwili:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (można działać. Linia opiera się na 61 prawdach, 48 z nich od ludzi; z odpowiedzi ludzi, które ta linia postawiłaby po stronie act, czyli 25, 96% było trafnych)
```

Wartości `band` są tylko cztery: `act` = działaj, `ask` = sprawdź z człowiekiem, `discard` = nic nie rób, `abstain` = brakuje danych wejściowych. Nigdy nie działasz na podstawie surowego prawdopodobieństwa.

- **Ręcznie**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Wszystko jest w [przewodniku](../guide.md)
- **Z agenta**: `shikii init` zapisuje konfigurację MCP, więc Claude Code, Cursor, OpenCode i Codex mogą bezpośrednio wywoływać narzędzia `shikii_decide` i `shikii_label` ([MCP](../../integrations/mcp.md))
- **Z własnego programu**: uruchom `shikii serve` i wywołuj `POST /v1/decide` (rozstrzygnij) i `POST /v1/label` (popraw). Treść jest w JSON i wymaga `-H 'Content-Type: application/json'` ([HTTP API](../guide.md#or-run-one-process-for-programs-and-people))
- **Gotowe decyzje**: sześć w `decisions/` tego repozytorium (typ zgłoszenia, zduplikowany raport o błędzie, to samo zapytanie, …). Skopiuj jedną poleceniem `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` i przeredaguj ją ([lista](../../decisions/README.md))
- **Masz już tabelę dawnych decyzji**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` zamienia ją w poprawki, więc nie musisz odpowiadać na pierwsze 50 ręcznie
- **Docker**: nie masz jeszcze Go ani modelu? `docker compose up` uruchamia lokalne AI i shikii, ze stroną pod http://127.0.0.1:8095 ([szczegóły](../../integrations/docker.md))
- **Przykład kogoś innego**: [kierowanie maili z zapytaniami](../examples/inquiry-route.md) (po japońsku). Osoba używająca shikii po raz pierwszy przeszła całe okrążenie tylko z README; plik decyzji i 22 wejścia są dołączone

## Co robisz na stronie

Na stronie odpowiedzi (`shikii serve`) człowiek robi trzy rzeczy. Cała reszta to zadanie maszyny.

1. **Odpowiadasz.** Strona pokazuje decyzje, przy których AI miało wątpliwości, plus 1 na 20 tych, przy których działało, a ty naciskasz przycisk z tym, co było naprawdę. Klawisze od 1 do 9 i Enter (to samo, co wybrało AI) przyspieszają pracę. „Odpowiedziane” pozwala poprawić wcześniejszą odpowiedź (liczy się najnowsza)
2. **Piszesz inne sformułowanie.** Jeśli myślisz „gdyby zapytać tak, wyszłoby lepiej”, wpisz to w panelu „Sformułowanie” własnymi słowami. Maszyna ocenia je wobec obecnego sformułowania na tych samych poprawkach (dla decyzji samego agenta automatycznie co 50 nowych odpowiedzi) i mówi „można przyjąć” tylko wtedy, gdy jest naprawdę lepsze. Możesz też pozwolić AI zaproponować sformułowania. Nigdy nie otwierasz pliku
3. **Naciskasz „przyjmij” albo „odrzuć”.** Patrzysz, czy znaczenie się zmieniło, i naciskasz. Maszyna sprawdza liczby; ty sprawdzasz znaczenie

„Szczegóły” dodają stan linii dla każdego modelu, „zapytaj AI ponownie” (ten sam tekst rozstrzygany na nowo z dzisiejszym sformułowaniem i linią, z „przed” i „po” obok siebie) oraz panel wiedzy (twierdzenia postaci „tak pytać działa” wraz z ich dowodami). Zapisy są tylko dopisywane, nigdy usuwane; nowa decyzja zajmuje miejsce starej.

## Instalacja

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 lub nowsze; w przeciwnym razie Docker (powyżej)
```

Co robi install.sh: instaluje pliki wykonywalne (`shikii` i `shikii-readout`), dodaje dwa hooki do Claude Code i Codex, umieszcza zasady agenta (skill) w `~/.claude/skills/shikii`, a jeśli llama.cpp jest obecne, uruchamia w tle lokalne AI (Kev-4B na :8080). Aby zamiast tego użyć hostowanego Jev albo OpenRouter, umieść klucz w zmiennej środowiskowej ([połączenia](../guide.md#what-you-can-connect)). `shikii doctor` mówi, co działa, a czego brakuje.

Przygotowanie projektu (`shikii init`) to zadanie agenta (możesz je też uruchomić sam). Tworzy jeden folder, `.shikii/` (w nim: `decisions/` = pliki decyzji, `wiki/` = wiedza, `records/` i `labels/` = zapisy i poprawki). W katalogu głównym projektu nie pojawia się nic więcej. `decisions/`, `wiki/` i `labels/` to dobra, które przetrwają zmianę AI, więc je commituj; `records/` ciągle rośnie, więc commitowanie go jest opcjonalne (`shikii gc` usuwa stare). Dodaje też wpis shikii do konfiguracji twoich agentów (`.mcp.json` w Claude Code, `.cursor/mcp.json` w Cursor, `opencode.json` w OpenCode, `~/.codex/config.toml` w Codex), nie ruszając wpisów, które napisałeś. Ponowne uruchomienie nie zmienia nic, co już istnieje.

Wyjście poleceń i komunikaty hooków są po angielsku (jeśli język w ustawieniach Claude Code to japoński, komunikaty hooków są pokazane po japońsku obok angielskiego). Strona ma wersję japońską. Drobniejsze kwestie, takie jak używanie wyłącznie hostowanego AI, `AGENTS.md` kontra `CLAUDE.md` i Node do zmiany samej strony, są w [przewodniku](../guide.md).

## Dlaczego można wymienić AI

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architektura shikii: trzy rzeczy, które robią ludzie, dobra (pytania, prawdy, pokwitowania), pętla, którą rozwija maszyna, sprawdzalna wiki, model jednorazowy" width="100%"></p>

- **Wiedza gromadzi się przy pytaniu i poprawkach, nie wewnątrz AI.** Poprawki przetrwają AI, więc nowe może z nich korzystać od razu, a linia jest wyznaczana na nowo. W naszych pomiarach pozwolenie maszynie wybrać sformułowanie i układ oraz dodać podobne przykłady dało tę samą trafność, co dostrajanie wag modelu (LoRA) ([liczby](../bench.md))
- **Automatyczne decyzje niosą pokwitowanie.** Nie „95% pewności”, lecz „precyzja 0,96 na 48 przypadkach sprawdzonych przez ludzi”
- **Wiki się nie czyta, wiki się mierzy.** „Tak pytać działa” gromadzi się jako twierdzenia z liczbami i można je sprawdzić ponownie na nowym AI. Tylko człowiek decyduje, że twierdzenie jest potwierdzone

## Czytaj dalej

- [Przewodnik](../guide.md): każde polecenie, HTTP API, FAQ · [opis spec](../../skills/shikii/references/spec.md)
- [Hook](../../integrations/claude-code.md) (po japońsku): jak mierzone jest myślenie samego agenta, tabela pasmo → zachowanie, budowa wewnętrzna
- [Architektura](../architecture.md) (po japońsku): cały obraz stojący za rysunkiem powyżej
- [Gotowe decyzje](../../decisions/README.md) · [wiki o tym, jak pytać](../../wiki/index.md) · [zmierzone liczby](../bench.md)
- [Połączenia](../../integrations/mcp.md): MCP, adapter dla zwykłych modeli czatowych, Docker

**Dla współtwórców**: celowo zepsuj test i zobacz, jak nie przechodzi, zanim go naprawisz. Każda liczba pochodzi ze skryptu. Tylko człowiek potwierdza twierdzenie w wiki. Nie dodawaj ustawień; zamiast tego stałe miejsca i stałe zasady.

## Licencja

[Apache-2.0 OR MIT](../../LICENSE). Licencje modeli są osobne (Kev-4B i rodzina Bonsai to Apache 2.0, OpenJev to CC BY-NC 4.0, hostowany Jev to usługa TypeSafe).
