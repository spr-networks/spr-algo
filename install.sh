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

KRUN_MAC="02:53:50:52:4b:03"
KRUN_TAP="kalgo0"
curl --fail-with-body --silent --show-error "http://127.0.0.1/device?identity=${KRUN_MAC}" \
  -H "Authorization: Bearer ${SPR_API_TOKEN}" -H "Content-Type: application/json" \
  -X PUT --data-raw "{\"MAC\":\"${KRUN_MAC}\",\"Name\":\"spr-algo\",\"Policies\":[\"wan\",\"dns\"],\"Groups\":[\"vpn-algo\"]}" >/dev/null
if ! sudo nft get element inet filter dhcp_access "{ \"${KRUN_TAP}\" . ${KRUN_MAC} }" >/dev/null 2>&1; then
  sudo nft add element inet filter dhcp_access "{ \"${KRUN_TAP}\" . ${KRUN_MAC} : accept }"
fi

docker compose -f docker-compose-krun.yml build
docker compose -f docker-compose-krun.yml up -d

CONTAINER_IP=
for _ in $(seq 1 30); do
  CONTAINER_IP="$(jq -r --arg mac "$KRUN_MAC" '.[$mac].RecentIP // empty' "$SUPERDIR/state/public/devices-public.json")"
  [ -n "$CONTAINER_IP" ] && break
  sleep 1
done
[ -n "$CONTAINER_IP" ] || { echo "spr-algo did not obtain an SPR DHCP lease" >&2; exit 1; }
API=127.0.0.1

# Grant the plugin container outbound internet + DNS (it must reach the
# DigitalOcean API and the new droplet over SSH). No lan/api access; the
# interface is isolated in the vpn-algo group.
curl "http://${API}/firewall/custom_interface" \
  -H "Authorization: Bearer ${SPR_API_TOKEN}" \
  -X 'PUT' \
  --data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"${KRUN_TAP}\",\"Policies\":[\"wan\",\"dns\"],\"Groups\":[\"vpn-algo\"]}"

docker compose -f docker-compose-krun.yml restart
echo "spr-algo installed. Open the SPR UI -> Plugins -> spr-algo to configure."
