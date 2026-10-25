// Package shikii is the module root. It embeds the skill (the working rules for agents) and the page (web/dist) into the binary.
package shikii

import "embed"

// SkillFS is everything under skills/shikii (SKILL.md and references/). `shikii init` copies it to ~/.claude/skills/shikii.
//
//go:embed all:skills/shikii
var SkillFS embed.FS

// WikiFS is the ready-made knowledge (wiki/'s index.md, types/, patterns/, models/). `shikii init` copies only the missing pages into a new project's wiki/.
// Per-decision pages (specs/) and evidence (runs/) are grown by that project's tune, so they are not copied. init does not overwrite pages a person has edited.
//
//go:embed wiki/index.md wiki/types wiki/patterns wiki/models
var WikiFS embed.FS

// AgentSpecsFS is the specs for the AI's own thinking (integrations/claude-code/specs/: prompt-kind and needs-plan before thinking, changes-in-scope and done-as-asked after).
// `shikii hook install` copies only the missing ones to ~/.shikii/agent/decisions/ (specs with a line, or edited by a person, are not touched).
//
//go:embed integrations/claude-code/specs
var AgentSpecsFS embed.FS

// WebFS is the answer-checking page (web/ built with Svelte; web/dist built by `make web`). `shikii serve` returns it at "/".
// dist is committed to the repo, so Node is not needed just to use it (Node 24 and `make web` only when changing the page).
//
//go:embed web/dist
var WebFS embed.FS
