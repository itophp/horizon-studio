package mail

import ar "horizon/foundation/kernel/applicationruntime"

const dialogState = "_mailDialog"

func rowAction(label, icon, event, grant string, when *ar.Expression) props {
	return props{"labelKey": label, "icon": icon, "mode": "event", "event": event, "permission": grant, "when": when}
}

func openDialog(event, kind string, record ar.Expression) ar.EventBinding {
	return setData(event, dialogState, object(ar.Const("kind", `"`+kind+`"`), ar.Mapping{Target: "record", Value: record}))
}

func closeDialog(event string) ar.EventBinding {
	return setData(event, dialogState, ar.Literal("null"))
}

func confirmation(kind, title, help, argument, field, capability string) ar.Node {
	close := actionButton(kind+"-dismiss", "admin:actions.close", "stop", "outline", closeDialog("action"))
	confirm := actionButton(kind+"-confirm", title, "stop", "destructive",
		invoke("action", capability, object(ar.Bind("id", "data."+dialogState+".record.id"))), closeDialog("action"))
	dialog := node(kind+"-dialog", "dialog", "", props{"titleKey": title, "descriptionKey": help}, closeDialog("close"))
	spec := component(dialog)
	spec.Bindings = []ar.Mapping{
		{Target: "open", Value: equal("data."+dialogState+".kind", kind)},
		{Target: "descriptionArgs", Value: object(ar.Bind(argument, "data."+dialogState+".record."+field))},
	}
	spec.Content = &ar.LayoutSpec{
		ID: kind + "-confirmation", Kind: "stack",
		Props:    ar.LayoutProps{ClassName: "form-actions mail-dialog-actions", Gap: "8px"},
		Children: []ar.Node{close, confirm},
	}
	return dialog
}

func deliveriesPage(source string) ar.PageV3 {
	failed := ar.Condition("in", ar.Path("record.status"), ar.Literal(`["failed","ambiguous"]`))
	queued := equal("record.status", "queued")
	table := node("mail-deliveries", "table-panel", "$deliveries", props{
		"rowsPath": "items", "search": false,
		"columns": []props{
			{"name": "subject", "labelKey": "mail:deliveries.subject", "display": "fallback", "emptyKey": "mail:message.noSubject"},
			{"name": "status", "labelKey": "mail:deliveries.statusLabel", "display": "enumBadge", "prefixKey": "mail:deliveries.status."},
			{"name": "attempts", "labelKey": "mail:deliveries.attempts"},
			{"name": "createdAt", "labelKey": "mail:deliveries.createdAt", "display": "date"},
		},
		"rowActions": []props{
			rowAction("mail:deliveries.retry", "refresh", "retry", "mail:send", &failed),
			rowAction("mail:deliveries.cancel", "reject", "cancel", "mail:send", &queued),
		},
	}, invoke("retry", "mail.delivery.retry", object(ar.Bind("id", "event.id"))),
		openDialog("cancel", "cancel-delivery", object(ar.Bind("id", "event.id"), ar.Bind("subject", "event.subject"))),
	)
	dialog := confirmation("cancel-delivery", "mail:deliveries.cancel", "mail:deliveries.cancelHelp", "subject", "subject", "mail.delivery.cancel")
	allowed := permission("mail:send")
	component(dialog).Visible = &allowed
	return page(deliveriesPageID, "/mail/deliveries", "mail:deliveries.title", "mail:read",
		[]ar.DataSpec{{ID: "deliveries", Provider: source}, {ID: dialogState}},
		pageLink("mail-delivery-back", "/mail", "mail:backToMail", "arrow-left", "ghost", false), mailHeading("mail:deliveries.title"), table, dialog)
}

