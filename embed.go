// Package shikii はモジュールのルート。skill(エージェント向けの作法)と画面(web/dist)を実行ファイルに埋め込む。
package shikii

import "embed"

// SkillFS は skills/shikii 以下(SKILL.md と references/)。`shikii init` が ~/.claude/skills/shikii に写す。
//
//go:embed all:skills/shikii
var SkillFS embed.FS

// WebFS は答え合わせの画面(web/ を Svelte で組み、`make web` でビルドした web/dist)。`shikii serve` が "/" で返す。
// dist は repo に commit してあるので、使うだけなら Node は要らない(画面を直す時だけ Node 24 と `make web`)。
//
//go:embed web/dist
var WebFS embed.FS
