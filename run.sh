#!/bin/sh

export PORT=${PORT:-8082}
export MYSQL_HOST=${MYSQL_HOST:-127.0.0.1}
export MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD:-password}
export ONYXCHAIN_NET=${ONYXCHAIN_NET:-main}

case ${ONYXCHAIN_NET} in
    main)
        export ONYXCHAIN_RPC=https://andromeda-sync.onyxpay.co:20336
        ;;
    test)
        export ONYXCHAIN_RPC=https://cepheus5.onyxpay.co:20336
        ;;
esac

envsubst < config.json.template > config.json

mkdir log
sleep 10 # Workaround: holder container starts way faster than MySQL container
exec ./onyxchain-holder
