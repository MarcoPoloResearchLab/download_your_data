package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type frontendDataContract struct {
	Credits                frontendCreditsContract                 `json:"credits"`
	ProviderRegistry       []frontendProviderDefinition            `json:"provider_registry"`
	InstructionScreenshots map[string][]instructionScreenshotAsset `json:"instruction_screenshots"`
	Strings                map[string]frontendLocalizedContract    `json:"strings"`
}

type frontendCreditsContract struct {
	TMDB frontendTMDBCreditsContract `json:"tmdb"`
}

type frontendTMDBCreditsContract struct {
	Notice  string `json:"notice"`
	Website string `json:"website"`
}

type frontendProviderDefinition struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	IconSrc string `json:"icon_src"`
}

type frontendLocalizedContract struct {
	SiteTitle string                      `json:"site_title"`
	UI        map[string]json.RawMessage  `json:"ui"`
	Platforms []frontendLocalizedProvider `json:"platforms"`
}

type frontendLocalizedProvider struct {
	ID    string              `json:"id"`
	Nav   string              `json:"nav"`
	Title string              `json:"title"`
	Intro string              `json:"intro"`
	Steps []frontendStep      `json:"steps"`
	Refs  []frontendReference `json:"refs"`
	Note  *string             `json:"note"`
}

type frontendStep struct {
	Text         string `json:"text"`
	ScreenshotID string `json:"screenshot_id"`
	Alt          string `json:"alt"`
}

type frontendReference struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

