#!/bin/bash
set -e

DOMAIN="${1:-dstat.coffee}"
MAIL_IP="${2:-$(hostname -I | awk '{print $1}')}"
DNS_CONFIG="${3:-/opt/dns-server/config.yaml}"
DKIM_KEY="/etc/mail-relay/dkim/$DOMAIN.pem"

if [ ! -f "$DNS_CONFIG" ]; then
    echo "DNS config not found: $DNS_CONFIG"
    exit 1
fi

if ! grep -q "name: \"mail\"" "$DNS_CONFIG"; then
    sed -i "/domain: \"$DOMAIN\"/,/subs:/ {
        /subs:/ a\\      - name: \"mail\"\\n        ip: \"$MAIL_IP\"
    }" "$DNS_CONFIG"
    echo "Added mail.$DOMAIN -> $MAIL_IP to DNS config"
fi

systemctl restart dns-server 2>/dev/null || true

DKIM_PUB=""
if [ -f "$DKIM_KEY" ]; then
    DKIM_PUB=$(openssl rsa -in "$DKIM_KEY" -pubout 2>/dev/null | grep -v "^-" | tr -d '\n')
fi

echo ""
echo "===== DNS config updated ====="
echo ""
echo "Your DNS server (config.yaml) now serves:"
echo "  mail.$DOMAIN  ->  $MAIL_IP"
echo ""
echo "Add these records MANUALLY to your DNS server config.yaml"
echo "under the zone '$DOMAIN' (requires TXT support in dns-server):"
echo ""
echo "  MX:   priority=10  mail.$DOMAIN"
echo "  TXT:  v=spf1 ip4:$MAIL_IP ~all"
if [ -n "$DKIM_PUB" ]; then
    echo "  TXT (DKIM selector=mail): v=DKIM1; k=rsa; p=$DKIM_PUB"
fi
echo "  TXT (DMARC): v=DMARC1; p=none; rua=mailto:admin@$DOMAIN"
echo ""
