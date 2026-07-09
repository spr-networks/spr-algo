#!/bin/bash
set -a
. /configs/base/config.sh
set +a

# Persist algo's output (generated VPN configs, SSH deploy key, PKI) under the
# plugin config volume. algo writes to <playbook_dir>/configs/.
mkdir -p /configs/spr-algo/algo
chmod 700 /configs/spr-algo /configs/spr-algo/algo || true
rm -rf /algo/configs
ln -sfn /configs/spr-algo/algo /algo/configs

# algo asserts the playbook dir is not world/group writable
chmod 755 /algo

mkdir -p /state/plugins/spr-algo
chmod 700 /state/plugins/spr-algo || true

exec /algo_plugin
