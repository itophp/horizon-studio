package mail

import (
	"strconv"

	ar "horizon/foundation/kernel/applicationruntime"
)

const (
	inboxPageID      = "mail.inbox"
	sentPageID       = "mail.sent-page"
	composePageID    = "mail.compose-page"
	deliveriesPageID = "mail.delivery-page"
	mailboxPageID    = "mail.mailbox-page"
	detailPageID     = "mail.detail-page"
)

type props = map[string]any

func node(id, kind, data string, properties props, events ...ar.EventBinding) ar.Node {
	return ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
		ID: id + "-layout", Kind: "grid", Props: ar.LayoutProps{Columns: "minmax(0, 1fr)"},
		Children: []ar.Node{{Type: "component", Component: &ar.ComponentSpec{
			ID: id, Kind: kind, Version: "1.0", Data: data, Props: properties, Events: events,
		}}},
	}}
}

func component(value ar.Node) *ar.ComponentSpec { return value.Layout.Children[0].Component }

func object(mappings ...ar.Mapping) ar.Expression {
	return ar.Expression{Kind: "object", Fields: mappings}
}

func equal(path, value string) ar.Expression {
	return ar.Condition("eq", ar.Path(path), ar.Literal(strconv.Quote(value)))
}

func permission(code string) ar.Expression {
	return ar.Condition("in", ar.Literal(strconv.Quote(code)), ar.Path("context.permissions"))
}

func invoke(event, capability string, payload ar.Expression) ar.EventBinding {
	return ar.EventBinding{Event: event, Action: "invoke", Target: capability + "@1.0.0", Payload: payload}
}

func navigate(event, page string, payload ar.Expression) ar.EventBinding {
	return ar.EventBinding{Event: event, Action: "navigate", Target: page, Payload: payload}
}

func setData(event, variable string, payload ar.Expression) ar.EventBinding {
	return ar.EventBinding{Event: event, Action: "setData", Target: variable, Payload: payload}
}

func actionButton(id, label, icon, variant string, events ...ar.EventBinding) ar.Node {
	return node(id, "button", "", props{
		"action": props{"id": id, "labelKey": label, "icon": icon, "variant": variant},
	}, events...)
}

func navigation(active string) ar.Node {
	links := []struct{ id, route, label, icon, grant string }{
		{inboxPageID, "/mail", "mail:inbox", "inbox", "mail:read"},
		{sentPageID, "/mail/sent", "mail:sent", "send", "mail:read"},
		{deliveriesPageID, "/mail/deliveries", "mail:deliveries.title", "clock", "mail:read"},
		{mailboxPageID, "/mail/mailboxes", "mail:mailboxes", "settings", "mail:configure"},
	}
	compose := pageLink(composePageID+"-link", "/mail/compose", "mail:compose.title", "pencil", "default", false)
	canCompose := ar.Condition("and", permission("mail:send"), permission("mail:configure"))
	component(compose).Visible = &canCompose
	children := []ar.Node{compose}
	for _, link := range links {
		variant := "ghost"
		if active == link.id {
			variant = "secondary"
		}
		linkNode := pageLink(link.id+"-link", link.route, link.label, link.icon, variant, active == link.id)
		visible := permission(link.grant)
		component(linkNode).Visible = &visible
		children = append(children, linkNode)
	}
	return ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
		ID: "mail-navigation", Kind: "stack", Props: ar.LayoutProps{ClassName: "mail-folder-navigation", Gap: "5px"}, Children: children,
	}}
}

func pageLink(id, route, label, icon, variant string, current bool) ar.Node {
	return node(id, "button", "", props{"action": props{
		"id": id, "labelKey": label, "icon": icon, "variant": variant, "current": current,
		"href": ar.Literal(strconv.Quote(route)),
	}})
}

func mailHeading(title string) ar.Node {
	return node("mail-heading", "page-heading", "", props{"titleKey": title})
}

func mailLayout(id, class string, children ...ar.Node) ar.Node {
	return ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
		ID: id, Kind: "grid", Props: ar.LayoutProps{ClassName: class}, Children: children,
	}}
}

