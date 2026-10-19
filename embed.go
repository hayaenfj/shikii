// Package shikii はモジュールのルート。skill(エージェント向けの作法)と画面(web/dist)を実行ファイルに埋め込む。
package shikii

import "embed"

// SkillFS は skills/shikii 以下(SKILL.md と references/)。`shikii init` が ~/.claude/skills/shikii に写す。
//
//go:embed all:skills/shikii
var SkillFS embed.FS

// WikiFS は既製の知識(wiki/ の index.md、types/、patterns/、models/)。`shikii init` が新しいプロジェクトの wiki/ に、無いページだけ写す。
// 判定ごとのページ(specs/)と証拠(runs/)はそのプロジェクトの tune が育てるので写さない。人が直したページは init が上書きしない。
//
//go:embed wiki/index.md wiki/types wiki/patterns wiki/models
var WikiFS embed.FS

// AgentSpecsFS は AI 自身の考え方の spec(integrations/claude-code/specs/: 考える前の prompt-kind と needs-plan、考え終わった後の changes-in-scope と done-as-asked)。
// `shikii hook install` が ~/.shikii/agent/decisions/ に、無いものだけ写す(線の入った spec や人が直した spec は触らない)。
//
//go:embed integrations/claude-code/specs
var AgentSpecsFS embed.FS

// WebFS は答え合わせの画面(web/ を Svelte で組み、`make web` でビルドした web/dist)。`shikii serve` が "/" で返す。
// dist は repo に commit してあるので、使うだけなら Node は要らない(画面を直す時だけ Node 24 と `make web`)。
//
//go:embed web/dist
var WebFS embed.FS
