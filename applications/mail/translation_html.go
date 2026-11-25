package mail

import (
	"context"
	stdhtml "html"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"horizon/foundation/kernel/capability/contracts"
)

type htmlTranslationPatch struct {
	start, end int
	text       string
	suffix     string
}

type htmlTranslationPart struct {
	patch     int
	text      string
	separator string
}

func (m *Module) translateHTML(ctx context.Context, locale any, subject, source string) (contracts.Output, error) {
	// Reuse the inbound allowlist so existing newsletter layout and permitted
	// image and link attributes survive. Only escaped text is written below.
	source, err := sanitizeHTML(inboundHTMLPolicy(), source, 26214400)
	if err != nil {
		return contracts.Output{}, err
	}
	patches, truncated, err := htmlTranslationPatches(source)
	if err != nil {
		return contracts.Output{}, err
	}

	parts := make([]htmlTranslationPart, 0, len(patches))
	for index, patch := range patches {
		for text := strings.TrimSpace(patch.text); text != ""; {
			chunk, separator := translationChunk(text)
			parts = append(parts, htmlTranslationPart{patch: index, text: chunk, separator: separator})
			text = text[len(chunk)+len(separator):]
		}
	}
	translations := make([]strings.Builder, len(patches))
	var translatedSubject string
	for subject != "" || len(parts) != 0 {
		var batch []string
		length := 0
		for _, part := range parts {
			length += utf8.RuneCountInString(part.text)
			if length > translationChunkLimit || len(batch) == 64 {
				break
			}
			batch = append(batch, part.text)
		}
		if batch == nil {
			batch = []string{}
		}
		output := contracts.Object(map[string]*contracts.Schema{
			"subject":  contracts.Str(0, 3000),
			"segments": contracts.Array(contracts.Str(1, 12000), len(batch), len(batch)),
		}, "subject", "segments")
		result, err := m.generateMailAI(ctx, locale,
			"Translate the email subject and ordered text segments faithfully into the target language. "+
				"Read adjacent segments together for context, then return one translation per segment in the same order. "+
				"Keep links, addresses, names, line breaks and factual details in the corresponding segments. "+
				"Keep an empty subject empty. Do not merge or omit segments.",
			map[string]any{"subject": subject, "segments": batch}, output)
		if err != nil {
			return contracts.Output{}, err
		}
		if subject != "" {
			translatedSubject = strings.TrimSpace(result["subject"].(string))
			if translatedSubject == "" {
				return contracts.Output{}, contracts.Problem("unavailable")
			}
			subject = ""
		}
		for index, item := range result["segments"].([]any) {
			part := parts[index]
			text := strings.TrimSpace(item.(string))
			if text == "" {
				return contracts.Output{}, contracts.Problem("unavailable")
			}
			translations[part.patch].WriteString(text)
			translations[part.patch].WriteString(part.separator)
		}
		parts = parts[len(batch):]
	}
	var translatedHTML strings.Builder
	offset := 0
	for index, patch := range patches {
		translatedHTML.WriteString(source[offset:patch.start])
		text := strings.TrimLeftFunc(patch.text, unicode.IsSpace)
		translatedHTML.WriteString(stdhtml.EscapeString(patch.text[:len(patch.text)-len(text)]))
		translatedHTML.WriteString(stdhtml.EscapeString(translations[index].String()))
		text = strings.TrimRightFunc(patch.text, unicode.IsSpace)
		translatedHTML.WriteString(stdhtml.EscapeString(patch.text[len(text):] + patch.suffix))
		offset = patch.end
	}
	translatedHTML.WriteString(source[offset:])
	bodyHTML := translatedHTML.String()
	if len(bodyHTML) > 27<<20 {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	body, _ := limitMailText(HTMLToText(bodyHTML), translationBodyOutputLimit)
	return contracts.Output{Data: map[string]any{
		"subject": translatedSubject, "body": body, "bodyHtml": bodyHTML, "truncated": truncated,
	}}, nil
}

func htmlTranslationPatches(source string) ([]htmlTranslationPatch, bool, error) {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	var patches []htmlTranslationPatch
	offset := 0
	remaining := translationBodyLimit
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				return patches, false, nil
			}
			return nil, false, contracts.Problem("invalid")
		}
		end := offset + len(tokenizer.Raw())
		if kind == html.TextToken {
			text := string(tokenizer.Text())
			if core := strings.TrimSpace(text); core != "" {
				if remaining == 0 {
					return patches, true, nil
				}
				prefix, truncated := limitMailText(core, remaining)
				if truncated {
					leading := len(text) - len(strings.TrimLeftFunc(text, unicode.IsSpace))
					boundary := leading + len(prefix)
					patches = append(patches, htmlTranslationPatch{start: offset, end: end, text: text[:boundary], suffix: text[boundary:]})
					return patches, true, nil
				}
				patches = append(patches, htmlTranslationPatch{start: offset, end: end, text: text})
				remaining -= utf8.RuneCountInString(core)
			}
		}
		offset = end
	}
}
