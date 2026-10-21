[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · **Français** · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii : transformer le « probablement » d'une IA en un « on peut agir » sur lequel bâtir une activité

### Un outil qui laisse une IA décider « oui ou non », « lequel » et « combien », et qui la fait progresser à partir de vos corrections. Tout ce que vous faites, c'est répondre, sur une page, à ce qui était vrai en réalité.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-221%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Le problème](#le-problème) · [Usage 1 : mesurer la réflexion de l'IA elle-même](#usage-1--mesurer-la-réflexion-de-lia-elle-même) · [Usage 2 : lui confier une décision métier](#usage-2--lui-confier-une-décision-métier) · [Ce que vous faites sur la page](#ce-que-vous-faites-sur-la-page) · [Installation](#installation) · [Pourquoi vous pouvez changer d'IA](#pourquoi-vous-pouvez-changer-dia) · [Pour aller plus loin](#pour-aller-plus-loin)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Le problème

Demandez à une IA « cette demande concerne-t-elle la même affaire que la précédente ? » et elle répond « probablement, 0,9 ». Pour du vrai travail, ce n'est pas assez. Ce 0,9 est-il vraiment juste neuf fois sur dix ? Est-il devenu un mensonge en silence le jour où vous êtes passé à une IA plus récente ? Les décisions qu'elle prend quand personne ne regarde sont-elles toujours bonnes ? Si une personne doit vérifier à chaque fois, utiliser une IA n'avait pas grand intérêt.

shikii trace une **ligne** à partir de vos corrections : au-dessus, l'IA peut agir seule. Tant qu'il n'y a pas de ligne, rien n'est tenu pour fiable. Dès qu'il y en a une, chaque décision automatique porte ses raisons : sur combien de corrections la ligne repose et à quelle fréquence elle avait raison. La ligne vit avec la question et les corrections, pas à l'intérieur de l'IA ; changer d'IA ne coûte donc aucun travail humain.

Il y a deux façons de l'utiliser : mesurer la réflexion de l'IA qui écrit votre code (la plus légère), et lui confier une décision de votre activité.

## Usage 1 : mesurer la réflexion de l'IA elle-même

Pour les personnes qui utilisent Claude Code ou Codex. L'agent de codage porte des jugements quand il reçoit une demande et quand il termine ; shikii les mesure en silence.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

C'est tout. Cela installe le binaire, ajoute deux hooks à Claude Code et Codex, met en place les règles de l'agent (un skill) et démarre en arrière-plan une petite IA locale (Kev-4B) (si llama.cpp manque, la ligne pour l'installer est affichée). Ouvrez un nouveau terminal et continuez à travailler avec l'agent comme d'habitude (dans Codex, lancez `/hooks` une fois pour faire confiance aux hooks de shikii). Quatre choses sont mesurées.

| Quand | Question | D'où vient la vérité |
|---|---|---|
| Une demande arrive | Cette demande finira-t-elle par modifier des fichiers ? | Automatique : l'arbre de travail a-t-il changé avant la demande suivante |
| Une demande arrive | L'agent devrait-il s'accorder avec vous sur une approche avant de toucher à quoi que ce soit ? | Vous, sur la page |
| Le tour se termine | Les modifications sont-elles restées dans ce que vous avez demandé ? | Vous, sur la page |
| Le tour se termine | A-t-il fini ce que vous avez demandé ? | Vous, sur la page |

À la fin d'un tour, une ligne apparaît, `shikii: 3 décisions de cette session attendent votre réponse : http://127.0.0.1:8096/`, et vous l'ouvrez quand vous avez une minute (le hook démarre aussi la page en arrière-plan) ; chaque réponse prend quelques secondes. Après 50 réponses, une ligne est tracée et le comportement de l'agent change : une demande qui a besoin d'un plan devient une note à l'agent, et un tour qui semble inachevé ou hors du cadre devient une ligne pour vous, chacune avec ses raisons, comme « ligne sur 61 vérités, précision vérifiée par des personnes 0,96 ». Jusque-là, les hooks ne font qu'enregistrer. Aucun hook n'arrête jamais un outil.

Tout vit en un seul endroit, `~/.shikii/agent`, avec une ligne par question pour l'ensemble de vos projets (les questions portent sur la réflexion de l'agent, donc la vérité est la même dans tous les dépôts). Chaque enregistrement porte la marque du dépôt d'où il vient, et la page peut filtrer selon ce dépôt. Détails : [le hook](../../integrations/claude-code.md) (en japonais).

## Usage 2 : lui confier une décision métier

Pour les décisions que vous répétez au travail, comme « cette demande concerne-t-elle la même affaire ? » ou « ce message est-il une demande de modification ? » : laissez l'IA décider et faites-la progresser à partir de vos corrections. Ici non plus, aucune commande à taper. Vous le dites à l'agent.

> Je veux que shikii décide si une demande concerne la même affaire qu'une demande antérieure, et qu'il progresse à partir de mes corrections

L'agent suit ses règles (le skill) : il crée `.shikii/` dans le projet (`shikii init`), fixe la question, les options et les entrées et écrit le fichier de décision (`shikii new` ; il vous montre une fois la question et les options, parce que c'est là votre intention), appelle la décision (`shikii decide`), démarre la page de réponse (http://127.0.0.1:8095) et vous prévient. Tant qu'il n'y a pas de ligne, aucune décision n'est `act` (allez-y). À mesure que vous répondez sur la page, la ligne est tracée automatiquement dès que le nombre propre à la décision est atteint (20 dans cet exemple), et à partir de là :

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (on peut agir. La ligne repose sur 61 vérités, dont 48 répondues par des personnes ; parmi les réponses humaines que cette ligne laisserait agir, 25, 96 % étaient justes)
```

Il n'y a que quatre valeurs de `band` : `act` = allez-y, `ask` = vérifier avec une personne, `discard` = ne pas agir, `abstain` = entrée insuffisante. Vous n'agissez jamais sur une probabilité brute.

- **À la main** : `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Tout est dans [le guide](../guide.md)
- **Depuis un agent** : `shikii init` écrit la configuration MCP, donc Claude Code, Cursor, OpenCode et Codex peuvent appeler directement les outils `shikii_decide` et `shikii_label` ([MCP](../../integrations/mcp.md))
- **Depuis votre propre programme** : lancez `shikii serve` et appelez `POST /v1/decide` (décider) et `POST /v1/label` (corriger). Le corps est en JSON et demande `-H 'Content-Type: application/json'` ([API HTTP](../guide.md#or-run-one-process-for-programs-and-people))
- **Décisions prêtes à l'emploi** : six dans le dossier `decisions/` de ce dépôt (type de demande, rapport de bogue en double, même demande, …). Copiez-en une avec `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` et reformulez-la ([liste](../../decisions/README.md))
- **Vous avez déjà un tableau de décisions passées** : `shikii import past.csv --spec .shikii/decisions/<id>.yaml` le transforme en corrections, pour ne pas répondre aux 50 premières à la main
- **Docker** : pas encore de Go ni de modèle ? `docker compose up` démarre une IA locale et shikii, avec la page sur http://127.0.0.1:8095 ([détails](../../integrations/docker.md))
- **L'exemple de quelqu'un d'autre** : [trier des courriels de demande](../examples/inquiry-route.md) (en japonais). Une personne qui découvrait shikii a fait tout le tour avec le seul README ; le fichier de décision et les 22 entrées sont inclus

## Ce que vous faites sur la page

Sur la page de réponse (`shikii serve`), une personne fait trois choses. Tout le reste est le travail de la machine.

1. **Répondre.** La page montre les décisions où l'IA hésitait, plus 1 sur 20 de celles qu'elle a exécutées, et vous appuyez sur le bouton de ce qui était vrai en réalité. Les touches 1 à 9 et Entrée (même choix que l'IA) vont vite. « Répondues » permet de corriger une réponse antérieure (la plus récente compte)
2. **Écrire une reformulation.** Si vous pensez « demander comme ceci marcherait mieux », tapez-le dans le panneau « Formulation » avec vos propres mots. La machine la note face à la formulation actuelle sur les mêmes corrections (pour les décisions propres à l'agent, automatiquement toutes les 50 nouvelles réponses) et ne dit « peut être adoptée » que si elle est vraiment meilleure. Vous pouvez aussi laisser une IA proposer des reformulations. Vous n'ouvrez jamais un fichier
3. **Appuyer sur adopter ou rejeter.** Vous regardez si le sens a changé et vous appuyez. La machine vérifie les chiffres ; vous vérifiez le sens

« Détails » ajoute l'état de la ligne pour chaque modèle, « redemander à l'IA » (redécider le même texte avec la formulation et la ligne d'aujourd'hui et voir l'avant et l'après), et le panneau des connaissances (des affirmations de la forme « demander comme ceci marche », avec leurs preuves). Les enregistrements sont ajoutés, jamais effacés ; une nouvelle décision prend la place de l'ancienne.

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 ou plus récent ; sinon Docker (ci-dessus)
```

Ce que fait install.sh : il installe les binaires (`shikii` et `shikii-readout`), ajoute deux hooks à Claude Code et Codex, met les règles de l'agent (le skill) dans `~/.claude/skills/shikii` et, si llama.cpp est présent, démarre l'IA locale (Kev-4B sur :8080) en arrière-plan. Pour utiliser à la place le Jev hébergé ou OpenRouter, mettez la clé dans une variable d'environnement ([branchements](../guide.md#what-you-can-connect)). `shikii doctor` vous dit ce qui tourne et ce qui manque.

La mise en place par projet (`shikii init`) est le travail de l'agent (vous pouvez aussi la lancer vous-même). Elle crée un seul dossier, `.shikii/` (à l'intérieur : `decisions/` = fichiers de décision, `wiki/` = connaissances, `records/` et `labels/` = enregistrements et corrections). Rien d'autre n'apparaît à la racine du projet. `decisions/`, `wiki/` et `labels/` sont un patrimoine qui survit à un changement d'IA, donc commitez-les ; `records/` grossit sans fin, donc le commiter est facultatif (`shikii gc` jette les anciens). Elle ajoute aussi une entrée shikii à la configuration de vos agents (`.mcp.json` de Claude Code, `.cursor/mcp.json` de Cursor, `opencode.json` d'OpenCode, `~/.codex/config.toml` de Codex) sans toucher aux entrées que vous avez écrites. La relancer ne change rien de ce qui existe.

La sortie des commandes et les messages des hooks sont en anglais (si la langue réglée dans Claude Code est le japonais, les messages des hooks sont affichés en japonais à côté de l'anglais). La page existe en japonais. Les points plus fins, comme n'utiliser qu'une IA hébergée, `AGENTS.md` face à `CLAUDE.md`, et Node pour modifier la page elle-même, sont dans [le guide](../guide.md).

## Pourquoi vous pouvez changer d'IA

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architecture de shikii : trois choses que font les gens, le patrimoine (questions, vérités, reçus), la boucle que la machine fait grandir, le wiki vérifiable, le modèle jetable" width="100%"></p>

- **Les connaissances s'accumulent avec la question et les corrections, pas à l'intérieur de l'IA.** Les corrections survivent à l'IA, donc une nouvelle peut les utiliser telles quelles et la ligne est retracée. Dans nos mesures, laisser la machine choisir la formulation et l'agencement et ajouter des exemples semblables a atteint la même justesse que l'affinage des poids du modèle (LoRA) ([chiffres](../bench.md))
- **Les décisions automatiques portent un reçu.** Pas « sûr à 95 % » mais « précision 0,96 sur 48 cas vérifiés par des personnes »
- **Le wiki ne se lit pas, il se mesure.** « Demander comme ceci marche » s'accumule sous forme d'affirmations chiffrées, et peut être retesté sur une nouvelle IA. Seule une personne décide qu'une affirmation est confirmée

## Pour aller plus loin

- [Le guide](../guide.md) : chaque commande, l'API HTTP, la FAQ · [la référence du fichier de décision](../../skills/shikii/references/spec.md)
- [Le hook](../../integrations/claude-code.md) (en japonais) : comment la réflexion de l'agent lui-même est mesurée, le tableau bande → comportement, les rouages
- [Architecture](../architecture.md) (en japonais) : la vue d'ensemble derrière le schéma ci-dessus
- [Décisions prêtes à l'emploi](../../decisions/README.md) · [le wiki des façons de demander](../../wiki/index.md) · [chiffres mesurés](../bench.md) · [notes d'exploitation](../operations.md)
- [Branchements](../../integrations/mcp.md) : MCP, l'adaptateur pour les modèles de chat ordinaires, Docker

**Pour les contributeurs** : cassez un test exprès et regardez-le échouer avant de le corriger. Chaque chiffre vient d'un script. Seule une personne confirme une affirmation du wiki. N'ajoutez pas de réglages ; des emplacements fixes et des règles fixes à la place.

## Licence

[Apache 2.0](../../LICENSE). Les licences des modèles sont à part (Kev-4B et la famille Bonsai sont en Apache 2.0, OpenJev est en CC BY-NC 4.0, le Jev hébergé est un service de TypeSafe).
