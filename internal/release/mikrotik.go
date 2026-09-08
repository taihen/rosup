package release

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const (
	newestLongTermURL = "https://upgrade.mikrotik.com/routeros/NEWEST6.long-term"
	downloadBaseURL   = "https://download.mikrotik.com/routeros"
)

// ExtraPackages are probed when the version directory has no listing.
var ExtraPackages = []string{
	"routeros",
	"wireless",
	"dhcp",
	"security",
	"routing",
	"mpls",
	"ppp",
	"ipv6",
	"hotspot",
	"advanced-tools",
	"ntp",
	"ups",
	"user-manager",
	"lcd",
	"gps",
	"tr069-client",
	"openflow",
	"multicast",
	"system",
	"iot",
	"lora",
	"calea",
	"dude",
}

var knownArchs = []string{
	"mipsbe", "smips", "mmips", "mipsle", "ppc", "tile", "arm64", "arm", "x86",
}

var (
	npkNameRe       = regexp.MustCompile(`(?i)[A-Za-z0-9._+-]+\.npk`)
	newestVersionRe = regexp.MustCompile(`^6\.\d+(\.\d+)*$`)
)

func NewestURL() string {
	return newestLongTermURL
}

func DirectoryURL(version string) string {
	return downloadBaseURL + "/" + url.PathEscape(version) + "/"
}

func PackageURL(version, arch, pkg string) string {
	dir := strings.TrimRight(DirectoryURL(version), "/")
	name := packageFileName(version, arch, pkg)
	return dir + "/" + name
}

func packageFileName(version, arch, pkg string) string {
	if pkg == "routeros" {
		return "routeros-" + arch + "-" + version + ".npk"
	}
	if arch == "x86" {
		return pkg + "-" + version + ".npk"
	}
	return pkg + "-" + version + "-" + arch + ".npk"
}

func packageFileNames(version, arch, pkg string) []string {
	primary := packageFileName(version, arch, pkg)
	out := []string{primary}
	seen := map[string]struct{}{primary: {}}
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if pkg == "routeros" {
		add("routeros-" + arch + "-" + version + ".npk")
		add("routeros-" + version + "-" + arch + ".npk")
		if arch == "x86" {
			add("routeros-" + version + ".npk")
		}
	} else if arch == "x86" {
		add(pkg + "-" + version + ".npk")
		add(pkg + "-" + version + "-" + arch + ".npk")
		add(pkg + "-" + arch + "-" + version + ".npk")
	} else {
		add(pkg + "-" + version + "-" + arch + ".npk")
		add(pkg + "-" + arch + "-" + version + ".npk")
	}
	return out
}

func ParseNewest(body []byte) (string, error) {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("release: empty NEWEST file")
	}
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", fmt.Errorf("release: empty NEWEST file")
	}
	v := fields[0]
	if strings.HasPrefix(v, "7.") {
		return "", fmt.Errorf("release: RouterOS 7 is not supported: %s", v)
	}
	if !newestVersionRe.MatchString(v) {
		return "", fmt.Errorf("release: unsupported version %s", v)
	}
	return v, nil
}

func ParseListing(html []byte) []string {
	matches := npkNameRe.FindAllString(string(html), -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := path.Base(m)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func FilterByArch(names []string, arch, version string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		_, fileArch, ok := ParseNPKName(name, version)
		if !ok {
			continue
		}
		if fileArch == arch {
			out = append(out, name)
		}
	}
	return out
}

func ParseNPKName(name, version string) (pkg, arch string, ok bool) {
	if !strings.HasSuffix(strings.ToLower(name), ".npk") {
		return "", "", false
	}
	base := name[:len(name)-len(".npk")]
	verSuffix := "-" + version

	for _, a := range knownArchs {
		rest, found := strings.CutSuffix(base, "-"+a)
		if !found || rest == "" {
			continue
		}
		pkgName, foundVer := strings.CutSuffix(rest, verSuffix)
		if foundVer && pkgName != "" {
			return pkgName, a, true
		}
	}

	rest, found := strings.CutSuffix(base, verSuffix)
	if !found || rest == "" {
		return "", "", false
	}
	for _, a := range knownArchs {
		pkgName, foundArch := strings.CutSuffix(rest, "-"+a)
		if foundArch && pkgName != "" {
			return pkgName, a, true
		}
	}
	return rest, "x86", true
}
