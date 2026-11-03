#!/bin/bash
# Выкладка релиза на сервере (запускает CI от пользователя emp): deploy.sh <release-id>
# Ожидает распакованный релиз в /opt/emp/releases/<id>: api, worker, migrate, web/.
# При неудачной проверке здоровья возвращает предыдущий релиз.
set -euo pipefail

ID=$1
BASE=/opt/emp
REL=$BASE/releases/$ID
WEB=/var/www/erp
KEEP=5

test -x "$REL/api" -a -x "$REL/worker" -a -x "$REL/migrate" -a -f "$REL/web/index.html" \
  || { echo "incomplete release in $REL" >&2; exit 1; }
PREV=$(readlink "$BASE/current" 2>/dev/null || true)
PREV_WEB=$(readlink "$WEB/current" 2>/dev/null || true)

# -setup идемпотентен: при первом деплое создаёт роли и базу.
# Миграции идут вперёд и не ломают предыдущую версию API — применяем до переключения.
(cd "$BASE/shared" && "$REL/migrate" -setup)

rm -rf "$WEB/$ID"
cp -r "$REL/web" "$WEB/$ID"

switch() { ln -sfn "$1" "$2.tmp" && mv -Tf "$2.tmp" "$2"; }
switch "$REL" "$BASE/current"
switch "$WEB/$ID" "$WEB/current"
sudo /usr/bin/systemctl restart emp-api emp-worker

healthy() {
  for _ in $(seq 1 20); do
    curl -fsS http://127.0.0.1:8080/health >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

if ! healthy; then
  echo "health check failed" >&2
  journalctl -u emp-api -n 30 --no-pager 2>/dev/null || true
  if [ -n "$PREV" ]; then
    echo "rollback → $PREV" >&2
    switch "$PREV" "$BASE/current"
    [ -n "$PREV_WEB" ] && switch "$PREV_WEB" "$WEB/current"
    sudo /usr/bin/systemctl restart emp-api emp-worker
  fi
  exit 1
fi

# оставляем последние $KEEP релизов
ls -1dt "$BASE"/releases/*/ | tail -n +$((KEEP + 1)) | xargs -r rm -rf
ls -1dt "$WEB"/*/ | grep -v '/current/$' | tail -n +$((KEEP + 1)) | xargs -r rm -rf
rm -f "$BASE"/releases/*.tar.gz
echo "deployed $ID"
