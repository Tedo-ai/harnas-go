#!/usr/bin/env python3
"""Check current README/status claims against the checked-out spec."""

from __future__ import annotations

import os
import sys
import hashlib
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> None:
    print(f"drift check failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def spec_root() -> Path:
    explicit = os.environ.get("HARNAS_SPEC")
    if explicit and Path(explicit).is_dir():
        return Path(explicit)
    sibling = ROOT.parent / "harnas"
    if (sibling / "conformance" / "agents").is_dir():
        return sibling
    fail("set HARNAS_SPEC to a Harnas spec checkout")


def version_fields(root: Path) -> dict[str, str]:
    fields: dict[str, str] = {}
    for line in (root / "VERSION").read_text(encoding="utf-8").splitlines():
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        fields[key.strip()] = value.strip()
    return fields


def fixture_count(root: Path) -> int:
    agents = root / "conformance" / "agents"
    return sum(1 for path in agents.iterdir() if path.is_dir() and (path / "manifest.json").exists())


def fixture_hashes(root: Path) -> dict[str, str]:
    agents = root / "conformance" / "agents"
    hashes: dict[str, str] = {}
    for path in sorted(agents.iterdir()):
        if not path.is_dir() or not (path / "manifest.json").exists():
            continue
        expected_log = path / "expected-log.jsonl"
        if not expected_log.exists():
            fail(f"{path.relative_to(root)} has no expected-log.jsonl")
        hashes[path.name] = hashlib.sha256(expected_log.read_bytes()).hexdigest()
    return hashes


def require_corpus_manifest(root: Path, fixtures_version: str) -> None:
    manifest_path = root / "conformance" / "corpus-manifest.json"
    if not manifest_path.exists():
        fail("spec conformance/corpus-manifest.json is missing")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    versions = manifest.get("versions")
    if not isinstance(versions, dict):
        fail("spec corpus manifest has no versions object")
    entry = versions.get(fixtures_version)
    if not isinstance(entry, dict):
        fail(f"spec corpus manifest has no entry for fixtures_version {fixtures_version}")
    expected = entry.get("agents")
    if not isinstance(expected, dict):
        fail(f"spec corpus manifest entry {fixtures_version} has no agents object")
    actual = fixture_hashes(root)
    if actual != expected:
        missing = sorted(set(actual) - set(expected))
        stale = sorted(set(expected) - set(actual))
        changed = sorted(name for name in set(actual) & set(expected) if actual[name] != expected[name])
        parts = []
        if missing:
            parts.append(f"new fixtures without version bump: {', '.join(missing)}")
        if stale:
            parts.append(f"manifest contains removed fixtures: {', '.join(stale)}")
        if changed:
            parts.append(f"expected-log hashes changed: {', '.join(changed)}")
        fail("; ".join(parts) or "spec corpus manifest does not match live fixtures")


def latest_changelog_version() -> str:
    for line in (ROOT / "CHANGELOG.md").read_text(encoding="utf-8").splitlines():
        if line.startswith("## [") and "Unreleased" not in line:
            return line.split("[", 1)[1].split("]", 1)[0]
    fail("CHANGELOG has no release entries")


def main() -> None:
    root = spec_root()
    fields = version_fields(root)
    spec_version = fields.get("harnas_version") or fail("spec VERSION has no harnas_version")
    fixtures_version = fields.get("fixtures_version") or fail("spec VERSION has no fixtures_version")
    require_corpus_manifest(root, fixtures_version)
    impl_version = latest_changelog_version()
    count = fixture_count(root)
    readme = (ROOT / "README.md").read_text(encoding="utf-8")

    required = [
        f"**Version {impl_version}**",
        f"Tracks Harnas spec {spec_version}",
        f"Agent conformance: {count}/{count} fixtures passing",
    ]
    for needle in required:
        if needle not in readme:
            fail(f"README does not contain {needle!r}")
    for stale in ("0.19.3", "70/70", "65/65"):
        if stale in readme:
            fail(f"README contains stale {stale}")

    print(f"drift ok: harnas-go {impl_version}, spec {spec_version}, fixtures v{fixtures_version}, {count} agent fixtures")


if __name__ == "__main__":
    main()
