package mail

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"horizon/foundation/kernel/capability/contracts"
)

const (
	messageTranslationCapability = "mail.message.translate"
	translationBodyLimit         = 12000
	translationChunkLimit        = 4000
	translationBodyOutputLimit   = 48003
)

func messageTranslationDefinition() contracts.Definition {
	def := analysisDefinition(messageTranslationCapability, "Translate an email",
		"Translate the subject and text of the caller's email while preserving HTML layout.")
	def.Input = messageAnalysisDefinition().Input
	def.Output = contracts.Object(map[string]*contracts.Schema{
		"subject":   contracts.Str(0, 3000),
		"body":      contracts.Str(0, translationBodyOutputLimit),
		"bodyHtml":  contracts.Str(0, 27<<20),
		"truncated": contracts.Bool(),
	}, "subject", "body", "truncated")
	return def
}

func (m *Module) translateMessage(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	message, err := m.loadAnalysisMail(ctx, "mail.message.get", contracts.Input{"id": in["id"]})
	if err != nil {
		return contracts.Output{}, err
	}
	subject := strings.TrimSpace(analysisText(message, "subject"))
	if source := analysisText(message, "bodyHtml"); strings.TrimSpace(source) != "" {
		return m.translateHTML(ctx, in["locale"], subject, source)
	}
	body, truncated := limitMailText(mailBodyText(message), translationBodyLimit)
	body = strings.TrimSpace(body)
	modelOutput := contracts.Object(map[string]*contracts.Schema{
		"subject": contracts.Str(0, 3000),
		"body":    contracts.Str(0, 12000),
	}, "subject", "body")
	var translatedSubject string
	var translatedBody strings.Builder
	for subject != "" || body != "" {
		chunk, separator := translationChunk(body)
		result, err := m.generateMailAI(ctx, in["locale"],
			"Translate the email subject and body faithfully into the target language. Preserve paragraph breaks, names, URLs, addresses and factual details. "+
				"Translate this segment only, without summaries, explanations or introductions. Keep empty input fields empty.",
			map[string]string{"subject": subject, "body": chunk}, modelOutput)
		if err != nil {
			return contracts.Output{}, err
		}
		nextSubject := strings.TrimSpace(result["subject"].(string))
		nextBody := strings.TrimSpace(result["body"].(string))
		if subject != "" {
			if nextSubject == "" {
				return contracts.Output{}, contracts.Problem("unavailable")
			}
			translatedSubject = nextSubject
		}
		if strings.TrimSpace(chunk) != "" {
			if nextBody == "" {
				return contracts.Output{}, contracts.Problem("unavailable")
			}
			translatedBody.WriteString(nextBody)
		}
		translatedBody.WriteString(separator)
		subject = ""
		body = body[len(chunk)+len(separator):]
	}
	return contracts.Output{Data: map[string]any{
		"subject": translatedSubject, "body": translatedBody.String(), "truncated": truncated,
	}}, nil
}

func mailBodyText(message map[string]any) string {
	body := analysisText(message, "bodyText")
	if strings.TrimSpace(body) == "" {
		return HTMLToText(analysisText(message, "bodyHtml"))
	}
	return body
}

func limitMailText(body string, limit int) (string, bool) {
	count := 0
	for offset := range body {
		if count == limit {
			return body[:offset], true
		}
		count++
	}
	return body, false
}

func translationChunk(text string) (string, string) {
	chunk, more := limitMailText(text, translationChunkLimit)
	if more {
		if index := strings.LastIndexByte(chunk, '\n'); index >= 0 && utf8.RuneCountInString(chunk[:index]) > translationChunkLimit*3/4 {
			return chunk[:index], "\n"
		}
		if index := strings.LastIndexByte(chunk, ' '); index >= 0 && utf8.RuneCountInString(chunk[:index]) > translationChunkLimit*3/4 {
			return chunk[:index], " "
		}
	}
	return chunk, ""
}
