#!/bin/bash
set -e

REPO_URL="https://github.com/mahmudali1337-lab/mail-relay"
INSTALL_DIR="/opt/mail-relay"
SERVICE="mail-relay"
QUEUE_DIR="/var/spool/mailrelay"
DKIM_DIR="/etc/mail-relay/dkim"
CONFIG="$INSTALL_DIR/config.yaml"
DOMAIN="${1:-dstat.coffee}"
SERVER_IP="${2:-$(hostname -I | awk '{print $1}')}"

apt-get update -qq
apt-get install -y -qq git golang-go openssl

mkdir -p "$INSTALL_DIR" "$QUEUE_DIR" "$QUEUE_DIR/failed" "$DKIM_DIR"

TMP=$(mktemp -d)
cd "$TMP"
git clone "$REPO_URL" src
cd src
go build -o "$INSTALL_DIR/mail-relay" .
cd /
rm -rf "$TMP"

DKIM_KEY="$DKIM_DIR/$DOMAIN.pem"
if [ ! -f "$DKIM_KEY" ]; then
    openssl genrsa -out "$DKIM_KEY" 2048
    chmod 600 "$DKIM_KEY"
    openssl rsa -in "$DKIM_KEY" -pubout -out "$DKIM_DIR/$DOMAIN.pub" 2>/dev/null
fi

PASS=$(openssl rand -hex 16)

cat > "$CONFIG" << EOF
listen: ":587"
hostname: "mail.$DOMAIN"
password: "$PASS"
queue_dir: "$QUEUE_DIR"
workers: 5
retry_max: 10

servers:
  - "198.46.199.132"
  - "77.90.185.102"
  - "213.177.179.55"

domains:
  - domain: "$DOMAIN"
    dkim_key: "$DKIM_KEY"
    selector: "mail"
EOF

cat > "/etc/systemd/system/$SERVICE.service" << EOF
[Unit]
Description=Mail Relay
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/mail-relay $CONFIG
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE"
systemctl restart "$SERVICE"

DKIM_PUB=$(openssl rsa -in "$DKIM_KEY" -pubout 2>/dev/null | grep -v "^-" | tr -d '\n')

echo ""
echo "===== DNS RECORDS TO ADD ====="
echo ""
echo "A:     mail.$DOMAIN  ->  $SERVER_IP"
echo "MX:    $DOMAIN       ->  mail.$DOMAIN  (priority 10)"
echo "TXT:   $DOMAIN       ->  \"v=spf1 ip4:$SERVER_IP ~all\""
echo "TXT:   mail._domainkey.$DOMAIN  ->  \"v=DKIM1; k=rsa; p=$DKIM_PUB\""
echo "TXT:   _dmarc.$DOMAIN  ->  \"v=DMARC1; p=none; rua=mailto:admin@$DOMAIN\""
echo ""
echo "===== SMTP CREDENTIALS ====="
echo ""
echo "Host:     $SERVER_IP"
echo "Port:     587"
echo "Password: $PASS"
echo ""
echo "Config: $CONFIG"
