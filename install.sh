#!/bin/bash
# Command line install alternative to the UI plugin install.
set -e

echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token (generate one on the Auth Keys page):"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

mkdir -p "$SUPERDIR/configs/plugins/spr-algo"
chmod 700 "$SUPERDIR/configs/plugins/spr-algo"

# The DigitalOcean token is configured later, in the plugin UI (or by writing
# Region/Users/DOToken into configs/plugins/spr-algo/config.json).

docker compose build
docker compose up -d

CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-algo")
API=127.0.0.1

# Grant the plugin container outbound internet + DNS (it must reach the
# DigitalOcean API and the new droplet over SSH). No lan/api access; the
# interface is isolated in the vpn-algo group.
curl "http://${API}/firewall/custom_interface" \
  -H "Authorization: Bearer ${SPR_API_TOKEN}" \
  -X 'PUT' \
  --data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"spr-algo\",\"Policies\":[\"wan\",\"dns\"],\"Groups\":[\"vpn-algo\"]}"

docker compose restart
echo "spr-algo installed. Open the SPR UI -> Plugins -> spr-algo to configure."
