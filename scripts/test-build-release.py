#!/usr/bin/env python3
"""Check the release document packing with isolated files, without building binaries."""
import ast
import json
import posixpath
import re
import shutil
import tempfile
from pathlib import Path
from urllib.parse import urlsplit


repository = Path(__file__).resolve().parents[1]
source = ast.parse((repository / "scripts/build-release.py").read_text())
declarations = [node for node in source.body if
                isinstance(node, ast.FunctionDef) and node.name == "bundled_document" or
                isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == "BUNDLED" for target in node.targets)]
build = next(node for node in source.body if isinstance(node, ast.FunctionDef) and node.name == "build")
packing = next(node for node in build.body if isinstance(node, ast.For) and isinstance(node.iter, ast.Name) and node.iter.id == "BUNDLED")

with tempfile.TemporaryDirectory(prefix="shikii-release-docs-") as directory:
    root = Path(directory)
    repo, stage = root / "repo", root / "stage"
    repo.mkdir()
    stage.mkdir()
    namespace = dict(repo=repo, stage=stage, version="9.8.7", posixpath=posixpath, re=re, urlsplit=urlsplit, shutil=shutil)
    exec(compile(ast.Module(body=declarations, type_ignores=[]), "release-documents", "exec"), namespace)
    fixtures = {
        "README.md": "README\n[Japanese](docs/readme/README_ja.md) [guide](docs/guide.md#prepare)\n",
        "docs/readme/README_ja.md": "README ja\n[English](../../README.md) [guide](../guide.ja.md#prepare)\n",
        "CHANGELOG.md": "Changes\n[Japanese](CHANGELOG_ja.md) [MCP](docs/mcp-knowledge.md)\n",
        "CHANGELOG_ja.md": "Changes ja\n[English](CHANGELOG.md) [MCP](docs/mcp-knowledge.ja.md)\n",
    }
    expected = {
        "README.md": "README\n[Japanese](README_ja.md) [guide](https://github.com/hayaenfj/shikii/blob/v9.8.7/docs/guide.md#prepare)\n",
        "README_ja.md": "README ja\n[English](README.md) [guide](https://github.com/hayaenfj/shikii/blob/v9.8.7/docs/guide.ja.md#prepare)\n",
        "CHANGELOG.md": "Changes\n[Japanese](CHANGELOG_ja.md) [MCP](https://github.com/hayaenfj/shikii/blob/v9.8.7/docs/mcp-knowledge.md)\n",
        "CHANGELOG_ja.md": "Changes ja\n[English](CHANGELOG.md) [MCP](https://github.com/hayaenfj/shikii/blob/v9.8.7/docs/mcp-knowledge.ja.md)\n",
    }
    for path in ["docs/guide.md", "docs/guide.ja.md", "docs/mcp-knowledge.md", "docs/mcp-knowledge.ja.md"]:
        target = repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("Linked document.\n")
    for path, _ in namespace["BUNDLED"]:
        target = repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(fixtures.get(path, "License fixture.\n"))
    exec(compile(ast.Module(body=[packing], type_ignores=[]), "release-packing", "exec"), namespace)
    for name, text in expected.items():
        assert (stage / name).read_text() == text, name + ": bundled document targets differ"
    for path, destination in namespace["BUNDLED"]:
        if destination not in expected:
            assert (repo / path).read_bytes() == (stage / destination).read_bytes(), destination
    print(json.dumps({"markdown_documents": len(expected), "links_and_nonlink_text_preserved": True, "license_bytes_preserved": True}))
