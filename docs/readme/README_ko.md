[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · **한국어** · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: 결정 모델 운영하기

### 모든 결정 모델에 같은 한 바퀴를: define → decide → record → label → calibrate → check → drift

**모델은 확률을 줍니다. shikii는 그 확률로 행동해도 되는지 알려줍니다.**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/github/actions/workflow/status/hayaenfj/shikii/test.yml?style=flat-square&label=tests)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[설계 노트](../decision-ops-framework.md) · [운영](../operations.md) · [모델 비교](../model-comparison.md) · [자세한 사용법(일본어)](../guide.ja.md)**

[빠른 시작](#빠른-시작) · [shikii의 동작 방식](#shikii의-동작-방식) · [연결할 수 있는 대상](#연결할-수-있는-대상) · [결과](#결과) · [FAQ](#faq) · [문서](#문서)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii는 모델을 직접 실행하지 않고 키체인도 읽지 않습니다. 이미 갖고 있는 결정 모델 엔드포인트와 통신하고, 기록을 보관하며, **이 모델에 이 질문을 했을 때의 확률**이 레이블로 검증되기 전까지는 **act**를 내지 않습니다. 보정 블록은 생성되는 것이며, 손으로 쓰지 않습니다.

---

결정 모델(TypeSafe Jev, llama.cpp의 Kev-4B와 OpenJev, OpenRouter Decisions 등)은 타입이 지정된 질문 `choice`, `score`, `noul`(예/아니오)에 문장을 생성하지 않고 확률로 답합니다. 어느 벤더나 같은 단서를 붙입니다. *확률은 여러분의 데이터에 맞게 보정되어 있지 않으며, 임계값은 모델이나 질문이 바뀌면 그대로 쓸 수 없다.*

shikii가 맡는 것은 아무도 제공하지 않는 부분, 곧 결정의 **수명 주기**입니다. spec 파일에 질문, 선택지, 상태(`state`) 템플릿, 임계값을 적습니다. `decide`는 모델에 물어 구간(`act` / `ask` / `discard` / `abstain`)을 반환하며, 날것의 숫자는 돌려주지 않습니다. `label`은 무엇이 사실이었는지 기록합니다. `calibrate`는 피팅과 교차 검증을 거친 뒤, 질문의 해시와 모델 이름으로 만든 키로 결과를 spec에 다시 씁니다. `check`는 문구나 모델이 어긋나면 CI를 실패시킵니다. `gc`는 store를 작게 유지합니다.

`shikii-readout`도 함께 제공합니다. 500줄짜리 어댑터로, 선택지 글자의 logit을 읽어 **stock llama.cpp 위의 어떤 채팅 GGUF든** 결정 모델로 바꿔 줍니다. 측정해 보니, 이렇게 읽은 평범한 27B ternary 모델이 학습된 결정 모델과 같은 정확도에 도달했고, 노트북에서 질문당 0.6초였습니다.

> 이름은 일본어입니다. *shikii*(閾)는 일본어로 임계값, 곧 확률을 행동으로 바꾸는 경계선을 뜻합니다. CLI 이름은 `shikii`입니다.

## 빠른 시작

Go 1.24 이상이 필요합니다. 첫 실행에는 그 밖에 아무것도 필요 없습니다.

### 키도 GPU도 없이 전체 루프 돌려 보기

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # 가짜 모델로 decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # 확률이 과신하는 가짜 모델. calibrate가 platt를 고르는 모습을 확인
```

### 실제 모델에 연결하기

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) llama.cpp 위의 로컬 결정 모델 (첫 실행 때 Kev-4B 약 2.5 GB를 내려받음)
make server                                       # llama-server를 :8080에서 실행
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) 호스팅된 TypeSafe Jev. 키는 환경 변수로만 읽음
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

응답은 구간, trace id, 확률로 이루어집니다. spec에 이 모델의 보정이 들어갈 때까지 구간은 높아야 `ask`입니다.

### 루프 닫기

```sh
bin/shikii label <trace_id> change_request          # 또는:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <응답에 나온 모델 이름> --write
bin/shikii check examples/prompt-intent.yaml        # 문구가 바뀌었거나 act에서의 precision이 act 미만이면 exit 1
```

### 아무 GGUF나 결정 모델로 쓰기

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # stock llama-server (flash attention, 8비트 KV), :8086
make readout                                               # 그 앞단에 shikii-readout, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### 설치와 업데이트

```sh
make install          # bin/decisions를 ~/.local/bin/decisions로 복사
git pull && make install
```

## shikii의 동작 방식

결정 하나가 YAML 파일 하나입니다. 질문, 선택지, 상태 템플릿, 상태를 보내도 되는 곳, 임계값, 그리고 `calibrate --write`가 생성하는 보정 블록이 들어갑니다.

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
  required: [message]            # 입력이 없으면 abstain. 모델은 호출하지 않음
policy:
  send_to: [hosted, local]       # tainted 세션은 hosted를 자동으로 건너뜀
  pii: possible                  # 기록에는 입력의 해시만 남기고 원문은 남기지 않음
thresholds:
  act: 0.75                      # ≥ act → 자동으로 행동. 보정된 경우에만
  ask: 0.60                      # ≥ ask → 사람에게 물음. 미만이면 discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**보정 키는 `(질문 + 템플릿 + 선택지의 해시, 엔드포인트가 반환한 모델 id)`입니다.** 질문 단어 하나를 고치거나 `jev-latest`가 새 버전으로 넘어가면 보정은 그냥 사라지고, 오래된 상수로 조용히 행동하는 대신 구간이 `ask`로 내려갑니다. 프롬프트 배치도 키의 일부입니다. `shikii-readout`은 모델 이름에 배치를 붙여 반환합니다(`…+state-last`). 같은 가중치인데도 상태를 프롬프트 안에서 옮기기만 했더니 27B 모델의 정확도가 5포인트 달라졌기 때문입니다.

`calibrate`는 세 가지 방법을 5-fold 교차 검증으로 비교해 가장 나은 것을 씁니다. `identity`는 원래 확률만으로 이미 act에서의 precision을 충족하는 경우로, 모델을 검증 완료로 표시할 뿐입니다. `platt`(σ((Δ−bias)/T))는 예/아니오 질문용입니다. `vector`(softmax((log p + b)/T))는 choice와 score용이며, "헷갈리면 *other*로 빠지는" 것 같은 선택지별 버릇을 바로잡습니다. 부분 레이블(`!option`, 즉 "그것은 아니었다")은 precision에는 포함되지만 피팅에는 쓰이지 않습니다. 파일에서 가져온 레이블은 영속 저장되므로, 이후 `check`와 `drift`는 오프라인으로 실행됩니다.

안전과 관련된 모든 분기(taint 라우팅, 보정 없이는 act 불가, stale hash 무효화, 레이블 검증, 역전 감지, CV 기반 방법 선택, dry-run `gc` 등)에는 수정을 받아들이기 전에 테스트를 실패시킨 변이(mutation)가 기록되어 있습니다. Go 테스트는 8개 패키지에 63개이며, 데모는 CI에서 실행됩니다.

## 연결할 수 있는 대상

shikii는 System One 요청 형식(`POST /v1/systemone`)을 사용하므로, 그것을 제공하는 것이면 무엇이든 연결됩니다. 나머지는 어댑터가 맡습니다.

| 대상 | shikii가 받는 것 | 요구 사항과 안내 |
|---|---|---|
| **TypeSafe Jev**(호스팅) | `choice` / `score` / `noul` 확률, `jev-1.13.0` 같은 모델 id | 환경 변수 `TYPESAFE_API_KEY`. `--hosted`의 기본값 |
| **llama.cpp 결정 모델**(Kev-4B, OpenJev, Clef 등) | 같은 형식을 로컬에서, 모델 id는 `/v1/models`에서 | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M`(`make server`). llama.cpp 0.6.0 이상 |
| **OpenRouter Decisions API** | OpenRouter를 거친 같은 형식 | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **임의의 채팅 GGUF**를 `shikii-readout`으로 | System One 형식의 글자 logit 읽기. 자체 보정은 없음 | 그 뒤에 stock `llama-server` 0.7.0. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai**를 [BonJev](https://github.com/ChezRD/bonjev)로 | 같음. BonJev 자체 프롬프트와 내장 보정 포함 | Rust 빌드와 Prism fork. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **가짜 모델**(`bin/fake-systemone`) | 데모와 테스트용의 결정적 확률 | 필요 없음. `make fake` |
| **Claude Code hook**(claude-jev) | 프롬프트마다 분류, 턴이 끝날 때 작업 트리를 보고 레이블 | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

taint는 일급 입력입니다. 운영 데이터를 건드린 세션에서 `--tainted`를 넘기면(또는 hook이 넘기게 하면) 요청은 그 기기를 벗어나지 않습니다. 로컬 엔드포인트가 설정되어 있지 않으면 결정은 abstain합니다.

## 결과

같은 spec, 같은 레이블, 평가 세트 4개(영어와 일본어 프롬프트 의도 62건, 일본어 32쌍의 동일 사건 연결). 자세한 내용과 전체 표는 [`docs/model-comparison.md`](../model-comparison.md)에 있습니다.

| 모델 | 라이선스 | top-1(전체 / 일본어) | 예/아니오 Brier(원값) | 선택된 보정 | 질문당 초 |
|---|---|---|---|---|---|
| Kev-4B(학습된 결정 모델) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B(학습됨) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B를 BonJev로 | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B를 `shikii-readout` + stock llama.cpp로** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**문구가 보정을 이깁니다.** 프롬프트 의도에서는 오류 21건 중 19건이 *other*로 빠졌습니다. 보정을 하기 전에 선택지 문구만 고쳤는데 top-1이 0.68에서 0.90으로 올랐습니다. 보정은 "순서는 맞는데 확신이 부족한" 경우를 위한 것입니다. 어느 쪽인지는 `calibrate`가 출력하는 혼동 행렬로 알 수 있습니다.

**이미 보정된 모델에는 Platt가 필요 없습니다.** Kev-4B의 예/아니오는 원값에서 Brier 0.046이었고, Platt는 교차 검증에서 오히려 나빠졌기 때문에 `identity`가 기록되었습니다. `calibrate`가 `platt`를 처음 고른 것은 평범한 LLM을 logit으로 읽었을 때였습니다. 보정이 필요한지는 모델 단독이 아니라 *모델 + 읽기 방식*의 성질입니다.

**평범한 GGUF가 학습된 결정 모델과 나란히 섭니다.** 27B ternary 채팅 모델, 메인라인 llama.cpp 0.7.0, 어댑터 하나. fork도 학습도 Rust도 없이, Apache 라이선스의 학습된 모델 중 최고와 같은 top-1, 일본어 1.000, flash attention과 워밍업된 접두사로 질문당 0.58초입니다. 상태를 선택지 앞에서 답 바로 앞으로 옮기기만 했더니 같은 가중치로 0.919에서 0.968이 되었고, 그래서 배치가 보정 키에 들어 있습니다.

## FAQ

**shikii는 모델을 실행하나요?** 아니요. 직접 운영하거나 빌린 엔드포인트에 System One 요청을 보내고, 답이 나온 뒤에 일어나는 모든 것을 보관합니다. `shikii-readout`만은 취지상 예외입니다. 채팅 모델의 logit을 같은 형식으로 변환하지만, 추론은 어디까지나 llama.cpp가 합니다.

**결정이 왜 `act`에 도달하지 않나요?** spec에 `(이 질문의 해시, 이 모델 id)`에 대한 보정 블록이 없기 때문입니다. 레이블을 모으고(`label` 또는 `--labeled file`) `calibrate --write`를 실행하면 `status`에 `calibrated: identity|platt|vector`가 표시됩니다. act 임계값에서의 precision이 검증되지 않은 결정은 act할 수 없습니다. 그게 핵심입니다.

**질문의 단어 하나를 바꿨더니 보정이 사라졌습니다.** 맞습니다. 해시가 바뀌었고, 상수는 다른 질문에 맞춰 피팅된 것이었습니다. `status`는 예전 레이블을 `stale hash`로 보여 줍니다. 새 문구에 대한 레이블이 모이면 `calibrate --write`를 다시 실행하세요.

**어떤 모델을 써야 하나요?** 라이선스, 속도, 언어에 따라 다르며 shikii는 대신 정해 주지 않습니다. 위의 표와 [`docs/model-comparison.md`](../model-comparison.md)가 그 근거입니다. 같은 spec과 같은 레이블로, 보정이 필요한지까지 포함해 숫자로 고를 수 있습니다. 새 엔드포인트에 대해서는 `scripts/eval-model.sh <url> <name>`으로 표를 재현할 수 있습니다.

**일본어에서도 동작하나요?** 네, 차이가 드러나는 곳이 바로 여기입니다. Kev-4B는 일본어 프롬프트에서 0.913, 27B 모델은 1.000입니다. 일본어 조사 요약의 동일 사건 연결은 모든 27B 모델에서 양성과 음성이 완벽하게 갈립니다.

**내 데이터는 어디로 가나요?** 렌더링된 상태만 `send_to`에 지정한 엔드포인트로 갑니다. `pii: possible`(기본값)이면 store에는 입력의 해시만 남고 원문은 남지 않습니다. `--tainted`를 쓰면 모든 것이 로컬에 머뭅니다. 키는 `DECISIONS_API_KEY` 또는 `TYPESAFE_API_KEY`에서만 읽습니다.

**store가 계속 커집니다.** `shikii gc`가 30일보다 오래되었고 레이블이 붙지 않은 기록을 지웁니다. 레이블이 붙은 기록과 레이블은 보정 재료이므로 영구히 남습니다. `--apply`를 주지 않으면 dry run입니다.

**첫 번째 선택지로 쏠리지 않나요?** 고치기 전에 측정하세요. `shikii bias <spec> --permutations 100`은 선택지를 섞어서 뒤집힘 횟수와 위치 이동량을 보고합니다. Kev-4B는 100회 중 뒤집힘 0, 이동 ≤ 0.024입니다. logit으로 읽는 평범한 LLM은 다를 수 있으며, 이 명령은 그런 모델을 위해 있습니다.

## 문서

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): 설계 노트. 프레임워크가 맡는 것과 맡지 않겠다고 한 것, 검증 계획, 확인된 내용.
- [`docs/operations.md`](../operations.md): 움직이는 모든 부품을 멈추거나 되돌리는 방법, 장애를 읽는 방법, 아직 없는 것.
- [`docs/model-comparison.md`](../model-comparison.md): 비교 표와 재현 방법.
- [`docs/guide.ja.md`](../guide.ja.md): 모든 명령과 실제 환경에서 한 바퀴 돈 전체 사례를 담은 일본어 장문 가이드.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): 실제로 운영 중인 세 가지 연결.
- [`examples/`](../../examples/)와 [`testdata/`](../../testdata/README.md): 보정 없이 시작할 수 있는 spec과, 테스트에 쓰는 합성 레이블 데이터.

