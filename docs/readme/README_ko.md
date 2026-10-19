[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · **한국어** · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: AI의 "아마도"를, 업무에서 믿고 쓸 수 있는 "맡겨도 된다"로 바꾼다

### AI에게 "예/아니오", "어느 것", "어느 정도"를 결정하게 하고, 그 결정을 일상 업무에서 계속 믿을 수 있게 유지하는 도구. 당신은 답을 일부 맞춰 보고, 가끔 "이게 아직 내가 원한 뜻인가?"에 답하면 됩니다. 나머지는 기계가 합니다.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-190%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[문제](#문제) · [shikii가 하는 일](#shikii가-하는-일) · [당신이 하는 일](#당신이-하는-일) · [설치](#설치) · [가장 빠른 입구](#가장-빠른-입구) · [한 바퀴 돌아보기](#한-바퀴-돌아보기) · [무엇이 새로운가](#무엇이-새로운가) · [더 읽기](#더-읽기)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: 모델은 확률을 주고, shikii는 그대로 실행해도 되는지 알려 준다" width="100%"></p>

## 문제

AI에게 "이 문의는 지난번과 같은 건인가?"라고 물으면 "아마도, 0.9"라고 답합니다. 실제 업무에는 이것만으로 부족합니다.

- 그 0.9는 정말 10번 중 9번 맞는가?
- 새 모델로 바꾼 날부터, 조용히 틀린 말을 하기 시작한 건 아닌가?
- 묻는 말을 조금 바꾸면 정확도는 어떻게 움직이는가?
- 아무도 보지 않는 곳에서 스스로 내린 결정들은 지금도 맞는가?

사람이 모든 답을 확인해야 한다면, AI를 쓴 의미가 거의 없습니다.

## shikii가 하는 일

1. **당신의 정답 맞추기를 모아, AI가 스스로 움직여도 되는 "선"을 그린다.** 당신이 준 답("실제로는 이랬다")으로 AI의 자신감과 실제로 맞는 빈도의 관계를 측정하고, 어디서부터 위를 믿어도 되는지 정합니다. 선이 생기기 전에는 아무것도 자동으로 실행되지 않습니다. 선이 생기면, 모든 자동 결정에 그 선이 몇 건의 확인된 답 위에 서 있고 그 답들이 얼마나 맞았는지를 적은 근거가 붙습니다
2. **다른 묻는 방식을 시험하고, 더 나은 쪽을 남긴다.** 같은 정답 데이터로 질문 문장, 선택지 설명, 프롬프트 배치의 다른 후보를 점수 매기고, 정말 나아졌을 때만 채택합니다. 당신은 뜻이 바뀌지 않았는지만 확인합니다
3. **모델을 바꿔도 처음부터 다시 하지 않는다.** 정답 데이터는 모델과 따로 보관되므로 새 모델이 그대로 재사용하고, 선은 기계가 다시 그립니다. 사람 손은 필요 없습니다
4. **배운 것을 wiki에 남긴다.** "이렇게 물으면 잘 맞는다"가 측정한 숫자와 함께 주장으로 쌓이고, 다른 모델에서 다시 시험할 수 있습니다. 다른 사람이 자기 모델에서 측정한 결과도 같은 페이지에 더해집니다

## 당신이 하는 일

- **무엇을 결정하고 싶은지 적는다.** 몇 가지 질문에 답하면 결정 파일이 만들어집니다(`shikii new`, 대화식으로도 `--type --question --option --input --pii` 플래그로도 가능. 전체 형식은 [spec 참고](../../skills/shikii/references/spec.md))
- **답을 맞춰 본다.** 화면에 AI가 확신하지 못한 결정과, 스스로 실행한 결정 20건 중 1건이 나오고, 실제로는 무엇이었는지 버튼으로 답합니다. 1~9 키와 Enter(AI의 선택과 같음)로 빠르게 넘길 수 있습니다. "답변 완료"에서 이전 답을 고칠 수 있습니다(가장 새 답이 유효). "자세히"를 열면 "AI에게 다시 묻기"가 나와, 같은 본문을 오늘의 묻는 방식과 선으로 다시 판정하고 전후를 나란히 보여 줍니다. 기록은 덧붙기만 하고 지워지지 않습니다
- **가끔 "이게 아직 내가 원한 뜻인가?"에 답한다.** 기계가 점수가 더 좋은 묻는 방식을 찾으면, 뜻이 바뀌었는지 보고 채택 또는 거부를 누릅니다

그 외는 모두 기계의 일입니다.

## 설치

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # needs Go 1.27 or newer
cd <project root> && shikii init
```

`shikii init`은 폴더 하나 `.shikii/`(안에 `decisions/` = 당신의 spec, `wiki/` = 지식, `records/`와 `labels/` = 기록과 당신의 정답 데이터. 프로젝트 루트에 다른 것은 늘어나지 않습니다)와 에이전트 연동을 한 번에 갖춥니다. Claude Code(`.mcp.json`), Cursor(`.cursor/mcp.json`), OpenCode(`opencode.json`), Codex(`~/.codex/config.toml`)에 shikii MCP 서버를 설정하고, `AGENTS.md`에 한 절을 더하고, 자세한 규칙(skill)을 `~/.claude/skills/shikii`에 둡니다. 당신이 직접 쓴 항목은 절대 건드리지 않습니다(이미 있는 shikii 항목도 그대로 둡니다). skill 폴더만은 매번 바꿔 놓으므로 손으로 고치지 마세요. `wiki/`에는 공유 지식(질문 유형별 묻는 방법, 효과가 있었던 문장 패턴, 모델별 습성)이 들어오고, 묻는 방식 개선 기능(`tune --propose`)이 그것을 읽습니다. 당신이 고친 페이지는 이후의 `init`에서도 덮어쓰지 않습니다. Go가 없다면 아래의 Docker를 쓰세요.

설정 항목들은 로컬 모델(`shikii up`, 포트 8080)을 가리킵니다. 호스팅 모델만 쓴다면 네 파일 각각에서 `--local`을 손으로 지우세요. 쓰지 않는 도구의 설정 파일도 만들어지니, 지우거나 `.gitignore`에 넣으세요(`shikii init`을 다시 실행해도 있는 것은 건드리지 않습니다). `.shikii/decisions/`, `.shikii/wiki/`, `.shikii/labels/`는 commit하세요(당신의 spec, 지식, 정답 데이터: 모델을 바꿔도 남는 자산입니다). `.shikii/records/`(모든 결정)는 계속 늘어나므로 commit은 선택입니다(`shikii gc`가 오래된 것을 지웁니다). Codex에 이미 `mcp_servers`가 있으면 추가하지 않고 붙여 넣을 줄을 출력합니다. `CLAUDE.md`만 있는 프로젝트에는 `AGENTS.md`를 만들지 않습니다(만들면 OpenCode가 `CLAUDE.md`를 읽지 않게 되기 때문).

정답 화면 자체(`web/`, Svelte)를 고치려면 Node 24가 필요합니다(`make web`). shikii를 쓰기만 할 때는 필요 없습니다. 빌드된 화면은 commit되어 있고 실행 파일에 포함되어 있습니다.

## 가장 빠른 입구

- **Docker**: Go도 모델도 없다면 `docker compose up`. 로컬 모델(Kev-4B)과 shikii가 뜨고, 정답 화면이 http://127.0.0.1:8095 에 나옵니다([자세히](../../integrations/docker.md))
- **미리 만들어진 결정**: 이 저장소의 `decisions/`에 여섯 개(요청 유형, 중복 버그 신고, 같은 문의, …). 하나를 당신의 `.shikii/decisions/`에 복사하거나, `shikii new --from <repo>/decisions/prompt-intent-v2.yaml`로 그것을 출발점으로 삼습니다([목록](../../decisions/README.md))
- **과거 결정의 표가 이미 있다**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml`이 그것을 정답 데이터로 바꿔 주므로, 처음 50건을 손으로 답하지 않아도 됩니다
- **내 프로그램에서**: `shikii serve`를 띄우고 `POST /v1/decide`(판정)와 `POST /v1/label`(정답)을 호출합니다. 본문은 JSON이고 `-H 'Content-Type: application/json'`이 필요합니다(모든 엔드포인트는 [긴 안내서](../guide.md#or-run-one-process-for-programs-and-people))
- **다른 사람의 예**: [문의 메일 분류](../examples/inquiry-route.md)(일본어). 처음 쓰는 사람이 README만으로 한 바퀴를 다 돌았고, spec과 22건의 입력이 들어 있습니다
- **AI 자신의 생각을 측정한다**(Claude Code와 Codex): `shikii hook install`로 hook 두 개가 들어가, 에이전트가 요청 하나를 처리하는 매 턴의 앞과 뒤에 shikii를 실행합니다. 앞에서는: 이 요청이 결국 파일을 바꾸게 되는가, 에이전트가 먼저 진행 방법을 합의해야 하는가? 뒤에서는: 바뀐 것이 요청한 범위 안인가, 요청한 일을 끝냈는가? 첫 정답은 작업 트리가 바뀌었는지로 공짜로 얻고, 나머지는 당신이 화면에서 답합니다. 선이 생기기 전에는 아무것도 바뀌지 않습니다. 생긴 뒤에는 계획이 필요한 요청이 에이전트에게 보내는 메모가 되고, 끝나지 않았거나 범위를 벗어난 듯한 턴은 당신에게 보내는 한 줄이 되며, 그 선이 몇 건의 정답 위에 서 있는지와 사람이 확인한 정밀도가 함께 나옵니다. hook이 도구를 막는 일은 결코 없습니다([자세히](../../integrations/claude-code.md), 일본어)
- **CI에서**: `uses: hayaenfj/shikii@main`이 모든 pull request에서 `shikii check`를 실행하므로, 묻는 방식이 바뀌거나 선이 약해지면 빌드가 실패합니다

## 한 바퀴 돌아보기

```sh
shikii up                                                             # start the local model (downloaded on first run)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (no line yet, so: ask a person)
shikii serve --local http://127.0.0.1:8080                                   # specs and knowledge are read from .shikii/
#   → answer at http://127.0.0.1:8095. Once a decision has the number it asks for (eval.min_n, 20 for this one) the line is drawn by itself
#     To try with fewer answers, lower eval.min_n in the spec to what you have (a line written with --force is flagged by check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (safe to act, with the evidence: the line rests on 61 corrections, 48 of them from people. Of the human answers that this line puts on the act side, 25, 96% were right)
```

구간(band)은 네 가지뿐입니다. `act` = 그대로 진행, `ask` = 사람에게 확인, `discard` = 아무것도 하지 않음, `abstain` = 입력 부족. 날것의 확률을 읽을 일은 없습니다.

## 무엇이 새로운가

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 구조: 사람이 하는 세 가지, 자산(질문, 정답, 근거), 기계가 키우는 순환, 검증 가능한 wiki, 쓰고 버리는 모델" width="100%"></p>

- **지식은 모델이 아니라 질문과 정답 데이터에 쌓인다.** 그래서 모델은 쓰고 버릴 수 있고, 바꿔도 사람 손이 필요 없습니다. 측정 결과: 기계가 묻는 방식과 배치를 고르고 비슷한 예를 더하면, 평범한 기본 모델이 정확도에서 미세 조정(LoRA)과 같은 수준에 도달했습니다([숫자](../bench.md))
- **자동 결정에 증거가 붙는다.** "95% 확신"이 아니라 "사람이 확인한 48건에서 정밀도 0.96"
- **wiki는 읽는 것이 아니라 측정하는 것이다.** "이렇게 물으면 잘 맞는다"가 숫자가 붙은 주장으로 쌓이고, 새 모델에서 다시 시험됩니다. 주장이 확인되었다고 정하는 것은 사람만입니다

## 더 읽기

- [긴 안내서](../guide.md): 모든 명령, 전체 빠른 시작, HTTP API, 연결할 수 있는 것, 측정 결과와 FAQ · [spec 참고](../../skills/shikii/references/spec.md)
- [설계 노트](../decision-ops-framework.md): 이 프레임워크가 맡는 것과 맡지 않는 것, 그리고 검증된 것 · [구조](../architecture.md)(일본어)
- [미리 만들어진 결정](../../decisions/README.md) · [묻는 방법의 wiki](../../wiki/index.md) · [벤치마크](../bench.md) · [모델 비교](../model-comparison.md) · [운영 기록](../operations.md)
- [연결](../../integrations/mcp.md): MCP, 일반 채팅 모델을 결정 모델로 바꾸는 어댑터, Docker, Claude Code
- [일본어 안내서](../guide.ja.md), 실제 업무의 한 바퀴 전체 포함

## 기여하기

이슈와 pull request를 환영합니다. 어떤 동작을 지키는 테스트는 먼저 실패하는 모습을 보입니다. 동작을 일부러 깨고, 테스트를 돌려, 어떻게 실패했는지 기록한 뒤, 고치고 통과하는 것을 기록합니다. 문서의 모든 숫자는 스크립트에서 나옵니다. wiki 주장의 상태(`confirmed` / `tentative` / `rejected`)는 사람만 바꾸며, 증거가 되는 실행을 함께 남깁니다. 새 옵션은 추가하지 않습니다. 배치(프로젝트 루트의 `decisions/` `wiki/` `.shikii/`)와 고정된 규칙이 그 일을 합니다.

## 라이선스

[Apache 2.0](../../LICENSE). 모델의 라이선스는 별도입니다. Kev-4B와 Bonsai 계열은 Apache 2.0, OpenJev는 CC BY-NC 4.0, 호스팅 Jev는 TypeSafe의 서비스입니다. shikii는 당신의 확률과 라벨만 저장하고, 모델의 것은 아무것도 저장하지 않습니다.
