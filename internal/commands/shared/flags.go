package shared

import (
	"fmt"
	"strings"
)

type flagType int

const (
	flagBool flagType = iota
	flagValue
)

type flagDef struct {
	name  string
	short string
	typ   flagType
}

func parseSpec(spec string) (byLong map[string]flagDef, byShort map[string]flagDef) {
	byLong = map[string]flagDef{}
	byShort = map[string]flagDef{}

	for _, entry := range strings.Fields(spec) {
		nameAndType := strings.SplitN(entry, ":", 2)
		name := nameAndType[0]
		typ := flagValue
		if len(nameAndType) == 2 && nameAndType[1] == "bool" {
			typ = flagBool
		}

		short := ""
		if idx := strings.Index(name, "|"); idx >= 0 {
			short = name[idx+1:]
			name = name[:idx]
		}

		def := flagDef{name: name, short: short, typ: typ}
		byLong[name] = def
		if short != "" {
			byShort[short] = def
		}
	}

	return byLong, byShort
}

// Parse parses args against spec (strict: an unrecognized flag is an
// error).
func Parse(spec string, args []string) (map[string]string, []string, error) {
	return parse(spec, args, false)
}

// ParsePassthrough parses args against spec, but pushes an unrecognized
// flag onto the remaining args instead of erroring.
func ParsePassthrough(spec string, args []string) (map[string]string, []string, error) {
	return parse(spec, args, true)
}

func parse(spec string, args []string, passthrough bool) (map[string]string, []string, error) {
	byLong, byShort := parseSpec(spec)

	opts := map[string]string{}
	var rest []string

	i := 0
	for i < len(args) {
		arg := args[i]

		switch {
		case arg == "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)

		case strings.HasPrefix(arg, "--") && len(arg) > 2:
			key := arg[2:]
			val := ""
			hasVal := false
			if idx := strings.Index(key, "="); idx >= 0 {
				val = key[idx+1:]
				key = key[:idx]
				hasVal = true
			}

			def, ok := byLong[key]
			if !ok {
				if passthrough {
					rest = append(rest, arg)
					i++
					continue
				}
				return nil, nil, fmt.Errorf("unknown flag '--%s'", key)
			}

			if def.typ == flagBool {
				opts[def.name] = "1"
				i++
				continue
			}

			if !hasVal {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("flag '--%s' requires a value", key)
				}
				val = args[i+1]
				i++
			}
			opts[def.name] = val
			i++

		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			key := arg[1:]
			val := ""
			hasVal := false
			if idx := strings.Index(key, "="); idx >= 0 {
				val = key[idx+1:]
				key = key[:idx]
				hasVal = true
			}

			def, ok := byShort[key]
			if !ok {
				if passthrough {
					rest = append(rest, arg)
					i++
					continue
				}
				return nil, nil, fmt.Errorf("unknown flag '-%s'", key)
			}

			if def.typ == flagBool {
				opts[def.name] = "1"
				i++
				continue
			}

			if !hasVal {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("flag '-%s' requires a value", key)
				}
				val = args[i+1]
				i++
			}
			opts[def.name] = val
			i++

		default:
			rest = append(rest, arg)
			i++
		}
	}

	return opts, rest, nil
}
