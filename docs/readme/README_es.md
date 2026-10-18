[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · **Español** · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Operar modelos de decisión

### Un único ciclo de vida para cualquier modelo de decisión: define → decide → record → label → calibrate → check → drift.

**Un modelo te da una probabilidad. shikii te dice si puedes actuar con ella.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-172%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Notas de diseño](../decision-ops-framework.md) · [Operaciones](../operations.md) · [Comparación de modelos](../model-comparison.md) · [Guía detallada (japonés)](../guide.ja.md)**

[Inicio rápido](#inicio-rápido) · [Cómo funciona shikii](#cómo-funciona-shikii) · [Qué puedes conectar](#qué-puedes-conectar) · [Resultados](#resultados) · [FAQ](#faq) · [Documentación](#documentación)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii nunca ejecuta un modelo ni lee un llavero de claves. Habla con un endpoint de modelo de decisión que ya tienes, guarda los registros y se niega a **actuar** hasta que las probabilidades de *esta pregunta en este modelo* hayan sido verificadas con etiquetas. Los bloques de calibración se generan, nunca se escriben a mano.

---

Los modelos de decisión (TypeSafe Jev, Kev-4B y OpenJev en llama.cpp, OpenRouter Decisions, …) responden preguntas tipadas, `choice`, `score` y `noul` (sí/no), con probabilidades y sin texto generado. Todos los proveedores incluyen el mismo aviso: *las probabilidades no están calibradas para tus datos, y los umbrales no se trasladan de un modelo o de una pregunta a otros.*

shikii se encarga de lo que nadie entrega: el **ciclo de vida** de una decisión. Un archivo spec contiene la pregunta, las opciones, la plantilla de estado y los umbrales. `decide` consulta a un modelo y devuelve una banda (`act` / `ask` / `discard` / `abstain`), nunca un número crudo. `label` registra qué era cierto. `calibrate` ajusta y valida de forma cruzada, y luego escribe el resultado en el spec bajo una clave formada por el hash de la pregunta y el nombre del modelo. `check` hace fallar la CI cuando la redacción o el modelo se han desviado. `gc` mantiene el store pequeño.

Incluye además `shikii-readout`, un adaptador de 500 líneas que convierte **cualquier GGUF de chat en un llama.cpp estándar** en un modelo de decisión leyendo los logits de las letras de las opciones. En nuestras mediciones, un modelo ternario corriente de 27B leído de este modo igualó en precisión a los modelos de decisión entrenados, a 0,6 segundos por pregunta en un portátil.

> El nombre es japonés: *shikii* (閾) significa umbral, la línea que convierte una probabilidad en una acción. La CLI se llama `shikii`.

## Inicio rápido

Go 1.27 o superior. No se necesita nada más para la primera ejecución.

### Ejecuta todo el ciclo sin clave ni GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # modelo falso: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # un modelo falso cuyas probabilidades necesitan calibración; observa cómo calibrate elige platt
```

### Apúntalo a un modelo real

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) modelo de decisión local en llama.cpp (descarga Kev-4B, unos 2,5 GB, en la primera ejecución)
make server                                       # llama-server en :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev alojado. La clave se toma solo del entorno
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) API OpenRouter Decisions
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

La respuesta es una banda, un trace id y las probabilidades. Mientras el spec no tenga una calibración para este modelo, la banda es como máximo `ask`.

### Cierra el ciclo

```sh
bin/shikii label <trace_id> change_request          # o:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 cuando la redacción cambió o precision@act cayó por debajo de act
```

### Usa cualquier GGUF como modelo de decisión

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server estándar con flash attention y KV de 8 bits, :8086
make readout                                               # shikii-readout delante, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Instalar y actualizar

```sh
make install          # copia bin/shikii a ~/.local/bin/shikii
git pull && make install
```

## Cómo funciona shikii

Cada decisión es un único archivo YAML. La pregunta, las opciones, la plantilla de estado, adónde se puede enviar el estado, los umbrales y, generados por `calibrate --write`, los bloques de calibración.

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
  required: [message]            # falta una entrada → abstain; el modelo nunca se llama
policy:
  send_to: [hosted, local]       # una sesión tainted omite hosted automáticamente
  pii: possible                  # los registros guardan un hash de las entradas, nunca el texto
thresholds:
  act: 0.75                      # ≥ act → act automáticamente, solo si está calibrado
  ask: 0.60                      # ≥ ask → preguntar a una persona; por debajo → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**La clave de calibración es `(hash de pregunta + plantilla + opciones, id del modelo tal como lo devuelve el endpoint)`.** Si editas una sola palabra de la pregunta, o dejas que `jev-latest` pase a una versión nueva, la calibración simplemente desaparece: la banda baja a `ask` en lugar de actuar en silencio con constantes obsoletas. La disposición del prompt también forma parte de la clave: `shikii-readout` informa de su disposición en el nombre del modelo (`…+state-last`), porque mover el estado dentro del prompt cambió en 5 puntos la precisión de un modelo de 27B con los mismos pesos.

`calibrate` compara tres métodos con validación cruzada de 5 pliegues y escribe el ganador. `identity` significa que las probabilidades crudas ya cumplen precision@act y el modelo simplemente se marca como verificado. `platt` (σ((Δ−bias)/T)) es para preguntas de sí/no. `vector` (softmax((log p + b)/T)) es para choice y score, y corrige vicios por opción como "recurrir a *other*". Las etiquetas parciales (`!option`, "no era *eso*") cuentan para la precisión pero no para el ajuste. Las etiquetas obtenidas de un archivo se persisten, de modo que `check` y `drift` se ejecutan después sin conexión.

Cada rama relevante para la seguridad (enrutado de taint, ningún act sin calibración, invalidación por hash obsoleto, validación de etiquetas, detección de inversiones, elección de método por CV, `gc` en modo simulación, …) tiene una mutación registrada que hizo fallar un test antes de aceptar la corrección. Hay 63 tests de Go en 8 paquetes; la demo se ejecuta en CI.

## Qué puedes conectar

shikii habla la forma de petición de System One (`POST /v1/systemone`), así que sirve cualquier cosa que la exponga. El adaptador cubre todo lo demás.

| Destino | Qué obtiene shikii | Requisitos y guía |
|---|---|---|
| **TypeSafe Jev** (alojado) | `choice` / `score` / `noul` con probabilidades, id de modelo como `jev-1.13.0` | `TYPESAFE_API_KEY` en el entorno. `--hosted` por defecto |
| **Modelos de decisión en llama.cpp** (Kev-4B, OpenJev, Clef, …) | La misma forma, en local, id de modelo desde `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 o superior |
| **API OpenRouter Decisions** | La misma forma a través de OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Cualquier GGUF de chat** mediante `shikii-readout` | Lectura de logits de letras en forma System One, sin calibración propia | `llama-server` estándar 0.7.0 detrás. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** mediante [BonJev](https://github.com/ChezRD/bonjev) | Lo mismo, con el prompt propio de BonJev y calibración integrada | Compilación en Rust y el fork de Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Modelo falso** (`bin/fake-systemone`) | Probabilidades deterministas para demos y tests | Nada. `make fake` |
| **Hook de Claude Code** (claude-jev) | Cada prompt clasificado, cada turno etiquetado a partir del árbol de trabajo | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

El taint es una entrada de primera clase: pasa `--tainted` (o deja que lo haga tu hook) en una sesión que haya tocado datos de producción y la petición nunca sale de la máquina. Si no hay ningún endpoint local configurado, la decisión se abstiene.

## Resultados

Mismos specs, mismas etiquetas, cuatro conjuntos de evaluación (intención de prompts en inglés y japonés, 62 peticiones; vinculación de casos idénticos en 32 pares en japonés). Los detalles y la tabla completa están en [`docs/model-comparison.md`](../model-comparison.md).

| Modelo | Licencia | top-1 (todo / ja) | Brier sí/no (crudo) | Calibración elegida | Segundos por pregunta |
|---|---|---|---|---|---|
| Kev-4B (modelo de decisión entrenado) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (entrenado) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B mediante BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B mediante `shikii-readout`, llama.cpp estándar** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**La redacción vence a la calibración.** En la intención de prompts, 19 de 21 errores caían en *other*; reformular las opciones llevó el top-1 de 0.68 a 0.90 antes de cualquier calibración. La calibración sirve para el caso "orden correcto, confianza insuficiente". La matriz de confusión que imprime `calibrate` te dice en cuál de los dos casos estás.

**Los modelos ya calibrados no necesitan Platt.** El sí/no de Kev-4B tenía un Brier de 0.046 en crudo; Platt lo empeoró bajo validación cruzada, así que se escribió `identity`. La primera vez que `calibrate` eligió `platt` fue con un LLM corriente leído mediante logits. Que haga falta calibrar es una propiedad de *modelo más lectura*, no del modelo.

**Un GGUF corriente queda al lado de un modelo de decisión entrenado.** Un modelo de chat ternario de 27B, llama.cpp 0.7.0 de la rama principal, un adaptador, sin fork, sin entrenamiento, sin Rust: el mismo top-1 que el mejor modelo entrenado con licencia Apache, 1.000 en japonés, 0.58 s por pregunta con flash attention y un prefijo precalentado. Mover el estado de antes de las opciones a justo antes de la respuesta lo llevó de 0.919 a 0.968 con los mismos pesos, y por eso la disposición forma parte de la clave de calibración.

## FAQ

**¿shikii ejecuta modelos?** No. Envía peticiones System One a un endpoint que tú ejecutas o alquilas, y guarda todo lo que ocurre después de la respuesta. `shikii-readout` es la única excepción en espíritu: convierte los logits de un modelo de chat a la misma forma, pero la inferencia sigue siendo de llama.cpp.

**¿Por qué mi decisión nunca llega a `act`?** Porque el spec no tiene un bloque de calibración para `(el hash de esta pregunta, este id de modelo)`. Reúne etiquetas (`label`, o `--labeled file`), ejecuta `calibrate --write` y `status` mostrará `calibrated: identity|platt|vector`. Una decisión sin precisión verificada en el umbral de act no tiene permitido actuar. Ese es el objetivo.

**Cambié una palabra de la pregunta y la calibración desapareció.** Correcto. El hash cambió, y las constantes se ajustaron a otra pregunta. `status` muestra las etiquetas antiguas como `stale hash`; vuelve a ejecutar `calibrate --write` cuando tengas etiquetas para la nueva redacción.

**¿Qué modelo debería usar?** Depende de la licencia, la velocidad y el idioma, y shikii no decide por ti. La tabla de arriba y [`docs/model-comparison.md`](../model-comparison.md) son el argumento: con el mismo spec y las mismas etiquetas puedes elegir por números, incluido si un modelo necesita calibración o no. `scripts/eval-model.sh <url> <name>` reproduce la tabla para un endpoint nuevo.

**¿Funciona en japonés?** Sí, y es donde se notan las diferencias. Kev-4B obtiene 0.913 en prompts en japonés; los modelos de 27B obtienen 1.000. La vinculación de casos idénticos en resúmenes de investigación en japonés separa por completo los positivos de los negativos en todos los modelos de 27B.

**¿Adónde van mis datos?** Solo el estado renderizado va al endpoint indicado en `send_to`. Con `pii: possible` (el valor por defecto) el store guarda un hash de las entradas, nunca el texto. `--tainted` mantiene todo en local. Las claves se leen de `DECISIONS_API_KEY` o `TYPESAFE_API_KEY` y de ningún otro sitio.

**El store no deja de crecer.** `shikii gc` elimina los registros de más de 30 días que nunca recibieron una etiqueta. Los registros etiquetados y las etiquetas se conservan para siempre, porque son el material de calibración. Es una simulación a menos que pases `--apply`.

**¿Tiende a favorecer la primera opción?** Mide antes de corregir: `shikii bias <spec> --permutations 100` baraja las opciones e informa de los cambios de respuesta y del desplazamiento de posición. Kev-4B: 0 cambios en 100, desplazamiento ≤ 0.024. Los LLM corrientes leídos mediante logits pueden diferir; el comando existe para ellos.

## Documentación

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): notas de diseño, qué posee el framework y qué se niega a poseer, el plan de verificación y lo que se confirmó.
- [`docs/operations.md`](../operations.md): cómo detener o revertir cada pieza en movimiento, cómo interpretar un fallo, qué falta todavía.
- [`docs/model-comparison.md`](../model-comparison.md): la tabla comparativa y cómo reproducirla.
- [`docs/guide.ja.md`](../guide.ja.md): la guía extensa en japonés, con todos los comandos y un ciclo completo en un caso real.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): las tres conexiones que usamos.
- [`examples/`](../../examples/) y [`testdata/`](../../testdata/README.md): un spec sin calibración para empezar, y los conjuntos etiquetados sintéticos usados en los tests.

Los comandos:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: hash obsoleto, bloques escritos a mano, precision@act por debajo de act → exit 1
shikii drift     <spec> --model M [--window N]            # etiquetas recientes frente al Brier y la precisión registrados en la calibración
shikii bias      <spec> [--rows K] [--permutations N]     # ¿la respuesta depende del orden de las opciones?
shikii status    [spec...] [--store DIR]                  # inventario: registros, etiquetas, orígenes, estado por (modelo, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # elimina registros antiguos sin etiqueta; simulación por defecto
shikii hash      <spec>
```

## Contribuir

Los issues y los pull requests son bienvenidos. Una sola regla: un test que protege un comportamiento debe mostrarse fallando primero. Rompe el comportamiento a propósito, ejecuta el test, registra cómo falló, y luego corrígelo y registra que pasa. La documentación enumera esas mutaciones para cada rama de seguridad; mantén esa lista honesta.

## Licencia

[Apache 2.0](../../LICENSE). Las licencias de los modelos son aparte: Kev-4B y la familia Bonsai son Apache 2.0, OpenJev es CC BY-NC 4.0, Jev alojado es el servicio de TypeSafe. shikii almacena tus probabilidades y etiquetas, nada de los modelos.
