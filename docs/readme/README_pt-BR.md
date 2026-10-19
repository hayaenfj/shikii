[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · **Português (Brasil)** · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: transforme o "provavelmente" de uma IA em um "pode agir" com que dá para tocar um negócio

### Uma ferramenta para deixar uma IA decidir sim/não, qual ou quanto, e manter essa decisão confiável no trabalho do dia a dia. Você confere algumas respostas e, de vez em quando, confirma "isso ainda é o que eu queria?". A máquina faz o resto.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-191%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[O problema](#o-problema) · [O que o shikii faz](#o-que-o-shikii-faz) · [O que você faz](#o-que-você-faz) · [Instalação](#instalação) · [Caminhos mais curtos](#caminhos-mais-curtos) · [Uma volta completa](#uma-volta-completa) · [O que há de novo](#o-que-há-de-novo) · [Leia mais](#leia-mais)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: um modelo dá uma probabilidade, o shikii diz se você pode agir com base nela" width="100%"></p>

## O problema

Pergunte a uma IA "esta solicitação é o mesmo assunto da anterior?" e ela responde "provavelmente, 0,9". Para o trabalho de verdade, isso não basta.

- Esse 0,9 acerta mesmo 9 vezes em 10?
- Desde o dia em que você trocou por um modelo novo, ele começou a mentir em silêncio?
- Se você muda um pouco a redação, como a precisão se move?
- As decisões que ela tomou sozinha, sem ninguém olhando: ainda estão certas?

Se uma pessoa precisa conferir cada resposta, usar a IA serviu para pouco.

## O que o shikii faz

1. **Reúne suas correções e traça a linha acima da qual a IA pode agir sozinha.** A partir das respostas que você deu ("isto é o que era verdade de fato"), ele mede como a confiança da IA se relaciona com a frequência com que ela acerta e decide a partir de onde se pode confiar nela. Enquanto não existe linha, nada age. Quando ela existe, cada decisão automática carrega um recibo que diz em quantas respostas conferidas a linha se apoia e com que frequência elas estavam certas
2. **Experimenta outras formas de perguntar e fica com a melhor.** Com as mesmas correções, ele pontua perguntas, descrições de opções e arranjos de prompt alternativos, e só adota um quando é de fato melhor. Você só confirma que o sentido não mudou
3. **Permite trocar o modelo sem começar do zero.** As correções ficam separadas do modelo, então um modelo novo as reaproveita e a máquina traça a linha de novo. Sem trabalho humano
4. **Guarda o que aprendeu em um wiki.** "Perguntar deste jeito funciona" se acumula como afirmações com os números medidos, que podem ser testadas de novo em outro modelo. O que outras pessoas mediram nos modelos delas é somado à mesma página

## O que você faz

- **Escreva o que você quer que seja decidido.** Responda a algumas perguntas e um arquivo de decisão aparece (`shikii new`, de forma interativa ou com `--type --question --option --input --pii`; o formato completo está na [referência do spec](../../skills/shikii/references/spec.md))
- **Confira respostas.** A página mostra as decisões em que a IA ficou em dúvida, mais 1 em cada 20 das que ela executou, e você aperta o botão do que era verdade de fato. As teclas 1 a 9 e Enter (igual à escolha da IA) agilizam. "Respondidas" permite corrigir uma resposta anterior (a mais nova é a que vale). "Detalhes" acrescenta "perguntar à IA de novo", que decide o mesmo texto outra vez com a redação e a linha de hoje e mostra o antes e o depois; os registros são acrescentados, nunca apagados
- **De vez em quando, confirme "isso ainda é o que eu queria?".** Quando a máquina encontra uma redação que pontua melhor, você olha se o sentido mudou e aperta adotar ou rejeitar

Todo o resto é trabalho da máquina.

## Instalação

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # precisa de Go 1.27 ou mais novo
cd <raiz do projeto> && shikii init
```

`shikii init` cria uma única pasta, `.shikii/` (com `decisions/` para seus specs, `wiki/` para o conhecimento, `records/` e `labels/` para os registros e suas correções; nada mais é acrescentado à raiz do projeto), e as integrações com agentes de uma só vez: o servidor MCP do shikii para Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) e Codex (`~/.codex/config.toml`), uma seção em `AGENTS.md` e as regras detalhadas (o skill) em `~/.claude/skills/shikii`. O que você mesmo escreveu nunca é tocado (uma entrada do shikii já existente é deixada como está). Só a pasta do skill é substituída a cada vez, então não a edite à mão. `wiki/` recebe o conhecimento compartilhado (como perguntar cada tipo de pergunta, padrões de redação que funcionaram, os hábitos de cada modelo); o melhorador de redação (`tune --propose`) o lê, e as páginas que você editou nunca são sobrescritas por um `init` posterior. Sem Go? Use o Docker, abaixo.

As entradas apontam para um modelo local (`shikii up`, porta 8080). Só modelo hospedado? Remova `--local` à mão de cada um dos quatro arquivos. Também são criados arquivos de configuração para ferramentas que você não usa; apague-os ou acrescente-os ao `.gitignore` (um segundo `shikii init` nunca toca no que já existe). Faça commit de `.shikii/decisions/`, `.shikii/wiki/` e `.shikii/labels/` (seus specs, o conhecimento e suas correções: os bens que sobrevivem a uma troca de modelo). `.shikii/records/` (cada decisão) não para de crescer, então o commit é opcional (`shikii gc` descarta os antigos). Se o Codex já tiver `mcp_servers`, nada é acrescentado e as linhas a colar são impressas. Um projeto que só tem `CLAUDE.md` não recebe `AGENTS.md` (o OpenCode deixaria de ler o `CLAUDE.md`).

Mudar a própria página de respostas (`web/`, Svelte) exige Node 24 (`make web`). Usar o shikii, não: a página já montada está no commit e embutida no binário.

## Caminhos mais curtos

- **Docker**: sem Go, sem modelo? `docker compose up` inicia um modelo local (Kev-4B) e o shikii, com a página de respostas em http://127.0.0.1:8095 ([detalhes](../../integrations/docker.md))
- **Decisões prontas**: seis em `decisions/` deste repositório (tipo de solicitação, relato de bug duplicado, mesma solicitação, …). Copie uma para o seu `.shikii/decisions/`, ou comece a partir dela com `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([lista](../../decisions/README.md))
- **Você já tem uma tabela de decisões passadas**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` a transforma em correções, para você não responder as primeiras 50 à mão
- **A partir do seu próprio programa**: inicie `shikii serve` e chame `POST /v1/decide` (decidir) e `POST /v1/label` (corrigir). O corpo é JSON e `-H 'Content-Type: application/json'` é obrigatório (todos os endpoints: [o guia longo](../guide.md#or-run-one-process-for-programs-and-people))
- **O exemplo de outra pessoa**: [encaminhar e-mails de atendimento](../examples/inquiry-route.md) (em japonês). Alguém que usava pela primeira vez percorreu a volta inteira só com o README; o spec e as 22 entradas estão incluídos
- **Medir o pensamento da própria IA** (Claude Code e Codex): `shikii hook install` acrescenta dois hooks que rodam o shikii antes e depois de cada turno. Antes: esta solicitação vai acabar mudando arquivos, e o agente deveria combinar primeiro um caminho? Depois: as mudanças ficaram dentro do que você pediu, e ele terminou o que você pediu? A primeira verdade vem de graça, de a árvore de trabalho ter mudado ou não; as outras você responde em uma página. Nada muda até existir uma linha. Depois, uma solicitação que precisa de um plano vira um aviso ao agente, e um turno que parece inacabado ou fora do que foi pedido vira uma única linha de aviso para você, com o número de verdades em que a linha se apoia e a precisão conferida por pessoas. Nenhum hook jamais interrompe uma ferramenta ([detalhes](../../integrations/claude-code.md), em japonês)
- **No CI**: `uses: hayaenfj/shikii@main` roda `shikii check` em cada pull request, então uma redação alterada ou uma linha enfraquecida faz o build falhar

## Uma volta completa

```sh
shikii up                                                             # inicia o modelo local (baixado na primeira vez)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (ainda não há linha, então: pergunte a uma pessoa)
shikii serve --local http://127.0.0.1:8080                                   # specs e conhecimento são lidos de .shikii/
#   → responda em http://127.0.0.1:8095. Quando uma decisão alcança o número que pede (eval.min_n, 20 para esta) a linha é traçada sozinha
#     Para testar com menos respostas, baixe eval.min_n no spec para o que você tem (uma linha escrita com --force é apontada pelo check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (pode agir, com a evidência: a linha se apoia em 61 correções, 48 delas de pessoas. Das respostas humanas que esta linha coloca do lado act, 25, 96% estavam certas)
```

Há só quatro faixas. `act` = vá em frente, `ask` = confira com uma pessoa, `discard` = não faça nada, `abstain` = falta entrada. Você nunca lê uma probabilidade bruta.

## O que há de novo

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arquitetura do shikii: três coisas que as pessoas fazem, os bens (perguntas, verdades, recibos), o ciclo que a máquina faz crescer, o wiki verificável, modelos descartáveis" width="100%"></p>

- **O conhecimento se acumula nas perguntas e nas correções, não no modelo.** Por isso o modelo é descartável, e trocá-lo não exige trabalho humano. Medido: com a máquina escolhendo a redação e o arranjo e acrescentando exemplos parecidos, um modelo base comum igualou o ajuste fino (LoRA) em acurácia ([números](../bench.md))
- **As decisões automáticas vêm com evidência.** Não "95% de confiança", mas "precisão 0,96 em 48 respostas conferidas por pessoas"
- **O wiki não se lê, se mede.** "Perguntar deste jeito funciona" se acumula como afirmações com números e é testado de novo em modelos novos. Só uma pessoa decide que uma afirmação está confirmada

## Leia mais

- [O guia longo](../guide.md): cada comando, o início rápido completo, a API HTTP, o que dá para conectar, os resultados medidos e as perguntas frequentes · [referência do spec](../../skills/shikii/references/spec.md)
- [Notas de projeto](../decision-ops-framework.md): o que o framework assume e o que se recusa a assumir, e o que foi verificado · [Arquitetura](../architecture.md) (em japonês)
- [Decisões prontas](../../decisions/README.md) · [o wiki de como perguntar](../../wiki/index.md) · [benchmark](../bench.md) · [comparação de modelos](../model-comparison.md) · [registro de operação](../operations.md)
- [Conexões](../../integrations/mcp.md): MCP, o adaptador que transforma um modelo de chat comum em um modelo de decisão, Docker, Claude Code
- [Guia em japonês](../guide.ja.md) com uma volta completa do mundo real

## Contribuindo

Issues e pull requests são bem-vindos. Um teste que protege um comportamento é mostrado falhando primeiro: quebre o comportamento de propósito, rode o teste, registre como falhou, depois corrija e registre que passa. Cada número da documentação vem de um script. Só pessoas mudam o estado de uma afirmação do wiki (`confirmed` / `tentative` / `rejected`), com a execução que serve de evidência. Sem opções novas: o arranjo (`decisions/` `wiki/` `.shikii/` na raiz do projeto) e regras fixas fazem o trabalho.

## Licença

[Apache 2.0](../../LICENSE). As licenças dos modelos são separadas: Kev-4B e a família Bonsai são Apache 2.0, OpenJev é CC BY-NC 4.0, o Jev hospedado é um serviço da TypeSafe. O shikii guarda suas probabilidades e rótulos, nada dos modelos.