func TestFrontendProviderWorkspaceContract(testContext *testing.T) {
	content, readError := os.ReadFile("data.json")
	if readError != nil {
		testContext.Fatalf("read data.json: %v", readError)
	}
	var data frontendDataContract
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&data); decodeError != nil {
		testContext.Fatalf("decode frontend contract: %v", decodeError)
	}

	expectedRegistry := []frontendProviderDefinition{
		{ID: "netflix", Surface: "workspace", IconSrc: "images/providers/netflix.png"},
		{ID: "openai", Surface: "workspace", IconSrc: "images/providers/openai.png"},
		{ID: "facebook", Surface: "guide", IconSrc: "images/providers/facebook.png"},
		{ID: "instagram", Surface: "guide", IconSrc: "images/providers/instagram.png"},
		{ID: "whatsapp", Surface: "guide", IconSrc: "images/providers/whatsapp.png"},
		{ID: "threads", Surface: "guide", IconSrc: "images/providers/threads.png"},
		{ID: "linkedin", Surface: "guide", IconSrc: "images/providers/linkedin.png"},
		{ID: "tiktok", Surface: "guide", IconSrc: "images/providers/tiktok.png"},
		{ID: "x", Surface: "guide", IconSrc: "images/providers/x.png"},
		{ID: "youtube", Surface: "guide", IconSrc: "images/providers/youtube.png"},
		{ID: "google", Surface: "guide", IconSrc: "images/providers/google.png"},
	}
	if !reflect.DeepEqual(data.ProviderRegistry, expectedRegistry) {
		testContext.Fatalf("provider registry = %#v; want %#v", data.ProviderRegistry, expectedRegistry)
	}
	if data.Credits.TMDB.Notice !=
		"This product uses the TMDB API but is not endorsed or certified by TMDB." ||
		data.Credits.TMDB.Website != "https://www.themoviedb.org" {
		testContext.Fatalf("TMDB credits = %+v; want canonical public attribution", data.Credits.TMDB)
	}

	expectedLocales := []string{"en", "es", "fr", "ru"}
	if len(data.Strings) != len(expectedLocales) {
		testContext.Fatalf("locale count = %d; want %d", len(data.Strings), len(expectedLocales))
	}
	var canonicalUIKeys []string
	for _, localeID := range expectedLocales {
		locale, exists := data.Strings[localeID]
		if !exists {
			testContext.Fatalf("localized frontend is missing %q", localeID)
		}
		if strings.TrimSpace(locale.SiteTitle) == "" {
			testContext.Fatalf("locale %q has an empty site title", localeID)
		}
		uiKeys := make([]string, 0, len(locale.UI))
		for key, encoded := range locale.UI {
			uiKeys = append(uiKeys, key)
			if key == "weekdays" {
				var weekdays []string
				if decodeError := json.Unmarshal(encoded, &weekdays); decodeError != nil ||
					len(weekdays) != 7 {
					testContext.Fatalf("locale %q weekdays are invalid: %v", localeID, decodeError)
				}
				continue
			}
			var value string
			if decodeError := json.Unmarshal(encoded, &value); decodeError != nil ||
				strings.TrimSpace(value) == "" {
				testContext.Fatalf("locale %q UI string %q is invalid", localeID, key)
			}
		}
		sort.Strings(uiKeys)
		if canonicalUIKeys == nil {
			canonicalUIKeys = uiKeys
		} else if !reflect.DeepEqual(uiKeys, canonicalUIKeys) {
			testContext.Fatalf("locale %q UI keys differ from English", localeID)
		}
		if len(locale.Platforms) != len(expectedRegistry) {
			testContext.Fatalf(
				"locale %q provider count = %d; want %d",
				localeID,
				len(locale.Platforms),
				len(expectedRegistry),
			)
		}
		for providerIndex, provider := range locale.Platforms {
			if provider.ID != expectedRegistry[providerIndex].ID {
				testContext.Fatalf(
					"locale %q provider %d = %q; want %q",
					localeID,
					providerIndex,
					provider.ID,
					expectedRegistry[providerIndex].ID,
				)
			}
			assets := data.InstructionScreenshots[provider.ID]
			if len(assets) == 0 {
				testContext.Fatalf("locale %q provider %q has no screenshots", localeID, provider.ID)
			}
			assetIDs := make(map[string]struct{}, len(assets))
			for _, asset := range assets {
				if strings.TrimSpace(asset.Href) == "" {
					testContext.Fatalf(
						"locale %q provider %q screenshot %q has no action link",
						localeID,
						provider.ID,
						asset.ID,
					)
				}
				assetIDs[asset.ID] = struct{}{}
			}
			usedAssetIDs := make(map[string]struct{}, len(assets))
			for stepIndex, step := range provider.Steps {
				if strings.TrimSpace(step.Text) == "" ||
					strings.TrimSpace(step.Alt) == "" ||
					strings.TrimSpace(step.ScreenshotID) == "" {
					testContext.Fatalf(
						"locale %q provider %q step %d is incomplete: %+v",
						localeID,
						provider.ID,
						stepIndex,
						step,
					)
				}
				if _, exists := assetIDs[step.ScreenshotID]; !exists {
					testContext.Fatalf(
						"locale %q provider %q step %d references unknown screenshot %q",
						localeID,
						provider.ID,
						stepIndex,
						step.ScreenshotID,
					)
				}
				usedAssetIDs[step.ScreenshotID] = struct{}{}
			}
			if len(usedAssetIDs) != len(assetIDs) {
				testContext.Fatalf(
					"locale %q provider %q leaves screenshots unused",
					localeID,
					provider.ID,
				)
			}
		}
		netflix := locale.Platforms[0]
		if netflix.Title != "Netflix" ||
			strings.TrimSpace(netflix.Intro) == "" ||
			len(netflix.Steps) < 5 ||
			len(netflix.Refs) != 1 ||
			netflix.Refs[0].Href != "https://help.netflix.com/en/node/101917" {
			testContext.Fatalf("locale %q has an incomplete Netflix contract: %+v", localeID, netflix)
		}
		openAI := locale.Platforms[1]
		if openAI.Title != "OpenAI (ChatGPT)" ||
			strings.TrimSpace(openAI.Intro) == "" ||
			len(openAI.Steps) != 7 ||
			len(openAI.Refs) != 1 ||
			openAI.Refs[0].Href !=
				"https://help.openai.com/en/articles/7260999-how-do-i-export-my-chatgpt-history-and-data" ||
			openAI.Note == nil ||
			strings.TrimSpace(*openAI.Note) == "" {
			testContext.Fatalf("locale %q has an incomplete OpenAI guide contract: %+v", localeID, openAI)
		}
		whatsApp := locale.Platforms[4]
		if whatsApp.Title != "WhatsApp" ||
			strings.TrimSpace(whatsApp.Intro) == "" ||
			len(whatsApp.Steps) != 7 ||
			len(whatsApp.Refs) != 2 ||
			whatsApp.Refs[0].Href != "https://faq.whatsapp.com/526463418847093/" ||
			whatsApp.Refs[1].Href != "https://faq.whatsapp.com/1180414079177245/" ||
			whatsApp.Note == nil ||
			strings.TrimSpace(*whatsApp.Note) == "" {
			testContext.Fatalf("locale %q has an incomplete WhatsApp guide contract: %+v", localeID, whatsApp)
		}
		threads := locale.Platforms[5]
		if threads.Title != "Threads" ||
			strings.TrimSpace(threads.Intro) == "" ||
			len(threads.Steps) != 7 ||
			len(threads.Refs) != 1 ||
			threads.Refs[0].Href != "https://www.facebook.com/help/instagram/259803026523198" ||
			threads.Note == nil ||
			strings.TrimSpace(*threads.Note) == "" {
			testContext.Fatalf("locale %q has an incomplete Threads guide contract: %+v", localeID, threads)
		}
	}
	if len(canonicalUIKeys) < 120 {
		testContext.Fatalf("localized UI key count = %d; want at least 120", len(canonicalUIKeys))
	}
}

