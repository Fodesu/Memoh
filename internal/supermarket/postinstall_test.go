package supermarket

import (
	"io"
	"strings"
	"testing"

	"github.com/memohai/memoh/internal/workspace/bridge"
	bridgepb "github.com/memohai/memoh/internal/workspace/bridgepb"
)

type postinstallTestStream struct {
	messages []*bridgepb.ExecOutput
	index    int
	sendDone bool
	closed   bool
}

func (s *postinstallTestStream) Recv() (*bridgepb.ExecOutput, error) {
	if s.index >= len(s.messages) {
		return nil, io.EOF
	}
	message := s.messages[s.index]
	s.index++
	return message, nil
}

func (s *postinstallTestStream) Close() error {
	s.closed = true
	return nil
}

func (s *postinstallTestStream) CloseSend() error {
	s.sendDone = true
	return nil
}

func TestValidatePackagePostinstall(t *testing.T) {
	valid := []PackagePostinstallCommand{{Command: "npm", Args: []string{"install", "--global", "opencli"}}}
	if err := validatePackagePostinstall(valid); err != nil {
		t.Fatalf("validatePackagePostinstall() error = %v", err)
	}
	for name, commands := range map[string][]PackagePostinstallCommand{
		"empty":        {},
		"shell":        {{Command: "sh", Args: []string{"-c", "echo unsafe"}}},
		"path":         {{Command: "/usr/bin/npm", Args: []string{}}},
		"missing args": {{Command: "npm"}},
		"control":      {{Command: "npm", Args: []string{"bad\nargument"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePackagePostinstall(commands); err == nil {
				t.Fatal("validatePackagePostinstall() accepted invalid commands")
			}
		})
	}
}

func TestPackagePostinstallCommandQuotesEveryToken(t *testing.T) {
	got := packagePostinstallCommand(PackagePostinstallCommand{
		Command: "npm",
		Args:    []string{"install", "it's quoted", ";", "$(touch /tmp/unsafe)", "`id`", ""},
	})
	want := "'npm' 'install' 'it'\"'\"'s quoted' ';' '$(touch /tmp/unsafe)' '`id`' ''"
	if got != want {
		t.Fatalf("packagePostinstallCommand() = %q, want %q", got, want)
	}
}

func TestPackagePostinstallJSONBytesMatchesProtocolEncoding(t *testing.T) {
	commands := []PackagePostinstallCommand{{Command: "opencli", Args: []string{`<>&"\\`, "日本語"}}}
	want := len(`[{"command":"opencli","args":["<>&\"\\\\","日本語"]}]`)
	if got := packagePostinstallJSONBytes(commands); got != want {
		t.Fatalf("packagePostinstallJSONBytes() = %d, want %d", got, want)
	}
}

func TestValidatePackagePostinstallWorkspaceRejectsWindows(t *testing.T) {
	commands := []PackagePostinstallCommand{{Command: "opencli", Args: []string{}}}
	if err := validatePackagePostinstallWorkspace(bridge.WorkspaceInfo{
		Backend: bridge.WorkspaceBackendRemote,
		OS:      "win32",
	}, commands); err == nil {
		t.Fatal("validatePackagePostinstallWorkspace() accepted a Windows workspace")
	}
	if err := validatePackagePostinstallWorkspace(bridge.WorkspaceInfo{
		Backend: bridge.WorkspaceBackendRemote,
		OS:      "linux",
	}, commands); err != nil {
		t.Fatalf("validatePackagePostinstallWorkspace() rejected Linux: %v", err)
	}
}

func TestReadPackagePostinstallOutputBoundsCombinedStreams(t *testing.T) {
	stream := &postinstallTestStream{messages: []*bridgepb.ExecOutput{
		{Stream: bridgepb.ExecOutput_STDOUT, Data: []byte("stdout")},
		{Stream: bridgepb.ExecOutput_STDERR, Data: []byte("stderr")},
		{Stream: bridgepb.ExecOutput_EXIT, ExitCode: 7},
	}}
	exitCode, outputBytes, err := readPackagePostinstallOutput(stream, maxPackagePostinstallOutputBytes)
	if err != nil || exitCode != 7 || outputBytes != len("stdoutstderr") {
		t.Fatalf("readPackagePostinstallOutput() = (%d, %d, %v)", exitCode, outputBytes, err)
	}

	overflow := &postinstallTestStream{messages: []*bridgepb.ExecOutput{
		{Stream: bridgepb.ExecOutput_STDOUT, Data: []byte(strings.Repeat("x", maxPackagePostinstallOutputBytes+1))},
	}}
	if _, _, err := readPackagePostinstallOutput(overflow, maxPackagePostinstallOutputBytes); err == nil {
		t.Fatal("readPackagePostinstallOutput() accepted oversized output")
	}
}

func TestReadPackagePostinstallOutputBoundsAllCommandsTogether(t *testing.T) {
	first := &postinstallTestStream{messages: []*bridgepb.ExecOutput{
		{Stream: bridgepb.ExecOutput_STDOUT, Data: []byte(strings.Repeat("x", 40*1024))},
		{Stream: bridgepb.ExecOutput_EXIT},
	}}
	_, used, err := readPackagePostinstallOutput(first, maxPackagePostinstallOutputBytes)
	if err != nil {
		t.Fatalf("first command output: %v", err)
	}
	second := &postinstallTestStream{messages: []*bridgepb.ExecOutput{
		{Stream: bridgepb.ExecOutput_STDERR, Data: []byte(strings.Repeat("y", 24*1024+1))},
	}}
	if _, _, err := readPackagePostinstallOutput(second, maxPackagePostinstallOutputBytes-used); err == nil {
		t.Fatal("readPackagePostinstallOutput() accepted output beyond the Package-wide limit")
	}
}
