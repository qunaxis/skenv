// Package buildinfo reports the skenv version (V7).
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// Set by goreleaser through -ldflags "-X ...".
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Info is the resolved build information.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// Get returns the ldflags values, falling back to the Go module version
// (`go install ...@vX.Y.Z`) and to 0.0.0-dev+<sha> for untagged builds.
func Get() Info {
	return resolve(Version, Commit, Date, debug.ReadBuildInfo)
}

func resolve(version, commit, date string, read func() (*debug.BuildInfo, bool)) Info {
	info := Info{Version: version, Commit: commit, Date: date}
	bi, ok := read()
	if ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			}
		}
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
	}
	if info.Version == "" {
		short := info.Commit
		if len(short) > 12 {
			short = short[:12]
		}
		if short == "" {
			short = "unknown"
		}
		info.Version = "0.0.0-dev+" + short
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Date == "" {
		info.Date = "unknown"
	}
	return info
}

// String formats Info for `skenv version`.
func (i Info) String() string {
	return fmt.Sprintf("skenv %s\ncommit: %s\nbuilt:  %s", i.Version, i.Commit, i.Date)
}
