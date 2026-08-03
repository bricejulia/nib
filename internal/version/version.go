// Package version holds nib's build version.
package version

// Version is the current build's version string. Override at build time
// via -ldflags "-X github.com/bricejulia/nib/internal/version.Version=1.2.3".
var Version = "dev"
