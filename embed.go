// Package shikii はモジュールのルート。skill(エージェント向けの作法)を実行ファイルに埋め込み、`shikii init` が配る。
package shikii

import "embed"

// SkillFS は skills/shikii 以下(SKILL.md と references/)。`shikii init` が ~/.claude/skills/shikii に写す。
//
//go:embed skills/shikii
var SkillFS embed.FS
