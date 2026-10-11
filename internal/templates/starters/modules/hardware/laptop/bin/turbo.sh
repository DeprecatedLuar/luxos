#!/usr/bin/env bash
# Toggle CPU turbo boost. TLP reapplies its own setting on the next profile
# switch or power-source change.
# Usage: turbo on|off|status

readonly INTEL_FILE="/sys/devices/system/cpu/intel_pstate/no_turbo"
readonly CPUFREQ_FILE="/sys/devices/system/cpu/cpufreq/boost"
readonly USAGE="Usage: turbo on|off|status"

# Prints the control file and the values meaning on and off.
control() {
  if [[ -e "$INTEL_FILE" ]]; then
    echo "$INTEL_FILE 0 1"
  elif [[ -e "$CPUFREQ_FILE" ]]; then
    echo "$CPUFREQ_FILE 1 0"
  else
    echo "Error: no turbo control found ($INTEL_FILE or $CPUFREQ_FILE)" >&2
    return 1
  fi
}

turbo_set() {
  local file on off value
  read -r file on off < <(control) || return 1
  if [[ "$1" == on ]]; then value=$on; else value=$off; fi
  if ! echo "$value" | sudo tee "$file" > /dev/null; then
    echo "Error: cannot write $file (locked by firmware?)" >&2
    return 1
  fi
  turbo_status
}

turbo_status() {
  local file on off value
  read -r file on off < <(control) || return 1
  value=$(<"$file")
  case "$value" in
    "$on")  echo "Turbo: on" ;;
    "$off") echo "Turbo: off" ;;
    *) echo "Error: unexpected value '$value' in $file" >&2; return 1 ;;
  esac
}

case "${1:-}" in
  on|off) turbo_set "$1" ;;
  status) turbo_status ;;
  *)      echo "$USAGE" >&2; exit 1 ;;
esac
