import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { $generateHtmlFromNodes } from "@lexical/html";
import {
  INSERT_ORDERED_LIST_COMMAND,
  INSERT_UNORDERED_LIST_COMMAND,
  ListItemNode,
  ListNode,
} from "@lexical/list";
import { $createQuoteNode, QuoteNode } from "@lexical/rich-text";
import {
  LexicalComposer,
  type InitialConfigType,
} from "@lexical/react/LexicalComposer";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { HistoryPlugin } from "@lexical/react/LexicalHistoryPlugin";
import { ListPlugin } from "@lexical/react/LexicalListPlugin";
import { OnChangePlugin } from "@lexical/react/LexicalOnChangePlugin";
import { RichTextPlugin } from "@lexical/react/LexicalRichTextPlugin";
import {
  $createLineBreakNode,
  $createParagraphNode,
  $createTextNode,
  $getRoot,
  $getSelection,
  $isRangeSelection,
  FORMAT_TEXT_COMMAND,
  HISTORY_PUSH_TAG,
  REDO_COMMAND,
  UNDO_COMMAND,
  type LexicalEditor,
  type TextFormatType,
} from "lexical";
import {
  Bold,
  CodeXml,
  Eye,
  Pencil,
  Italic,
  List,
  ListOrdered,
  Redo2,
  Underline,
  Undo2,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { HtmlMailFrame } from "./email-html";

const formatting = [
  { format: "bold", icon: Bold, label: "editor.bold" },
  { format: "italic", icon: Italic, label: "editor.italic" },
  { format: "underline", icon: Underline, label: "editor.underline" },
] as const;

function EditorControls({ readOnly }: { readOnly: boolean }) {
  const { t } = useTranslation("mail");
  const [editor] = useLexicalComposerContext();
  const [activeFormats, setActiveFormats] = useState<TextFormatType[]>([]);
  useEffect(() => editor.setEditable(!readOnly), [editor, readOnly]);
  useEffect(
    () =>
      editor.registerUpdateListener(({ editorState }) => {
        editorState.read(() => {
          const selection = $getSelection();
          setActiveFormats(
            $isRangeSelection(selection)
              ? formatting.flatMap(({ format }) =>
                  selection.hasFormat(format) ? [format] : [],
                )
              : [],
          );
        });
      }),
    [editor],
  );

  return (
    <div
      role="group"
      aria-label={t("editor.toolbar")}
      className="flex flex-wrap items-center gap-1 border-b border-border bg-muted/30 px-2 py-1.5"
      onMouseDown={(event) => event.preventDefault()}
    >
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        disabled={readOnly}
        title={t("editor.undo")}
        aria-label={t("editor.undo")}
        onClick={() => editor.dispatchCommand(UNDO_COMMAND, undefined)}
      >
        <Undo2 />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        disabled={readOnly}
        title={t("editor.redo")}
        aria-label={t("editor.redo")}
        onClick={() => editor.dispatchCommand(REDO_COMMAND, undefined)}
      >
        <Redo2 />
      </Button>
      <span className="mx-1 h-5 border-l border-border" aria-hidden="true" />
      {formatting.map(({ format, icon: Icon, label }) => (
        <Button
          key={format}
          type="button"
          variant={activeFormats.includes(format) ? "secondary" : "ghost"}
          size="icon-sm"
          disabled={readOnly}
          title={t(label)}
          aria-label={t(label)}
          aria-pressed={activeFormats.includes(format)}
          onClick={() => editor.dispatchCommand(FORMAT_TEXT_COMMAND, format)}
        >
          <Icon />
        </Button>
      ))}
      <span className="mx-1 h-5 border-l border-border" aria-hidden="true" />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        disabled={readOnly}
        title={t("editor.bullets")}
        aria-label={t("editor.bullets")}
        onClick={() =>
          editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined)
        }
      >
        <List />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        disabled={readOnly}
        title={t("editor.numberedList")}
        aria-label={t("editor.numberedList")}
        onClick={() =>
          editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined)
        }
      >
        <ListOrdered />
      </Button>
    </div>
  );
}

function seedQuote(text: string) {
  const root = $getRoot();
  root.append($createParagraphNode());
  if (!text.trim()) return;
  const quote = $createQuoteNode();
  text.split(/\r?\n/).forEach((line, index) => {
    if (index > 0) quote.append($createLineBreakNode());
    quote.append($createTextNode(line));
  });
  root.append(quote);
}

