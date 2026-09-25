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
	updates := mappingValue(top, "updates_page")
	for _, name := range []string{"bootc_status_group", "channel_group"} {
		group := mappingValue(legacy, name)
		if group == nil || group.Kind != yaml.MappingNode {
			continue
		}
		if updates == nil {
			updates = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			top.Content = append(top.Content, stringKey("updates_page"), updates)
		} else if updates.Kind != yaml.MappingNode {
			*updates = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		current := mappingValue(updates, name)
		if current == nil {
			updates.Content = append(updates.Content, stringKey(name), group)
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
