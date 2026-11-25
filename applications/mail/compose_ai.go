package mail

import (
	"context"
	"strings"
	"time"
	"unicode"

	"horizon/foundation/kernel/capability/contracts"
)

const draftSuggestionCapability = "mail.message.suggest-draft"

func draftSuggestionDefinition() contracts.Definition {
	def := analysisDefinition(draftSuggestionCapability, "Suggest email draft",
		"Suggest subject, body, and explicitly supplied recipient addresses from the sender's writing brief.")
	def.Layer = contracts.LayerAtomic
	def.Permission = "mail:send"
	def.Input = contracts.Object(map[string]*contracts.Schema{
		"sourceText": contracts.Str(1, 2000), "locale": contracts.Enum("zh-CN", "en"),
		"includeSubject": contracts.Bool(), "includeTo": contracts.Bool(), "includeCc": contracts.Bool(),
	}, "sourceText", "locale", "includeSubject")
	def.Output = contracts.Object(map[string]*contracts.Schema{
		"suggestions": contracts.Array(contracts.Object(map[string]*contracts.Schema{
			"field": contracts.Enum("subject", "body", "to", "cc"), "value": contracts.Str(1, 4000),
		}, "field", "value"), 0, 4),
	}, "suggestions")
	return def
}

func (m *Module) suggestDraft(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	brief := strings.TrimSpace(in["sourceText"].(string))
	if brief == "" {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	includeSubject := in["includeSubject"].(bool)
	prompt := "Draft a concise email from the writing brief. The brief describes what the email should communicate. " +
		"Do not invent names, dates, links, facts, or commitments. Return an empty suggestions array when the brief lacks actionable context. " +
		"Return a body suggestion as plain text with line breaks and no subject line. "
	if includeSubject {
		prompt += "Include a separate single-line subject suggestion of at most 998 characters."
	} else {
		prompt += "The sender already has a subject. Omit the subject suggestion."
	}
	includeTo, _ := in["includeTo"].(bool)
	includeCc, _ := in["includeCc"].(bool)
	if includeTo {
		prompt += " Suggest to using only literal email addresses explicitly designated as primary recipients."
	} else {
		prompt += " Omit to."
	}
	if includeCc {
		prompt += " Suggest cc using only literal email addresses explicitly designated as CC recipients."
	} else {
		prompt += " Omit cc."
	}
	prompt += " For each address field return a single comma-separated list of bare addresses. " +
		"Never infer an address from a name, invent an address, or add an address merely mentioned in the email content. Omit empty fields."
	result, err := m.generateMailAI(ctx, in["locale"], prompt, map[string]string{"writingBrief": brief}, draftSuggestionDefinition().Output)
	if err != nil {
		return contracts.Output{}, err
	}
	seen := map[string]bool{}
	explicit := explicitDraftAddresses(brief)
	suggestions := make([]any, 0, len(result["suggestions"].([]any)))
	for _, item := range result["suggestions"].([]any) {
		suggestion := item.(map[string]any)
		field, value := suggestion["field"].(string), strings.TrimSpace(suggestion["value"].(string))
		if seen[field] || value == "" || strings.ContainsRune(value, 0) ||
			field == "subject" && (!includeSubject || !headerSafe(value) || len([]rune(value)) > 998) {
			return contracts.Output{}, contracts.Problem("unavailable")
		}
		seen[field] = true
		switch field {
		case "to", "cc":
			if field == "to" && !includeTo || field == "cc" && !includeCc {
				continue
			}
			value = filterDraftAddresses(value, explicit)
			if value == "" {
				continue
			}
		}
		suggestion["value"] = value
		suggestions = append(suggestions, suggestion)
	}
	result["suggestions"] = suggestions
	return contracts.Output{Data: result}, nil
}

func explicitDraftAddresses(brief string) map[string]bool {
	addresses := map[string]bool{}
	for _, token := range strings.FieldsFunc(brief, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;<>():[]{}\"，、；。！？：（）【】《》“”‘’", r)
	}) {
		token = strings.TrimRight(token, ".!?")
		address, err := safeAddress(token)
		if err == nil && address == token && strings.Contains(address, "@") {
			addresses[address] = true
		}
	}
	return addresses
}

func filterDraftAddresses(value string, explicit map[string]bool) string {
	var addresses []string
	seen := map[string]bool{}
	for _, raw := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；' || r == '、'
	}) {
		address := strings.TrimSpace(raw)
		if explicit[address] && !seen[address] {
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	return strings.Join(addresses, ", ")
}
