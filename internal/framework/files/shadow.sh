LUXOS_BIN="/etc/luxos/bin"
if [[ -x "$LUXOS_BIN" ]]; then
    exec "$LUXOS_BIN" "${LUXOS_ARGS[@]}" "$@"
fi
if [[ -z "$REAL" ]]; then
    echo "Error: luxos not found at $LUXOS_BIN (run luxos rebuild switch from the binary's location)" >&2
    exit 1
fi
echo "Warning: luxos not found at $LUXOS_BIN, running plain $(basename "$0") (no self-heal)" >&2
exec "$REAL" "$@"
