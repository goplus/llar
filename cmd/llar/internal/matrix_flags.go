package internal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/pflag"
)

var matrixKeyRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func parseMatrixArgs(args []string, flags *pflag.FlagSet) ([]string, string, error) {
	matrixFlags := map[string]matrixFlagDef{}
	parseFlags := true

	for _, arg := range args {
		if !parseFlags {
			continue
		}
		if arg == "--" {
			parseFlags = false
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key, _, _, err := splitLongFlag(arg)
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
				matrixFlags[key] = ensureMatrixFlag(flags, key, matrixKey)
				continue
			}
			if flag := flags.Lookup(key); flag != nil {
				if matrixKey, ok := matrixKeyForFlag(flag); ok {
					matrixFlags[key] = matrixFlagDef{flagName: key, matrixKey: matrixKey}
				}
				continue
			}
			if !validMatrixKey(key) {
				return nil, "", fmt.Errorf("invalid matrix key %q", key)
			}
			matrixFlags[key] = ensureMatrixFlag(flags, key, key)
			continue
		}
		key := strings.TrimPrefix(arg, "-")
		if len(key) != 1 {
			return nil, "", fmt.Errorf("unknown short flag %q", arg)
		}
		if flags.ShorthandLookup(key) != nil {
			continue
		}
		return nil, "", fmt.Errorf("unknown short flag %q", arg)
	}

	resetMatrixFlags(flags)
	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}

	matrix := parsedMatrixValues(flags, matrixFlags)
	if len(matrix) == 0 {
		return flags.Args(), hostMatrixCombo(), nil
	}
	matrixStr, err := encodeMatrix(matrix)
	if err != nil {
		return nil, "", err
	}
	return flags.Args(), matrixStr, nil
}

const matrixFlagAnnotation = "llar.matrix"
const matrixFlagKeyAnnotation = "llar.matrix-key"

type matrixFlagDef struct {
	flagName  string
	matrixKey string
}

func ensureMatrixFlag(flags *pflag.FlagSet, flagName, matrixKey string) matrixFlagDef {
	if flag := flags.Lookup(flagName); flag == nil {
		flags.String(flagName, "", "")
		flag = flags.Lookup(flagName)
		flag.Annotations = map[string][]string{
			matrixFlagAnnotation:    {"true"},
			matrixFlagKeyAnnotation: {matrixKey},
		}
		_ = flags.MarkHidden(flagName)
	}
	return matrixFlagDef{flagName: flagName, matrixKey: matrixKey}
}

func isMatrixFlag(flag *pflag.Flag) bool {
	return flag != nil && len(flag.Annotations[matrixFlagAnnotation]) > 0
}

func matrixKeyForFlag(flag *pflag.Flag) (string, bool) {
	if !isMatrixFlag(flag) {
		return "", false
	}
	values := flag.Annotations[matrixFlagKeyAnnotation]
	if len(values) == 0 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

func resetMatrixFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(flag *pflag.Flag) {
		if !isMatrixFlag(flag) {
			return
		}
		flag.Changed = false
		_ = flag.Value.Set(flag.DefValue)
	})
}

func parsedMatrixValues(flags *pflag.FlagSet, matrixFlags map[string]matrixFlagDef) map[string]string {
	matrix := map[string]string{}
	for flagName, def := range matrixFlags {
		flag := flags.Lookup(flagName)
		if flag == nil || !flag.Changed {
			continue
		}
		value := flag.Value.String()
		if value == "" || strings.HasPrefix(value, "-") {
			continue
		}
		matrix[def.matrixKey] = value
	}
	return matrix
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
