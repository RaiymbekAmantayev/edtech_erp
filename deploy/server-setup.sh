#!/bin/bash
# Первичная настройка сервера (AlmaLinux 8, рядом с уже работающим Apache/PHP-сайтом). Идемпотентна.
# Запуск от root: sudo bash server-setup.sh
#
# Раскладка:
#   /opt/emp/releases/<sha>   бинарники api, worker, migrate (выкладывает CI)
#   /opt/emp/current          → активный релиз
#   /opt/emp/shared/.env      секреты (создаются здесь, в git не попадают)
#   /var/www/erp/<sha>        статика фронтенда, /var/www/erp/current → активная
#   127.0.0.1:8080            API (наружу не открыт, только через Apache)
set -euo pipefail

DOMAIN=erp.bilim-shynyna-42-qadam.kz
APP_USER=emp
EMAIL=${CERTBOT_EMAIL:-}

# --- PostgreSQL 16 (AppStream), только localhost ---
if ! rpm -q postgresql-server >/dev/null; then
  dnf -y module reset postgresql
  dnf -y module enable postgresql:16
  dnf -y install postgresql-server postgresql-contrib
fi
if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
  postgresql-setup --initdb
fi
# Сервер маленький (1 CPU, <1 ГБ RAM, рядом MariaDB и PHP) — умеренные настройки памяти
cat > /var/lib/pgsql/data/conf.d.emp.conf <<'EOF'
listen_addresses = 'localhost'
max_connections = 40
shared_buffers = 96MB
effective_cache_size = 256MB
work_mem = 4MB
maintenance_work_mem = 32MB
EOF
chown postgres:postgres /var/lib/pgsql/data/conf.d.emp.conf
grep -q "conf.d.emp.conf" /var/lib/pgsql/data/postgresql.conf \
  || echo "include_if_exists = 'conf.d.emp.conf'" >> /var/lib/pgsql/data/postgresql.conf
# Пароли по TCP на localhost (scram), peer — для локального postgres
sed -i -E 's#^(host\s+all\s+all\s+127\.0\.0\.1/32\s+)\S+#\1scram-sha-256#; s#^(host\s+all\s+all\s+::1/128\s+)\S+#\1scram-sha-256#' /var/lib/pgsql/data/pg_hba.conf
systemctl enable --now postgresql
systemctl restart postgresql

# --- пользователь приложения и каталоги ---
id "$APP_USER" >/dev/null 2>&1 || useradd --system --create-home --home-dir /opt/emp --shell /bin/bash "$APP_USER"
chmod 755 /opt/emp
install -d -o "$APP_USER" -g "$APP_USER" -m 755 /opt/emp/releases /var/www/erp
install -d -o "$APP_USER" -g "$APP_USER" -m 700 /opt/emp/shared /opt/emp/.ssh
touch /opt/emp/.ssh/authorized_keys
chown "$APP_USER:$APP_USER" /opt/emp/.ssh/authorized_keys
chmod 600 /opt/emp/.ssh/authorized_keys
restorecon -R /opt/emp/.ssh /var/www/erp || true

# --- секреты: генерируются один раз ---
ENV=/opt/emp/shared/.env
if [ ! -f "$ENV" ]; then
  gen() { openssl rand -hex 24; }
  OWNER_PW=$(gen); APP_PW=$(gen); ADMIN_PW=$(gen)
  sudo -u postgres psql -qc "ALTER ROLE postgres PASSWORD '$ADMIN_PW'"
  cat > "$ENV" <<EOF
# Production-конфигурация EMP. Создана server-setup.sh, в git не хранится.
HTTP_ADDR=127.0.0.1:8080
POSTGRES_ADMIN_URL=postgres://postgres:$ADMIN_PW@127.0.0.1:5432/postgres?sslmode=disable
DATABASE_URL=postgres://emp_app:$APP_PW@127.0.0.1:5432/emp?sslmode=disable
DATABASE_SYSTEM_URL=postgres://emp_owner:$OWNER_PW@127.0.0.1:5432/emp?sslmode=disable
JWT_SECRET=$(openssl rand -hex 32)
TELEGRAM_BOT_TOKEN=
TELEGRAM_BOT_NAME=emp_center_bot
COOKIE_SECURE=true
PUBLIC_URL=https://$DOMAIN
EOF
  chown "$APP_USER:$APP_USER" "$ENV"
  chmod 600 "$ENV"
