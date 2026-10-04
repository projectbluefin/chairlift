package troubleshoot

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"gopkg.in/yaml.v3"
)

// BackupSuffix is appended to the Goose configuration filename when saving
// a backup before repairing an existing configuration.
const BackupSuffix = ".chairlift-backup"

// decodeConfig rejects duplicate keys, multiple documents, and non-mapping
// roots before any user data can be changed. Nodes retain comments and unknown
// Goose settings; only diagnostic extension fields are edited.
func decodeConfig(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid YAML; repair the file in Goose before connecting tools")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the configuration must be a YAML mapping")
	}
	var values map[string]any
	if err := document.Decode(&values); err != nil {
		return nil, fmt.Errorf("invalid or duplicate configuration keys")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("the configuration must contain only one YAML document")
	}
	return &document, nil
}

func configField(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func hasConfigMerge(mapping *yaml.Node) bool {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].ShortTag() == "!!merge" {
			return true
		}
	}
	return false
}

func setConfigField(mapping *yaml.Node, key string, value any) {
	var node yaml.Node
	// Only YAML-encodable diagnostic literals are passed here.
	_ = node.Encode(value)
	if old := configField(mapping, key); old != nil {
		node.HeadComment, node.LineComment, node.FootComment = old.HeadComment, old.LineComment, old.FootComment
		*old = node
		return
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &node)
}

func prepareDiagnostics(data []byte, existing bool) ([]byte, error) {
	document, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	state := ParseConfig(data)
	if state.Wired {
		for _, command := range state.commands {
			if command != "" && !lookPath(command) {
				state.Wired = false
				break
			}
		}
		if state.Wired {
			return data, nil
		}
	}
	root := document.Content[0]
	if hasConfigMerge(root) {
		return nil, fmt.Errorf("merged configuration needs review in Goose before connecting tools")
	}
	extensions := configField(root, "extensions")
	if extensions == nil {
		extensions = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "extensions"}, extensions)
	}
	if extensions.Kind != yaml.MappingNode || hasConfigMerge(extensions) {
		return nil, fmt.Errorf("extensions must be a mapping; repair it in Goose")
	}
	command := resolveTool("linux-mcp-server")
	if command == "" {
		command = "linux-mcp-server"
	}
	found := false
	for _, key := range diagnosticExtensionKeys {
		node := configField(extensions, key)
		if node == nil {
			continue
		}
		found = true
		var extension gooseExt
		if node.Kind != yaml.MappingNode || hasConfigMerge(node) || node.Anchor != "" || node.Decode(&extension) != nil ||
			(extension.Type != "" && extension.Type != "stdio") ||
			(extension.Cmd != "" && filepath.Base(extension.Cmd) != "linux-mcp-server") {
			return nil, fmt.Errorf("%s conflicts with Linux diagnostics; review it in Goose", key)
		}
		if !existing && !extension.enabled() {
			return nil, fmt.Errorf("the shipped Linux diagnostic extension is disabled")
		}
		if extension.Cmd == "" || resolveTool(extension.Cmd) == "" {
			setConfigField(node, "cmd", command)
			extension.Cmd = command
		}
		extension.Type = "stdio"
		if len(extension.Args) == 0 {
			extension.Args = []string{"--toolset", "FIXED", "--no-search-for-ssh-key", "--verify-host-keys"}
		}
		if !extension.valid() {
			return nil, fmt.Errorf("%s has a conflicting diagnostic policy; select fixed tools without SSH in Goose", key)
		}
		setConfigField(node, "type", "stdio")
		setConfigField(node, "enabled", true)
		setConfigField(node, "args", extension.Args)
		if configField(node, "name") == nil {
			setConfigField(node, "name", key)
		}
		if configField(node, "envs") == nil {
			setConfigField(node, "envs", map[string]string{})
		}
	}
	if !found {
		if !existing {
			return nil, fmt.Errorf("the shipped configuration has no Linux diagnostic extension")
		}
		setConfigField(extensions, "linux-tools", map[string]any{
			"name": "linux-tools", "type": "stdio", "enabled": true, "bundled": false,
			"cmd": command, "args": []string{"--toolset", "FIXED", "--no-search-for-ssh-key", "--verify-host-keys"},
			"envs": map[string]string{}, "timeout": 300,
		})
	}
	prepared, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	if !ParseConfig(prepared).Wired {
		return nil, fmt.Errorf("linux diagnostics could not be connected")
	}
	return prepared, nil
}

func readUserConfig(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return nil, nil, fmt.Errorf("the configuration must be an owned regular file, not a link")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil, fmt.Errorf("the configuration changed while it was read; try again")
	}
	data, err := io.ReadAll(file)
	return data, info, err
}

// writeTemp writes data to a new private 0600 file in root and returns its
// name. On success the caller owns removing the temporary name.
func writeTemp(root *os.Root, data []byte) (string, error) {
	name := ".chairlift-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return name, nil
}

func writeAtomicBackup(root *os.Root, backupName string, data []byte) error {
	if bInfo, err := root.Lstat(backupName); err == nil {
		stat, ok := bInfo.Sys().(*syscall.Stat_t)
		if !bInfo.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
			return fmt.Errorf("the backup file must be an owned regular file, not a link or directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tempName, err := writeTemp(root, data)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tempName) }()
	return root.Rename(tempName, backupName)
}

// writeUserConfig installs a complete 0600 file atomically in the owned Goose
// directory. A fresh install cannot replace a file created by Goose meanwhile;
// an update refuses a changed file or snapshot rather than losing user edits.
// Before replacing an existing configuration, an atomic backup is saved to
// <filename>.chairlift-backup with 0600 permissions.
func writeUserConfig(path string, original []byte, previous os.FileInfo, prepared []byte) error {
	if dryrun.Enabled() {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("the Goose directory must be owned by you and not writable by others")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("the Goose directory changed; try again")
	}
	base := filepath.Base(path)
	if previous == nil {
		name, err := writeTemp(root, prepared)
		if err != nil {
			return err
		}
		defer func() { _ = root.Remove(name) }()
		// Link is atomic and refuses an existing destination; the temporary
		// name is then removed by the deferred cleanup.
		return root.Link(name, base)
	}
	if bytes.Equal(original, prepared) {
		return nil
	}
	current, err := root.Lstat(base)
	if err != nil || !os.SameFile(previous, current) || !current.Mode().IsRegular() {
		return fmt.Errorf("the Goose configuration changed during setup; try again")
	}
	observed, err := root.ReadFile(base)
	if err != nil || !bytes.Equal(original, observed) {
		return fmt.Errorf("the Goose configuration changed during setup; try again")
	}
	name, err := writeTemp(root, prepared)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	if err := writeAtomicBackup(root, base+BackupSuffix, original); err != nil {
		return fmt.Errorf("saving Goose configuration backup: %w", err)
	}
	return root.Rename(name, base)
}
