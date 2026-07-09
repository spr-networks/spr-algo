#!/bin/sh
# Minimal `uv` shim for the vendored algo playbooks.
#
# algo's main.yml runs `uv pip list` to verify the installed ansible version,
# and playbooks/cloud-pre.yml runs `uv pip install '.[<provider>]'` to install
# cloud provider extras. In this image every Python dependency is baked in at
# build time from hash-pinned wheels (/opt/algo-venv), and the DigitalOcean
# path needs no provider extras, so:
#   - `uv pip list`      -> forwarded to the venv's pip
#   - `uv pip install *` -> no-op (deps are preinstalled, image has no
#                           network-installable pip by design)
# Anything else fails loudly.
set -eu

VENV_PIP=/opt/algo-venv/bin/pip

if [ "${1:-}" = "pip" ] && [ "${2:-}" = "list" ]; then
    shift 2
    exec "$VENV_PIP" list "$@"
fi

if [ "${1:-}" = "pip" ] && [ "${2:-}" = "install" ]; then
    echo "uv-shim: skipping '$*' (dependencies are preinstalled at image build time)"
    exit 0
fi

echo "uv-shim: unsupported invocation: uv $*" >&2
exit 1
