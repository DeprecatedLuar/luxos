#!/usr/bin/env bash
# flags.sh - shared, non-POSIX-strict flag parser for scripts under bin/lib.
#
# Long (--flag, --flag=value, --flag value) and short (-f, -f value,
# -f=value) flags may appear anywhere in the argument list, interleaved
# with positional args, in any order.
#
# Usage:
#   source "$LIB_DIR/flags.sh"
#   declare -A opts=()
#   declare -a rest=()
#   # spec: space-separated "name[|alias]:type" pairs, type is 'bool' or 'value'
#   flags::parse opts rest "verbose|v:bool force:bool output|o:value" "$@"
#
#   [[ -n "${opts[verbose]:-}" ]] && echo "verbose on"
#   echo "output = ${opts[output]:-}"
#   echo "positionals: ${rest[*]:-}"
#
# Passthrough (for wrappers around a real binary, e.g. nixos-rebuild): call
# flags::parse_passthrough instead. Same signature and behavior, except an
# unrecognized flag is pushed onto rest verbatim instead of erroring — it
# and everything after it belong to the wrapped binary, not to this parser.

flags::parse() {
    _flags_parse 0 "$@"
}

flags::parse_passthrough() {
    _flags_parse 1 "$@"
}

_flags_parse() {
    local passthrough=$1
    shift
    local -n _flags_out=$1
    local -n _rest_out=$2
    local spec=$3
    shift 3

    local -A _long_to_name=()
    local -A _short_to_name=()
    local -A _type=()

    local entry name type short
    for entry in $spec; do
        name="${entry%%:*}"
        type="${entry##*:}"
        short=""
        [[ "$name" == *"|"* ]] && short="${name##*|}"
        name="${name%%|*}"
        _type["$name"]="$type"
        _long_to_name["$name"]="$name"
        [[ -n "$short" ]] && _short_to_name["$short"]="$name"
    done

    local arg key val resolved
    while [[ $# -gt 0 ]]; do
        arg=$1
        case "$arg" in
            --)
                shift
                _rest_out+=("$@")
                break
                ;;
            --?*)
                key="${arg#--}"
                if [[ "$key" == *"="* ]]; then
                    val="${key#*=}"
                    key="${key%%=*}"
                else
                    val=""
                fi
                resolved="${_long_to_name[$key]:-}"
                if [[ -z "$resolved" ]]; then
                    if [[ "$passthrough" == 1 ]]; then
                        _rest_out+=("$arg")
                        shift
                        continue
                    fi
                    echo "Error: unknown flag '--$key'" >&2
                    return 1
                fi
                if [[ "${_type[$resolved]}" == "bool" ]]; then
                    _flags_out["$resolved"]=1
                else
                    if [[ -z "$val" ]]; then
                        [[ $# -lt 2 ]] && { echo "Error: flag '--$key' requires a value" >&2; return 1; }
                        val=$2
                        shift
                    fi
                    _flags_out["$resolved"]="$val"
                fi
                ;;
            -?*)
                key="${arg#-}"
                if [[ "$key" == *"="* ]]; then
                    val="${key#*=}"
                    key="${key%%=*}"
                else
                    val=""
                fi
                resolved="${_short_to_name[$key]:-}"
                if [[ -z "$resolved" ]]; then
                    if [[ "$passthrough" == 1 ]]; then
                        _rest_out+=("$arg")
                        shift
                        continue
                    fi
                    echo "Error: unknown flag '-$key'" >&2
                    return 1
                fi
                if [[ "${_type[$resolved]}" == "bool" ]]; then
                    _flags_out["$resolved"]=1
                else
                    if [[ -z "$val" ]]; then
                        [[ $# -lt 2 ]] && { echo "Error: flag '-$key' requires a value" >&2; return 1; }
                        val=$2
                        shift
                    fi
                    _flags_out["$resolved"]="$val"
                fi
                ;;
            *)
                _rest_out+=("$arg")
                ;;
        esac
        shift
    done
}
