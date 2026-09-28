package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	vcs := func(ver string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: ver}, Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef0123"},
				{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
			}}, true
		}
	}
	// Release build: ldflags win.
	if got := resolve("v0.1.0", "abc", "2026-09-25", vcs("(devel)")); got.Version != "v0.1.0" || got.Commit != "abc" {
		t.Errorf("ldflags: %+v", got)
	}
	// go install ...@v0.1.0
	if got := resolve("", "", "", vcs("v0.1.0")); got.Version != "v0.1.0" || got.Commit != "0123456789abcdef0123" {
		t.Errorf("module version: %+v", got)
	}
	// Untagged build (V7): 0.0.0-dev+<sha>.
	got := resolve("", "", "", vcs("(devel)"))
	if got.Version != "0.0.0-dev+0123456789ab" || got.Date != "2026-01-02T03:04:05Z" {
		t.Errorf("dev: %+v", got)
	}
	if !strings.HasPrefix(got.String(), "skenv 0.0.0-dev+") {
		t.Errorf("String() = %q", got.String())
	}
}
