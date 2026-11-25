package deployment

import (
	"horizon/applications/articles"
	"horizon/applications/projects"
	ar "horizon/foundation/kernel/applicationruntime"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/runtime/app"
	"strings"
	"testing"
)

func fixture() ([]ar.ApplicationV2, []contracts.Entity, []contracts.Definition) {
	registration := Register(nil)
	entries := (app.CapabilityModules{Registration: registration}).Entries()
	defs := []contracts.Definition{}
	for _, entry := range entries {
		defs = append(defs, entry.Definition)
	}
	// Keep exercising the historical article contract without registering it in the deployment.
	for _, entry := range (&articles.Module{}).Entries() {
		defs = append(defs, entry.Definition)
	}
	entities := append(app.EntitiesWithRegistration(registration), articles.Entities()...)
	return []ar.ApplicationV2{articles.RuntimeApplication(), projects.RuntimeApplication()}, entities, defs
}

func TestArticleManagementIsAbsentFromDeployment(t *testing.T) {
	registration := Register(nil)
	for _, source := range app.RuntimeContributions(registration) {
		if source.ID == "articles" || source.Source == "builtin:articles" {
			t.Fatal("article management is still in the Runtime catalog")
		}
	}
	for _, entry := range (app.CapabilityModules{Registration: registration}).Entries() {
		if strings.HasPrefix(entry.Definition.Name, "article.") {
			t.Fatalf("retired article capability is still registered: %s", entry.Definition.Name)
		}
	}
	for _, workflow := range WorkflowDefinitions() {
		if strings.HasPrefix(workflow.Name, "article-") {
			t.Fatalf("retired article workflow is still registered: %s", workflow.Name)
		}
	}
}

func TestAssembleUsesOnlyProtocolApplications(t *testing.T) {
	sources, entities, defs := fixture()
	apps, err := ar.Assemble(sources, entities, defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Fatal("second module missing")
	}
	for _, app := range apps {
		if app.Protocol == nil || app.SchemaVersion != "3.0" {
			t.Fatal(app.ID, app.SchemaVersion)
		}
		if len(app.Pages) != 0 || len(app.Providers) != 0 || len(app.Extensions) != 0 {
			t.Fatal("legacy execution fields remain", app.ID)
		}
		if app.MetadataVersion == "" {
			t.Fatal("metadata digest missing", app.ID)
		}
	}
}

func TestProtocolFormCommandsBindCapabilityContracts(t *testing.T) {
	sources, entities, defs := fixture()
	apps, err := ar.Assemble(sources, entities, defs)
	if err != nil {
		t.Fatal(err)
	}
	article := apps[0]
	component := article.Protocol.Pages[0].Root.Layout.Children[1].Layout.Children[0].Component
	forms := component.Props["forms"].(map[string]any)
	create := forms["create"].(map[string]any)["create"].(map[string]any)
	edit := forms["create"].(map[string]any)["edit"].(map[string]any)
	if create["permission"] != "articles:write" || edit["permission"] != "articles:write" {
		t.Fatal(create, edit)
	}
	if create["inputSchema"] == "" || edit["inputSchema"] == "" {
		t.Fatal("capability input schema missing")
	}
	inputs := edit["input"].([]any)
	foundRevision := false
	for _, value := range inputs {
		mapping := value.(map[string]any)
		if mapping["target"] == "revision" {
			foundRevision = true
		}
	}
	if !foundRevision {
		t.Fatal(edit)
	}
}

func TestProtocolInvokeEventsCarryCapabilityInputSchemas(t *testing.T) {
	sources, entities, defs := fixture()
	apps, err := ar.Assemble(sources, entities, defs)
	if err != nil {
		t.Fatal(err)
	}
	project := apps[1]
	var binding ar.EventBinding
	for _, page := range project.Protocol.Pages {
		if page.ID != "project.detail" {
			continue
		}
		members, _ := ar.FindAIComponent(page.Root, "project-members")
		if members == nil {
			t.Fatal("project members component missing")
		}
		for _, event := range members.Events {
			if event.Event == "addMember" {
				binding = event
			}
		}
	}
	if binding.InputSchema == "" || !strings.Contains(binding.InputSchema, `"projectId":{"type":"integer"`) {
		t.Fatal(binding.InputSchema)
	}
}

func TestCatalogRejectsUnsafeProtocolDefinitions(t *testing.T) {
	sources, entities, defs := fixture()
	cases := []struct {
		name   string
		mutate func([]ar.ApplicationV2) []ar.ApplicationV2
	}{
		{"missing protocol", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol = nil
			return a
		}},
		{"duplicate app", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			return append(a, a[0])
		}},
		{"unknown version", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].SchemaVersion = "2.3"
			return a
		}},
		{"unknown data provider", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol.Pages[0].Root.Layout.Data[0].Provider = "absent"
			return a
		}},
		{"unknown capability version", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol.Providers[0].Capability.Version = "99.0.0"
			return a
		}},
		{"unknown invoke target", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol.Pages[1].Root.Layout.Children[0].Layout.Children[0].Component.Events[1].Target = "article.decide@99.0.0"
			return a
		}},
		{"unsafe event payload", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol.Pages[1].Root.Layout.Children[0].Layout.Children[0].Component.Events[1].Payload =
				ar.Expression{Kind: "path", Path: "event.__proto__.polluted"}
			return a
		}},
		{"unknown navigation target", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Protocol.Pages[0].Root.Layout.Children[1].Layout.Children[0].Component.Events[0].Target = "missing.page"
			return a
		}},
		{"cycle", func(a []ar.ApplicationV2) []ar.ApplicationV2 {
			a[0].Dependencies = []string{"projects"}
			a[1].Dependencies = []string{"articles"}
			return a
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ar.Assemble(test.mutate(ar.Canonical(sources)), entities, defs); err == nil {
				t.Fatal("invalid contribution accepted")
			}
		})
	}
}
