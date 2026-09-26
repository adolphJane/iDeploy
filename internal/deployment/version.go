package deployment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var semanticVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func versionParts(s string) ([3]uint64, string, bool) {
	m := semanticVersion.FindStringSubmatch(s)
	var n [3]uint64
	if m == nil {
		return n, "", false
	}
	for i := range n {
		v, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return n, "", false
		}
		n[i] = v
	}
	for _, part := range strings.Split(m[4], ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return n, "", false
		}
	}
	return n, m[4], true
}

// CompareVersion implements semantic precedence, including numeric prerelease identifiers.
func CompareVersion(a, b string) (int, bool) {
	x, xp, xok := versionParts(a)
	y, yp, yok := versionParts(b)
	if !xok || !yok {
		return 0, false
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, true
		}
		if x[i] > y[i] {
			return 1, true
		}
	}
	if xp == yp {
		return 0, true
	}
	if xp == "" {
		return 1, true
	}
	if yp == "" {
		return -1, true
	}
	xs, ys := strings.Split(xp, "."), strings.Split(yp, ".")
	for i := 0; i < len(xs) && i < len(ys); i++ {
		a, b := xs[i], ys[i]
		if a == b {
			continue
		}
		an, bn := strings.Trim(a, "0123456789") == "", strings.Trim(b, "0123456789") == ""
		if an && bn {
			if len(a) < len(b) {
				return -1, true
			}
			if len(a) > len(b) {
				return 1, true
			}
		} else if an != bn {
			if an {
				return -1, true
			}
			return 1, true
		}
		if a < b {
			return -1, true
		}
		return 1, true
	}
	if len(xs) < len(ys) {
		return -1, true
	}
	return 1, true
}

func ValidateNewVersion(version string, history []Release) error {
	if _, _, ok := versionParts(version); !ok {
		return fmt.Errorf("新发布请使用语义版本，例如 1.0.0、1.0.1 或 1.1.0-rc.1")
	}
	for _, r := range history {
		cmp, ok := CompareVersion(version, r.Version)
		if version == r.Version || (ok && cmp == 0) {
			return fmt.Errorf("版本 %s 已有发布记录（包括失败记录），请使用新版本", version)
		}
		if ok && cmp < 0 {
			return fmt.Errorf("待发布版本 %s 低于历史版本 %s；请升级版本，恢复旧版本请使用发布记录中的回滚", version, r.Version)
		}
	}
	return nil
}

func SuggestVersion(source string, history []Release) string {
	base := source
	if _, _, ok := versionParts(base); !ok {
		base = "0.1.0"
	}
	for _, r := range history {
		if c, ok := CompareVersion(r.Version, base); ok && c >= 0 {
			base = r.Version
		}
	}
	if ValidateNewVersion(base, history) == nil {
		return base
	}
	n, pre, _ := versionParts(base)
	if pre != "" {
		candidate := fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2])
		if ValidateNewVersion(candidate, history) == nil {
			return candidate
		}
	}
	if n[2] == ^uint64(0) {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]+1)
}
