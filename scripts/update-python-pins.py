#!/usr/bin/env python3
"""Regenerate requirements-algo.txt from algo upstream's lockfile.

Reads ALGO_COMMIT from reproducible.env, fetches uv.lock and pyproject.toml at
that commit, walks the dependency closure of algo's base dependencies (cloud
provider extras excluded; only the DigitalOcean path is supported and it needs
none), then for every package==version downloads each wheel the container
could select (pure py3 wheels, plus manylinux/musllinux wheels for
x86_64/aarch64 with cp312 or abi3 tags), sha256s it locally, cross-checks the
digest PyPI's JSON API reports, and writes requirements-algo.txt.

Run on a trusted host: ./scripts/update-python-pins.py
"""
import hashlib
import json
import re
import sys
import tempfile
import tomllib
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
RAW = "https://raw.githubusercontent.com/trailofbits/algo/{commit}/{path}"
OUT = REPO / "requirements-algo.txt"

HEADER = """\
# Fully pinned, hash-checked Python deps for the vendored algo checkout.
# Versions come from algo upstream uv.lock at ALGO_COMMIT (see reproducible.env).
# Hashes cover every wheel the image can select (pure py3 + manylinux/musllinux
# cp312/abi3 wheels for x86_64 and aarch64); each file was downloaded on the
# pinning host and sha256-verified against PyPI. Regenerate with
# ./scripts/update-python-pins.py.
"""


def fetch(url: str) -> bytes:
    with urllib.request.urlopen(url) as r:
        return r.read()


def algo_commit() -> str:
    for line in (REPO / "reproducible.env").read_text().splitlines():
        if line.startswith("ALGO_COMMIT="):
            return line.split("=", 1)[1].strip()
    sys.exit("ALGO_COMMIT not found in reproducible.env")


def norm(name: str) -> str:
    return re.sub(r"[-_.]+", "-", name).lower()


def closure(commit: str) -> dict[str, str]:
    pyproject = tomllib.loads(fetch(RAW.format(commit=commit, path="pyproject.toml")).decode())
    roots = [norm(re.split(r"[<>=!~\[; ]", d)[0]) for d in pyproject["project"]["dependencies"]]
    lock = tomllib.loads(fetch(RAW.format(commit=commit, path="uv.lock")).decode())
    pkgs = {norm(p["name"]): p for p in lock["package"]}
    pins: dict[str, str] = {}
    stack = list(roots)
    while stack:
        name = stack.pop()
        if name in pins:
            continue
        if name not in pkgs:
            sys.exit(f"{name} missing from uv.lock")
        p = pkgs[name]
        pins[name] = p["version"]
        stack.extend(norm(d["name"]) for d in p.get("dependencies", []))
    return pins


def want(filename: str) -> bool:
    if not filename.endswith(".whl"):
        return False
    if "py3-none-any" in filename or "py2.py3-none-any" in filename:
        return True
    if ("manylinux" in filename or "musllinux" in filename) and (
        "x86_64" in filename or "aarch64" in filename
    ):
        return bool(re.search(r"(cp312|abi3)", filename))
    return False


def main() -> None:
    commit = algo_commit()
    print(f"resolving closure from uv.lock @ {commit}", file=sys.stderr)
    pins = closure(commit)
    entries = []
    with tempfile.TemporaryDirectory() as tmp:
        for pkg, ver in sorted(pins.items()):
            meta = json.loads(fetch(f"https://pypi.org/pypi/{pkg}/{ver}/json"))
            files = [f for f in meta["urls"] if want(f["filename"])]
            if not files:
                sys.exit(f"no matching wheels for {pkg}=={ver}")
            hashes = []
            for f in files:
                dest = Path(tmp) / f["filename"]
                dest.write_bytes(fetch(f["url"]))
                local = hashlib.sha256(dest.read_bytes()).hexdigest()
                if local != f["digests"]["sha256"]:
                    sys.exit(f"HASH MISMATCH for {f['filename']}")
                hashes.append(local)
            entries.append(
                f"{pkg}=={ver} \\\n"
                + " \\\n".join(f"    --hash=sha256:{h}" for h in sorted(hashes))
            )
            print(f"ok {pkg}=={ver}: {len(hashes)} wheels verified", file=sys.stderr)
    OUT.write_text(HEADER + "\n".join(entries) + "\n")
    print(f"wrote {OUT}", file=sys.stderr)


if __name__ == "__main__":
    main()
