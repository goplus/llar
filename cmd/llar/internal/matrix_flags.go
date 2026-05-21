package internal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var matrixKeyRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

type knownMatrixFlags struct {
	long       map[string]bool
	short      map[string]bool
	needsValue map[string]bool
}

func knownMakeMatrixFlags() knownMatrixFlags {
	return knownMatrixFlags{
		long:       map[string]bool{"help": true, "verbose": true, "output": true},
		short:      map[string]bool{"h": true, "v": true, "o": true},
		needsValue: map[string]bool{"output": true, "o": true},
	}
}

func knownTestMatrixFlags() knownMatrixFlags {
	return knownMatrixFlags{
		long:       map[string]bool{"help": true, "verbose": true},
		short:      map[string]bool{"h": true, "v": true},
		needsValue: map[string]bool{},
	}
}

func parseMatrixArgs(args []string, known knownMatrixFlags) ([]string, string, error) {
	matrix := map[string]string{}
	clean := make([]string, 0, len(args))
	parseFlags := true

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !parseFlags {
			clean = append(clean, arg)
			continue
		}
		if arg == "--" {
			parseFlags = false
			clean = append(clean, arg)
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			clean = append(clean, arg)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key, value, hasValue, err := splitLongFlag(arg)
			if err != nil {
				return nil, "", err
			}
			if strings.HasPrefix(key, "matrix-") {
				matrixKey := strings.TrimPrefix(key, "matrix-")
				if matrixKey == "" {
					return nil, "", fmt.Errorf("missing matrix key in --matrix-")
				}
				if !validMatrixKey(matrixKey) {
					return nil, "", fmt.Errorf("invalid matrix key %q", matrixKey)
				}
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
					}
					i++
					value = args[i]
				}
				if value == "" {
					return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
				}
				matrix[matrixKey] = value
				continue
			}
			if known.long[key] {
				clean = append(clean, arg)
				if !hasValue && known.needsValue[key] {
					if i+1 >= len(args) {
						return nil, "", fmt.Errorf("missing value for --%s", key)
					}
					i++
					clean = append(clean, args[i])
				}
				continue
			}
			if !validMatrixKey(key) {
				return nil, "", fmt.Errorf("invalid matrix key %q", key)
			}
			if !hasValue {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
			}
			matrix[key] = value
			continue
		}
		key := strings.TrimPrefix(arg, "-")
		if known.short[key] {
			clean = append(clean, arg)
			if known.needsValue[key] {
				if i+1 >= len(args) {
					return nil, "", fmt.Errorf("missing value for %s", arg)
				}
				i++
				clean = append(clean, args[i])
			}
			continue
		}
		return nil, "", fmt.Errorf("unknown short flag %q", arg)
	}

	if len(matrix) == 0 {
		return clean, hostMatrixCombo(), nil
	}
	matrixStr, err := encodeMatrix(matrix)
	if err != nil {
		return nil, "", err
	}
	return clean, matrixStr, nil
}

func splitLongFlag(arg string) (key, value string, hasValue bool, err error) {
	body := strings.TrimPrefix(arg, "--")
	if body == "" {
		return "", "", false, fmt.Errorf("invalid flag %q", arg)
	}
	key, value, hasValue = strings.Cut(body, "=")
	if key == "" {
		return "", "", false, fmt.Errorf("invalid flag %q", arg)
	}
	return key, value, hasValue, nil
}

func validMatrixKey(key string) bool {
	return matrixKeyRE.MatchString(key)
}

func encodeMatrix(matrix map[string]string) (string, error) {
	arch := matrix["arch"]
	osName := matrix["os"]
	var primary string
	switch {
	case arch != "" && osName != "":
		primary = arch + "-" + osName
	case arch != "":
		primary = arch
	case osName != "":
		return "", fmt.Errorf("matrix requires arch when os is set")
	default:
		primary = hostMatrixCombo()
	}

	keys := make([]string, 0, len(matrix))
	for key := range matrix {
		if key == "arch" || key == "os" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return primary, nil
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+matrix[key])
	}
	return primary + "|" + strings.Join(parts, ","), nil
}
