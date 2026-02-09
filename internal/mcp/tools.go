package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type FSReadInput struct {
	Path string `json:"path" jsonschema:"relative file path"`
}

type FSReadOutput struct {
	Content string `json:"content" jsonschema:"file content"`
}

type FSWriteInput struct {
	Path    string `json:"path" jsonschema:"relative file path"`
	Content string `json:"content" jsonschema:"file content"`
}

type FSWriteOutput struct {
	OK bool `json:"ok" jsonschema:"write result"`
}

type FSListInput struct {
	Path      string `json:"path" jsonschema:"relative directory path"`
	Recursive bool   `json:"recursive" jsonschema:"recursive listing"`
}

type FSFileEntry struct {
	Path    string    `json:"path" jsonschema:"relative entry path"`
	IsDir   bool      `json:"is_dir" jsonschema:"is directory"`
	Size    int64     `json:"size" jsonschema:"entry size"`
	Mode    uint32    `json:"mode" jsonschema:"file mode"`
	ModTime time.Time `json:"mod_time" jsonschema:"modification time"`
}

type FSListOutput struct {
	Path    string        `json:"path" jsonschema:"listed path"`
	Entries []FSFileEntry `json:"entries" jsonschema:"entries"`
}

type FSEditInput struct {
	Path    string `json:"path" jsonschema:"relative file path"`
	OldText string `json:"old_text" jsonschema:"exact text to find"`
	NewText string `json:"new_text" jsonschema:"replacement text"`
}

type FSEditOutput struct {
	OK bool `json:"ok" jsonschema:"apply result"`
}

type ExecInput struct {
	Command string   `json:"command" jsonschema:"command to run"`
	Args    []string `json:"args" jsonschema:"command arguments"`
}

type ExecOutput struct {
	OK       bool   `json:"ok" jsonschema:"execution success"`
	ExitCode int    `json:"exit_code" jsonschema:"process exit code"`
	Stdout   string `json:"stdout" jsonschema:"standard output"`
	Stderr   string `json:"stderr" jsonschema:"standard error"`
}

func RegisterTools(server *sdkmcp.Server) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "read", Description: "read file content"}, fsReadTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "write", Description: "write file content"}, fsWriteTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "list", Description: "list directory entries"}, fsListTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "edit", Description: "replace exact text in a file"}, fsEditTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "exec", Description: "execute command"}, execTool)
}

