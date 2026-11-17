package studio

import (
	"testing"

	"horizon/foundation/kernel/capability/contracts"
)

func TestValidateDefinitions(t *testing.T) {
	m := New(nil)
	for _, entry := range m.Entries() {
		if problems := entry.Definition.Normalized().Problems(); len(problems) > 0 {
			t.Errorf("%s: %v", entry.Definition.Name, problems)
		}
		_ = contracts.Problem("x")
	}
}
