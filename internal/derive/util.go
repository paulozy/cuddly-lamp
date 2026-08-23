package derive

import (
	"fmt"
	"sort"
	"strconv"
)

// Small shared helpers. They exist so the parsers can stay declarative and so
// every fact payload comes out in a deterministic order — a payload whose key
// order shifted between runs would look changed when nothing changed, and that
// would defeat the tree-SHA guard.

func sortStrings(values []string) { sort.Strings(values) }

func sortServices(services []ComposeService) {
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
}

func itoa(i int) string { return strconv.Itoa(i) }

func parsePort(text string) (int, error) {
	port, err := strconv.Atoi(text)
	if err != nil {
		return 0, err
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("derive: port %d out of range", port)
	}
	return port, nil
}

// scalarString renders a YAML scalar as text. A port written as `5432` and one
// written as `"5432"` are the same value, and a parser that only accepted the
// quoted form would silently drop half the real files.
func scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", typed)
	}
}
