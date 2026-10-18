[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · **Português (Brasil)** · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Operar modelos de decisão

### Um único ciclo de vida para todo modelo de decisão: define → decide → record → label → calibrate → check → drift.

**Um modelo entrega uma probabilidade. O shikii diz se você pode agir com base nela.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-169%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Notas de design](../decision-ops-framework.md) · [Operação](../operations.md) · [Comparação de modelos](../model-comparison.md)**

[Início rápido](#início-rápido) · [Como o shikii funciona](#como-o-shikii-funciona) · [O que você pode conectar](#o-que-você-pode-conectar) · [Resultados](#resultados) · [FAQ](#faq) · [Documentação](#documentação)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> O shikii nunca executa um modelo e nunca lê um chaveiro. Ele conversa com um endpoint de modelo de decisão que você já tem, guarda os registros e se recusa a **agir** até que as probabilidades para *esta pergunta neste modelo* tenham sido verificadas contra rótulos. Os blocos de calibração são gerados, nunca escritos à mão.

---

Modelos de decisão (TypeSafe Jev, Kev-4B e OpenJev no llama.cpp, OpenRouter Decisions, …) respondem a perguntas tipadas, `choice`, `score` e `noul` (sim/não), com probabilidades e sem texto gerado. Todo fornecedor traz o mesmo aviso: *as probabilidades não estão calibradas para os seus dados, e os limiares não valem de um modelo ou de uma pergunta para outro.*

O shikii cuida da parte que ninguém entrega: o **ciclo de vida** de uma decisão. Um arquivo spec guarda a pergunta, as opções, o template do estado e os limiares. O `decide` consulta um modelo e devolve uma faixa (`act` / `ask` / `discard` / `abstain`), nunca um número bruto. O `label` registra o que era verdade. O `calibrate` faz o ajuste e a validação cruzada, e então grava o resultado de volta na spec sob uma chave composta pelo hash da pergunta e pelo nome do modelo. O `check` derruba a CI quando a redação ou o modelo mudou. O `gc` mantém o store pequeno.

Também vem com o `shikii-readout`, um adaptador de 500 linhas que transforma **qualquer GGUF de chat em um llama.cpp padrão** em um modelo de decisão, lendo os logits das letras das opções. Em nossas medições, um modelo ternário comum de 27B lido dessa forma igualou em acurácia os modelos de decisão treinados, a 0.6 segundo por pergunta em um notebook.

> O nome é japonês: *shikii* (閾) significa limiar, a linha que transforma uma probabilidade em uma ação. A CLI se chama `shikii`.

## Início rápido

Go 1.27 ou mais recente. Nada além disso é necessário para a primeira execução.

### Rode o ciclo inteiro sem chave nem GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # modelo falso: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # um modelo falso cujas probabilidades precisam de calibração; veja o calibrate escolher platt
```

### Aponte para um modelo real

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) modelo de decisão local no llama.cpp (baixa o Kev-4B, cerca de 2.5 GB, na primeira execução)
make server                                       # llama-server em :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev hospedado. A chave vem apenas do ambiente
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

A resposta é uma faixa, um trace id e as probabilidades. Enquanto a spec não tiver uma calibração para este modelo, a faixa é no máximo `ask`.

### Feche o ciclo

```sh
bin/shikii label <trace_id> change_request          # ou:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <nome do modelo na resposta> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 quando a redação mudou ou precision@act caiu abaixo de act
```

### Use qualquer GGUF como modelo de decisão

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server padrão com flash attention e KV de 8 bits, :8086
make readout                                               # shikii-readout na frente dele, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Instalar e atualizar

```sh
make install          # copia bin/shikii para ~/.local/bin/shikii
git pull && make install
```

## Como o shikii funciona

Cada decisão é um único arquivo YAML. A pergunta, as opções, o template do estado, para onde o estado pode ser enviado, os limiares e, gerados por `calibrate --write`, os blocos de calibração.

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
  required: [message]            # entrada ausente → abstain; o modelo nunca é chamado
policy:
  send_to: [hosted, local]       # uma sessão tainted pula o hosted automaticamente
  pii: possible                  # os registros guardam o hash das entradas, nunca o texto
thresholds:
  act: 0.75                      # ≥ act → act automaticamente, só se calibrado
  ask: 0.60                      # ≥ ask → pergunta a uma pessoa; abaixo → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**A chave de calibração é `(hash de pergunta + template + opções, id do modelo como devolvido pelo endpoint)`.** Mude uma palavra da pergunta, ou deixe o `jev-latest` passar para uma nova versão, e a calibração simplesmente desaparece: a faixa cai para `ask` em vez de agir em silêncio com constantes desatualizadas. O layout do prompt também faz parte da chave: o `shikii-readout` informa seu layout no nome do modelo (`…+state-last`), porque mover o estado para dentro do prompt mudou em 5 pontos a acurácia de um modelo de 27B com os mesmos pesos.

O `calibrate` compara três métodos com validação cruzada de 5 folds e grava o vencedor. `identity` significa que as probabilidades brutas já atingem precision@act e o modelo é apenas marcado como verificado. `platt` (σ((Δ−bias)/T)) é para perguntas de sim/não. `vector` (softmax((log p + b)/T)) é para choice e score, e corrige vícios por opção, como «recorrer a *other*». Rótulos parciais (`!option`, «não era *aquilo*») contam para a precision, mas não para o ajuste. Rótulos obtidos de um arquivo são persistidos, então `check` e `drift` rodam offline depois.

Todo ramo relevante para a segurança (roteamento de taint, nenhum act sem calibração, invalidação de hash obsoleto, validação de rótulos, detecção de inversão, escolha de método por CV, `gc` em dry run, …) tem uma mutação registrada que fez um teste falhar antes de a correção ser aceita. São 63 testes Go em 8 pacotes; a demo roda na CI.

## O que você pode conectar

O shikii fala o formato de requisição do System One (`POST /v1/systemone`), então qualquer coisa que o ofereça funciona. O adaptador cobre todo o resto.

| Destino | O que o shikii recebe | Requisitos e guia |
|---|---|---|
| **TypeSafe Jev** (hospedado) | `choice` / `score` / `noul` com probabilidades, id de modelo como `jev-1.13.0` | `TYPESAFE_API_KEY` no ambiente. `--hosted` padrão |
| **Modelos de decisão no llama.cpp** (Kev-4B, OpenJev, Clef, …) | Mesmo formato, localmente, id do modelo vindo de `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 ou mais recente |
| **OpenRouter Decisions API** | Mesmo formato via OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Qualquer GGUF de chat** via `shikii-readout` | Leitura dos logits das letras no formato System One, sem calibração própria | `llama-server` padrão 0.7.0 por trás. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** via [BonJev](https://github.com/ChezRD/bonjev) | O mesmo, com o prompt do próprio BonJev e calibração embutida | Build em Rust e o fork do Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Modelo falso** (`bin/fake-systemone`) | Probabilidades determinísticas para demos e testes | Nada. `make fake` |
| **Hook do Claude Code** (claude-jev) | Cada prompt classificado, cada turno rotulado a partir da working tree | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

O taint é uma entrada de primeira classe: passe `--tainted` (ou deixe seu hook fazer isso) em uma sessão que tocou dados de produção e a requisição nunca sai da máquina. Se nenhum endpoint local estiver configurado, a decisão faz abstain.

## Resultados

Mesmas specs, mesmos rótulos, quatro conjuntos de avaliação (intenção de prompts em inglês e japonês, 62 requisições; vinculação de casos idênticos em 32 pares japoneses). Os detalhes e a tabela completa estão em [`docs/model-comparison.md`](../model-comparison.md).

| Modelo | Licença | top-1 (todos / ja) | Brier sim/não (bruto) | Calibração escolhida | Segundos por pergunta |
|---|---|---|---|---|---|
| Kev-4B (modelo de decisão treinado) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (treinado) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B via BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B via `shikii-readout`, llama.cpp padrão** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**A redação vence a calibração.** Na intenção de prompts, 19 de 21 erros caíram em *other*; reescrever as opções levou o top-1 de 0.68 para 0.90 antes de qualquer calibração. A calibração serve para o caso «ordem certa, confiança insuficiente». A matriz de confusão que o `calibrate` imprime mostra qual é o seu caso.

**Modelos já calibrados não precisam de Platt.** O sim/não do Kev-4B tinha Brier 0.046 bruto; o Platt piorou na validação cruzada, então foi gravado `identity`. A primeira vez que o `calibrate` escolheu `platt` foi com um LLM comum lido por logits. Se a calibração é necessária é uma propriedade de *modelo mais leitura*, não do modelo.

**Um GGUF comum fica ao lado de um modelo de decisão treinado.** Um modelo de chat ternário de 27B, llama.cpp mainline 0.7.0, um adaptador, sem fork, sem treinamento, sem Rust: o mesmo top-1 do melhor modelo treinado com licença Apache, japonês 1.000, 0.58 s por pergunta com flash attention e prefixo aquecido. Mover o estado de antes das opções para logo antes da resposta levou de 0.919 a 0.968 com os mesmos pesos, e é por isso que o layout faz parte da chave de calibração.

## FAQ

**O shikii executa modelos?** Não. Ele envia requisições System One a um endpoint que você mantém ou aluga, e guarda tudo o que acontece depois da resposta. O `shikii-readout` é a única exceção em espírito: converte os logits de um modelo de chat no mesmo formato, mas a inferência continua sendo do llama.cpp.

**Por que minha decisão nunca chega a `act`?** Porque a spec não tem bloco de calibração para `(hash desta pergunta, id deste modelo)`. Colete rótulos (`label`, ou `--labeled file`), rode `calibrate --write`, e o `status` mostra `calibrated: identity|platt|vector`. Uma decisão sem precision verificada no limiar de act não tem permissão para agir. É esse o ponto.

**Mudei uma palavra na pergunta e a calibração sumiu.** Correto. O hash mudou, as constantes foram ajustadas para outra pergunta. O `status` mostra os rótulos antigos como `stale hash`; rode `calibrate --write` de novo quando tiver rótulos para a nova redação.

**Qual modelo devo usar?** Depende de licença, velocidade e idioma, e o shikii não decide por você. A tabela acima e [`docs/model-comparison.md`](../model-comparison.md) são o argumento: com a mesma spec e os mesmos rótulos você escolhe pelos números, inclusive se um modelo precisa ou não de calibração. `scripts/eval-model.sh <url> <name>` reproduz a tabela para um novo endpoint.

**Funciona em japonês?** Sim, e é aí que as diferenças aparecem. O Kev-4B obtém 0.913 em prompts em japonês; os modelos de 27B obtêm 1.000. A vinculação de casos idênticos em resumos de investigação em japonês separa completamente positivos de negativos em todos os modelos de 27B.

**Para onde vão os meus dados?** Apenas o estado renderizado vai para o endpoint indicado em `send_to`. Com `pii: possible` (o padrão), o store guarda um hash das entradas, nunca o texto. `--tainted` mantém tudo local. As chaves são lidas de `DECISIONS_API_KEY` ou `TYPESAFE_API_KEY` e de nenhum outro lugar.

**O store não para de crescer.** O `shikii gc` remove registros com mais de 30 dias que nunca receberam um rótulo. Registros rotulados e rótulos ficam para sempre, pois são o material de calibração. É um dry run, a menos que você passe `--apply`.

**Há viés para a primeira opção?** Meça antes de corrigir: `shikii bias <spec> --permutations 100` embaralha as opções e informa as trocas de resposta e o deslocamento de posição. Kev-4B: 0 trocas em 100, deslocamento ≤ 0.024. LLMs comuns lidos por logits podem se comportar de outra forma; o comando existe para eles.

## Documentação

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): notas de design, o que o framework assume e o que se recusa a assumir, o plano de verificação e o que foi confirmado.
- [`docs/operations.md`](../operations.md): como parar ou reverter cada parte em movimento, como ler uma falha, o que ainda falta.
- [`docs/model-comparison.md`](../model-comparison.md): a tabela de comparação e como reproduzi-la.
- [`docs/guide.ja.md`](../guide.ja.md): o guia longo em japonês, com todos os comandos e um ciclo completo em um caso real.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): as três conexões que operamos.
- [`examples/`](../../examples/) e [`testdata/`](../../testdata/README.md): uma spec sem calibração para começar e os conjuntos sintéticos rotulados usados nos testes.

Os comandos:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, blocos escritos à mão, precision@act abaixo de act → exit 1
shikii drift     <spec> --model M [--window N]            # rótulos recentes contra o Brier e a precision registrados na calibração
shikii bias      <spec> [--rows K] [--permutations N]     # a resposta depende da ordem das opções?
shikii status    [spec...] [--store DIR]                  # inventário: registros, rótulos, fontes, estado por (modelo, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # remove registros antigos sem rótulo; dry run por padrão
shikii hash      <spec>
```

## Contribuindo

Issues e pull requests são bem-vindos. Uma regra: um teste que protege um comportamento precisa ser mostrado falhando primeiro. Quebre o comportamento de propósito, rode o teste, registre como ele falhou, depois corrija e registre que passa. A documentação lista essas mutações para cada ramo relevante para a segurança; mantenha a lista honesta.

## Licença

[Apache 2.0](../../LICENSE). As licenças dos modelos são separadas: Kev-4B e a família Bonsai são Apache 2.0, o OpenJev é CC BY-NC 4.0, o Jev hospedado é um serviço da TypeSafe. O shikii guarda as suas probabilidades e os seus rótulos, nada dos modelos.
