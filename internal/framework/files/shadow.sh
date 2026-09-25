if [[ -z "$LUXOS" ]]; then
    echo "Error: luxos path was not baked into this shadow (regenerate with luxos rebuild switch)" >&2
    exit 1
fi
exec "$LUXOS" "${LUXOS_ARGS[@]}" "$@"
