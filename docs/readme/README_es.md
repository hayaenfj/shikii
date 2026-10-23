[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · **Español** · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: convertir el «probablemente» de la IA en un «se puede usar» sobre el que montar un negocio

### Una herramienta que deja que una IA decida «sí o no», «cuál» y «cuánto», y la hace crecer con sus correcciones. Lo único que usted hace es responder, en una página, qué era cierto de verdad.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-361%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[El problema](#el-problema) · [Uso 1: medir el propio pensamiento de la IA](#uso-1-medir-el-propio-pensamiento-de-la-ia) · [Uso 2: encargarle una decisión del negocio](#uso-2-encargarle-una-decisión-del-negocio) · [Qué hace usted en la página](#qué-hace-usted-en-la-página) · [Instalación](#instalación) · [Por qué puede cambiar la IA](#por-qué-puede-cambiar-la-ia) · [Leer más](#leer-más)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## El problema

Pregunte a una IA «¿esta consulta es el mismo asunto que la anterior?» y responde «probablemente, 0,9». Para el trabajo, eso no basta. ¿Ese 0,9 acierta de verdad nueve veces de cada diez? ¿Se convirtió en mentira, sin que nadie lo notara, el día en que cambió a una IA más nueva? Las decisiones que toma mientras nadie mira, ¿siguen siendo correctas? Si una persona tiene que revisar cada vez, usar una IA tenía poco sentido.

shikii traza una **línea** a partir de sus correcciones: por encima de ella, la IA puede actuar sola. Hasta que exista una línea, no se confía en nada. Cuando existe, cada decisión automática lleva su fundamento: sobre cuántas correcciones descansa la línea y con qué frecuencia acertó. La línea vive junto a la pregunta y las correcciones, no dentro de la IA, así que cambiar la IA no cuesta trabajo humano.

Hay dos formas de usarla: medir el pensamiento de la IA que escribe su código (la más ligera) y encargarle una decisión de su negocio.

## Uso 1: medir el propio pensamiento de la IA

Para quienes usan Claude Code o Codex. El agente que escribe código toma decisiones cuando recibe una petición y cuando termina; shikii las mide en silencio.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Eso es todo. Instala el binario, añade dos hooks a Claude Code y Codex, coloca las reglas del agente (una skill) y arranca en segundo plano una pequeña IA local (Kev-4B) (si falta llama.cpp, imprime la línea para instalarlo). Abra una terminal nueva y siga trabajando con el agente como siempre (en Codex, ejecute `/hooks` una vez para dar confianza a los hooks de shikii). Se miden cuatro cosas.

| Cuándo | Pregunta | De dónde sale la verdad |
|---|---|---|
| Llega una petición | ¿Esta petición acabará cambiando archivos? | Automático: si el árbol de trabajo cambió antes de la siguiente petición |
| Llega una petición | ¿Debería el agente acordar un enfoque con usted antes de tocar nada? | Usted, en la página |
| Termina el turno | ¿Los cambios quedaron dentro de lo que pidió? | Usted, en la página |
| Termina el turno | ¿Terminó lo que pidió? | Usted, en la página |

Al final de un turno aparece un renglón, `shikii: 3 decisiones de esta sesión esperan su respuesta: http://127.0.0.1:8096/`, y usted lo abre cuando tiene un minuto (el hook también arranca la página en segundo plano); cada respuesta lleva segundos. Tras 50 respuestas se traza una línea y el comportamiento del agente cambia: una petición que necesita un plan se convierte en una nota para el agente, y un turno que parece sin terminar o fuera de lo pedido se convierte en un renglón para usted, cada uno con fundamentos como «línea sobre 61 verdades, precisión revisada por personas 0,96». Hasta entonces, los hooks solo registran. Ningún hook detiene nunca una herramienta.

Todo vive en un solo lugar, `~/.shikii/agent`, con una línea por pregunta para todos sus proyectos (las preguntas tratan del pensamiento del agente, así que la verdad es la misma en cualquier repositorio). Cada registro lleva la marca del repositorio del que salió, y la página puede filtrar por él. Detalles: [el hook](../../integrations/claude-code.md) (japonés).

## Uso 2: encargarle una decisión del negocio

Para decisiones que repite en el trabajo, como «¿esta consulta es el mismo asunto?» o «¿este mensaje es una solicitud de cambio?»: deje que la IA decida y hágala crecer con sus correcciones. Aquí tampoco hay comandos que escribir. Se lo dice al agente.

> Quiero que shikii decida si una consulta es el mismo asunto que una anterior, y que crezca con mis correcciones

El agente sigue sus reglas (la skill): crea `.shikii/` en el proyecto (`shikii init`), fija la pregunta, las opciones y las entradas y escribe el archivo de decisión (`shikii new`; le muestra una vez la pregunta y las opciones, porque esa es su intención), llama a la decisión (`shikii decide`), arranca la página de respuestas (http://127.0.0.1:8095) y le avisa. Hasta que exista una línea, ninguna decisión es `act` (adelante). Según usted responde en la página, la línea se traza sola cuando se alcanza el número propio de esa decisión (20 en este ejemplo), y desde entonces:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (se puede actuar. La línea descansa sobre 61 verdades, 48 de ellas respondidas por personas; de las respondidas por personas que esta línea dejaría actuar, 25, el 96 % fueron correctas)
```

Solo hay cuatro valores de `band`: `act` = adelante, `ask` = consultar a una persona, `discard` = no actuar, `abstain` = falta entrada. Nunca se actúa sobre probabilidades en bruto.

- **A mano**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Todo está en [la guía](../guide.md)
- **Desde un agente**: `shikii init` escribe la configuración MCP, así que Claude Code, Cursor, OpenCode y Codex pueden llamar directamente a las herramientas `shikii_decide` y `shikii_label` ([MCP](../../integrations/mcp.md))
- **Desde su propio programa**: arranque `shikii serve` y llame a `POST /v1/decide` (decidir) y `POST /v1/label` (corregir). El cuerpo es JSON y necesita `-H 'Content-Type: application/json'` ([API HTTP](../guide.md#or-run-one-process-for-programs-and-people))
- **Decisiones listas para usar**: seis en la carpeta `decisions/` de este repositorio (tipo de solicitud, informe de error duplicado, misma consulta, …). Copie una con `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` y reescríbala ([lista](../../decisions/README.md))
- **Ya tiene una tabla de decisiones pasadas**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` la convierte en correcciones, así no tiene que responder las primeras 50 a mano
- **Docker**: ¿sin Go y sin modelo todavía? `docker compose up` arranca una IA local y shikii, con la página en http://127.0.0.1:8095 ([detalles](../../integrations/docker.md))
- **El ejemplo de otra persona**: [encaminar correos de consulta](../examples/inquiry-route.md) (japonés). Alguien que usaba shikii por primera vez dio la vuelta completa solo con el README; se incluyen el archivo de decisión y las 22 entradas

## Qué hace usted en la página

En la página de respuestas (`shikii serve`) una persona hace tres cosas. Todo lo demás es trabajo de la máquina.

1. **Responder.** La página muestra las decisiones en las que la IA dudó, más 1 de cada 20 de las que ejecutó, y usted pulsa el botón de lo que era cierto de verdad. Las teclas 1 a 9 y Enter (igual que la elección de la IA) van rápido. «Respondidas» permite corregir una respuesta anterior (cuenta la más reciente)
2. **Escribir otra redacción.** Si piensa «preguntado así funcionaría mejor», escríbalo con sus propias palabras en el panel «Redacción». La máquina la puntúa frente a la redacción actual sobre las mismas correcciones (para las decisiones propias del agente, automáticamente cada 50 respuestas nuevas) y solo dice «se puede adoptar» cuando es realmente mejor. También puede dejar que una IA proponga redacciones. Usted nunca abre un archivo
3. **Pulsar adoptar o rechazar.** Usted mira si cambió el significado y pulsa. La máquina revisa los números; usted revisa el significado

«Detalles» añade el estado de la línea por modelo, «preguntar otra vez a la IA» (volver a decidir el mismo texto con la redacción y la línea de hoy y ver el antes y el después) y el panel de conocimiento (afirmaciones del tipo «preguntar así funciona», con sus pruebas). Los registros se añaden, nunca se borran; una decisión nueva ocupa el lugar de la antigua.

## Instalación

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 o más reciente; si no, Docker (arriba)
```

Qué hace install.sh: instala los binarios (`shikii` y `shikii-readout`), añade dos hooks a Claude Code y Codex, coloca las reglas del agente (la skill) en `~/.claude/skills/shikii` y, si llama.cpp está presente, arranca en segundo plano la IA local (Kev-4B en :8080). Para usar en su lugar el Jev alojado u OpenRouter, ponga la clave en una variable de entorno ([conexiones](../guide.md#what-you-can-connect)). `shikii doctor` le dice qué está en marcha y qué falta.

La preparación por proyecto (`shikii init`) es tarea del agente (también puede ejecutarla usted). Crea una sola carpeta, `.shikii/` (dentro: `decisions/` = archivos de decisión, `wiki/` = conocimiento, `records/` y `labels/` = registros y correcciones). Nada más aparece en la raíz del proyecto. `decisions/`, `wiki/` y `labels/` son activos que sobreviven a un cambio de IA, así que haga commit de ellos; `records/` crece sin parar, así que hacerle commit es opcional (`shikii gc` descarta los antiguos). También añade una entrada de shikii a la configuración de sus agentes (el `.mcp.json` de Claude Code, el `.cursor/mcp.json` de Cursor, el `opencode.json` de OpenCode, el `~/.codex/config.toml` de Codex) sin tocar las entradas que usted escribió. Ejecutarlo otra vez no cambia nada de lo que ya existe.

La salida de los comandos y los mensajes de los hooks están en inglés (si el idioma configurado en Claude Code es el japonés, los mensajes de los hooks se muestran en japonés junto al inglés). La página tiene japonés. Los detalles finos, como usar solo una IA alojada, `AGENTS.md` frente a `CLAUDE.md` y Node para cambiar la propia página, están en [la guía](../guide.md).

## Por qué puede cambiar la IA

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arquitectura de shikii: tres cosas que hacen las personas, los activos (preguntas, verdades, recibos), el bucle que la máquina hace crecer, la wiki verificable, el modelo desechable" width="100%"></p>

- **El conocimiento se acumula junto a la pregunta y las correcciones, no dentro de la IA.** Las correcciones sobreviven a la IA, así que una nueva puede usarlas tal cual y la línea se vuelve a trazar. En nuestras mediciones, dejar que la máquina elija la redacción y la disposición y añada ejemplos parecidos alcanzó el mismo acierto que el ajuste fino de los pesos del modelo (LoRA) ([números](../bench.md))
- **Las decisiones automáticas llevan un recibo.** No «95 % de confianza», sino «precisión 0,96 sobre 48 casos revisados por personas»
- **La wiki no se lee, se mide.** «Preguntar así funciona» se acumula como afirmaciones con números, y se puede volver a probar en una IA nueva. Solo una persona decide que una afirmación está confirmada

## Leer más

- [La guía](../guide.md): cada comando, la API HTTP, preguntas frecuentes · [la referencia del archivo de decisión](../../skills/shikii/references/spec.md)
- [El hook](../../integrations/claude-code.md) (japonés): cómo se mide el propio pensamiento del agente, la tabla banda → comportamiento, el funcionamiento interno
- [Arquitectura](../architecture.md) (japonés): el cuadro completo detrás del diagrama de arriba
- [Decisiones listas para usar](../../decisions/README.md) · [la wiki de cómo preguntar](../../wiki/index.md) · [números medidos](../bench.md)
- [Conexiones](../../integrations/mcp.md): MCP, el adaptador para modelos de chat corrientes, Docker

**Para quienes colaboran**: rompa un test a propósito y vea cómo falla antes de arreglarlo. Cada número sale de un script. Solo una persona confirma una afirmación de la wiki. No añada opciones; en su lugar, lugares fijos y reglas fijas.

## Licencia

[Apache-2.0 OR MIT](../../LICENSE). Las licencias de los modelos son aparte (Kev-4B y la familia Bonsai son Apache 2.0, OpenJev es CC BY-NC 4.0, el Jev alojado es un servicio de TypeSafe).