func page(id, route, title, grant string, data []ar.DataSpec, children ...ar.Node) ar.PageV3 {
	return ar.PageV3{
		SchemaVersion: "3.0", ID: id, Route: route, TitleKey: title,
		SubtitleKey: "mail:description", Permission: grant,
		Root: ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
			ID: id + "-root", Kind: "stack", Props: ar.LayoutProps{Gap: "14px", Width: "full", ClassName: "mail-page"}, Data: data,
			Children: children,
		}},
	}
}

func provider(id, capability string, input ...ar.Mapping) ar.Provider {
	return ar.Provider{ID: id, Capability: ar.Ref(capability), KeyField: "id", Pagination: "none", Input: input}
}

func messagesPage(id, route, title, source string) ar.PageV3 {
	folder := "inbox"
	if id == sentPageID {
		folder = "sent"
	}
	events := []ar.EventBinding{navigate("open", detailPageID, object(ar.Bind("id", "event.id")))}
	if folder == "inbox" {
		events = append(events, invoke("recommend", recentAnalysisCapability, object(ar.Bind("locale", "event.locale"))))
	}
	return page(id, route, title, "mail:read", []ar.DataSpec{{ID: "messages", Provider: source}},
		mailLayout("mail-folder-workspace", "mail-folder-workspace", navigation(id),
			mailLayout("mail-folder-content", "mail-folder-content", mailHeading(title),
				node("mail-messages", "mail-message-list", "$messages", props{"folder": folder},
					events...))),
	)
}

func RuntimeApplication() ar.ApplicationV2 {
	inbox := provider("mail.inbox", "mail.message.list", ar.Const("folder", `"inbox"`), ar.Bind("page", "query.page"), ar.Bind("pageSize", "query.pageSize"))
	sent := provider("mail.sent", "mail.message.list", ar.Const("folder", `"sent"`), ar.Bind("page", "query.page"), ar.Bind("pageSize", "query.pageSize"))
	inbox.Pagination, inbox.PageSize = "server", 50
	sent.Pagination, sent.PageSize = "server", 50
	deliveries := provider("mail.deliveries", "mail.delivery.list")
	mailboxes := provider("mail.mailboxes", "mail.mailbox.list")
	detail := provider("mail.detail", "mail.message.get", ar.Bind("id", "route.id"))

	protocol := &ar.ApplicationV3{
		SchemaVersion: "3.0", ID: "mail", Version: "1.0.10", Source: "builtin:mail",
		RuntimeCompatibility: "3.0", LabelKey: "mail:heading", Permission: "mail:read", Route: "/mail",
		Pages: []ar.PageV3{
			messagesPage(inboxPageID, "/mail", "mail:inbox", inbox.ID),
			messagesPage(sentPageID, "/mail/sent", "mail:sent", sent.ID),
			page(composePageID, "/mail/compose", "mail:compose.title", "mail:send", []ar.DataSpec{{ID: "mailboxes", Provider: mailboxes.ID}},
				mailLayout("mail-compose-workspace", "mail-compose-workspace", mailHeading("mail:compose.title"),
					node("mail-compose-form", "mail-compose-form", "$mailboxes", props{}, invoke("suggest-draft", draftSuggestionCapability, object(ar.Bind("sourceText", "event.sourceText"), ar.Bind("locale", "event.locale"), ar.Bind("includeSubject", "event.includeSubject"), ar.Bind("includeTo", "event.includeTo"), ar.Bind("includeCc", "event.includeCc"))),
						invoke("submit", "mail.message.send", ar.Expression{}),
						navigate("sent", sentPageID, ar.Expression{})))),
			deliveriesPage(deliveries.ID),
			mailboxPage(mailboxes.ID),
			messageDetailPage(detail.ID, mailboxes.ID),
		},
		Providers:  []ar.Provider{inbox, sent, detail, deliveries, mailboxes},
		Navigation: []ar.Navigation{{PageID: inboxPageID, LabelKey: "mail:heading", Icon: "mail"}},
	}
	return ar.ApplicationV2{
		Protocol: protocol, SchemaVersion: "3.0", ID: protocol.ID, Version: protocol.Version,
		MetadataVersion: "1", Source: protocol.Source,
		RuntimeCompatibility: "3.0", LabelKey: protocol.LabelKey, Permission: protocol.Permission, Route: protocol.Route,
	}
}
