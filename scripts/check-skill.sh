#!/bin/sh
# Skill check (CI): SKILL.md's front matter has name and description, the references the body points to exist, and plugin.json reads as JSON.
set -eu
f=skills/shikii/SKILL.md
head -1 "$f" | grep -q '^---$' || { echo "check-skill: $f must start with front matter"; exit 1; }
grep -q '^name: shikii$' "$f" || { echo "check-skill: name missing"; exit 1; }
grep -q '^description: ' "$f" || { echo "check-skill: description missing"; exit 1; }
for ref in $(grep -o 'references/[a-z-]*\.md' "$f" | sort -u); do
  test -f "skills/shikii/$ref" || { echo "check-skill: $ref referenced but missing"; exit 1; }
done
python3 -c 'import json; json.load(open(".claude-plugin/plugin.json"))' || { echo "check-skill: plugin.json is not JSON"; exit 1; }
echo "check-skill: ok ($(grep -c . "$f") lines, $(ls skills/shikii/references | wc -l | tr -d ' ') references)"
