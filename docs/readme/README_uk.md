[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · **Українська** · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Експлуатація моделей рішень

### Один життєвий цикл для будь-якої моделі рішень: define → decide → record → label → calibrate → check → drift.

**Модель видає ймовірність. shikii каже, чи можна діяти на її основі.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-161%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Нотатки з проєктування](../decision-ops-framework.md) · [Експлуатація](../operations.md) · [Порівняння моделей](../model-comparison.md) · [Докладний посібник (японською)](../guide.ja.md)**

[Швидкий старт](#швидкий-старт) · [Як працює shikii](#як-працює-shikii) · [Що можна підключити](#що-можна-підключити) · [Результати](#результати) · [FAQ](#faq) · [Документація](#документація)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii ніколи не запускає модель і ніколи не читає зв'язку ключів. Він звертається до наявного у вас ендпоінта моделі рішень, веде записи й відмовляється **діяти** (`act`), доки ймовірності для *цього запитання на цій моделі* не перевірено за мітками. Блоки калібрування генеруються, а не пишуться вручну.

---

Моделі рішень (TypeSafe Jev, llama.cpp Kev-4B та OpenJev, OpenRouter Decisions та ін.) відповідають на типізовані запитання, `choice`, `score` і `noul` (так/ні), ймовірностями, без генерації тексту. Кожен вендор додає те саме застереження: *ймовірності не відкалібровано під ваші дані, а пороги не переносяться між моделями та запитаннями.*

shikii бере на себе те, чого ніхто не постачає: **життєвий цикл** рішення. Файл spec зберігає запитання, варіанти, шаблон стану та пороги. `decide` звертається до моделі й повертає зону (`act` / `ask` / `discard` / `abstain`), але ніколи не сире число. `label` записує, що було правдою. `calibrate` підбирає параметри, перевіряє їх крос-валідацією та записує результат назад у spec під ключем із хеша запитання й назви моделі. `check` валить CI, якщо формулювання або модель змістилися. `gc` не дає сховищу розростатися.

До комплекту входить і `shikii-readout`, адаптер на 500 рядків, який перетворює **будь-яку чат-модель у форматі GGUF на звичайному llama.cpp** на модель рішень, зчитуючи логіти літер варіантів. За нашими вимірами, звичайна тернарна модель на 27B, прочитана таким способом, зрівнялася за точністю з навченими моделями рішень при 0.6 секунди на запитання на ноутбуці.

> Назва японська: *shikii* (閾) японською означає «поріг», межу, що перетворює ймовірність на дію. CLI називається `shikii`.

## Швидкий старт

Go 1.24 або новіший. Для першого запуску більше нічого не потрібно.

### Увесь цикл без ключа й без GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # фіктивна модель: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # фіктивна модель із завищеними ймовірностями; дивимося, як calibrate обирає platt
```

### Підключення до справжньої моделі

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) локальна модель рішень на llama.cpp (під час першого запуску завантажує Kev-4B, близько 2.5 ГБ)
make server                                       # llama-server на :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) хмарний TypeSafe Jev. Ключ береться лише із середовища
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

У відповіді приходять зона, trace id та ймовірності. Доки в spec немає калібрування для цієї моделі, зона не вища за `ask`.

### Замкнути цикл

```sh
bin/shikii label <trace_id> change_request          # або:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <назва моделі з відповіді> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1, якщо формулювання змінилося або precision@act впала нижче act
```

### Будь-який GGUF як модель рішень

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # звичайний llama-server із flash attention та 8-бітним KV, :8086
make readout                                               # перед ним shikii-readout, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Встановлення й оновлення

```sh
make install          # копіює bin/shikii до ~/.local/bin/shikii
git pull && make install
```

## Як працює shikii

Кожне рішення це один YAML-файл. Запитання, варіанти, шаблон стану, куди можна надсилати стан, пороги та, згенеровані командою `calibrate --write`, блоки калібрування.

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
  required: [message]            # немає вхідних даних → abstain; модель не викликається
policy:
  send_to: [hosted, local]       # tainted-сесія автоматично пропускає hosted
  pii: possible                  # у записах зберігається хеш вхідних даних, а не текст
thresholds:
  act: 0.75                      # ≥ act → діяти автоматично, лише якщо відкалібровано
  ask: 0.60                      # ≥ ask → запитати людину; нижче → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Ключ калібрування: `(хеш запитання + шаблону + варіантів, id моделі в тому вигляді, як його повернув ендпоінт)`.** Змініть у запитанні одне слово або дозвольте `jev-latest` перейти на нову версію, і калібрування просто зникне: зона опуститься до `ask`, а не мовчки діятиме за застарілими константами. Розкладка промпту теж входить у ключ: `shikii-readout` повідомляє свою розкладку в назві моделі (`…+state-last`), бо перенесення стану всередині промпту змінило точність моделі на 27B на 5 пунктів за тих самих ваг.

`calibrate` порівнює три методи за 5-fold крос-валідації та записує переможця. `identity` означає, що сирі ймовірності вже забезпечують precision@act, і модель просто позначається як перевірена. `platt` (σ((Δ−bias)/T)) призначений для запитань так/ні. `vector` (softmax((log p + b)/T)) призначений для choice і score та виправляє звички щодо окремих варіантів, наприклад «за невпевненості тікає в *other*». Часткові мітки (`!option`, «це було не *те*») враховуються в precision, але не в підгонці. Мітки, отримані з файлу, зберігаються, тож `check` і `drift` потім працюють офлайн.

Кожна гілка, що впливає на безпеку (маршрутизація за taint, заборона `act` без калібрування, інвалідація за застарілим хешем, валідація міток, виявлення інверсії, вибір методу за CV, `gc` у режимі dry-run тощо), має зафіксовану мутацію, яка валила тест до того, як виправлення було прийнято. Усього 63 Go-тести у 8 пакетах; демо запускається в CI.

## Що можна підключити

shikii використовує формат запиту System One (`POST /v1/systemone`), тож підійде все, що його обслуговує. Решту закриває адаптер.

| Ціль | Що отримує shikii | Вимоги та посібник |
|---|---|---|
| **TypeSafe Jev** (хмара) | `choice` / `score` / `noul` з ймовірностями, id моделі на кшталт `jev-1.13.0` | `TYPESAFE_API_KEY` у середовищі. `--hosted` за замовчуванням |
| **Моделі рішень на llama.cpp** (Kev-4B, OpenJev, Clef, …) | Той самий формат, локально, id моделі з `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 або новіший |
| **OpenRouter Decisions API** | Той самий формат через OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Будь-яка чат-модель GGUF** через `shikii-readout` | Зчитування логітів літер у форматі System One, власного калібрування немає | За ним звичайний `llama-server` 0.7.0. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** через [BonJev](https://github.com/ChezRD/bonjev) | Те саме, з власним промптом BonJev і вбудованим калібруванням | Збірка на Rust і форк Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Фіктивна модель** (`bin/fake-systemone`) | Детерміновані ймовірності для демо й тестів | Нічого. `make fake` |
| **Hook для Claude Code** (claude-jev) | Кожен промпт класифікується, кожен хід розмічається за робочим деревом | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint тут повноправний вхідний параметр: передайте `--tainted` (або нехай це зробить ваш hook) у сесії, що торкалася продуктивних даних, і запит не залишить машину. Якщо локальний ендпоінт не налаштовано, рішення завершується як abstain.

## Результати

Ті самі spec, ті самі мітки, чотири набори для оцінювання (намір промпту англійською та японською, 62 запити; зв'язування однієї й тієї самої справи на 32 японських парах). Подробиці та повна таблиця в [`docs/model-comparison.md`](../model-comparison.md).

| Модель | Ліцензія | top-1 (усі / ja) | Brier для так/ні (сирий) | Обране калібрування | Секунд на запитання |
|---|---|---|---|---|---|
| Kev-4B (навчена модель рішень) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (навчена) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B через BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B через `shikii-readout`, звичайний llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**Формулювання важливіше за калібрування.** У задачі про намір промпту 19 із 21 помилки припадали на *other*; переформулювання варіантів підняло top-1 з 0.68 до 0.90 ще до будь-якого калібрування. Калібрування потрібне для випадку «порядок правильний, упевненості бракує». Який із випадків перед вами, показує матриця помилок, яку друкує `calibrate`.

**Уже відкаліброваним моделям Platt не потрібен.** У Kev-4B на так/ні Brier у сирому вигляді становив 0.046; Platt під час крос-валідації погіршив результат, тому записано `identity`. Уперше `calibrate` обрав `platt` на звичайній LLM, прочитаній через логіти. Чи потрібне калібрування, визначає властивість *моделі разом зі способом зчитування*, а не самої моделі.

**Звичайний GGUF опиняється поруч із навченою моделлю рішень.** Чат-модель 27B на тернарних вагах, основна гілка llama.cpp 0.7.0, один адаптер, без форка, без навчання, без Rust: той самий top-1, що й у найкращої навченої моделі під ліцензією Apache, 1.000 японською, 0.58 с на запитання з flash attention і прогрітим префіксом. Перенесення стану з позиції перед варіантами в позицію просто перед відповіддю підняло результат з 0.919 до 0.968 на тих самих вагах, тому розкладка й входить у ключ калібрування.

## FAQ

**shikii запускає моделі?** Ні. Він надсилає запити System One на ендпоінт, який ви запускаєте або орендуєте, і зберігає все, що відбувається після відповіді. `shikii-readout` єдиний виняток за духом: він перетворює логіти чат-моделі на той самий формат, але виведення однаково виконує llama.cpp.

**Чому моє рішення ніколи не доходить до `act`?** Тому що в spec немає блока калібрування для `(хеш цього запитання, id цієї моделі)`. Зберіть мітки (`label` або `--labeled file`), виконайте `calibrate --write`, і `status` покаже `calibrated: identity|platt|vector`. Рішенню без перевіреної precision на порозі act діяти не дозволено. У цьому й сенс.

**Я змінив у запитанні одне слово, і калібрування зникло.** Так і має бути. Хеш змінився, константи було підібрано під інше запитання. `status` показує старі мітки як `stale hash`; коли назбираються мітки для нового формулювання, знову виконайте `calibrate --write`.

**Яку модель обрати?** Це залежить від ліцензії, швидкості та мови, і shikii за вас не вирішує. Таблиця вище та [`docs/model-comparison.md`](../model-comparison.md) і є аргументом: з тим самим spec і тими самими мітками можна обирати за цифрами, зокрема й за тим, чи потрібне моделі калібрування взагалі. `scripts/eval-model.sh <url> <name>` відтворює таблицю для нового ендпоінта.

**Чи працює японською?** Так, і саме тут відмінності помітні найбільше. Kev-4B отримує 0.913 на японських промптах; моделі на 27B отримують 1.000. Зв'язування однієї й тієї самої справи за японськими зведеннями розслідувань повністю розділяє позитивні та негативні приклади на кожній моделі 27B.

**Куди йдуть мої дані?** До ендпоінта, указаного в `send_to`, іде лише відрендерений стан. За `pii: possible` (за замовчуванням) store зберігає хеш вхідних даних, а не текст. `--tainted` залишає все локально. Ключі читаються з `DECISIONS_API_KEY` або `TYPESAFE_API_KEY` і більше ніде.

**Store постійно росте.** `shikii gc` видаляє записи старші за 30 днів, яким так і не поставили мітку. Записи з мітками та самі мітки зберігаються назавжди, бо це матеріал для калібрування. Без `--apply` це лише пробний запуск.

**Чи є зміщення на користь першого варіанта?** Спершу виміряйте, потім виправляйте: `shikii bias <spec> --permutations 100` перемішує варіанти та повідомляє кількість перевертань і зсув позиції. Kev-4B: 0 перевертань зі 100, зсув ≤ 0.024. Звичайні LLM, прочитані через логіти, можуть поводитися інакше; команда існує для них.

## Документація

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): нотатки з проєктування, що фреймворк бере на себе і від чого відмовляється, план перевірки та що підтверджено.
- [`docs/operations.md`](../operations.md): як зупинити або відкотити кожну рухому частину, як читати збої, чого ще бракує.
- [`docs/model-comparison.md`](../model-comparison.md): порівняльна таблиця та як її відтворити.
- [`docs/guide.ja.md`](../guide.ja.md): докладний посібник японською з усіма командами та повним реальним циклом.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): три підключення, якими ми користуємося.
- [`examples/`](../../examples/) і [`testdata/`](../../testdata/README.md): spec без калібрування, з якого можна почати, та синтетичні розмічені набори, що використовуються в тестах.

Команди:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: застарілий хеш, блоки, написані вручну, precision@act нижче act → exit 1
shikii drift     <spec> --model M [--window N]            # нещодавні мітки проти Brier і precision, записаних під час калібрування
shikii bias      <spec> [--rows K] [--permutations N]     # чи залежить відповідь від порядку варіантів?
shikii status    [spec...] [--store DIR]                  # інвентаризація: записи, мітки, джерела, стан за кожною парою (model, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # видалити старі записи без міток; за замовчуванням пробний запуск
shikii hash      <spec>
```

## Участь у розробці

Issues і pull request вітаються. Одне правило: тест, що охороняє поведінку, потрібно спершу показати впалим. Навмисно зламайте поведінку, запустіть тест, запишіть, як він упав, потім виправте й запишіть, що він проходить. У документації перелічено такі мутації для кожної гілки, що впливає на безпеку; тримайте цей список чесним.

## Ліцензія

[Apache 2.0](../../LICENSE). Ліцензії моделей окремі: Kev-4B і родина Bonsai під Apache 2.0, OpenJev під CC BY-NC 4.0, хмарний Jev це сервіс TypeSafe. shikii зберігає ваші ймовірності та мітки, але нічого від самих моделей.
