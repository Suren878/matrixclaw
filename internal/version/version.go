package version

import "strings"

var (
	Version = "0.1.0"
	Commit  = ""
	Date    = ""
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

func Current() Info {
	return Info{
		Version: strings.TrimSpace(Version),
		Commit:  strings.TrimSpace(Commit),
		Date:    strings.TrimSpace(Date),
	}
}

func String() string {
	info := Current()
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		return info.Version
	}
	return info.Version + " (" + info.Commit + ")"
}