function editorHTML(editor: LexicalEditor) {
  return $getRoot().getTextContent().trim()
    ? $generateHtmlFromNodes(editor)
    : "";
}

function InsertSuggestedBody({
  insertion,
  onChange,
}: {
  insertion?: { text: string };
  onChange: (html: string) => void;
}) {
  const [editor] = useLexicalComposerContext();
  const applied = useRef<typeof insertion>(undefined);
  useLayoutEffect(() => {
    if (!insertion || insertion === applied.current) return;
    applied.current = insertion;
    editor.update(
      () => {
        const root = $getRoot();
        if (!root.getTextContent().trim()) root.clear();
        const existing = root.getFirstChild();
        for (const line of insertion.text.split(/\r\n|\r|\n/)) {
          const paragraph = $createParagraphNode();
          if (line) paragraph.append($createTextNode(line));
          if (existing) existing.insertBefore(paragraph);
          else root.append(paragraph);
        }
      },
      { discrete: true, tag: HISTORY_PUSH_TAG },
    );
    onChange(editor.getEditorState().read(() => editorHTML(editor)));
  }, [editor, insertion, onChange]);
  return null;
}

export function MailHtmlEditor({
  quote,
  html,
  insertion,
  readOnly,
  labelledBy,
  errorId,
  onChange,
  onBlur,
}: {
  quote: string;
  html: string;
  insertion?: { text: string };
  readOnly: boolean;
  labelledBy: string;
  errorId?: string;
  onChange: (html: string) => void;
  onBlur: () => void;
}) {
  const { t } = useTranslation("mail");
  const [preview, setPreview] = useState(false);
  const config: InitialConfigType = {
    namespace: "mail-compose",
    nodes: [ListNode, ListItemNode, QuoteNode],
    editable: !readOnly,
    theme: {
      text: { underline: "underline", bold: "font-bold", italic: "italic" },
    },
    editorState: () => seedQuote(quote),
    onError(error) {
      throw error;
    },
  };
  const placeholder = t("editor.placeholder");
  return (
    <div className="mail-editor overflow-hidden rounded-lg border border-border bg-card">
      <div className="mail-editor-modes">
        <span className="inline-flex items-center gap-2 text-xs font-medium">
          <CodeXml className="size-4" />
          {t("message.htmlFormat")}
        </span>
        <div className="flex gap-1">
          <Button
            type="button"
            size="sm"
            variant={preview ? "ghost" : "secondary"}
            aria-pressed={!preview}
            onClick={() => setPreview(false)}
          >
            <Pencil />
            {t("editor.edit")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant={preview ? "secondary" : "ghost"}
            aria-pressed={preview}
            onClick={() => setPreview(true)}
          >
            <Eye />
            {t("editor.preview")}
          </Button>
        </div>
      </div>
      {preview &&
        (html ? (
          <HtmlMailFrame html={html} title={t("editor.preview")} />
        ) : (
          <p className="px-5 py-8 text-sm text-muted-foreground">
            {t("message.noBody")}
          </p>
        ))}
      <LexicalComposer initialConfig={config}>
        <div hidden={preview}>
          <EditorControls readOnly={readOnly} />
          <div className="relative">
            <RichTextPlugin
              contentEditable={
                <ContentEditable
                  className="mail-editor-content"
                  aria-labelledby={labelledBy}
                  aria-describedby={errorId}
                  aria-invalid={!!errorId}
                  aria-multiline
                  aria-placeholder={placeholder}
                  placeholder={
                    <div className="pointer-events-none absolute start-5 top-4 text-muted-foreground">
                      {placeholder}
                    </div>
                  }
                  onBlur={onBlur}
                />
              }
              ErrorBoundary={LexicalErrorBoundary}
            />
          </div>
        </div>
        <InsertSuggestedBody insertion={insertion} onChange={onChange} />
        <HistoryPlugin />
        <ListPlugin />
        <OnChangePlugin
          ignoreSelectionChange
          onChange={(state, editor) => {
            onChange(state.read(() => editorHTML(editor)));
          }}
        />
      </LexicalComposer>
    </div>
  );
}