func mailboxPage(source string) ar.PageV3 {
	active := equal("record.status", "active")
	inactive := ar.Condition("not", active)
	notDefault := ar.Condition("not", ar.Path("record.defaultSend"))
	columns := []props{
		{"name": "name", "labelKey": "mail:mailbox.name", "display": "entity"},
		{"name": "emailAddress", "labelKey": "mail:mailbox.address"},
		{"name": "status", "labelKey": "mail:mailbox.statusLabel", "display": "enumBadge", "prefixKey": "mail:mailbox.status."},
		{"name": "defaultSend", "labelKey": "mail:mailbox.sending", "display": "enumBadge", "prefixKey": "mail:mailbox.defaultState."},
	}
	actions := []props{
		rowAction("mail:mailbox.edit", "pencil", "edit", "mail:configure", nil),
		rowAction("mail:mailbox.newSmtpPassword", "key", "smtp", "mail:configure", nil),
		rowAction("mail:mailbox.newReceivePassword", "key", "receive", "mail:configure", nil),
		rowAction("mail:mailbox.makeDefault", "check", "default", "mail:configure", &notDefault),
		rowAction("mail:mailbox.test", "refresh", "test", "mail:configure", nil),
		rowAction("mail:mailbox.disable", "reject", "disable", "mail:configure", &active),
		rowAction("mail:mailbox.enable", "check", "enable", "mail:configure", &inactive),
	}
	sending := testConnection("send", "_sendTest")
	receiving := testConnection("receive", "_receiveTest")
	table := node("mail-mailboxes", "table-panel", "$mailboxes", props{
		"rowsPath": "items", "search": false, "columns": columns, "rowActions": actions, "className": "admin-panel mail-compact-table",
	},
		openDialog("edit", "edit", mailboxDialogRecord()),
		openDialog("smtp", "smtp", mailboxDialogRecord()),
		openDialog("receive", "receive", mailboxDialogRecord()),
		openDialog("disable", "disable-mailbox", mailboxDialogRecord()),
		invoke("enable", "mail.mailbox.enable", object(ar.Bind("id", "event.id"))),
		invoke("default", "mail.mailbox.update", defaultMailboxInput()),
		setData("test", "_sendTest", ar.Literal("null")),
		setData("test", "_receiveTest", ar.Literal("null")),
		sending, receiving,
	)
	success := ar.Condition("and", equal("data._sendTest", "ok"), equal("data._receiveTest", "ok"))
	failed := ar.Condition("or", equal("data._sendTest", "failed"), equal("data._receiveTest", "failed"))
	okNotice := node("mail-test-success", "text", "", props{"role": "status", "textKey": "mail:mailbox.tested"})
	component(okNotice).Visible = &success
	errorNotice := node("mail-test-failure", "text", "", props{"role": "status", "textKey": "mail:mailbox.testFailed"})
	component(errorNotice).Visible = &failed

	connect := actionButton("mail-connect", "mail:mailbox.connect", "plus", "default",
		openDialog("action", "connect", ar.Literal("null")))
	feedback := node("mail-feedback", "text", "", props{"role": "status"})
	feedbackVisible := ar.Condition("exists", ar.Path("data._mailFeedback"))
	component(feedback).Visible = &feedbackVisible
	component(feedback).Bindings = []ar.Mapping{{
		Target: "textKey", Value: ar.Condition("concat", ar.Literal(`"mail:mailbox."`), ar.Path("data._mailFeedback")),
	}}
	return page(mailboxPageID, "/mail/mailboxes", "mail:mailboxes", "mail:configure",
		[]ar.DataSpec{{ID: "mailboxes", Provider: source}, {ID: dialogState}, {ID: "_sendTest"}, {ID: "_receiveTest"}, {ID: "_mailFeedback"}},
		pageLink("mail-mailboxes-back", "/mail", "mail:backToMail", "arrow-left", "ghost", false),
		mailLayout("mail-settings-heading", "mail-settings-heading", mailHeading("mail:mailboxes"), connect),
		feedback, okNotice, errorNotice, table, mailboxForms(), confirmation("disable-mailbox",
			"mail:mailbox.disable", "mail:mailbox.disableHelp", "address", "emailAddress", "mail.mailbox.disable"))
}

func mailboxDialogRecord() ar.Expression {
	return object(ar.Bind("id", "event.id"), ar.Bind("revision", "event.revision"),
		ar.Bind("name", "event.name"), ar.Bind("emailAddress", "event.emailAddress"),
		ar.Bind("defaultSend", "event.defaultSend"), ar.Bind("channels", "event.channels"))
}

func testConnection(direction, result string) ar.EventBinding {
	binding := invoke("test", "mail.mailbox.connection.test",
		object(ar.Bind("mailboxId", "event.id"), ar.Const("direction", `"`+direction+`"`)))
	binding.ResultTarget = result
	path := ar.Path("event.data.status")
	binding.Result = &path
	return binding
}

func defaultMailboxInput() ar.Expression {
	channelField := func(target, direction, field string) ar.Mapping {
		channel := ar.Condition("find", ar.Path("event.channels"), equal("record.direction", direction))
		return ar.Mapping{Target: target, Value: ar.Condition("get", channel, ar.Literal(`"`+field+`"`))}
	}
	return object(
		ar.Bind("id", "event.id"), ar.Bind("revision", "event.revision"), ar.Bind("name", "event.name"),
		ar.Bind("emailAddress", "event.emailAddress"), ar.Const("defaultSend", "true"),
		channelField("smtpHost", "send", "host"), channelField("smtpPort", "send", "port"),
		channelField("smtpUsername", "send", "username"), channelField("smtpTls", "send", "tlsMode"),
		channelField("receiveProtocol", "receive", "protocol"), channelField("receiveTls", "receive", "tlsMode"),
		channelField("receiveHost", "receive", "host"), channelField("receivePort", "receive", "port"),
		channelField("receiveUsername", "receive", "username"),
	)
}

func mailboxForms() ar.Node {
	result := node("mail-mailbox-form", "mail-mailbox-form", "$"+dialogState, props{},
		invoke("connect", "mail.mailbox.onboard", ar.Expression{}),
		invoke("update", "mail.mailbox.update", ar.Expression{}),
		invoke("replace", "mail.mailbox.credential.replace", ar.Expression{}),
		closeDialog("close"), setData("feedback", "_mailFeedback", ar.Expression{}), ar.EventBinding{Event: "refresh", Action: "refresh", Target: "mailboxes"},
	)
	visible := ar.Condition("in", ar.Path("data."+dialogState+".kind"), ar.Literal(`["connect","edit","smtp","receive"]`))
	component(result).Visible = &visible
	return result
}

func messageDetailPage(source, mailboxSource string) ar.PageV3 {
	optional := permission("mail:configure")
	return page(detailPageID, "/mail/:id", "mail:message.title", "mail:read",
		[]ar.DataSpec{{ID: "message", Provider: source}, {ID: "_mailboxes", Provider: mailboxSource, EnabledWhen: &optional, NonBlocking: true}},
		node("mail-message-content", "mail-message-content", "$message", props{},
			invoke("read", "mail.message.flag.update", object(ar.Bind("id", "event.id"), ar.Const("read", "true"))),
			invoke("analyze", messageAnalysisCapability, object(ar.Bind("id", "route.id"), ar.Bind("locale", "event.locale"))),
			invoke("translate", messageTranslationCapability, object(ar.Bind("id", "route.id"), ar.Bind("locale", "event.locale")))),
	)
}
