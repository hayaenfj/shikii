[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · **Français** · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: Exploiter des modèles de décision

### Un seul cycle de vie pour tout modèle de décision : define → decide → record → label → calibrate → check → drift.

**Un modèle vous donne une probabilité. shikii vous dit si vous pouvez agir dessus.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-170%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Notes de conception](../decision-ops-framework.md) · [Exploitation](../operations.md) · [Comparaison des modèles](../model-comparison.md) · [Guide détaillé (japonais)](../guide.ja.md)**

[Démarrage rapide](#démarrage-rapide) · [Comment fonctionne shikii](#comment-fonctionne-shikii) · [Ce que vous pouvez connecter](#ce-que-vous-pouvez-connecter) · [Résultats](#résultats) · [FAQ](#faq) · [Documentation](#documentation)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii n'exécute jamais de modèle et ne lit jamais un trousseau de clés. Il parle à un endpoint de modèle de décision que vous avez déjà, conserve les enregistrements, et refuse d'**agir** tant que les probabilités de *cette question sur ce modèle* n'ont pas été vérifiées avec des labels. Les blocs de calibration sont générés, jamais écrits à la main.

---

Les modèles de décision (TypeSafe Jev, Kev-4B et OpenJev sous llama.cpp, OpenRouter Decisions, …) répondent à des questions typées, `choice`, `score` et `noul` (oui/non), avec des probabilités et sans texte généré. Chaque éditeur livre le même avertissement : *les probabilités ne sont pas calibrées pour vos données, et les seuils ne se transposent pas d'un modèle ou d'une question à l'autre.*

shikii prend en charge ce que personne ne livre : le **cycle de vie** d'une décision. Un fichier spec contient la question, les options, le modèle d'état et les seuils. `decide` interroge un modèle et renvoie une bande (`act` / `ask` / `discard` / `abstain`), jamais un nombre brut. `label` enregistre ce qui était vrai. `calibrate` ajuste et valide par validation croisée, puis réécrit le résultat dans le spec sous une clé composée du hash de la question et du nom du modèle. `check` fait échouer la CI quand la formulation ou le modèle ont dérivé. `gc` garde le store petit.

Il est aussi livré avec `shikii-readout`, un adaptateur de 500 lignes qui transforme **n'importe quel GGUF de chat sur un llama.cpp standard** en modèle de décision en lisant les logits des lettres d'option. Dans nos mesures, un modèle ternaire ordinaire de 27B lu de cette façon a égalé en précision les modèles de décision entraînés, à 0,6 seconde par question sur un portable.

> Le nom est japonais : *shikii* (閾) signifie seuil, la ligne qui transforme une probabilité en action. La CLI s'appelle `shikii`.

## Démarrage rapide

Go 1.27 ou plus récent. Rien d'autre n'est nécessaire pour la première exécution.

### Lancer toute la boucle sans clé ni GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # faux modèle : decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # un faux modèle dont les probabilités ont besoin d'être calibrées ; observez calibrate choisir platt
```

### Le pointer vers un vrai modèle

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) modèle de décision local sur llama.cpp (télécharge Kev-4B, environ 2,5 Go, à la première exécution)
make server                                       # llama-server sur :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev hébergé. La clé vient uniquement de l'environnement
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) API OpenRouter Decisions
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

La réponse est une bande, un trace id et les probabilités. Tant que le spec ne porte pas de calibration pour ce modèle, la bande est au plus `ask`.

### Fermer la boucle

```sh
bin/shikii label <trace_id> change_request          # ou :  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 quand la formulation a changé ou que precision@act est tombée sous act
```

### Utiliser n'importe quel GGUF comme modèle de décision

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server standard avec flash attention et KV 8 bits, :8086
make readout                                               # shikii-readout devant, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Installer et mettre à jour

```sh
make install          # copie bin/shikii vers ~/.local/bin/shikii
git pull && make install
```

## Comment fonctionne shikii

Chaque décision tient dans un seul fichier YAML. La question, les options, le modèle d'état, les destinations où l'état peut être envoyé, les seuils et, générés par `calibrate --write`, les blocs de calibration.

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
  required: [message]            # entrée manquante → abstain ; le modèle n'est jamais appelé
policy:
  send_to: [hosted, local]       # une session tainted saute hosted automatiquement
  pii: possible                  # les enregistrements gardent un hash des entrées, jamais le texte
thresholds:
  act: 0.75                      # ≥ act → act automatiquement, seulement si calibré
  ask: 0.60                      # ≥ ask → demander à un humain ; en dessous → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**La clé de calibration est `(hash de la question + modèle + options, id du modèle tel que renvoyé par l'endpoint)`.** Modifiez un seul mot de la question, ou laissez `jev-latest` passer à une nouvelle version, et la calibration disparaît simplement : la bande retombe à `ask` au lieu d'agir silencieusement avec des constantes périmées. La disposition du prompt fait aussi partie de la clé : `shikii-readout` indique sa disposition dans le nom du modèle (`…+state-last`), car déplacer l'état à l'intérieur du prompt a changé de 5 points la précision d'un modèle de 27B avec les mêmes poids.

`calibrate` compare trois méthodes en validation croisée à 5 plis et écrit la gagnante. `identity` signifie que les probabilités brutes satisfont déjà precision@act et que le modèle est simplement marqué comme vérifié. `platt` (σ((Δ−bias)/T)) est pour les questions oui/non. `vector` (softmax((log p + b)/T)) est pour choice et score, et corrige les habitudes propres à chaque option, comme « se rabattre sur *other* ». Les labels partiels (`!option`, « ce n'était pas *ça* ») comptent pour la précision mais pas pour l'ajustement. Les labels obtenus depuis un fichier sont persistés, si bien que `check` et `drift` fonctionnent ensuite hors ligne.

Chaque branche touchant à la sécurité (routage du taint, pas de act sans calibration, invalidation par hash périmé, validation des labels, détection d'inversion, choix de méthode par CV, `gc` en simulation, …) possède une mutation enregistrée qui a fait échouer un test avant que le correctif soit accepté. Il y a 63 tests Go répartis sur 8 paquets ; la démo tourne en CI.

## Ce que vous pouvez connecter

shikii parle le format de requête System One (`POST /v1/systemone`), donc tout ce qui le sert fonctionne. L'adaptateur couvre tout le reste.

| Cible | Ce que shikii obtient | Prérequis et guide |
|---|---|---|
| **TypeSafe Jev** (hébergé) | `choice` / `score` / `noul` avec probabilités, id de modèle comme `jev-1.13.0` | `TYPESAFE_API_KEY` dans l'environnement. `--hosted` par défaut |
| **Modèles de décision llama.cpp** (Kev-4B, OpenJev, Clef, …) | Le même format, en local, id de modèle depuis `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 ou plus récent |
| **API OpenRouter Decisions** | Le même format via OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **N'importe quel GGUF de chat** via `shikii-readout` | Lecture des logits de lettres au format System One, sans calibration propre | `llama-server` standard 0.7.0 derrière. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** via [BonJev](https://github.com/ChezRD/bonjev) | Idem, avec le prompt propre à BonJev et sa calibration intégrée | Compilation Rust et fork de Prism. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Faux modèle** (`bin/fake-systemone`) | Probabilités déterministes pour les démos et les tests | Rien. `make fake` |
| **Hook Claude Code** (claude-jev) | Chaque prompt classé, chaque tour labellisé à partir de l'arbre de travail | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Le taint est une entrée de premier ordre : passez `--tainted` (ou laissez votre hook le faire) dans une session qui a touché des données de production et la requête ne quitte jamais la machine. Si aucun endpoint local n'est configuré, la décision s'abstient.

## Résultats

Mêmes specs, mêmes labels, quatre jeux d'évaluation (intention de prompt en anglais et en japonais, 62 requêtes ; rapprochement de dossiers identiques sur 32 paires en japonais). Les détails et le tableau complet sont dans [`docs/model-comparison.md`](../model-comparison.md).

| Modèle | Licence | top-1 (tout / ja) | Brier oui/non (brut) | Calibration retenue | Secondes par question |
|---|---|---|---|---|---|
| Kev-4B (modèle de décision entraîné) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (entraîné) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B via BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B via `shikii-readout`, llama.cpp standard** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**La formulation l'emporte sur la calibration.** Sur l'intention de prompt, 19 erreurs sur 21 tombaient sur *other* ; reformuler les options a fait passer le top-1 de 0.68 à 0.90 avant toute calibration. La calibration sert pour le cas « bon ordre, confiance insuffisante ». La matrice de confusion qu'affiche `calibrate` vous dit dans lequel des deux cas vous êtes.

**Les modèles déjà calibrés n'ont pas besoin de Platt.** Le oui/non de Kev-4B avait un Brier de 0.046 en brut ; Platt l'a dégradé en validation croisée, donc `identity` a été écrit. La première fois que `calibrate` a choisi `platt`, c'était sur un LLM ordinaire lu via les logits. Le besoin de calibration est une propriété du *modèle plus sa lecture*, pas du modèle seul.

**Un GGUF ordinaire se place à côté d'un modèle de décision entraîné.** Un modèle de chat ternaire de 27B, llama.cpp 0.7.0 de la branche principale, un adaptateur, pas de fork, pas d'entraînement, pas de Rust : le même top-1 que le meilleur modèle entraîné sous licence Apache, 1.000 en japonais, 0.58 s par question avec flash attention et un préfixe préchauffé. Déplacer l'état d'avant les options à juste avant la réponse l'a fait passer de 0.919 à 0.968 avec les mêmes poids, et c'est pourquoi la disposition fait partie de la clé de calibration.

## FAQ

**shikii exécute-t-il des modèles ?** Non. Il envoie des requêtes System One à un endpoint que vous hébergez ou louez, et conserve tout ce qui se passe après la réponse. `shikii-readout` est la seule exception dans l'esprit : il convertit les logits d'un modèle de chat dans le même format, mais l'inférence reste celle de llama.cpp.

**Pourquoi ma décision n'atteint-elle jamais `act` ?** Parce que le spec n'a pas de bloc de calibration pour `(le hash de cette question, cet id de modèle)`. Collectez des labels (`label`, ou `--labeled file`), lancez `calibrate --write`, et `status` affichera `calibrated: identity|platt|vector`. Une décision sans précision vérifiée au seuil act n'a pas le droit d'agir. C'est le but.

**J'ai changé un mot de la question et la calibration a disparu.** Normal. Le hash a changé, les constantes avaient été ajustées pour une autre question. `status` montre les anciens labels comme `stale hash` ; relancez `calibrate --write` une fois que vous avez des labels pour la nouvelle formulation.

**Quel modèle utiliser ?** Cela dépend de la licence, de la vitesse et de la langue, et shikii ne décide pas à votre place. Le tableau ci-dessus et [`docs/model-comparison.md`](../model-comparison.md) sont l'argumentaire : avec le même spec et les mêmes labels, vous pouvez choisir sur des chiffres, y compris pour savoir si un modèle a besoin de calibration. `scripts/eval-model.sh <url> <name>` reproduit le tableau pour un nouvel endpoint.

**Cela fonctionne-t-il en japonais ?** Oui, et c'est là que les différences apparaissent. Kev-4B obtient 0.913 sur les prompts japonais ; les modèles de 27B obtiennent 1.000. Le rapprochement de dossiers identiques sur des synthèses d'enquête en japonais sépare complètement les positifs des négatifs sur chaque modèle de 27B.

**Où vont mes données ?** Seul l'état rendu part vers l'endpoint nommé dans `send_to`. Avec `pii: possible` (la valeur par défaut), le store garde un hash des entrées, jamais le texte. `--tainted` garde tout en local. Les clés sont lues depuis `DECISIONS_API_KEY` ou `TYPESAFE_API_KEY` et nulle part ailleurs.

**Le store ne cesse de grossir.** `shikii gc` supprime les enregistrements de plus de 30 jours qui n'ont jamais reçu de label. Les enregistrements labellisés et les labels sont conservés pour toujours, car ils constituent la matière de calibration. C'est une simulation, sauf si vous passez `--apply`.

**Y a-t-il un biais vers la première option ?** Mesurez avant de corriger : `shikii bias <spec> --permutations 100` mélange les options et rapporte les inversions et le décalage de position. Kev-4B : 0 inversion sur 100, décalage ≤ 0.024. Les LLM ordinaires lus via les logits peuvent différer ; la commande existe pour eux.

## Documentation

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md) : notes de conception, ce que le framework prend en charge et ce qu'il refuse de prendre en charge, le plan de vérification et ce qui a été confirmé.
- [`docs/operations.md`](../operations.md) : comment arrêter ou annuler chaque pièce mobile, comment lire une panne, ce qui manque encore.
- [`docs/model-comparison.md`](../model-comparison.md) : le tableau de comparaison et comment le reproduire.
- [`docs/guide.ja.md`](../guide.ja.md) : le guide détaillé en japonais, avec toutes les commandes et une boucle complète en situation réelle.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md) : les trois connexions que nous faisons tourner.
- [`examples/`](../../examples/) et [`testdata/`](../../testdata/README.md) : un spec sans calibration pour démarrer, et les jeux labellisés synthétiques utilisés dans les tests.

Les commandes :

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: hash périmé, blocs écrits à la main, precision@act sous act → exit 1
shikii drift     <spec> --model M [--window N]            # labels récents face au Brier et à la précision enregistrés à la calibration
shikii bias      <spec> [--rows K] [--permutations N]     # la réponse dépend-elle de l'ordre des options ?
shikii status    [spec...] [--store DIR]                  # inventaire : enregistrements, labels, sources, état par (modèle, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # supprime les anciens enregistrements sans label ; simulation par défaut
shikii hash      <spec>
```

## Contribuer

Les issues et les pull requests sont les bienvenues. Une seule règle : un test qui protège un comportement doit d'abord être montré en échec. Cassez le comportement exprès, lancez le test, consignez comment il a échoué, puis corrigez et consignez qu'il passe. La documentation liste ces mutations pour chaque branche de sécurité ; gardez cette liste honnête.

## Licence

[Apache 2.0](../../LICENSE). Les licences des modèles sont distinctes : Kev-4B et la famille Bonsai sont sous Apache 2.0, OpenJev est sous CC BY-NC 4.0, Jev hébergé est le service de TypeSafe. shikii stocke vos probabilités et vos labels, rien des modèles.
