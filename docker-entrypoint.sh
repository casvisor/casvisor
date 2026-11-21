#!/bin/bash
# MariaDB only listens inside the container; without MYSQL_ROOT_PASSWORD it gets a random password on every start
if [ "${MYSQL_ROOT_PASSWORD}" = "" ]; then MYSQL_ROOT_PASSWORD=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9'); fi

service mariadb start

mysqladmin -u root password "${MYSQL_ROOT_PASSWORD}"

export dataSourceName="root:${MYSQL_ROOT_PASSWORD}@tcp(localhost:3306)/"
exec /server --createDatabase=true
