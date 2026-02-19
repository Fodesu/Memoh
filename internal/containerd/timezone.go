package containerd

import (
	"os"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// TimezoneSpecOpts returns OCI spec options that propagate the host timezone
// into the container via /etc/localtime bind-mount and TZ environment variable.
func TimezoneSpecOpts() []oci.SpecOpts {
	var opts []oci.SpecOpts
	if _, err := os.Stat("/etc/localtime"); err == nil {
		opts = append(opts, oci.WithMounts([]specs.Mount{{
			Destination: "/etc/localtime",
			Type:        "bind",
			Source:      "/etc/localtime",
			Options:     []string{"rbind", "ro"},
		}}))
	}
	if tz := os.Getenv("TZ"); tz != "" {
		opts = append(opts, oci.WithEnv([]string{"TZ=" + tz}))
	}
	return opts
}
