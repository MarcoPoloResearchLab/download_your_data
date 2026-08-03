package uiconfig

import (
	"strings"
	"testing"
)

func TestRenderProducesTheSharedBrowserAuthenticationContract(testContext *testing.T) {
	encoded, renderError := Render(validInput())
	if renderError != nil {
		testContext.Fatalf("render browser configuration: %v", renderError)
	}
	document := string(encoded)
	for _, expected := range []string{
		"description: Download Your Data",
		"- https://dyd.mprlab.com",
		"tauthUrl: https://dyd-api.mprlab.com",
		"googleClientId: 283383931996-test.apps.googleusercontent.com",
		"tenantId: download-your-data",
		"loginPath: /auth/google",
		"logoutPath: /auth/logout",
		"noncePath: /auth/nonce",
		"sessionPath: /auth/session",
	} {
		if !strings.Contains(document, expected) {
			testContext.Fatalf("browser configuration is missing %q:\n%s", expected, document)
		}
	}
}

func TestRenderAcceptsOnlyHTTPSOrLoopbackOrigins(testContext *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:4173",
		"http://[::1]:4173",
		"http://localhost:4173",
		"https://dyd.mprlab.com",
	} {
		input := validInput()
		input.PublicOrigin = origin
		if _, renderError := Render(input); renderError != nil {
			testContext.Fatalf("accepted origin %q failed: %v", origin, renderError)
		}
	}

	for _, origin := range []string{
		"",
		"http://dyd.mprlab.com",
		"https://user@dyd.mprlab.com",
		"https://dyd.mprlab.com/path",
		"https://dyd.mprlab.com?preview=true",
	} {
		input := validInput()
		input.PublicOrigin = origin
		if _, renderError := Render(input); renderError == nil {
			testContext.Fatalf("invalid origin %q was accepted", origin)
		}
	}
}

func TestRenderRejectsInvalidAuthenticationInputs(testContext *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*Input)
	}{
		{name: "description", mutate: func(input *Input) { input.Description = " " }},
		{name: "client ID", mutate: func(input *Input) { input.GoogleWebClientID = "native-client" }},
		{name: "tenant", mutate: func(input *Input) { input.TenantID = "Download_Your_Data" }},
		{name: "path", mutate: func(input *Input) { input.SessionPath = "auth/session" }},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			input := validInput()
			testCase.mutate(&input)
			if _, renderError := Render(input); renderError == nil {
				testContext.Fatalf("invalid %s was accepted", testCase.name)
			}
		})
	}
}

func validInput() Input {
	return Input{
		Description:       "Download Your Data",
		PublicOrigin:      "https://dyd.mprlab.com",
		TAuthOrigin:       "https://dyd-api.mprlab.com",
		GoogleWebClientID: "283383931996-test.apps.googleusercontent.com",
		TenantID:          "download-your-data",
		LoginPath:         "/auth/google",
		LogoutPath:        "/auth/logout",
		NoncePath:         "/auth/nonce",
		SessionPath:       "/auth/session",
	}
}