fi

# --- systemd ---
cat > /etc/systemd/system/emp-api.service <<'EOF'
[Unit]
Description=EMP API
After=network.target postgresql.service
Requires=postgresql.service

[Service]
User=emp
WorkingDirectory=/opt/emp/shared
ExecStart=/opt/emp/current/api
Restart=always
RestartSec=3
MemoryMax=200M
NoNewPrivileges=true
ProtectSystem=full
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/emp-worker.service <<'EOF'
[Unit]
Description=EMP worker (outbox, Telegram, расписание)
After=network.target postgresql.service
Requires=postgresql.service

[Service]
User=emp
WorkingDirectory=/opt/emp/shared
ExecStart=/opt/emp/current/worker
Restart=always
RestartSec=5
MemoryMax=150M
NoNewPrivileges=true
ProtectSystem=full
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable emp-api emp-worker

# CI (пользователь emp) может только перезапускать свои сервисы
cat > /etc/sudoers.d/emp-deploy <<'EOF'
emp ALL=(root) NOPASSWD: /usr/bin/systemctl restart emp-api emp-worker, /usr/bin/systemctl is-active emp-api emp-worker
EOF
chmod 440 /etc/sudoers.d/emp-deploy
visudo -cf /etc/sudoers.d/emp-deploy

# --- Apache: отдельный vhost, существующий сайт не затрагивается ---
# SELinux: разрешить Apache проксировать на 127.0.0.1:8080
setsebool -P httpd_can_network_connect 1
cat > /etc/httpd/conf.d/erp.conf <<EOF
<VirtualHost *:80>
    ServerName $DOMAIN
    DocumentRoot /var/www/erp/current
    RewriteEngine on
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^ https://%{SERVER_NAME}%{REQUEST_URI} [END,NE,R=permanent]
</VirtualHost>
EOF
if [ ! -d /etc/letsencrypt/live/$DOMAIN ]; then
  install -d -o "$APP_USER" -g "$APP_USER" /var/www/erp/current
  systemctl reload httpd
  certbot certonly --webroot -w /var/www/erp/current -d "$DOMAIN" --non-interactive --agree-tos \
    ${EMAIL:+--email "$EMAIL"} ${EMAIL:---register-unsafely-without-email}
  rmdir /var/www/erp/current 2>/dev/null || true
fi
cat > /etc/httpd/conf.d/erp-ssl.conf <<EOF
<IfModule mod_ssl.c>
<VirtualHost *:443>
    ServerName $DOMAIN
    DocumentRoot /var/www/erp/current
    Include /etc/letsencrypt/options-ssl-apache.conf
    SSLCertificateFile /etc/letsencrypt/live/$DOMAIN/fullchain.pem
    SSLCertificateKeyFile /etc/letsencrypt/live/$DOMAIN/privkey.pem

    Header always set Strict-Transport-Security "max-age=31536000"
    ProxyPreserveHost On
    ProxyTimeout 70
    ProxyPass        /api    http://127.0.0.1:8080/api
    ProxyPassReverse /api    http://127.0.0.1:8080/api
    ProxyPass        /health http://127.0.0.1:8080/health
    RequestHeader set X-Forwarded-Proto "https"

    <Directory /var/www/erp>
        Options FollowSymLinks
        AllowOverride None
        Require all granted
    </Directory>
    # SPA: неизвестные пути отдают index.html
    FallbackResource /index.html
    <LocationMatch "^/assets/">
        Header set Cache-Control "public, max-age=31536000, immutable"
    </LocationMatch>
    <Files "index.html">
        Header set Cache-Control "no-cache"
    </Files>
    ErrorLog /var/log/httpd/erp_error.log
    CustomLog /var/log/httpd/erp_access.log combined
</VirtualHost>
</IfModule>
EOF
apachectl configtest
echo "server-setup: ok"
