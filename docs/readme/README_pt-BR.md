[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · **Português (Brasil)** · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: transforme o "provavelmente" da IA em um "pode usar" com que dá para tocar um negócio

### Uma ferramenta que deixa uma IA decidir "sim ou não", "qual" e "quanto", e a faz crescer a partir das suas correções. Tudo o que você faz é responder, em uma página, o que era verdade de fato.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-313%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[O problema](#o-problema) · [Uso 1: medir o pensamento da própria IA](#uso-1-medir-o-pensamento-da-própria-ia) · [Uso 2: delegar uma decisão do negócio](#uso-2-delegar-uma-decisão-do-negócio) · [O que você faz na página](#o-que-você-faz-na-página) · [Instalação](#instalação) · [Por que dá para trocar a IA](#por-que-dá-para-trocar-a-ia) · [Leia mais](#leia-mais)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## O problema

Pergunte a uma IA "esta solicitação é o mesmo caso da anterior?" e ela responde "provavelmente, 0,9". Para o trabalho, isso não basta. Esse 0,9 acerta mesmo nove vezes em dez? Virou mentira em silêncio no dia em que você trocou por uma IA mais nova? As decisões que ela toma enquanto ninguém está olhando continuam certas? Se uma pessoa precisa conferir toda vez, usar a IA serviu para pouco.

O shikii traça uma **linha** a partir das suas correções: acima dela, a IA pode agir sozinha. Enquanto não existe linha, nada é confiado. Quando ela existe, cada decisão automática carrega seus fundamentos: em quantas correções a linha se apoia e com que frequência acertou. A linha vive junto da pergunta e das correções, não dentro da IA, então trocar a IA não custa trabalho humano.

Há duas formas de usar: medir o pensamento da IA que escreve o seu código (a mais leve) e delegar a ela uma decisão do seu negócio.

## Uso 1: medir o pensamento da própria IA

Para quem usa Claude Code ou Codex. O agente de código faz julgamentos quando recebe uma solicitação e quando termina; o shikii os mede em silêncio.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Só isso. Ele instala o binário, acrescenta dois hooks ao Claude Code e ao Codex, coloca as regras do agente (um skill) no lugar e inicia uma pequena IA local (Kev-4B) em segundo plano (se o llama.cpp estiver faltando, imprime a única linha para instalá-lo). Abra um terminal novo e continue trabalhando com o agente como sempre (no Codex, rode `/hooks` uma vez para confiar nos hooks do shikii). Quatro coisas são medidas.

| Quando | Pergunta | De onde vem a verdade |
|---|---|---|
| Chega uma solicitação | Esta solicitação vai acabar mudando arquivos? | Automático: a árvore de trabalho mudou antes da próxima solicitação |
| Chega uma solicitação | O agente deveria combinar um caminho com você antes de tocar em algo? | Você, na página |
| O turno termina | As mudanças ficaram dentro do que você pediu? | Você, na página |
| O turno termina | Ele terminou o que você pediu? | Você, na página |

No fim de um turno aparece uma linha, `shikii: 3 decisões desta sessão aguardam sua resposta: http://127.0.0.1:8096/`, e você a abre quando tiver um minuto (o hook também inicia a página em segundo plano); cada resposta leva segundos. Depois de 50 respostas uma linha é traçada e o comportamento do agente muda: uma solicitação que precisa de um plano vira um aviso ao agente, e um turno que parece inacabado ou fora do escopo vira uma única linha para você, cada um com fundamentos como "linha sobre 61 verdades, precisão conferida por pessoas 0,96". Até então os hooks só registram. Nenhum hook jamais interrompe uma ferramenta.

Tudo vive em um só lugar, `~/.shikii/agent`, com uma linha por pergunta para todos os seus projetos (as perguntas são sobre o pensamento do agente, então a verdade é a mesma em qualquer repositório). Cada registro é marcado com o repositório de onde veio, e a página pode filtrar por ele. Detalhes: [o hook](../../integrations/claude-code.md) (em japonês).

## Uso 2: delegar uma decisão do negócio

Para decisões que você repete no trabalho, como "esta solicitação é o mesmo caso?" ou "esta mensagem é um pedido de mudança?": deixe a IA decidir e faça-a crescer a partir das suas correções. Aqui também não há comando para digitar. Você diz ao agente.

> Quero que o shikii decida se uma solicitação é o mesmo caso de uma anterior, e que cresça a partir das minhas correções

O agente segue suas regras (o skill): cria `.shikii/` no projeto (`shikii init`), define a pergunta, as opções e as entradas e escreve o arquivo de decisão (`shikii new`; ele mostra a pergunta e as opções a você uma vez, porque essa é a sua intenção), chama a decisão (`shikii decide`), inicia a página de respostas (http://127.0.0.1:8095) e avisa você. Enquanto não existe linha, nenhuma decisão é `act` (vá em frente). Conforme você responde na página, a linha é traçada sozinha quando a decisão alcança o número que ela mesma pede (20 neste exemplo), e daí em diante:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (pode agir. A linha se apoia em 61 verdades, 48 delas respondidas por pessoas; das respondidas por pessoas que esta linha deixaria agir, 25, 96% estavam certas)
```

Há só quatro valores de `band`: `act` = vá em frente, `ask` = confira com uma pessoa, `discard` = não aja, `abstain` = falta entrada. Você nunca age com base em probabilidades brutas.

- **À mão**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Está tudo no [guia](../guide.md)
- **A partir de um agente**: `shikii init` escreve a configuração MCP, então Claude Code, Cursor, OpenCode e Codex podem chamar as ferramentas `shikii_decide` e `shikii_label` diretamente ([MCP](../../integrations/mcp.md))
- **A partir do seu próprio programa**: inicie `shikii serve` e chame `POST /v1/decide` (decidir) e `POST /v1/label` (corrigir). O corpo é JSON e precisa de `-H 'Content-Type: application/json'` ([API HTTP](../guide.md#or-run-one-process-for-programs-and-people))
- **Decisões prontas**: seis em `decisions/` deste repositório (tipo de solicitação, relato de bug duplicado, mesma solicitação, …). Copie uma com `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` e reescreva ([lista](../../decisions/README.md))
- **Você já tem uma tabela de decisões passadas**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` a transforma em correções, para você não responder as primeiras 50 à mão
- **Docker**: ainda sem Go e sem modelo? `docker compose up` inicia uma IA local e o shikii, com a página em http://127.0.0.1:8095 ([detalhes](../../integrations/docker.md))
- **O exemplo de outra pessoa**: [encaminhar e-mails de atendimento](../examples/inquiry-route.md) (em japonês). Alguém que usava pela primeira vez percorreu a volta inteira só com o README; o arquivo de decisão e as 22 entradas estão incluídos

## O que você faz na página

Na página de respostas (`shikii serve`) uma pessoa faz três coisas. Todo o resto é trabalho da máquina.

1. **Responder.** A página mostra as decisões em que a IA ficou em dúvida, mais 1 em cada 20 das que ela executou, e você aperta o botão do que era verdade de fato. As teclas 1 a 9 e Enter (igual à escolha da IA) agilizam. "Respondidas" permite corrigir uma resposta anterior (a mais nova é a que vale)
2. **Escrever uma nova redação.** Se você pensar "perguntar deste jeito funcionaria melhor", digite no painel "Redação" com suas próprias palavras. A máquina a pontua contra a redação atual nas mesmas correções (para as decisões do próprio agente, automaticamente a cada 50 respostas novas) e só diz "pode ser adotada" quando é de fato melhor. Você também pode deixar uma IA propor redações. Você nunca abre um arquivo
3. **Apertar adotar ou rejeitar.** Você olha se o sentido mudou e aperta. A máquina confere os números; você confere o sentido

"Detalhes" acrescenta o estado da linha por modelo, "perguntar à IA de novo" (decidir o mesmo texto outra vez com a redação e a linha de hoje e ver o antes e o depois) e o painel de conhecimento (afirmações do tipo "perguntar deste jeito funciona", com suas evidências). Os registros são acrescentados, nunca apagados; uma decisão nova toma o lugar da antiga.

## Instalação

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 ou mais novo; senão, Docker (acima)
```

O que o install.sh faz: instala os binários (`shikii` e `shikii-readout`), acrescenta dois hooks ao Claude Code e ao Codex, coloca as regras do agente (o skill) em `~/.claude/skills/shikii` e, se o llama.cpp estiver presente, inicia a IA local (Kev-4B em :8080) em segundo plano. Para usar o Jev hospedado ou o OpenRouter em vez disso, coloque a chave em uma variável de ambiente ([conexões](../guide.md#what-you-can-connect)). `shikii doctor` diz o que está rodando e o que está faltando.

A preparação por projeto (`shikii init`) é trabalho do agente (você também pode rodá-la). Ela cria uma única pasta, `.shikii/` (dentro: `decisions/` = arquivos de decisão, `wiki/` = conhecimento, `records/` e `labels/` = registros e correções). Nada mais aparece na raiz do projeto. `decisions/`, `wiki/` e `labels/` são bens que sobrevivem a uma troca de IA, então faça commit deles; `records/` não para de crescer, então o commit é opcional (`shikii gc` descarta os antigos). Ela também acrescenta uma entrada do shikii à configuração dos seus agentes (`.mcp.json` do Claude Code, `.cursor/mcp.json` do Cursor, `opencode.json` do OpenCode, `~/.codex/config.toml` do Codex) sem tocar nas entradas que você escreveu. Rodar de novo não muda nada do que já existe.

A saída dos comandos e as mensagens dos hooks são em inglês (se o idioma configurado no Claude Code for japonês, as mensagens dos hooks aparecem em japonês ao lado do inglês). A página tem japonês. Os detalhes finos, como usar só uma IA hospedada, `AGENTS.md` versus `CLAUDE.md`, e o Node para mudar a própria página, estão no [guia](../guide.md).

## Por que dá para trocar a IA

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="arquitetura do shikii: três coisas que as pessoas fazem, os bens (perguntas, verdades, recibos), o ciclo que a máquina faz crescer, o wiki verificável, o modelo descartável" width="100%"></p>

- **O conhecimento se acumula na pergunta e nas correções, não dentro da IA.** As correções sobrevivem à IA, então uma nova pode usá-las como estão e a linha é traçada de novo. Nas nossas medições, deixar a máquina escolher a redação e o arranjo e acrescentar exemplos parecidos alcançou a mesma acurácia que o ajuste fino dos pesos do modelo (LoRA) ([números](../bench.md))
- **As decisões automáticas carregam um recibo.** Não "95% de confiança", mas "precisão 0,96 em 48 casos conferidos por pessoas"
- **O wiki não se lê, se mede.** "Perguntar deste jeito funciona" se acumula como afirmações com números, e pode ser testado de novo em uma IA nova. Só uma pessoa decide que uma afirmação está confirmada

## Leia mais

- [O guia](../guide.md): cada comando, a API HTTP, perguntas frequentes · [a referência do spec](../../skills/shikii/references/spec.md)
- [O hook](../../integrations/claude-code.md) (em japonês): como o pensamento do próprio agente é medido, a tabela faixa → comportamento, o funcionamento interno
- [Arquitetura](../architecture.md) (em japonês): o quadro completo por trás do diagrama acima
- [Decisões prontas](../../decisions/README.md) · [o wiki de como perguntar](../../wiki/index.md) · [números medidos](../bench.md)
- [Conexões](../../integrations/mcp.md): MCP, o adaptador para modelos de chat comuns, Docker

**Para quem contribui**: quebre um teste de propósito e veja-o falhar antes de corrigir. Cada número vem de um script. Só uma pessoa confirma uma afirmação do wiki. Não acrescente configurações; lugares fixos e regras fixas no lugar delas.

## Licença

[Apache-2.0 OR MIT](../../LICENSE). As licenças dos modelos são separadas (Kev-4B e a família Bonsai são Apache 2.0, OpenJev é CC BY-NC 4.0, o Jev hospedado é um serviço da TypeSafe).
