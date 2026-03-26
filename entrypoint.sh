#!/bin/bash
set -e

if [ "$PROXY_USE_VARNISH" = "true" ] || [ "$PROXY_USE_VARNISH" = "1" ]; then
    echo "Starting Varnish cache on port 6081..."

    VARNISH_SIZE="${PROXY_VARNISH_CACHE_SIZE:-256m}"

    varnishd \
        -a 127.0.0.1:6081 \
        -f /etc/varnish/default.vcl \
        -s malloc,${VARNISH_SIZE} &

    for _ in $(seq 1 100); do
        if (echo > /dev/tcp/127.0.0.1/6081) >/dev/null 2>&1; then
            break
        fi
        sleep 0.1
    done
fi

echo "Starting proxy server..."
exec ./proxy
