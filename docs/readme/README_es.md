[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · **Español** · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: convertir el «probablemente» de una IA en un «se puede actuar» sobre el que montar un negocio

### Una herramienta para dejar que una IA decida sí/no, cuál o cuánto, y para que esa decisión siga siendo de fiar en el trabajo diario. Usted revisa algunas respuestas y, de vez en cuando, confirma «¿esto sigue siendo lo que quería decir?». La máquina hace el resto.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-190%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[El problema](#el-problema) · [Qué hace shikii](#qué-hace-shikii) · [Qué hace usted](#qué-hace-usted) · [Instalación](#instalación) · [Los caminos más cortos](#los-caminos-más-cortos) · [Una vuelta completa](#una-vuelta-completa) · [Qué hay de nuevo](#qué-hay-de-nuevo) · [Leer más](#leer-más)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: un modelo le da una probabilidad, shikii le dice si puede actuar con ella" width="100%"></p>

## El problema

Pregunte a una IA «¿esta consulta es el mismo asunto que la anterior?» y responde «probablemente, 0,9». Para el trabajo real, eso no basta.

- ¿Ese 0,9 acierta de verdad 9 veces de cada 10?
- Desde el día en que cambió a un modelo nuevo, ¿ha empezado a mentir sin que nadie lo note?
- Si cambia un poco la redacción, ¿cómo se mueve el acierto?
- Las decisiones que tomó por su cuenta, sin que nadie mirara: ¿siguen siendo correctas?

Si una persona tiene que revisar cada respuesta, usar la IA tenía poco sentido.

## Qué hace shikii

1. **Reúne sus correcciones y traza la línea por encima de la cual la IA puede actuar sola.** A partir de las respuestas que usted dio («esto es lo que era cierto de verdad»), mide cómo se relaciona la confianza de la IA con la frecuencia con que acierta y decide desde dónde se puede confiar en ella. Hasta que exista una línea, nada actúa. Cuando existe, cada decisión automática lleva un recibo que dice sobre cuántas respuestas revisadas descansa la línea y con qué frecuencia acertaron
2. **Prueba otras formas de preguntar y se queda con la mejor.** Sobre las mismas correcciones puntúa preguntas alternativas, descripciones de las opciones y disposiciones del prompt, y adopta una solo cuando es realmente mejor. Usted solo confirma que el significado no cambió
3. **Le permite cambiar de modelo sin empezar de cero.** Las correcciones viven aparte del modelo, así que un modelo nuevo las reutiliza y la máquina vuelve a trazar la línea. Sin trabajo humano
4. **Guarda lo aprendido en una wiki.** «Preguntar así funciona» se acumula como afirmaciones con los números medidos, que se pueden volver a probar en otro modelo. Lo que otras personas midieron con sus modelos se añade a la misma página

## Qué hace usted

- **Escribe lo que quiere que se decida.** Responda unas pocas preguntas y aparece un archivo de decisión (`shikii new`, de forma interactiva o con `--type --question --option --input --pii`; el formato completo está en [la referencia del archivo de decisión](../../skills/shikii/references/spec.md))
- **Revisa respuestas.** La página muestra las decisiones en las que la IA dudó, más 1 de cada 20 de las que ejecutó, y usted pulsa el botón de lo que era cierto de verdad. Las teclas 1 a 9 y Enter (igual que la elección de la IA) van rápido. «Respondidas» permite corregir una respuesta anterior (cuenta la más reciente). «Detalles» añade «preguntar otra vez a la IA», que vuelve a decidir el mismo texto con la redacción y la línea de hoy y muestra el antes y el después; los registros se añaden, nunca se borran
- **De vez en cuando confirma «¿esto sigue siendo lo que quería decir?».** Cuando la máquina encuentra una redacción que puntúa mejor, usted mira si cambió el significado y pulsa adoptar o rechazar

Todo lo demás es trabajo de la máquina.

## Instalación

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # necesita Go 1.27 o más reciente
cd <raíz del proyecto> && shikii init
```

`shikii init` prepara una carpeta, `.shikii/` (con `decisions/` para sus archivos de decisión, `wiki/` para el conocimiento, `records/` y `labels/` para los registros y sus correcciones; nada más se añade a la raíz del proyecto), y de una vez las conexiones con los agentes: el servidor MCP de shikii para Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) y Codex (`~/.codex/config.toml`), una sección en `AGENTS.md` y las reglas detalladas (la skill) en `~/.claude/skills/shikii`. Sus propias entradas nunca se tocan (una entrada de shikii ya existente se deja tal cual). Solo la carpeta de la skill se reemplaza cada vez, así que no la edite a mano. `wiki/` recibe el conocimiento compartido (cómo preguntar cada tipo de pregunta, patrones de redacción que funcionaron, las costumbres de cada modelo); el mejorador de redacción (`tune --propose`) lo lee, y las páginas que usted editó nunca las sobrescribe un `init` posterior. ¿Sin Go? Use Docker, más abajo.

Las entradas apuntan a un modelo local (`shikii up`, puerto 8080). ¿Solo alojado? Quite `--local` a mano de cada uno de los cuatro archivos. También se crean archivos de configuración para herramientas que no usa; bórrelos o añádalos a `.gitignore` (un segundo `shikii init` nunca toca lo que ya existe). Haga commit de `.shikii/decisions/`, `.shikii/wiki/` y `.shikii/labels/` (sus archivos de decisión, el conocimiento y sus correcciones: los activos que sobreviven a un cambio de modelo). `.shikii/records/` (cada decisión) crece sin parar, así que hacerle commit es opcional (`shikii gc` descarta los antiguos). Si Codex ya tiene `mcp_servers`, no se añade nada y se imprimen las líneas que hay que pegar. Un proyecto con solo un `CLAUDE.md` no recibe `AGENTS.md` (OpenCode dejaría de leer `CLAUDE.md`).

Cambiar la propia página de respuestas (`web/`, Svelte) necesita Node 24 (`make web`). Usar shikii no: la página ya construida está en el repositorio e incrustada en el binario.

## Los caminos más cortos

- **Docker**: ¿sin Go, sin modelo? `docker compose up` arranca un modelo local (Kev-4B) y shikii, con la página de respuestas en http://127.0.0.1:8095 ([detalles](../../integrations/docker.md))
- **Decisiones listas para usar**: seis en la carpeta `decisions/` de este repositorio (tipo de solicitud, informe de error duplicado, misma consulta, …). Copie una a su `.shikii/decisions/`, o parta de ella con `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([lista](../../decisions/README.md))
- **Ya tiene una tabla de decisiones pasadas**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` la convierte en correcciones, así no responde las primeras 50 a mano
- **Desde su propio programa**: arranque `shikii serve` y llame a `POST /v1/decide` (decidir) y `POST /v1/label` (corregir). El cuerpo es JSON y `-H 'Content-Type: application/json'` es obligatorio (todos los endpoints: [la guía larga](../guide.md#or-run-one-process-for-programs-and-people))
- **El ejemplo de otra persona**: [encaminar correos de consulta](../examples/inquiry-route.md) (japonés). Alguien que usaba shikii por primera vez dio la vuelta completa solo con el README; se incluyen el archivo de decisión y las 22 entradas
- **Medir el propio pensamiento de la IA** (Claude Code y Codex): `shikii hook install` añade dos hooks que ejecutan shikii antes y después de cada turno. Antes: ¿esta petición acabará cambiando archivos, y debería el agente acordar primero un enfoque? Después: ¿los cambios quedaron dentro de lo que usted pidió, y terminó lo que usted pidió? La primera verdad sale gratis de si el árbol de trabajo cambió; el resto lo responde usted en una página. Nada cambia hasta que exista una línea. Después, una petición que necesita un plan se convierte en una nota para el agente, y un turno que parece sin terminar o fuera de lo pedido se convierte en un renglón para usted, con el número de verdades sobre las que descansa la línea y la precisión revisada por personas. Ningún hook detiene nunca una herramienta ([detalles](../../integrations/claude-code.md), japonés)
- **En CI**: `uses: hayaenfj/shikii@main` ejecuta `shikii check` en cada pull request, de modo que una redacción cambiada o una línea debilitada hace fallar la build

## Una vuelta completa

```sh
shikii up                                                             # arrancar el modelo local (se descarga la primera vez)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (aún no hay línea, así que: preguntar a una persona)
shikii serve --local http://127.0.0.1:8080                                   # los archivos de decisión y el conocimiento se leen de .shikii/
#   → responda en http://127.0.0.1:8095. Cuando una decisión tiene el número que pide (eval.min_n, 20 en esta), la línea se traza sola
#     Para probar con menos respuestas, baje eval.min_n en el archivo de decisión a lo que tenga (una línea escrita con --force la marca check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (se puede actuar, con la prueba: la línea descansa sobre 61 correcciones, 48 de ellas de personas. De las respuestas humanas que esta línea pone del lado de act, 25, el 96 % fueron correctas)
```

Solo hay cuatro bandas. `act` = adelante, `ask` = consultar a una persona, `discard` = no hacer nada, `abstain` = falta la entrada. Usted nunca lee una probabilidad en bruto.

## Qué hay de nuevo

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arquitectura de shikii: tres cosas que hacen las personas, los activos (preguntas, verdades, recibos), el bucle que la máquina hace crecer, la wiki verificable, modelos desechables" width="100%"></p>

- **El conocimiento se acumula en las preguntas y las correcciones, no en el modelo.** Así que el modelo es desechable y cambiarlo no necesita trabajo humano. Medido: con la máquina eligiendo la redacción y la disposición y añadiendo ejemplos parecidos, un modelo base corriente igualó en acierto al ajuste fino (LoRA) ([números](../bench.md))
- **Las decisiones automáticas vienen con pruebas.** No «95 % de confianza», sino «precisión 0,96 sobre 48 respuestas revisadas por personas»
- **La wiki no se lee, se mide.** «Preguntar así funciona» se acumula como afirmaciones con números y se vuelve a probar en modelos nuevos. Solo una persona decide que una afirmación está confirmada

## Leer más

- [La guía larga](../guide.md): cada comando, el inicio rápido completo, la API HTTP, qué se puede conectar, los resultados medidos y las preguntas frecuentes · [la referencia del archivo de decisión](../../skills/shikii/references/spec.md)
- [Notas de diseño](../decision-ops-framework.md): de qué se hace cargo el framework y de qué se niega a hacerse cargo, y qué se verificó · [Arquitectura](../architecture.md) (japonés)
- [Decisiones listas para usar](../../decisions/README.md) · [la wiki de cómo preguntar](../../wiki/index.md) · [benchmark](../bench.md) · [comparación de modelos](../model-comparison.md) · [registro de operación](../operations.md)
- [Conexiones](../../integrations/mcp.md): MCP, el adaptador que convierte un modelo de chat corriente en un modelo de decisión, Docker, Claude Code
- [Guía en japonés](../guide.ja.md) con una vuelta completa de un caso real

## Contribuir

Los issues y los pull requests son bienvenidos. Un test que protege un comportamiento se muestra primero fallando: rompa el comportamiento a propósito, ejecute el test, anote cómo falló, luego arréglelo y anote que pasa. Cada número de la documentación sale de un script. Solo las personas cambian el estado de una afirmación de la wiki (`confirmed` / `tentative` / `rejected`), con la ejecución que sirve de prueba. Sin opciones nuevas: la disposición (`decisions/` `wiki/` `.shikii/` en la raíz del proyecto) y las reglas fijas hacen el trabajo.

## Licencia

[Apache 2.0](../../LICENSE). Las licencias de los modelos son aparte: Kev-4B y la familia Bonsai son Apache 2.0, OpenJev es CC BY-NC 4.0, el Jev alojado es un servicio de TypeSafe. shikii guarda sus probabilidades y etiquetas, nada de los modelos.
