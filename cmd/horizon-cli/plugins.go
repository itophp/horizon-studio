package main

import (
	"fmt"
	"slices"
	"strings"

	"horizon/deployment"
	"horizon/foundation/kernel/plugin"
)

func inspectPlugins(args []string) error {
	var manifests []plugin.Manifest
	for _, instance := range deployment.Register(nil).Plugins.Plugins() {
		manifests = append(manifests, instance.Manifest())
	}
	slices.SortFunc(manifests, func(a, b plugin.Manifest) int { return strings.Compare(a.ID, b.ID) })
	result := catalog{SchemaVersion: 1, Source: "compiled", Items: []item{}}
	for _, manifest := range manifests {
		result.Items = append(result.Items, item{
			Kind: "plugin", ID: manifest.ID, Version: manifest.Version,
			Definition: manifest,
		})
	}
	if len(args) == 0 {
		return printJSON(result)
	}
	if len(args) != 2 || args[0] != "schema" {
		return fmt.Errorf("expected plugin or plugin schema id@version")
	}
	for _, entry := range result.Items {
		if entry.ID+"@"+entry.Version == args[1] {
			return printJSON(map[string]any{"entry": entry})
		}
	}
	return fmt.Errorf("plugin %q not found", args[1])
}
