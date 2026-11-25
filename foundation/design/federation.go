package design

import . "goa.design/goa/v3/dsl"

var FederationProvider = Type("FederationProvider", func() {
	Attribute("id", String)
	Attribute("name", String)
	Required("id", "name")
})

var FederationStartResult = Type("FederationStartResult", func() {
	Attribute("authorizationUrl", String)
	Attribute("cookie", String)
	Required("authorizationUrl", "cookie")
})

var FederationCompleteResult = Type("FederationCompleteResult", func() {
	Attribute("profile", SessionProfile)
	Attribute("cookie", String)
	Attribute("linkProof", String)
})

func federationMethods() {
	Method("saml_callback", func() {
		Description("Accept only a browser-bound SP-initiated signed SAML POST response, then redirect without assertion or credentials in the URL.")
		NoSecurity()
		Payload(func() {
			Attribute("providerId", String, func() { Pattern("^[a-zA-Z][a-zA-Z0-9_-]{0,39}$") })
			Attribute("SAMLResponse", String, func() { MinLength(1); MaxLength(262144) })
			Attribute("RelayState", String, func() { Pattern("^[a-f0-9]{64}$") })
			Attribute("cookie", String, func() { MaxLength(16384) })
			Required("providerId", "SAMLResponse", "RelayState", "cookie")
		})
		Result(FederationStartResult)
		Error("unauthenticated", IAMProblem)
		Error("rate_limited", IAMProblem)
		HTTP(func() {
			POST("/api/auth/federation/saml/{providerId}/acs")
			FormRequest()
			Header("cookie:Cookie")
			Response(StatusSeeOther, func() { Header("authorizationUrl:Location"); Header("cookie:Set-Cookie") })
			Response("unauthenticated", StatusUnauthorized)
			Response("rate_limited", StatusTooManyRequests)
		})
	})
	Method("complete_saml", func() {
		NoSecurity()
		Payload(func() {
			Attribute("cookie", String, func() { MaxLength(16384) })
			Attribute("mfaCode", String, func() { Pattern("^[0-9]{6}$") })
			Attribute("recoveryCode", String, func() { Pattern("^[a-f0-9]{32}$") })
			Required("cookie")
		})
		Result(FederationCompleteResult)
		Error("unauthenticated", IAMProblem)
		Error("rate_limited", IAMProblem)
		HTTP(func() {
			POST("/api/auth/federation/saml/complete")
			Header("cookie:Cookie")
			Response(StatusOK, func() { Header("cookie:Set-Cookie") })
			Response("unauthenticated", StatusUnauthorized)
			Response("rate_limited", StatusTooManyRequests)
		})
	})
	Method("federation_providers", func() {
		NoSecurity()
		Result(ArrayOf(FederationProvider))
		HTTP(func() { GET("/api/auth/federation/providers"); Response(StatusOK) })
	})
	Method("start_federation", func() {
		NoSecurity()
		Payload(func() {
			Attribute("providerId", String, func() { Pattern("^[a-zA-Z][a-zA-Z0-9_-]{0,39}$") })
			Attribute("purpose", String, func() { Enum("login", "link") })
			Required("providerId", "purpose")
		})
		Result(FederationStartResult)
		Error("invalid", IAMProblem)
		Error("rate_limited", IAMProblem)
		HTTP(func() {
			POST("/api/auth/federation/start")
			Response(StatusOK, func() { Header("cookie:Set-Cookie") })
			Response("invalid", StatusBadRequest)
			Response("rate_limited", StatusTooManyRequests)
		})
	})
	Method("complete_federation", func() {
		NoSecurity()
		Payload(func() {
			Attribute("state", String, func() { Pattern("^[a-f0-9]{64}$") })
			Attribute("code", String, func() { MinLength(1); MaxLength(4096) })
			Attribute("cookie", String, func() { MaxLength(16384) })
			Attribute("mfaCode", String, func() { Pattern("^[0-9]{6}$") })
			Attribute("recoveryCode", String, func() { Pattern("^[a-f0-9]{32}$") })
			Required("state", "code", "cookie")
		})
		Result(FederationCompleteResult)
		Error("unauthenticated", IAMProblem)
		Error("rate_limited", IAMProblem)
		HTTP(func() {
			POST("/api/auth/federation/complete")
			Header("cookie:Cookie")
			Response(StatusOK, func() { Header("cookie:Set-Cookie") })
			Response("unauthenticated", StatusUnauthorized)
			Response("rate_limited", StatusTooManyRequests)
		})
	})
}
