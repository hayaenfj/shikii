[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · **한국어** · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: AI의 "아마도"를, 업무에서 믿고 쓸 수 있는 "맡겨도 된다"로 바꾼다

### AI에게 "예/아니오", "어느 것", "어느 정도"를 결정하게 하고, 당신의 정답 맞추기로 키우는 도구. 당신이 하는 일은 화면에서 "실제로는 어땠는지"를 답하는 것뿐입니다.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-283%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[문제](#문제) · [사용법 1: AI 자신의 생각을 측정한다](#사용법-1-ai-자신의-생각을-측정한다) · [사용법 2: 업무의 결정을 맡긴다](#사용법-2-업무의-결정을-맡긴다) · [화면에서 하는 일](#화면에서-하는-일) · [설치](#설치) · [왜 AI를 바꿔도 되는가](#왜-ai를-바꿔도-되는가) · [더 읽기](#더-읽기)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## 문제

AI에게 "이 문의는 지난번과 같은 건인가?"라고 물으면 "아마도, 0.9"라고 답합니다. 실제 업무에는 이것만으로 부족합니다. 그 0.9는 정말 10번 중 9번 맞는가? 새 AI로 바꾼 날부터 조용히 틀린 말이 되어 버린 건 아닌가? 아무도 보지 않는 곳에서 스스로 내린 결정은 지금도 맞는가? 사람이 매번 확인해야 한다면, AI를 쓴 의미가 거의 없습니다.

shikii는 당신의 정답 맞추기에서 **선**을 그립니다. 이 위라면 AI가 스스로 움직여도 됩니다. 선이 생기기 전에는 아무것도 믿지 않습니다. 선이 생기면, 모든 자동 결정에 근거가 붙습니다. 그 선이 몇 건의 정답 위에 서 있고, 얼마나 자주 맞았는지입니다. 선은 AI 안이 아니라 질문과 정답 쪽에 남으므로, AI를 바꿔도 사람 손은 들지 않습니다.

쓰는 방법은 두 가지입니다. 코드를 쓰는 AI의 생각을 측정하는 것(가장 가벼움)과, 업무의 결정을 맡기는 것입니다.

## 사용법 1: AI 자신의 생각을 측정한다

Claude Code나 Codex를 쓰는 사람을 위한 것입니다. 코딩 에이전트는 요청을 받을 때와 작업을 끝낼 때 판단을 내립니다. shikii는 그것을 조용히 측정합니다.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

이것으로 끝입니다. 실행 파일을 설치하고, Claude Code와 Codex에 hook을 두 개 더하고, 에이전트의 규칙(skill)을 두고, 작은 로컬 AI(Kev-4B)를 뒤에서 띄웁니다(llama.cpp가 없으면 설치하는 한 줄을 보여 줍니다). 새 터미널을 열고 평소처럼 에이전트와 일하면 됩니다(Codex에서는 `/hooks`를 한 번 실행해 shikii의 hook을 trust합니다). 측정하는 것은 네 가지입니다.

| 언제 | 질문 | 정답이 어디서 오는가 |
|---|---|---|
| 요청이 들어올 때 | 이 요청은 결국 파일을 바꾸게 되는가? | 자동: 다음 요청까지 작업 트리가 바뀌었는가 |
| 요청이 들어올 때 | 손대기 전에 진행 방법을 당신과 합의해야 하는가? | 당신이 화면에서 |
| 턴이 끝날 때 | 바뀐 것이 요청한 범위 안이었는가? | 당신이 화면에서 |
| 턴이 끝날 때 | 요청한 일을 끝냈는가? | 당신이 화면에서 |

턴이 끝나면 `shikii: 이 세션의 결정 3건이 당신의 답을 기다립니다: http://127.0.0.1:8096/`라는 한 줄이 나오고, 시간이 날 때 열어 보면 됩니다(화면도 hook이 뒤에서 띄웁니다). 답 하나에 몇 초면 됩니다. 답이 50건 쌓이면 선이 그려지고 에이전트의 행동이 바뀝니다. 계획이 필요한 요청은 에이전트에게 보내는 메모가 되고, 끝나지 않았거나 범위를 벗어난 듯한 턴은 당신에게 보내는 한 줄이 되며, 각각에 "정답 61건 위의 선, 사람이 확인한 정밀도 0.96" 같은 근거가 붙습니다. 그때까지 hook은 기록만 합니다. hook이 도구를 막는 일은 결코 없습니다.

모든 것은 한 곳 `~/.shikii/agent`에 있고, 프로젝트를 통틀어 질문마다 선은 하나입니다(질문은 에이전트의 생각에 관한 것이라, 어느 저장소에서도 정답의 기준이 같기 때문). 각 기록에는 어느 저장소에서 온 것인지 표시가 붙고, 화면에서 그것으로 걸러 볼 수 있습니다. 자세히: [hook](../../integrations/claude-code.md)(일본어).

## 사용법 2: 업무의 결정을 맡긴다

"이 문의는 같은 건인가?", "이 메시지는 변경 요청인가?"처럼 업무에서 반복하는 결정을 AI에게 맡기고, 당신의 정답 맞추기로 키우는 방법입니다. 여기서도 입력할 명령은 없습니다. 에이전트에게 말하면 됩니다.

> 문의가 이전 것과 같은 건인지 shikii로 판정하고, 내 정답 맞추기로 키워 가고 싶어

에이전트는 규칙(skill)에 따라 진행합니다. 프로젝트에 `.shikii/`를 만들고(`shikii init`), 질문·선택지·입력을 정해 결정 파일을 쓰고(`shikii new`. 질문과 선택지는 한 번 당신에게 보여 줍니다. 그것이 당신의 의도이기 때문입니다), 판정을 호출하고(`shikii decide`), 정답 화면(http://127.0.0.1:8095)을 띄워 당신에게 알립니다. 선이 생기기 전에는 어떤 결정도 `act`(그대로 진행)가 되지 않습니다. 당신이 화면에서 답해 가면, 그 결정에 정해 둔 수(이 예에서는 20건)에 이르렀을 때 선이 자동으로 그려지고, 그다음부터는 이렇게 됩니다.

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (그대로 진행해도 된다. 선은 61건의 정답 위에 서 있고, 그중 48건은 사람이 답한 것. 사람이 답한 것 가운데 이 선이 act로 보내는 25건 중 96%가 맞았다)
```

`band`는 네 가지뿐입니다. `act` = 그대로 진행, `ask` = 사람에게 확인, `discard` = 아무것도 하지 않음, `abstain` = 입력 부족. 날것의 확률을 보고 움직이는 일은 결코 없습니다.

- **직접 입력하려면**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. 전부 [안내서](../guide.md)에 있습니다
- **에이전트에서**: `shikii init`이 MCP 설정을 쓰므로, Claude Code, Cursor, OpenCode, Codex가 도구 `shikii_decide`와 `shikii_label`을 바로 호출할 수 있습니다([MCP](../../integrations/mcp.md))
- **내 프로그램에서**: `shikii serve`를 띄우고 `POST /v1/decide`(판정)와 `POST /v1/label`(정답)을 호출합니다. 본문은 JSON이고 `-H 'Content-Type: application/json'`이 필요합니다([HTTP API](../guide.md#or-run-one-process-for-programs-and-people))
- **미리 만들어진 결정**: 이 저장소의 `decisions/`에 여섯 개(요청 유형, 중복 버그 신고, 같은 문의, …). `shikii new --from <repo>/decisions/prompt-intent-v2.yaml`로 하나를 복사해 문장을 고칩니다([목록](../../decisions/README.md))
- **과거 결정의 표가 이미 있다**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml`이 그것을 정답으로 바꿔 주므로, 처음 50건을 손으로 답하지 않아도 됩니다
- **Docker**: Go도 모델도 아직 없다면 `docker compose up`. 로컬 AI와 shikii가 뜨고, 화면은 http://127.0.0.1:8095 에 나옵니다([자세히](../../integrations/docker.md))
- **다른 사람의 예**: [문의 메일 분류](../examples/inquiry-route.md)(일본어). 처음 쓰는 사람이 README만으로 한 바퀴를 다 돌았고, 결정 파일과 22건의 입력이 들어 있습니다

## 화면에서 하는 일

정답 화면(`shikii serve`)에서 사람이 하는 일은 세 가지입니다. 그 외는 모두 기계의 일입니다.

1. **답한다.** 화면에 AI가 확신하지 못한 결정과, 스스로 실행한 결정 20건 중 1건이 나오고, 실제로는 무엇이었는지 버튼으로 답합니다. 1~9 키와 Enter(AI의 선택과 같음)로 빠르게 넘길 수 있습니다. "답변 완료"에서 이전 답을 고칠 수 있습니다(가장 새 답이 유효)
2. **묻는 방식의 안을 쓴다.** "이렇게 물으면 더 잘 맞겠다"고 생각되면, "묻는 방식" 칸에 자신의 말로 입력합니다. 기계가 같은 정답으로 지금의 묻는 방식과 비교해 점수를 매기고(에이전트 자신의 결정은 새 답이 50건 늘어날 때마다 자동으로), 정말 나아졌을 때만 "채택할 수 있습니다"라고 말합니다. AI에게 안을 내게 할 수도 있습니다. 파일은 열지 않습니다
3. **채택 또는 거부를 누른다.** 뜻이 바뀌었는지 보고 누릅니다. 숫자는 기계가 확인하고, 뜻은 당신이 확인합니다

"자세히"를 열면 모델별 선의 상태, "AI에게 다시 묻기"(같은 본문을 오늘의 묻는 방식과 선으로 다시 판정해 전후를 나란히 봄), 지식 패널("이렇게 물으면 잘 맞는다"는 형태의 주장과 그 증거)이 더 나옵니다. 기록은 덧붙기만 하고 지워지지 않습니다. 새 결정이 이전 것의 자리를 대신합니다.

## 설치

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 이상. 없으면 Docker(위)
```

install.sh가 하는 일: 실행 파일(`shikii`와 `shikii-readout`)을 설치하고, Claude Code와 Codex에 hook을 두 개 더하고, 에이전트의 규칙(skill)을 `~/.claude/skills/shikii`에 두고, llama.cpp가 있으면 로컬 AI(Kev-4B, :8080)를 뒤에서 띄웁니다. 대신 호스팅 Jev나 OpenRouter를 쓰려면 키를 환경 변수에 넣으세요([연결할 수 있는 것](../guide.md#what-you-can-connect)). 무엇이 돌고 있고 무엇이 빠졌는지는 `shikii doctor`가 알려 줍니다.

프로젝트별 준비(`shikii init`)는 에이전트의 일입니다(직접 실행해도 됩니다). 폴더 하나 `.shikii/`를 만듭니다(안에 `decisions/` = 결정 파일, `wiki/` = 지식, `records/`와 `labels/` = 기록과 정답). 프로젝트 루트에 다른 것은 늘어나지 않습니다. `decisions/`, `wiki/`, `labels/`는 AI를 바꿔도 남는 자산이니 commit하세요. `records/`는 계속 늘어나므로 commit은 선택입니다(`shikii gc`가 오래된 것을 지웁니다). 에이전트의 설정(Claude Code의 `.mcp.json`, Cursor의 `.cursor/mcp.json`, OpenCode의 `opencode.json`, Codex의 `~/.codex/config.toml`)에도 shikii 항목을 더하지만, 당신이 쓴 항목은 건드리지 않습니다. 다시 실행해도 이미 있는 것은 바꾸지 않습니다.

명령의 출력과 hook의 메시지는 영어입니다(Claude Code의 언어 설정이 일본어이면 hook 메시지는 영어 옆에 일본어도 나옵니다). 화면에는 일본어가 있습니다. 호스팅 AI만 쓰는 경우, `AGENTS.md`와 `CLAUDE.md`의 차이, 화면 자체를 고칠 때의 Node 같은 세부 사항은 [안내서](../guide.md)에 있습니다.

## 왜 AI를 바꿔도 되는가

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 구조: 사람이 하는 세 가지, 자산(질문, 정답, 근거), 기계가 키우는 순환, 검증 가능한 wiki, 쓰고 버리는 모델" width="100%"></p>

- **지식은 AI 안이 아니라 질문과 정답 쪽에 쌓인다.** 정답은 AI와 따로 남으므로 새 AI가 그대로 쓸 수 있고, 선은 다시 그려집니다. 우리의 측정에서는, 기계가 묻는 방식과 배치를 고르고 비슷한 예를 더하면 모델의 가중치를 미세 조정하는 방법(LoRA)과 같은 정확도에 도달했습니다([숫자](../bench.md))
- **자동 결정에 근거가 붙는다.** "95% 확신"이 아니라 "사람이 확인한 48건에서 정밀도 0.96"
- **wiki는 읽는 것이 아니라 측정하는 것이다.** "이렇게 물으면 잘 맞는다"가 숫자가 붙은 주장으로 쌓이고, 새 AI에서 다시 시험할 수 있습니다. 주장이 확인되었다고 정하는 것은 사람만입니다

## 더 읽기

- [안내서](../guide.md): 모든 명령, HTTP API, FAQ · [spec 참고](../../skills/shikii/references/spec.md)
- [hook](../../integrations/claude-code.md)(일본어): 에이전트 자신의 생각을 측정하는 방식, 구간(band) → 행동의 표, 내부 구조
- [구조](../architecture.md)(일본어): 위 그림 뒤에 있는 전체 모습
- [미리 만들어진 결정](../../decisions/README.md) · [묻는 방법의 wiki](../../wiki/index.md) · [측정한 숫자](../bench.md)
- [연결](../../integrations/mcp.md): MCP, 일반 채팅 모델용 어댑터, Docker

**기여하는 분께**: 테스트는 일부러 깨서 실패하는 모습을 본 뒤에 고칩니다. 모든 숫자는 스크립트에서 나옵니다. wiki의 주장을 확인하는 것은 사람만입니다. 설정 항목은 더하지 않습니다. 정해진 자리와 정해진 규칙이 그 일을 합니다.

## 라이선스

[Apache-2.0 OR MIT](../../LICENSE). 모델의 라이선스는 별도입니다(Kev-4B와 Bonsai 계열은 Apache 2.0, OpenJev는 CC BY-NC 4.0, 호스팅 Jev는 TypeSafe의 서비스).
