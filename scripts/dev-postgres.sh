#!/usr/bin/env bash
# 在无系统 PG 权限时，于用户目录初始化一个 PostgreSQL 实例（默认端口 5433）。
# 用法： scripts/dev-postgres.sh [start|stop|status]
set -e
PORT="${PGPORT:-5433}"
PGDATA="${PGDATA:-$HOME/.pgdata-ski}"
PGUSER="${PGUSER:-postgres}"
SOCKET_DIR="/tmp"

if ! command -v initdb >/dev/null 2>&1; then
  # Debian/Ubuntu 下 initdb 可能不在 PATH
  if [ -x /usr/lib/postgresql/15/bin/initdb ]; then
    export PATH=/usr/lib/postgresql/15/bin:$PATH
  else
    echo "未找到 initdb，请先安装 postgresql（如 apt install postgresql）" >&2
    exit 1
  fi
fi

cmd="${1:-start}"
case "$cmd" in
  start)
    if [ ! -d "$PGDATA" ]; then
      initdb -D "$PGDATA" -U "$PGUSER" --auth=trust --encoding=UTF8
      echo "port = $PORT" >> "$PGDATA/postgresql.conf"
      echo "unix_socket_directories = '$SOCKET_DIR'" >> "$PGDATA/postgresql.conf"
    fi
    pg_ctl -D "$PGDATA" -l /tmp/pg-ski.log start -w
    sleep 1
    createdb -h 127.0.0.1 -p "$PORT" -U "$PGUSER" skirental 2>/dev/null || echo "数据库 skirental 已存在"
    echo "PostgreSQL 已启动: 127.0.0.1:$PORT 库名 skirental 用户 $PGUSER（trust 免密）"
    ;;
  stop)
    pg_ctl -D "$PGDATA" stop || true
    ;;
  status)
    pg_ctl -D "$PGDATA" status
    ;;
  *)
    echo "用法: $0 [start|stop|status]"
    exit 1
    ;;
esac
