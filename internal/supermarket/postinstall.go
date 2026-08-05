package supermarket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/memohai/memoh/internal/workspace/bridge"
	bridgepb "github.com/memohai/memoh/internal/workspace/bridgepb"
)

const (
	maxPackagePostinstallCommands     = 8
	maxPackagePostinstallArgs         = 64
	maxPackagePostinstallCommandBytes = 128
	maxPackagePostinstallArgBytes     = 4 * 1024
	maxPackagePostinstallBytes        = 64 * 1024
	maxPackagePostinstallOutputBytes  = 64 * 1024
	packagePostinstallTimeout         = 10 * time.Minute
	packagePostinstallWorkDir         = "/data"
)

var (
	postinstallExecutablePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	unsupportedPostinstallExecutables = map[string]struct{}{
		"bash": {}, "cmd": {}, "cmd.exe": {}, "dash": {}, "env": {}, "fish": {},
		"powershell": {}, "powershell.exe": {}, "pwsh": {}, "sh": {}, "sudo": {}, "zsh": {},
	}
)

type packagePostinstallStream interface {
	Recv() (*bridgepb.ExecOutput, error)
	CloseSend() error
	Close() error
}

func validatePackagePostinstall(commands []PackagePostinstallCommand) error {
	if commands == nil {
		return nil
	}
	if len(commands) == 0 || len(commands) > maxPackagePostinstallCommands {
		return fmt.Errorf("package postinstall must contain between 1 and %d commands", maxPackagePostinstallCommands)
	}
	for index, item := range commands {
		if len(item.Command) == 0 || len(item.Command) > maxPackagePostinstallCommandBytes || !utf8.ValidString(item.Command) ||
			!postinstallExecutablePattern.MatchString(item.Command) || containsControlCharacter(item.Command) {
			return fmt.Errorf("package postinstall command %d has an invalid executable", index)
		}
		if _, unsupported := unsupportedPostinstallExecutables[strings.ToLower(item.Command)]; unsupported {
			return fmt.Errorf("package postinstall command %d uses an unsupported executable", index)
		}
		if item.Args == nil || len(item.Args) > maxPackagePostinstallArgs {
			return fmt.Errorf("package postinstall command %d has invalid arguments", index)
		}
		for _, arg := range item.Args {
			if len(arg) > maxPackagePostinstallArgBytes || !utf8.ValidString(arg) || containsControlCharacter(arg) {
				return fmt.Errorf("package postinstall command %d has an invalid argument", index)
			}
		}
	}
	if packagePostinstallJSONBytes(commands) > maxPackagePostinstallBytes {
		return errors.New("package postinstall exceeds its metadata limit")
	}
	return nil
}

func packagePostinstallJSONBytes(commands []PackagePostinstallCommand) int {
	size := len("[") + len("]")
	for commandIndex, item := range commands {
		if commandIndex > 0 {
			size += len(",")
		}
		size += len(`{"command":`) + jsonStringBytes(item.Command) + len(`,"args":[`)
		for argIndex, arg := range item.Args {
			if argIndex > 0 {
				size += len(",")
			}
			size += jsonStringBytes(arg)
		}
		size += len("]}")
	}
	return size
}

func jsonStringBytes(value string) int {
	size := len(value) + len(`""`)
	for index := range len(value) {
		if value[index] == '"' || value[index] == '\\' {
			size++
		}
	}
	return size
}

func containsControlCharacter(value string) bool {
	for index := range len(value) {
		if value[index] < 0x20 || value[index] == 0x7f {
			return true
		}
	}
	return false
}

func packagePostinstallCommand(item PackagePostinstallCommand) string {
	parts := make([]string, 0, len(item.Args)+1)
	parts = append(parts, shellQuotePostinstallToken(item.Command))
	for _, arg := range item.Args {
		parts = append(parts, shellQuotePostinstallToken(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuotePostinstallToken(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func validatePackagePostinstallWorkspace(info bridge.WorkspaceInfo, commands []PackagePostinstallCommand) error {
	if len(commands) == 0 || strings.TrimSpace(info.Backend) == "" || strings.EqualFold(info.Backend, bridge.WorkspaceBackendContainer) {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(info.OS)) {
	case "darwin", "linux":
		return nil
	case "win32", "windows":
		return errors.New("package postinstall is not supported on Windows workspaces")
	default:
		return fmt.Errorf("package postinstall is not supported on workspace OS %q", info.OS)
	}
}

func runPackagePostinstall(
	ctx context.Context,
	client *bridge.Client,
	commands []PackagePostinstallCommand,
	botID, registryID, packageID, revision string,
) error {
	if len(commands) == 0 {
		return nil
	}
	if client == nil {
		return errors.New("workspace is not reachable")
	}
	runCtx, cancel := context.WithTimeout(ctx, packagePostinstallTimeout)
	defer cancel()
	env := []string{
		"MEMOH_BOT_ID=" + botID,
		"MEMOH_REGISTRY_ID=" + registryID,
		"MEMOH_PACKAGE_ID=" + packageID,
		"MEMOH_PACKAGE_REVISION=" + revision,
	}
	remainingOutput := maxPackagePostinstallOutputBytes
	for index, item := range commands {
		stream, err := client.ExecStreamWithOptions(
			runCtx,
			packagePostinstallCommand(item),
			packagePostinstallWorkDir,
			-1,
			bridge.ExecOptions{Env: env},
		)
		if err != nil {
			if contextErr := packagePostinstallContextError(runCtx); contextErr != nil {
				return contextErr
			}
			return fmt.Errorf("start Package postinstall command %d (%s): %w", index+1, item.Command, err)
		}
		if err := stream.CloseSend(); err != nil {
			_ = stream.Close()
			if contextErr := packagePostinstallContextError(runCtx); contextErr != nil {
				return contextErr
			}
			return fmt.Errorf("close Package postinstall command %d (%s) stdin: %w", index+1, item.Command, err)
		}
		exitCode, outputBytes, readErr := readPackagePostinstallOutput(stream, remainingOutput)
		_ = stream.Close()
		if readErr != nil {
			if contextErr := packagePostinstallContextError(runCtx); contextErr != nil {
				return contextErr
			}
			return fmt.Errorf("run Package postinstall command %d (%s): %w", index+1, item.Command, readErr)
		}
		remainingOutput -= outputBytes
		if exitCode != 0 {
			return fmt.Errorf("package postinstall command %d (%s) exited with code %d", index+1, item.Command, exitCode)
		}
	}
	return nil
}

func packagePostinstallContextError(ctx context.Context) error {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return fmt.Errorf("package postinstall timed out: %w", context.DeadlineExceeded)
	case context.Canceled:
		return fmt.Errorf("package postinstall canceled: %w", context.Canceled)
	default:
		return nil
	}
}

func readPackagePostinstallOutput(stream packagePostinstallStream, maximum int) (int32, int, error) {
	outputBytes := 0
	var exitCode int32
	sawExit := false
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if !sawExit {
				return 0, outputBytes, errors.New("package postinstall ended without an exit status")
			}
			return exitCode, outputBytes, nil
		}
		if err != nil {
			return 0, outputBytes, err
		}
		switch message.GetStream() {
		case bridgepb.ExecOutput_STDOUT, bridgepb.ExecOutput_STDERR:
			data := message.GetData()
			if len(data) > maximum-outputBytes {
				return 0, outputBytes, fmt.Errorf("package postinstall output exceeds %d bytes", maxPackagePostinstallOutputBytes)
			}
			outputBytes += len(data)
		case bridgepb.ExecOutput_EXIT:
			exitCode = message.GetExitCode()
			sawExit = true
		}
	}
}
