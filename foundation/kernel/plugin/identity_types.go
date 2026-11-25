package plugin

import (
	"fmt"
	"slices"
	"strings"

	"horizon/foundation/kernel/capability/contracts"
)

func (r *Registrar) identityTypes() ([]IdentityType, error) {
	if len(r.manifest.IdentityTypes) == 0 {
		return nil, nil
	}
	if !slices.Contains(r.manifest.Extensions, "identity-type") {
		return nil, fmt.Errorf("plugin %s has no identity-type extension", r.manifest.ID)
	}
	for _, identityType := range r.manifest.IdentityTypes {
		if !idPattern.MatchString(identityType.Code) || !strings.HasPrefix(identityType.Code, r.manifest.ID+".") ||
			!versionPattern.MatchString(identityType.Version) || !versionPattern.MatchString(identityType.VerifyVersion) || identityType.RevocationEvent == "" ||
			len([]rune(identityType.DisplayName)) > 100 || identityType.SubjectScope != "" && identityType.SubjectScope != "tenant" ||
			identityType.ValidForMinutes < 0 || identityType.ValidForMinutes > 525600 || identityType.ReviewRequired && identityType.ValidForMinutes == 0 {
			return nil, fmt.Errorf("plugin %s has an invalid identity type %q", r.manifest.ID, identityType.Code)
		}
		var verifies, revokes bool
		for _, entry := range r.entries {
			definition := entry.Definition
			if definition.Name == identityType.Verify && definition.Version == identityType.VerifyVersion && validBindingVerifier(definition) {
				verifies = true
			}
			if slices.Contains(definition.SuccessEvents, identityType.RevocationEvent) {
				revokes = true
			}
		}
		if !verifies || !revokes {
			return nil, fmt.Errorf("plugin %s identity type %q requires an owned verifier and revocation event", r.manifest.ID, identityType.Code)
		}
	}
	return r.manifest.IdentityTypes, nil
}

func validBindingVerifier(definition contracts.Definition) bool {
	if !definition.ReadOnly || definition.Risk != contracts.RiskLow || definition.Permission == "" || !definition.AllowsOrigin(contracts.OriginCapability) || definition.Input == nil || definition.Output == nil {
		return false
	}
	id := definition.Input.Properties["id"]
	ready := definition.Output.Properties["ready"]
	return id != nil && id.Type == "integer" && ready != nil && ready.Type == "boolean" && slices.Contains(definition.Output.Required, "ready")
}
