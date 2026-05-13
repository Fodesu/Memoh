package handlers

import "testing"

func TestResolveContainerPathUsesPOSIXSeparators(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "root", path: `\`, want: "/"},
		{name: "windows separators", path: `\data\projects`, want: "/data/projects"},
		{name: "mixed separators", path: `/data\projects//demo`, want: "/data/projects/demo"},
		{name: "relative path", path: `data\projects`, want: "/data/projects"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveContainerPath(tt.path)
			if err != nil {
				t.Fatalf("resolveContainerPath(%q) error = %v", tt.path, err)
			}
			if got != tt.want {
				t.Fatalf("resolveContainerPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFSFileInfoFromEntryUsesPOSIXSeparators(t *testing.T) {
	got := fsFileInfoFromEntry("/data", "projects", true, 0, "drwxr-xr-x", "2026-05-13T00:00:00Z")
	if got.Path != "/data/projects" {
		t.Fatalf("fsFileInfoFromEntry path = %q, want %q", got.Path, "/data/projects")
	}
}

func TestIsContainerMediaPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/data/media", want: true},
		{path: "/data/media/0f/demo.jpg", want: true},
		{path: "data/media/0f/demo.jpg", want: true},
		{path: "/data/mediakit/demo.jpg", want: false},
		{path: "/etc/passwd", want: false},
	}

	for _, tt := range tests {
		if got := isContainerMediaPath(tt.path); got != tt.want {
			t.Fatalf("isContainerMediaPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
