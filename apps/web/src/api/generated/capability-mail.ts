// Code generated from Mail analysis capability definitions. DO NOT EDIT.

export type MailInboxRecommendInput = { locale: "zh-CN" | "en" };
export type MailInboxRecommendOutput = {
  items: Array<{ from: string; id: number; reason: string; subject: string }>;
};

export type MailMessageAnalyzeInput = { id: number; locale: "zh-CN" | "en" };
export type MailMessageAnalyzeOutput = {
  actionItems: Array<{ deadline: string; task: string }>;
  deadlines: Array<string>;
  keyPoints: Array<string>;
  summary: string;
  truncated: boolean;
};

export type MailMessageTranslateInput = { id: number; locale: "zh-CN" | "en" };
export type MailMessageTranslateOutput = {
  body: string;
  bodyHtml?: string;
  subject: string;
  truncated: boolean;
};