func fsReadTool(ctx context.Context, req *sdkmcp.CallToolRequest, input FSReadInput) (
	*sdkmcp.CallToolResult,
	FSReadOutput,
	error,
) {
	root := dataRoot()
	target, err := resolvePath(root, input.Path)
	if err != nil {
		return nil, FSReadOutput{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, FSReadOutput{}, err
	}
	return nil, FSReadOutput{Content: string(data)}, nil
}

func fsWriteTool(ctx context.Context, req *sdkmcp.CallToolRequest, input FSWriteInput) (
	*sdkmcp.CallToolResult,
	FSWriteOutput,
	error,
) {
	root := dataRoot()
	target, err := resolvePath(root, input.Path)
	if err != nil {
		return nil, FSWriteOutput{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, FSWriteOutput{}, err
	}
	if err := os.WriteFile(target, []byte(input.Content), 0o644); err != nil {
		return nil, FSWriteOutput{}, err
	}
	return nil, FSWriteOutput{OK: true}, nil
}

func fsListTool(ctx context.Context, req *sdkmcp.CallToolRequest, input FSListInput) (
	*sdkmcp.CallToolResult,
	FSListOutput,
	error,
) {
	root := dataRoot()
	target, err := resolvePathAllowRoot(root, input.Path)
	if err != nil {
		return nil, FSListOutput{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, FSListOutput{}, err
	}
	if !info.IsDir() {
		return nil, FSListOutput{}, fmt.Errorf("path is not a directory")
	}

	entries := []FSFileEntry{}
	if input.Recursive {
		err = filepath.WalkDir(target, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if p == target {
				return nil
			}
			entryInfo, err := d.Info()
			if err != nil {
				return err
			}
			entry, err := entryForPath(root, p, entryInfo)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
			return nil
		})
	} else {
		dirEntries, err := os.ReadDir(target)
		if err != nil {
			return nil, FSListOutput{}, err
		}
		for _, entry := range dirEntries {
			entryInfo, err := entry.Info()
			if err != nil {
				return nil, FSListOutput{}, err
			}
			fullPath := filepath.Join(target, entry.Name())
			fileEntry, err := entryForPath(root, fullPath, entryInfo)
			if err != nil {
				return nil, FSListOutput{}, err
			}
			entries = append(entries, fileEntry)
		}
	}
	if err != nil {
		return nil, FSListOutput{}, err
	}

	listedPath := strings.TrimSpace(input.Path)
	if listedPath == "" {
		listedPath = "."
	}
	return nil, FSListOutput{Path: listedPath, Entries: entries}, nil
}

func fsEditTool(ctx context.Context, req *sdkmcp.CallToolRequest, input FSEditInput) (
	*sdkmcp.CallToolResult,
	FSEditOutput,
	error,
) {
	root := dataRoot()
	target, err := resolvePath(root, input.Path)
	if err != nil {
		return nil, FSEditOutput{}, err
	}
	orig, err := os.ReadFile(target)
	if err != nil {
		return nil, FSEditOutput{}, err
	}
	raw := string(orig)
	bom, content := stripBOM(raw)
	originalEnding := detectLineEnding(content)
	normalizedContent := normalizeToLF(content)
	normalizedOld := normalizeToLF(input.OldText)
	normalizedNew := normalizeToLF(input.NewText)

	match := fuzzyFindText(normalizedContent, normalizedOld)
	if !match.Found {
		return nil, FSEditOutput{}, fmt.Errorf(
			"could not find the exact text in %s. the old text must match exactly including all whitespace and newlines",
			input.Path,
		)
	}

	fuzzyContent := normalizeForFuzzyMatch(normalizedContent)
	fuzzyOld := normalizeForFuzzyMatch(normalizedOld)
	occurrences := strings.Count(fuzzyContent, fuzzyOld)
	if occurrences > 1 {
		return nil, FSEditOutput{}, fmt.Errorf(
			"found %d occurrences of the text in %s. the text must be unique. please provide more context to make it unique",
			occurrences,
			input.Path,
		)
	}

	baseContent := match.ContentForReplacement
	updated := baseContent[:match.Index] + normalizedNew + baseContent[match.Index+match.MatchLength:]
	if baseContent == updated {
		return nil, FSEditOutput{}, fmt.Errorf(
			"no changes made to %s. the replacement produced identical content. this might indicate an issue with special characters or the text not existing as expected",
			input.Path,
		)
	}

	finalContent := bom + restoreLineEndings(updated, originalEnding)
	info, err := os.Stat(target)
	if err != nil {
		return nil, FSEditOutput{}, err
	}
	if err := os.WriteFile(target, []byte(finalContent), info.Mode().Perm()); err != nil {
		return nil, FSEditOutput{}, err
	}
	return nil, FSEditOutput{OK: true}, nil
}

func execTool(ctx context.Context, req *sdkmcp.CallToolRequest, input ExecInput) (
	*sdkmcp.CallToolResult,
	ExecOutput,
	error,
) {
	if strings.TrimSpace(input.Command) == "" {
		return nil, ExecOutput{}, fmt.Errorf("command is required")
	}
	cmd := exec.CommandContext(ctx, input.Command, input.Args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, ExecOutput{
				OK:       false,
				ExitCode: exitErr.ExitCode(),
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
			}, nil
		}
		return nil, ExecOutput{}, err
	}

	return nil, ExecOutput{
		OK:       true,
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func dataRoot() string {
	root := strings.TrimSpace(os.Getenv("MCP_DATA_DIR"))
	if root == "" {
		root = "/data"
	}
	return root
}

func resolvePathAllowRoot(root, requestPath string) (string, error) {
	if strings.TrimSpace(requestPath) == "" {
		return root, nil
	}
	return resolvePath(root, requestPath)
}

func resolvePath(root, requestPath string) (string, error) {
	clean := filepath.Clean(requestPath)
	if clean == "." || clean == "" {
		return "", os.ErrInvalid
	}
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", os.ErrInvalid
	}
	return filepath.Join(root, clean), nil
}

func entryForPath(root, target string, info os.FileInfo) (FSFileEntry, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return FSFileEntry{}, err
	}
	if strings.HasPrefix(rel, "..") {
		return FSFileEntry{}, os.ErrInvalid
	}
	if rel == "." {
		rel = ""
	}
	return FSFileEntry{
		Path:    filepath.ToSlash(rel),
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		Mode:    uint32(info.Mode().Perm()),
		ModTime: info.ModTime(),
	}, nil
}

type FuzzyMatchResult struct {
	Found                 bool
	Index                 int
	MatchLength           int
	UsedFuzzyMatch        bool
	ContentForReplacement string
}

func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 {
		return "\n"
	}
	if crlfIdx == -1 {
		return "\n"
	}
	if crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

func stripBOM(content string) (string, string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[1:]
	}
	return "", content
}

func normalizeForFuzzyMatch(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	trimmed := strings.Join(lines, "\n")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u2018', '\u2019', '\u201A', '\u201B':
			return '\''
		case '\u201C', '\u201D', '\u201E', '\u201F':
			return '"'
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			return '-'
		case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
			return ' '
		default:
			return r
		}
	}, trimmed)
}

func fuzzyFindText(content, oldText string) FuzzyMatchResult {
	exactIndex := strings.Index(content, oldText)
	if exactIndex != -1 {
		return FuzzyMatchResult{
			Found:                 true,
			Index:                 exactIndex,
			MatchLength:           len(oldText),
			UsedFuzzyMatch:        false,
			ContentForReplacement: content,
		}
	}

	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOld := normalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOld)
	if fuzzyIndex == -1 {
		return FuzzyMatchResult{
			Found:                 false,
			Index:                 -1,
			MatchLength:           0,
			UsedFuzzyMatch:        false,
			ContentForReplacement: content,
		}
	}
	return FuzzyMatchResult{
		Found:                 true,
		Index:                 fuzzyIndex,
		MatchLength:           len(fuzzyOld),
		UsedFuzzyMatch:        true,
		ContentForReplacement: fuzzyContent,
	}
}
