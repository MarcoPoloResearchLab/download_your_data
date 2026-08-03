package productionprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedProductionProfileIsExact(testContext *testing.T) {
	profile, loadError := Load(filepath.Join("..", "..", "configs", "production.yml"))
	if loadError != nil {
		testContext.Fatalf("load committed production profile: %v", loadError)
	}

	if profile.Browser.PublicOrigin != "https://dyd.mprlab.com" ||
		profile.Browser.APIOrigin != "https://dyd-api.mprlab.com" ||
		profile.Browser.TAuthOrigin != profile.Browser.APIOrigin ||
		profile.Browser.TenantID != "download-your-data" ||
		profile.Session.CookieDomain != ".mprlab.com" ||
		profile.Session.SessionCookieName != "download_your_data_session" ||
		profile.Session.RefreshCookieName != "download_your_data_refresh" ||
		!profile.Session.Secure ||
		profile.Session.SameSite != "None" ||
		profile.CORS.AllowedOrigin != profile.Browser.PublicOrigin ||
		!profile.CORS.Credentials ||
		profile.Runtime.ContainerPort != 8787 ||
		profile.Runtime.HealthPath != "/api/health" ||
		profile.Runtime.DataMount != "/var/lib/download-your-data" {
		testContext.Fatalf("committed production profile drifted: %+v", profile)
	}
}

func TestLoadRejectsNonCanonicalProductionProfiles(testContext *testing.T) {
	committedPath := filepath.Join("..", "..", "configs", "production.yml")
	committed, readError := os.ReadFile(committedPath)
	if readError != nil {
		testContext.Fatalf("read committed production profile: %v", readError)
	}

	testCases := []struct {
		name    string
		encoded string
	}{
		{
			name: "unknown field",
			encoded: strings.Replace(
				string(committed),
				"schema_version: 1",
				"schema_version: 1\nlegacy_profile: true",
				1,
			),
		},
		{
			name: "same frontend and API origin",
			encoded: strings.Replace(
				string(committed),
				"api_origin: https://dyd-api.mprlab.com",
				"api_origin: https://dyd.mprlab.com",
				1,
			),
		},
		{
			name: "insecure hosted cookie",
			encoded: strings.Replace(
				string(committed),
				"secure: true",
				"secure: false",
				1,
			),
		},
		{
			name: "noncanonical cookie name",
			encoded: strings.Replace(
				string(committed),
				"session_cookie_name: download_your_data_session",
				"session_cookie_name: Download-Your-Data",
				1,
			),
		},
		{
			name:    "second YAML document",
			encoded: string(committed) + "---\nschema_version: 1\n",
		},
		{
			name:    "oversized profile",
			encoded: string(committed) + strings.Repeat("#", maximumProfileBytes),
		},
	}

	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			profilePath := filepath.Join(testContext.TempDir(), "production.yml")
			if writeError := os.WriteFile(profilePath, []byte(testCase.encoded), 0o600); writeError != nil {
				testContext.Fatalf("write invalid production profile: %v", writeError)
			}
			if _, loadError := Load(profilePath); loadError == nil {
				testContext.Fatalf("invalid production profile was accepted")
			}
		})
	}
}
