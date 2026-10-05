package config

import "gopkg.in/yaml.v3"

// system_page is a compatibility input, not a navigable page. Validate its
// historical inventory before migration, including values later superseded
// or retired, so compatibility cannot conceal typos or bypass sudo checks.
func validateLegacySystemPage(src configSource, value *yaml.Node) *LoadError {
	if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return validatorPageValueShapeError(src.path, "system_page", value)
	}
	return validateNamedGroupEntries(src, []string{
		"system_info_group", "bootc_status_group", "channel_group", "health_group",
	}, value)
}

// legacyGroups are groups a current page still accepts under their old
// configuration home, each validated with that page's ordinary group rules
// and then moved by migrateLegacyGroups. troubleshooting_group (Ask
// Bluefin's Goose row) moved from Help to the Agents page, beside the model
// it runs on.
var legacyGroups = []legacyGroup{
	{From: "help_page", To: "agents_page", Group: "troubleshooting_group"},
}

type legacyGroup struct {
	From, To, Group string
}

// acceptedGroups returns the groups a page's mapping may name: its schema
// groups plus any legacy group whose old home it is.
func acceptedGroups(page string, groups []string) []string {
	for _, legacy := range legacyGroups {
		if legacy.From == page {
			groups = append(groups, legacy.Group)
		}
	}
	return groups
}

// migrateLegacySystemPage runs only after source-graph and schema validation.
// The effective tree is alias-free and privately owned. Move surviving groups
// to Updates, preserving explicit false values. Current non-null fields win;
// nulls remain no-op overlays, just as they are in the ordinary config merge.
// No source file is rewritten and retired groups never reach runtime Config.
func migrateLegacySystemPage(top *yaml.Node) {
	legacy := mappingValue(top, "system_page")
	if legacy == nil || legacy.Kind != yaml.MappingNode {
		return
	}
	for _, name := range []string{"bootc_status_group", "channel_group"} {
		moveGroup(top, legacy, "updates_page", name)
	}
}

// migrateLegacyGroups moves each legacy group to its current page with the
// same precedence as migrateLegacySystemPage, then drops it from its old
// home so it never reaches the runtime Config twice.
func migrateLegacyGroups(top *yaml.Node) {
	for _, legacy := range legacyGroups {
		from := mappingValue(top, legacy.From)
		if from == nil || from.Kind != yaml.MappingNode {
			continue
		}
		moveGroup(top, from, legacy.To, legacy.Group)
		removeKey(from, legacy.Group)
	}
}

// moveGroup merges from's group name into page's group of the same name:
// current non-null fields win, and a null current field takes the legacy
// value.
func moveGroup(top, from *yaml.Node, page, name string) {
	group := mappingValue(from, name)
	if group == nil || group.Kind != yaml.MappingNode {
		return
	}
	target := mappingValue(top, page)
	if target == nil {
		target = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		top.Content = append(top.Content, stringKey(page), target)
	} else if target.Kind != yaml.MappingNode {
		*target = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	current := mappingValue(target, name)
	if current == nil {
		target.Content = append(target.Content, stringKey(name), group)
	} else if current.Kind != yaml.MappingNode {
		*current = *group
	} else {
		for i := 0; i < len(group.Content); i += 2 {
			key, value := group.Content[i], group.Content[i+1]
			field := mappingValue(current, key.Value)
			if field == nil {
				current.Content = append(current.Content, key, value)
			} else if field.Tag == "!!null" {
				*field = *value
			}
		}
	}
}

func removeKey(node *yaml.Node, name string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func mappingValue(node *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func stringKey(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
}
