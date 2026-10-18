[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · **Русский** · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Эксплуатация моделей решений

### Один жизненный цикл для любой модели решений: define → decide → record → label → calibrate → check → drift.

**Модель выдаёт вероятность. shikii говорит, можно ли действовать на её основе.**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-161%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Заметки по проектированию](../decision-ops-framework.md) · [Эксплуатация](../operations.md) · [Сравнение моделей](../model-comparison.md) · [Подробное руководство (на японском)](../guide.ja.md)**

[Быстрый старт](#быстрый-старт) · [Как работает shikii](#как-работает-shikii) · [Что можно подключить](#что-можно-подключить) · [Результаты](#результаты) · [FAQ](#faq) · [Документация](#документация)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii никогда не запускает модель и никогда не читает связку ключей. Он обращается к уже имеющемуся у вас эндпоинту модели решений, ведёт записи и отказывается **действовать** (`act`), пока вероятности для *этого вопроса на этой модели* не проверены по меткам. Блоки калибровки генерируются, а не пишутся вручную.

---

Модели решений (TypeSafe Jev, llama.cpp Kev-4B и OpenJev, OpenRouter Decisions и др.) отвечают на типизированные вопросы, `choice`, `score` и `noul` (да/нет), вероятностями, без генерации текста. У каждого вендора одна и та же оговорка: *вероятности не откалиброваны под ваши данные, а пороги не переносятся между моделями и вопросами.*

shikii берёт на себя то, что никто не поставляет: **жизненный цикл** решения. Файл spec хранит вопрос, варианты, шаблон состояния и пороги. `decide` обращается к модели и возвращает зону (`act` / `ask` / `discard` / `abstain`), но никогда не сырое число. `label` записывает, что было верно. `calibrate` подбирает параметры, проверяет их кросс-валидацией и записывает результат обратно в spec под ключом из хеша вопроса и имени модели. `check` роняет CI, если формулировка или модель сместились. `gc` не даёт хранилищу разрастаться.

В комплект входит и `shikii-readout`, адаптер на 500 строк, который превращает **любую чат-модель в формате GGUF на обычном llama.cpp** в модель решений, считывая логиты букв вариантов. По нашим измерениям, обычная тернарная модель на 27B, прочитанная таким образом, сравнялась по точности с обученными моделями решений при 0.6 секунды на вопрос на ноутбуке.

> Имя японское: *shikii* (閾) по-японски означает «порог», черту, которая превращает вероятность в действие. CLI называется `shikii`.

## Быстрый старт

Go 1.24 или новее. Для первого запуска больше ничего не нужно.

### Весь цикл без ключа и без GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # фиктивная модель: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # фиктивная модель с завышенными вероятностями; смотрим, как calibrate выбирает platt
```

### Подключение к настоящей модели

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) локальная модель решений на llama.cpp (при первом запуске скачивает Kev-4B, около 2.5 ГБ)
make server                                       # llama-server на :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) облачный TypeSafe Jev. Ключ берётся только из окружения
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

В ответе приходят зона, trace id и вероятности. Пока в spec нет калибровки для этой модели, зона не выше `ask`.

### Замкнуть цикл

```sh
bin/shikii label <trace_id> change_request          # или:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <имя модели из ответа> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1, если формулировка изменилась или precision@act упал ниже act
```

### Любой GGUF как модель решений

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # обычный llama-server с flash attention и 8-битным KV, :8086
make readout                                               # перед ним shikii-readout, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Установка и обновление

```sh
make install          # копирует bin/shikii в ~/.local/bin/shikii
git pull && make install
```

## Как работает shikii

Каждое решение это один YAML-файл. Вопрос, варианты, шаблон состояния, куда можно отправлять состояние, пороги и, сгенерированные командой `calibrate --write`, блоки калибровки.

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
  required: [message]            # нет входных данных → abstain; модель не вызывается
policy:
  send_to: [hosted, local]       # tainted-сессия автоматически пропускает hosted
  pii: possible                  # в записях хранится хеш входных данных, но не текст
thresholds:
  act: 0.75                      # ≥ act → действовать автоматически, только если откалибровано
  ask: 0.60                      # ≥ ask → спросить человека; ниже → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Ключ калибровки: `(хеш вопроса + шаблона + вариантов, id модели в том виде, как его вернул эндпоинт)`.** Измените в вопросе одно слово или позвольте `jev-latest` перейти на новую версию, и калибровка просто исчезнет: зона опустится до `ask`, а не будет молча действовать по устаревшим константам. Раскладка промпта тоже входит в ключ: `shikii-readout` сообщает свою раскладку в имени модели (`…+state-last`), потому что перенос состояния внутри промпта изменил точность модели на 27B на 5 пунктов при тех же весах.

`calibrate` сравнивает три метода при 5-fold кросс-валидации и записывает победителя. `identity` значит, что сырые вероятности уже обеспечивают precision@act, и модель просто помечается как проверенная. `platt` (σ((Δ−bias)/T)) подходит для вопросов да/нет. `vector` (softmax((log p + b)/T)) подходит для choice и score и исправляет привычки по отдельным вариантам, например «при неуверенности уходит в *other*». Частичные метки (`!option`, «это было не *то*») учитываются в precision, но не в подгонке. Метки, полученные из файла, сохраняются, поэтому `check` и `drift` потом работают офлайн.

У каждой ветки, влияющей на безопасность (маршрутизация по taint, запрет `act` без калибровки, инвалидация по устаревшему хешу, валидация меток, обнаружение инверсии, выбор метода по CV, `gc` в режиме dry-run и т. д.), есть зафиксированная мутация, которая роняла тест до того, как исправление было принято. Всего 63 Go-теста в 8 пакетах; демо запускается в CI.

## Что можно подключить

shikii использует формат запроса System One (`POST /v1/systemone`), поэтому подойдёт всё, что его обслуживает. Остальное закрывает адаптер.

| Цель | Что получает shikii | Требования и руководство |
|---|---|---|
| **TypeSafe Jev** (облако) | `choice` / `score` / `noul` с вероятностями, id модели вида `jev-1.13.0` | `TYPESAFE_API_KEY` в окружении. `--hosted` по умолчанию |
| **Модели решений на llama.cpp** (Kev-4B, OpenJev, Clef, …) | Тот же формат, локально, id модели из `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 или новее |
| **OpenRouter Decisions API** | Тот же формат через OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Любая чат-модель GGUF** через `shikii-readout` | Считывание логитов букв в формате System One, собственной калибровки нет | За ним обычный `llama-server` 0.7.0. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** через [BonJev](https://github.com/ChezRD/bonjev) | То же, с собственным промптом BonJev и встроенной калибровкой | Сборка на Rust и форк Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Фиктивная модель** (`bin/fake-systemone`) | Детерминированные вероятности для демо и тестов | Ничего. `make fake` |
| **Hook для Claude Code** (claude-jev) | Каждый промпт классифицируется, каждый ход размечается по рабочему дереву | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint здесь полноправный входной параметр: передайте `--tainted` (или пусть это сделает ваш hook) в сессии, которая касалась продуктивных данных, и запрос не покинет машину. Если локальный эндпоинт не настроен, решение завершается как abstain.

## Результаты

Одни и те же spec, одни и те же метки, четыре набора для оценки (намерение промпта на английском и японском, 62 запроса; связывание одного и того же дела на 32 японских парах). Подробности и полная таблица в [`docs/model-comparison.md`](../model-comparison.md).

| Модель | Лицензия | top-1 (все / ja) | Brier для да/нет (сырой) | Выбранная калибровка | Секунд на вопрос |
|---|---|---|---|---|---|
| Kev-4B (обученная модель решений) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (обученная) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B через BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B через `shikii-readout`, обычный llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**Формулировка важнее калибровки.** В задаче о намерении промпта 19 из 21 ошибки приходились на *other*; переформулировка вариантов подняла top-1 с 0.68 до 0.90 ещё до всякой калибровки. Калибровка нужна для случая «порядок верный, уверенности не хватает». Какой из случаев перед вами, показывает матрица ошибок, которую печатает `calibrate`.

**Уже откалиброванным моделям Platt не нужен.** У Kev-4B на да/нет Brier в сыром виде составил 0.046; Platt при кросс-валидации ухудшил результат, поэтому записан `identity`. Впервые `calibrate` выбрал `platt` на обычной LLM, прочитанной через логиты. Нужна ли калибровка, определяется свойством *модели вместе со способом считывания*, а не одной моделью.

**Обычный GGUF оказывается рядом с обученной моделью решений.** Чат-модель 27B на тернарных весах, основная ветка llama.cpp 0.7.0, один адаптер, без форка, без обучения, без Rust: тот же top-1, что у лучшей обученной модели под лицензией Apache, 1.000 на японском, 0.58 с на вопрос с flash attention и прогретым префиксом. Перенос состояния из позиции перед вариантами в позицию прямо перед ответом поднял результат с 0.919 до 0.968 на тех же весах, поэтому раскладка и входит в ключ калибровки.

## FAQ

**shikii запускает модели?** Нет. Он отправляет запросы System One на эндпоинт, который вы запускаете или арендуете, и хранит всё, что происходит после ответа. `shikii-readout` единственное исключение по духу: он преобразует логиты чат-модели в тот же формат, но вывод всё равно выполняет llama.cpp.

**Почему моё решение никогда не доходит до `act`?** Потому что в spec нет блока калибровки для `(хеш этого вопроса, id этой модели)`. Соберите метки (`label` или `--labeled file`), выполните `calibrate --write`, и `status` покажет `calibrated: identity|platt|vector`. Решению без проверенной precision на пороге act действовать нельзя. В этом и смысл.

**Я изменил в вопросе одно слово, и калибровка пропала.** Так и должно быть. Хеш изменился, константы были подобраны под другой вопрос. `status` показывает старые метки как `stale hash`; когда накопятся метки для новой формулировки, снова выполните `calibrate --write`.

**Какую модель выбрать?** Это зависит от лицензии, скорости и языка, и shikii за вас не решает. Таблица выше и [`docs/model-comparison.md`](../model-comparison.md) и есть аргумент: с тем же spec и теми же метками можно выбирать по цифрам, включая вопрос, нужна ли модели калибровка вообще. `scripts/eval-model.sh <url> <name>` воспроизводит таблицу для нового эндпоинта.

**Работает ли на японском?** Да, и именно здесь различия заметнее всего. Kev-4B получает 0.913 на японских промптах; модели на 27B получают 1.000. Связывание одного и того же дела по японским сводкам расследований полностью разделяет положительные и отрицательные примеры на каждой модели 27B.

**Куда уходят мои данные?** К эндпоинту, указанному в `send_to`, уходит только отрендеренное состояние. При `pii: possible` (по умолчанию) store хранит хеш входных данных, но не текст. `--tainted` оставляет всё локально. Ключи читаются из `DECISIONS_API_KEY` или `TYPESAFE_API_KEY` и больше ниоткуда.

**Store продолжает расти.** `shikii gc` удаляет записи старше 30 дней, которым так и не поставили метку. Записи с метками и сами метки хранятся вечно, потому что это материал для калибровки. Без `--apply` это только пробный прогон.

**Есть ли смещение в сторону первого варианта?** Сначала измерьте, потом исправляйте: `shikii bias <spec> --permutations 100` перемешивает варианты и сообщает число переворотов и сдвиг позиции. Kev-4B: 0 переворотов из 100, сдвиг ≤ 0.024. Обычные LLM, прочитанные через логиты, могут вести себя иначе; команда существует для них.

## Документация

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): заметки по проектированию, что фреймворк берёт на себя и от чего отказывается, план проверки и что подтверждено.
- [`docs/operations.md`](../operations.md): как остановить или откатить каждую движущуюся часть, как читать сбои, чего ещё не хватает.
- [`docs/model-comparison.md`](../model-comparison.md): сравнительная таблица и как её воспроизвести.
- [`docs/guide.ja.md`](../guide.ja.md): подробное руководство на японском со всеми командами и полным реальным циклом.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): три подключения, которые мы используем.
- [`examples/`](../../examples/) и [`testdata/`](../../testdata/README.md): spec без калибровки, с которого можно начать, и синтетические размеченные наборы, используемые в тестах.

Команды:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: устаревший хеш, блоки, написанные вручную, precision@act ниже act → exit 1
shikii drift     <spec> --model M [--window N]            # недавние метки против Brier и precision, записанных при калибровке
shikii bias      <spec> [--rows K] [--permutations N]     # зависит ли ответ от порядка вариантов?
shikii status    [spec...] [--store DIR]                  # инвентаризация: записи, метки, источники, состояние по каждой паре (model, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # удалить старые записи без меток; по умолчанию пробный прогон
shikii hash      <spec>
```

## Участие в разработке

Issues и pull request приветствуются. Одно правило: тест, охраняющий поведение, нужно сначала показать упавшим. Намеренно сломайте поведение, запустите тест, запишите, как он упал, затем исправьте и запишите, что он проходит. В документации перечислены такие мутации для каждой ветки, влияющей на безопасность; держите этот список честным.

## Лицензия

[Apache 2.0](../../LICENSE). Лицензии моделей отдельные: Kev-4B и семейство Bonsai под Apache 2.0, OpenJev под CC BY-NC 4.0, облачный Jev это сервис TypeSafe. shikii хранит ваши вероятности и метки, но ничего от самих моделей.
