#!/bin/sh
set -eu

until curl -fsS http://127.0.0.1:3000/api/healthz >/dev/null; do
  sleep 1
done

mkdir -p /state
chown git:git /state

create_user() {
  username="$1"
  password="$2"
  admin_flag="$3"

  if ! su-exec git /usr/local/bin/gitea admin user list | grep -Eq "[[:space:]]${username}[[:space:]]"; then
    # shellcheck disable=SC2086
    su-exec git /usr/local/bin/gitea admin user create \
      ${admin_flag} \
      --username "${username}" \
      --password "${password}" \
      --email "${username}@example.test" \
      --must-change-password=false
  fi
}

create_user gateway gateway-password --admin
create_user forker forker-password ""

timestamp="$(date +%s)"
su-exec git /usr/local/bin/gitea admin user generate-access-token \
  --username gateway \
  --token-name "gateway-e2e-${timestamp}" \
  --scopes all \
  --raw > /state/gateway.token
su-exec git /usr/local/bin/gitea admin user generate-access-token \
  --username forker \
  --token-name "forker-e2e-${timestamp}" \
  --scopes all \
  --raw > /state/forker.token
chmod 0600 /state/gateway.token /state/forker.token

exec sleep infinity