명령어:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, 손으로 쓴 블록, act에서 precision 부족 → exit 1
shikii drift     <spec> --model M [--window N]            # 최근 레이블을 보정 시점의 Brier, precision과 비교
shikii bias      <spec> [--rows K] [--permutations N]     # 답이 선택지 순서에 좌우되는가?
shikii status    [spec...] [--store DIR]                  # 현황: 기록, 레이블, 출처, (model, hash)별 상태
shikii gc        [spec...] [--older-than 30d] [--apply]   # 오래된 무레이블 기록 삭제. 기본은 dry run
shikii hash      <spec>
```

## 기여하기

이슈와 pull request를 환영합니다. 규칙은 하나입니다. 동작을 지키는 테스트는 먼저 실패하는 모습을 보여야 합니다. 일부러 동작을 망가뜨리고, 테스트를 실행해 어떻게 실패했는지 기록한 다음, 고쳐서 통과하는 것을 기록하세요. 문서에는 안전과 관련된 모든 분기에 대해 그 변이가 나열되어 있으니, 이 목록을 정직하게 유지해 주세요.

## 라이선스

[Apache 2.0](../../LICENSE). 모델 라이선스는 별개입니다. Kev-4B와 Bonsai 계열은 Apache 2.0, OpenJev는 CC BY-NC 4.0, 호스팅 Jev는 TypeSafe의 서비스입니다. shikii가 저장하는 것은 여러분의 확률과 레이블이며, 모델에 속한 것은 아무것도 저장하지 않습니다.
