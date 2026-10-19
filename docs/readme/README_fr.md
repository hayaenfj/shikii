[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · **Français** · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii : transformer le « probablement » d'une IA en un « on peut agir » sur lequel bâtir une activité

### Un outil pour laisser une IA décider oui/non, lequel ou combien, et pour que cette décision reste fiable dans le travail de tous les jours. Vous vérifiez quelques réponses et, de temps en temps, vous confirmez « est-ce toujours ce que je voulais dire ? ». La machine fait le reste.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-192%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Le problème](#le-problème) · [Ce que fait shikii](#ce-que-fait-shikii) · [Ce que vous faites](#ce-que-vous-faites) · [Installation](#installation) · [Les chemins les plus courts](#les-chemins-les-plus-courts) · [Un tour complet](#un-tour-complet) · [Ce qui est nouveau](#ce-qui-est-nouveau) · [Pour aller plus loin](#pour-aller-plus-loin)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii : un modèle vous donne une probabilité, shikii vous dit si vous pouvez agir dessus" width="100%"></p>

## Le problème

Demandez à une IA « cette demande concerne-t-elle la même affaire que la précédente ? » et elle répond « probablement, 0,9 ». Pour du vrai travail, ce n'est pas assez.

- Ce 0,9 est-il vraiment juste 9 fois sur 10 ?
- Depuis le jour où vous avez mis un nouveau modèle, s'est-elle mise à mentir en silence ?
- Si vous changez un peu la formulation, comment bouge la justesse ?
- Les décisions qu'elle a prises seule, sans que personne ne regarde : sont-elles toujours bonnes ?

Si une personne doit vérifier chaque réponse, utiliser l'IA n'avait pas grand intérêt.

## Ce que fait shikii

1. **Rassemble vos corrections et trace la ligne au-dessus de laquelle l'IA peut agir seule.** À partir des réponses que vous avez données (« voici ce qui était vrai en réalité »), il mesure le lien entre la confiance de l'IA et la fréquence à laquelle elle a raison, et décide à partir d'où on peut lui faire confiance. Tant qu'il n'y a pas de ligne, rien n'agit. Dès qu'il y en a une, chaque décision automatique porte un reçu qui dit sur combien de réponses vérifiées la ligne repose et à quelle fréquence elles étaient justes
2. **Essaie d'autres façons de demander et garde la meilleure.** Sur les mêmes corrections, il note d'autres questions, d'autres descriptions des options et d'autres agencements du prompt, et n'en adopte une que si elle est vraiment meilleure. Vous confirmez seulement que le sens n'a pas changé
3. **Vous laisse changer de modèle sans repartir de zéro.** Les corrections vivent à part du modèle, donc un nouveau modèle les réutilise et la machine retrace la ligne. Aucun travail humain
4. **Garde ce qu'il a appris dans un wiki.** « Demander comme ceci marche » s'accumule sous forme d'affirmations avec les chiffres mesurés, que l'on peut retester sur un autre modèle. Ce que d'autres personnes ont mesuré sur leurs modèles s'ajoute à la même page

## Ce que vous faites

- **Vous écrivez ce que vous voulez faire décider.** Répondez à quelques questions et un fichier de décision apparaît (`shikii new`, de façon interactive ou avec `--type --question --option --input --pii` ; le format complet est dans [la référence du fichier de décision](../../skills/shikii/references/spec.md))
- **Vous vérifiez des réponses.** La page montre les décisions où l'IA hésitait, plus 1 sur 20 de celles qu'elle a exécutées, et vous appuyez sur le bouton de ce qui était vrai en réalité. Les touches 1 à 9 et Entrée (même choix que l'IA) vont vite. « Répondues » permet de corriger une réponse antérieure (la plus récente compte). « Détails » ajoute « redemander à l'IA », qui redécide le même texte avec la formulation et la ligne d'aujourd'hui et montre l'avant et l'après ; les enregistrements sont ajoutés, jamais effacés
- **De temps en temps, vous confirmez « est-ce toujours ce que je voulais dire ? ».** Quand la machine trouve une formulation qui obtient un meilleur score, vous regardez si le sens a changé et vous appuyez sur adopter ou rejeter

Tout le reste est le travail de la machine.

## Installation

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # nécessite Go 1.27 ou plus récent
cd <racine du projet> && shikii init
```

`shikii init` met en place un dossier, `.shikii/` (avec `decisions/` pour vos fichiers de décision, `wiki/` pour les connaissances, `records/` et `labels/` pour les enregistrements et vos corrections ; rien d'autre n'est ajouté à la racine du projet), et d'un seul coup les branchements aux agents : le serveur MCP de shikii pour Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) et Codex (`~/.codex/config.toml`), une section dans `AGENTS.md`, et les règles détaillées (le skill) dans `~/.claude/skills/shikii`. Vos propres entrées ne sont jamais touchées (une entrée shikii déjà présente est laissée telle quelle). Seul le dossier du skill est remplacé à chaque fois, donc ne le modifiez pas à la main. `wiki/` reçoit les connaissances partagées (comment poser chaque type de question, les tournures qui ont marché, les habitudes de chaque modèle) ; l'améliorateur de formulation (`tune --propose`) les lit, et les pages que vous avez modifiées ne sont jamais écrasées par un `init` ultérieur. Pas de Go ? Utilisez Docker ci-dessous.

Les entrées pointent vers un modèle local (`shikii up`, port 8080). Uniquement hébergé ? Retirez `--local` à la main de chacun des quatre fichiers. Des fichiers de configuration pour des outils que vous n'utilisez pas sont aussi créés ; supprimez-les ou ajoutez-les à `.gitignore` (un second `shikii init` ne touche jamais à ce qui existe). Commitez `.shikii/decisions/`, `.shikii/wiki/` et `.shikii/labels/` (vos fichiers de décision, les connaissances et vos corrections : le patrimoine qui survit à un changement de modèle). `.shikii/records/` (chaque décision) grossit sans fin, donc le commiter est facultatif (`shikii gc` jette les anciens). Si Codex a déjà `mcp_servers`, rien n'est ajouté et les lignes à coller sont affichées. Un projet qui n'a qu'un `CLAUDE.md` ne reçoit pas d'`AGENTS.md` (OpenCode cesserait de lire `CLAUDE.md`).

Modifier la page de réponse elle-même (`web/`, Svelte) demande Node 24 (`make web`). Utiliser shikii, non : la page construite est commitée et intégrée dans le binaire.

## Les chemins les plus courts

- **Docker** : pas de Go, pas de modèle ? `docker compose up` démarre un modèle local (Kev-4B) et shikii, avec la page de réponse sur http://127.0.0.1:8095 ([détails](../../integrations/docker.md))
- **Décisions prêtes à l'emploi** : six dans le dossier `decisions/` de ce dépôt (type de demande, rapport de bogue en double, même demande, …). Copiez-en une dans votre `.shikii/decisions/`, ou partez-en avec `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([liste](../../decisions/README.md))
- **Vous avez déjà un tableau de décisions passées** : `shikii import past.csv --spec .shikii/decisions/<id>.yaml` le transforme en corrections, pour ne pas répondre aux 50 premières à la main
- **Depuis votre propre programme** : lancez `shikii serve` et appelez `POST /v1/decide` (décider) et `POST /v1/label` (corriger). Le corps est en JSON et `-H 'Content-Type: application/json'` est obligatoire (tous les points d'entrée : [le guide long](../guide.md#or-run-one-process-for-programs-and-people))
- **L'exemple de quelqu'un d'autre** : [trier des courriels de demande](../examples/inquiry-route.md) (japonais). Une personne qui découvrait shikii a fait tout le tour avec le seul README ; le fichier de décision et les 22 entrées sont inclus
- **Mesurer la réflexion de l'IA elle-même** (Claude Code et Codex) : `shikii hook install` ajoute deux hooks qui font tourner shikii avant et après chaque tour. Avant : cette requête finira-t-elle par modifier des fichiers, et l'agent devrait-il d'abord s'accorder sur une approche ? Après : les modifications sont-elles restées dans ce que vous avez demandé, et a-t-il fini ce que vous avez demandé ? La première vérité vient gratuitement du fait que l'arbre de travail a changé ou non ; au reste, vous répondez sur une page. Rien ne change tant qu'il n'y a pas de ligne. Ensuite, une requête qui a besoin d'un plan devient une note à l'agent, et un tour qui semble inachevé ou hors de ce qui était demandé devient une ligne pour vous, avec le nombre de vérités sur lesquelles la ligne repose et la précision vérifiée par des humains. Aucun hook n'arrête jamais un outil ([détails](../../integrations/claude-code.md), japonais)
- **En CI** : `uses: hayaenfj/shikii@main` lance `shikii check` sur chaque pull request, de sorte qu'une formulation changée ou une ligne affaiblie fait échouer la build

## Un tour complet

```sh
shikii up                                                             # démarrer le modèle local (téléchargé au premier lancement)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (pas encore de ligne, donc : demander à une personne)
shikii serve --local http://127.0.0.1:8080                                   # les fichiers de décision et les connaissances sont lus depuis .shikii/
#   → répondez sur http://127.0.0.1:8095. Dès qu'une décision a le nombre qu'elle demande (eval.min_n, 20 pour celle-ci), la ligne se trace toute seule
#     Pour essayer avec moins de réponses, baissez eval.min_n dans le fichier de décision à ce que vous avez (une ligne écrite avec --force est signalée par check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (on peut agir, avec la preuve : la ligne repose sur 61 corrections, dont 48 venant de personnes. Parmi les réponses humaines que cette ligne place du côté act, 25, 96 % étaient justes)
```

Il n'y a que quatre bandes. `act` = allez-y, `ask` = vérifier avec une personne, `discard` = ne rien faire, `abstain` = entrée manquante. Vous ne lisez jamais une probabilité brute.

## Ce qui est nouveau

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="architecture de shikii : trois choses que font les gens, le patrimoine (questions, vérités, reçus), la boucle que la machine fait grandir, le wiki vérifiable, des modèles jetables" width="100%"></p>

- **Les connaissances s'accumulent dans les questions et les corrections, pas dans le modèle.** Le modèle est donc jetable, et le changer ne demande aucun travail humain. Mesuré : avec la machine qui choisit la formulation et l'agencement et ajoute des exemples semblables, un modèle de base ordinaire a égalé l'affinage (LoRA) en justesse ([chiffres](../bench.md))
- **Les décisions automatiques viennent avec des preuves.** Pas « sûr à 95 % » mais « précision 0,96 sur 48 réponses vérifiées par des personnes »
- **Le wiki ne se lit pas, il se mesure.** « Demander comme ceci marche » s'accumule sous forme d'affirmations chiffrées et se reteste sur les nouveaux modèles. Seule une personne décide qu'une affirmation est confirmée

## Pour aller plus loin

- [Le guide long](../guide.md) : chaque commande, le démarrage rapide complet, l'API HTTP, ce que vous pouvez brancher, les résultats mesurés et la FAQ · [la référence du fichier de décision](../../skills/shikii/references/spec.md)
- [Notes de conception](../decision-ops-framework.md) : ce dont le framework se charge et ce qu'il refuse de prendre en charge, et ce qui a été vérifié · [Architecture](../architecture.md) (japonais)
- [Décisions prêtes à l'emploi](../../decisions/README.md) · [le wiki des façons de demander](../../wiki/index.md) · [benchmark](../bench.md) · [comparaison des modèles](../model-comparison.md) · [journal d'exploitation](../operations.md)
- [Branchements](../../integrations/mcp.md) : MCP, l'adaptateur qui transforme un simple modèle de chat en modèle de décision, Docker, Claude Code
- [Guide en japonais](../guide.ja.md) avec un tour complet sur un cas réel

## Contribuer

Les issues et les pull requests sont bienvenus. Un test qui protège un comportement est d'abord montré en échec : cassez le comportement exprès, lancez le test, notez comment il a échoué, puis corrigez et notez qu'il passe. Chaque chiffre de la documentation vient d'un script. Seules des personnes changent le statut d'une affirmation du wiki (`confirmed` / `tentative` / `rejected`), avec l'exécution qui en est la preuve. Pas de nouvelles options : la disposition (`decisions/` `wiki/` `.shikii/` à la racine du projet) et des règles fixes font le travail.

## Licence

[Apache 2.0](../../LICENSE). Les licences des modèles sont à part : Kev-4B et la famille Bonsai sont en Apache 2.0, OpenJev est en CC BY-NC 4.0, le Jev hébergé est un service de TypeSafe. shikii stocke vos probabilités et vos étiquettes, rien des modèles.