func TestFrontendAssetsUseCurrentMPRShell(testContext *testing.T) {
	index := readFrontendAsset(testContext, "index.html")
	const sharedStylesheet = "https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.css"
	const sharedConfigScript = "https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui-config.js"
	const sharedBundleScript = "https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.js"
	if !strings.Contains(index, `<script type="module" src="app.js"></script>`) ||
		!strings.Contains(index, `<script src="auth-lifecycle.js"></script>`) ||
		!strings.Contains(index, `<link rel="stylesheet" href="styles.css">`) ||
		!strings.Contains(index, `href="`+sharedStylesheet+`"`) ||
		!strings.Contains(index, `src="https://accounts.google.com/gsi/client" async defer`) ||
		!strings.Contains(index, `src="https://cdn.jsdelivr.net/npm/js-yaml@4.1.0/dist/js-yaml.min.js"`) ||
		!strings.Contains(index, `src="`+sharedConfigScript+`"`) ||
		!strings.Contains(index, `data-mpr-ui-bundle-src="`+sharedBundleScript+`"`) ||
		!strings.Contains(index, `<mpr-header`) ||
		!strings.Contains(index, `data-config-url="/config-ui.yaml"`) ||
		!strings.Contains(index, `completionEvent`) ||
		!strings.Contains(index, `<mpr-user`) ||
		!strings.Contains(index, `<mpr-footer`) ||
		!strings.Contains(index, `slot="brand"`) ||
		!strings.Contains(index, `slot="nav-left"`) ||
		!strings.Contains(index, `slot="aux"`) ||
		!strings.Contains(index, `slot="legal"`) ||
		strings.Count(index, "mpr-ui@latest") != 3 ||
		strings.Contains(index, "mpr-ui@v") ||
		strings.Contains(index, "tauth.js") ||
		strings.Contains(index, "tauth-url=") ||
		strings.Contains(index, "tauth-login-path=") ||
		strings.Contains(index, "tauth-logout-path=") ||
		strings.Contains(index, "tauth-nonce-path=") ||
		strings.Contains(index, `<header class="app-bar"`) ||
		strings.Contains(index, `<footer class="app-footer"`) ||
		strings.Contains(index, "http://") ||
		strings.Contains(strings.ToLower(index), "bootstrap") {
		testContext.Fatalf("index.html is not the canonical authenticated mpr-ui integration")
	}
	if strings.Index(index, `<script type="module" src="app.js"></script>`) >
		strings.Index(index, `src="`+sharedConfigScript+`"`) {
		testContext.Fatalf("app lifecycle listeners are registered after mpr-ui startup")
	}
	if strings.Index(index, `<script src="auth-lifecycle.js"></script>`) >
		strings.Index(index, `src="`+sharedConfigScript+`"`) {
		testContext.Fatalf("auth lifecycle buffer is registered after mpr-ui startup")
	}
	authLifecycleScript := readFrontendAsset(testContext, "auth-lifecycle.js")
	appScript := readFrontendAsset(testContext, "app.js")
	apiScript := readFrontendAsset(testContext, "api.js")
	chartsScript := readFrontendAsset(testContext, "charts.js")
	styles := readFrontendAsset(testContext, "styles.css")
	for path, content := range map[string]string{
		"auth-lifecycle.js": authLifecycleScript,
		"app.js":            appScript,
		"api.js":            apiScript,
		"charts.js":         chartsScript,
		"styles.css":        styles,
	} {
		if strings.Contains(content, "http://") ||
			strings.Contains(content, "https://") ||
			strings.Contains(content, "innerHTML") ||
			strings.Contains(content, "style=") {
			testContext.Fatalf("%s contains a forbidden external or unchecked application boundary", path)
		}
		if strings.HasSuffix(path, ".js") &&
			!strings.HasPrefix(content, "// @ts-check\n") {
			testContext.Fatalf("%s is missing the checked-JavaScript contract", path)
		}
	}
	if !strings.Contains(appScript, "from './api.js'") ||
		!strings.Contains(appScript, "from './charts.js'") ||
		!strings.Contains(authLifecycleScript, "mpr-ui:auth:authenticated") ||
		!strings.Contains(authLifecycleScript, "mpr-ui:auth:unauthenticated") ||
		strings.Contains(authLifecycleScript, "document.cookie") ||
		strings.Contains(authLifecycleScript, "localStorage") ||
		!strings.Contains(appScript, "mpr-ui:auth:authenticated") ||
		!strings.Contains(appScript, "mpr-ui:auth:unauthenticated") ||
		!strings.Contains(appScript, "whenAutoOrchestrationReady") ||
		!strings.Contains(appScript, "download-your-data:app-ready") ||
		strings.Contains(appScript, "resolveAuthProfileSnapshot") ||
		strings.Contains(appScript, "document.cookie") ||
		strings.Contains(appScript, "localStorage.getItem('token") ||
		!strings.Contains(apiScript, "credentials: 'include'") ||
		!strings.Contains(apiScript, "data-api-origin") ||
		strings.Contains(styles, "gradient(") ||
		strings.Contains(styles, "@import") {
		testContext.Fatalf("frontend module or MPR style contract is incomplete")
	}
	config := testRuntimeConfig(testContext)
	securityPolicy := buildContentSecurityPolicy(config)
	if !strings.Contains(securityPolicy, "default-src 'self'") ||
		!strings.Contains(securityPolicy, config.Authentication().APIOrigin()) ||
		!strings.Contains(securityPolicy, config.Authentication().TAuthURL()) ||
		!strings.Contains(securityPolicy, "frame-ancestors 'none'") ||
		strings.Contains(securityPolicy, "unsafe-eval") {
		testContext.Fatalf(
			"content security policy does not isolate the shared shell: %s",
			securityPolicy,
		)
	}
}

func readFrontendAsset(testContext *testing.T, path string) string {
	testContext.Helper()
	content, readError := os.ReadFile(path)
	if readError != nil {
		testContext.Fatalf("read %s: %v", path, readError)
	}
	return string(content)
}
