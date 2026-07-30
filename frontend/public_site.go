package frontend

import (
	"bytes"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	resourcesIndexPath = "/resources/"
	sitemapPath        = "/sitemap.xml"
	robotsPath         = "/robots.txt"
	englishLocale      = "en"
	resourceDateLayout = "2006-01-02"
	sourceRepository   = "https://github.com/MarcoPoloResearchLab/download_your_data/blob/master/"
)

var resourceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

//go:embed content/resources.json content/application.json manifests/instruction-screenshots.json templates/*.html
var publicSiteSources embed.FS

// Site is an immutable collection of rendered public documents whose absolute
// indexing signals are bound to one validated public origin.
type Site interface {
	Paths() []string
	Read(path string) (body []byte, contentType string, found bool)
}

type publicSite struct {
	paths     []string
	documents map[string]publicDocument
}

type publicDocument struct {
	body        []byte
	contentType string
}

type resourceRegistry struct {
	SchemaVersion     int                  `json:"schema_version"`
	SignificantUpdate string               `json:"significant_update"`
	Author            resourceAuthor       `json:"author"`
	Hub               resourceHub          `json:"hub"`
	Resources         []resourceDefinition `json:"resources"`
}

type resourceAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type resourceHub struct {
	MetaTitle       string `json:"meta_title"`
	MetaDescription string `json:"meta_description"`
	H1              string `json:"h1"`
	Intro           string `json:"intro"`
	QuickVerdict    string `json:"quick_verdict"`
}

type resourceDefinition struct {
	Slug            string          `json:"slug"`
	ProviderID      string          `json:"provider_id"`
	Kind            string          `json:"kind"`
	MetaTitle       string          `json:"meta_title"`
	MetaDescription string          `json:"meta_description"`
	H1              string          `json:"h1"`
	PrimaryKeyword  string          `json:"primary_keyword"`
	Intro           string          `json:"intro"`
	Verdict         resourceVerdict `json:"verdict"`
	Problem         string          `json:"problem"`
	Coverage        []string        `json:"coverage"`
	Limitations     []string        `json:"limitations"`
	WorkflowSteps   []string        `json:"workflow_steps"`
	Snippet         resourceSnippet `json:"snippet"`
	FAQs            []resourceFAQ   `json:"faqs"`
	RelatedSlugs    []string        `json:"related_slugs"`
	CTALabel        string          `json:"cta_label"`
}

type resourceVerdict struct {
	BestFor       string `json:"best_for"`
	RequiredInput string `json:"required_input"`
	ProductFit    string `json:"product_fit"`
}

type resourceSnippet struct {
	Label      string `json:"label"`
	Content    string `json:"content"`
	SourcePath string `json:"source_path"`
}

type resourceFAQ struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type applicationResourceData struct {
	Credits                json.RawMessage                     `json:"credits"`
	ProviderRegistry       []applicationProviderDefinition     `json:"provider_registry"`
	InstructionScreenshots map[string][]applicationScreenshot  `json:"instruction_screenshots"`
	Strings                map[string]applicationLocalizedData `json:"strings"`
}

type applicationProviderDefinition struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	IconSrc string `json:"icon_src"`
}

type applicationScreenshot struct {
	ID   string `json:"id"`
	Src  string `json:"src"`
	Href string `json:"href"`
}

type applicationLocalizedData struct {
	SiteTitle string                         `json:"site_title"`
	UI        map[string]json.RawMessage     `json:"ui"`
	Platforms []applicationLocalizedProvider `json:"platforms"`
}

type applicationLocalizedProvider struct {
	ID    string                 `json:"id"`
	Nav   string                 `json:"nav"`
	Title string                 `json:"title"`
	Intro string                 `json:"intro"`
	Steps []applicationGuideStep `json:"steps"`
	Refs  []applicationReference `json:"refs"`
	Note  *string                `json:"note"`
}

type applicationGuideStep struct {
	Text         string `json:"text"`
	ScreenshotID string `json:"screenshot_id"`
	Alt          string `json:"alt"`
}

type applicationReference struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type screenshotManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	Screenshots   []screenshotManifestRecord `json:"screenshots"`
}

type screenshotManifestRecord struct {
	ID              string                    `json:"id"`
	OutputPath      string                    `json:"output_path"`
	PixelDimensions screenshotPixelDimensions `json:"pixel_dimensions"`
	ReviewStatus    string                    `json:"review_status"`
	Provenance      screenshotProvenance      `json:"provenance"`
}

type screenshotPixelDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type screenshotProvenance struct {
	Attribution string `json:"attribution"`
}

type resourcePageData struct {
	MetaTitle                string
	MetaDescription          string
	CanonicalURL             string
	StructuredData           template.JS
	H1                       string
	Intro                    string
	ProviderName             string
	BestFor                  string
	RequiredInput            string
	ProductFit               string
	Problem                  string
	StepsTitle               string
	Steps                    []resourcePageStep
	Visuals                  []resourcePageVisual
	Coverage                 []string
	Limitations              []string
	SnippetLabel             string
	SnippetContent           string
	SnippetSourcePath        string
	SnippetSourceURL         string
	OfficialReferences       []resourcePageReference
	ProviderNote             string
	FAQs                     []resourceFAQ
	RelatedResources         []resourceCardData
	AuthorName               string
	AuthorURL                string
	SignificantUpdate        string
	SignificantUpdateDisplay string
	CTAURL                   string
	CTALabel                 string
}

type resourcePageStep struct {
	Number    int
	Text      string
	ActionURL string
}

type resourcePageVisual struct {
	ImageURL    string
	ActionURL   string
	Alt         string
	Width       int
	Height      int
	Attribution string
}

type resourcePageReference struct {
	Label string
	URL   string
}

type resourceCardData struct {
	Path            string
	H1              string
	MetaDescription string
	ProviderName    string
	IconURL         string
	BestFor         string
	RequiredInput   string
}

type resourceHubPageData struct {
	MetaTitle                string
	MetaDescription          string
	CanonicalURL             string
	StructuredData           template.JS
	H1                       string
	Intro                    string
	QuickVerdict             string
	Resources                []resourceCardData
	AuthorName               string
	AuthorURL                string
	SignificantUpdate        string
	SignificantUpdateDisplay string
}

type sitemapDocument struct {
	XMLName xml.Name       `xml:"urlset"`
	XMLNS   string         `xml:"xmlns,attr"`
	URLs    []sitemapEntry `xml:"url"`
}

type sitemapEntry struct {
	Location     string `xml:"loc"`
	LastModified string `xml:"lastmod"`
}

// NewPublicSite validates the repository-owned SEO registry and renders every
// indexable public document against the selected public origin.
func NewPublicSite(publicOrigin string) (Site, error) {
	origin, originError := validatePublicOrigin(publicOrigin)
	if originError != nil {
		return nil, originError
	}
	registry, registryError := loadResourceRegistry()
	if registryError != nil {
		return nil, registryError
	}
	applicationData, applicationError := loadApplicationResourceData()
	if applicationError != nil {
		return nil, applicationError
	}
	manifest, manifestError := loadScreenshotManifest()
	if manifestError != nil {
		return nil, manifestError
	}
	if validationError := validateResourceRegistry(
		registry,
		applicationData,
		manifest,
	); validationError != nil {
		return nil, validationError
	}
	return renderPublicSite(origin, registry, applicationData, manifest)
}

func (site *publicSite) Paths() []string {
	return slices.Clone(site.paths)
}

func (site *publicSite) Read(path string) ([]byte, string, bool) {
	document, exists := site.documents[path]
	if !exists {
		return nil, "", false
	}
	return bytes.Clone(document.body), document.contentType, true
}

func validatePublicOrigin(rawOrigin string) (string, error) {
	parsedOrigin, parseError := url.Parse(rawOrigin)
	if parseError != nil ||
		(parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") ||
		parsedOrigin.Host == "" ||
		parsedOrigin.User != nil ||
		parsedOrigin.RawQuery != "" ||
		parsedOrigin.Fragment != "" ||
		(parsedOrigin.Path != "" && parsedOrigin.Path != "/") {
		return "", errors.New("build public site: public origin must be an HTTP origin without credentials, path, query, or fragment")
	}
	parsedOrigin.Path = ""
	return parsedOrigin.String(), nil
}

func loadResourceRegistry() (resourceRegistry, error) {
	var registry resourceRegistry
	if decodeError := decodeStrictJSON(
		publicSiteSources,
		"content/resources.json",
		&registry,
	); decodeError != nil {
		return resourceRegistry{}, fmt.Errorf("load public resource registry: %w", decodeError)
	}
	return registry, nil
}

func loadApplicationResourceData() (applicationResourceData, error) {
	var applicationData applicationResourceData
	if decodeError := decodeStrictJSON(
		publicSiteSources,
		"content/application.json",
		&applicationData,
	); decodeError != nil {
		return applicationResourceData{}, fmt.Errorf("load application resource data: %w", decodeError)
	}
	return applicationData, nil
}

func loadScreenshotManifest() (screenshotManifest, error) {
	encodedManifest, readError := publicSiteSources.ReadFile(
		"manifests/instruction-screenshots.json",
	)
	if readError != nil {
		return screenshotManifest{}, fmt.Errorf("read screenshot manifest: %w", readError)
	}
	var manifest screenshotManifest
	if decodeError := json.Unmarshal(encodedManifest, &manifest); decodeError != nil {
		return screenshotManifest{}, fmt.Errorf("decode screenshot manifest: %w", decodeError)
	}
	return manifest, nil
}

func decodeStrictJSON(source embed.FS, path string, destination any) error {
	encodedValue, readError := source.ReadFile(path)
	if readError != nil {
		return fmt.Errorf("read %s: %w", path, readError)
	}
	decoder := json.NewDecoder(bytes.NewReader(encodedValue))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(destination); decodeError != nil {
		return fmt.Errorf("decode %s: %w", path, decodeError)
	}
	if trailingError := decoder.Decode(&struct{}{}); !errors.Is(trailingError, io.EOF) {
		return fmt.Errorf("decode %s: trailing content is not allowed", path)
	}
	return nil
}

func validateResourceRegistry(
	registry resourceRegistry,
	applicationData applicationResourceData,
	manifest screenshotManifest,
) error {
	if registry.SchemaVersion != 1 {
		return fmt.Errorf(
			"validate public resource registry: schema version = %d; want 1",
			registry.SchemaVersion,
		)
	}
	if _, dateError := time.Parse(resourceDateLayout, registry.SignificantUpdate); dateError != nil {
		return fmt.Errorf("validate public resource registry: significant update: %w", dateError)
	}
	if strings.TrimSpace(registry.Author.Name) == "" ||
		!strings.HasPrefix(registry.Author.URL, "https://github.com/") {
		return errors.New("validate public resource registry: author name and GitHub URL are required")
	}
	if metadataError := validateMetadata(
		"resource hub",
		registry.Hub.MetaTitle,
		registry.Hub.MetaDescription,
	); metadataError != nil {
		return metadataError
	}
	if strings.TrimSpace(registry.Hub.H1) == "" ||
		strings.TrimSpace(registry.Hub.Intro) == "" ||
		strings.TrimSpace(registry.Hub.QuickVerdict) == "" {
		return errors.New("validate public resource registry: hub copy is incomplete")
	}
	if len(registry.Resources) < 2 {
		return errors.New("validate public resource registry: at least two distinct resources are required")
	}

	english, englishExists := applicationData.Strings[englishLocale]
	if !englishExists {
		return errors.New("validate public resource registry: English application content is required")
	}
	providers := make(map[string]applicationLocalizedProvider, len(english.Platforms))
	for _, provider := range english.Platforms {
		providers[provider.ID] = provider
	}
	providerIcons := make(map[string]string, len(applicationData.ProviderRegistry))
	for _, provider := range applicationData.ProviderRegistry {
		providerIcons[provider.ID] = provider.IconSrc
	}
	manifestAssets := make(map[string]screenshotManifestRecord, len(manifest.Screenshots))
	for _, asset := range manifest.Screenshots {
		manifestAssets[asset.ID] = asset
	}

	resourcesBySlug := make(map[string]resourceDefinition, len(registry.Resources))
	providerExportCoverage := make(map[string]struct{}, len(providers))
	metaTitles := make(map[string]string, len(registry.Resources))
	metaDescriptions := make(map[string]string, len(registry.Resources))
	headings := make(map[string]string, len(registry.Resources))
	analysisPageCount := 0
	for resourceIndex, resource := range registry.Resources {
		label := fmt.Sprintf("resource %d (%s)", resourceIndex+1, resource.Slug)
		if !resourceSlugPattern.MatchString(resource.Slug) {
			return fmt.Errorf("validate public resource registry: %s has an invalid slug", label)
		}
		if _, duplicate := resourcesBySlug[resource.Slug]; duplicate {
			return fmt.Errorf("validate public resource registry: duplicate slug %q", resource.Slug)
		}
		resourcesBySlug[resource.Slug] = resource
		provider, providerExists := providers[resource.ProviderID]
		if !providerExists {
			return fmt.Errorf(
				"validate public resource registry: %s references unknown provider %q",
				label,
				resource.ProviderID,
			)
		}
		if strings.TrimSpace(providerIcons[resource.ProviderID]) == "" {
			return fmt.Errorf(
				"validate public resource registry: %s provider has no icon",
				label,
			)
		}
		switch resource.Kind {
		case "provider-export":
			if len(resource.WorkflowSteps) != 0 {
				return fmt.Errorf(
					"validate public resource registry: %s duplicates provider workflow steps",
					label,
				)
			}
			providerExportCoverage[resource.ProviderID] = struct{}{}
		case "netflix-analysis":
			analysisPageCount++
			if resource.ProviderID != "netflix" || len(resource.WorkflowSteps) < 4 {
				return fmt.Errorf(
					"validate public resource registry: %s has an invalid Netflix analysis workflow",
					label,
				)
			}
		default:
			return fmt.Errorf(
				"validate public resource registry: %s has unknown kind %q",
				label,
				resource.Kind,
			)
		}
		if metadataError := validateMetadata(
			label,
			resource.MetaTitle,
			resource.MetaDescription,
		); metadataError != nil {
			return metadataError
		}
		for value, values := range map[string]map[string]string{
			resource.MetaTitle:       metaTitles,
			resource.MetaDescription: metaDescriptions,
			resource.H1:              headings,
		} {
			if existingSlug, duplicate := values[value]; duplicate {
				return fmt.Errorf(
					"validate public resource registry: resources %q and %q reuse page copy %q",
					existingSlug,
					resource.Slug,
					value,
				)
			}
			values[value] = resource.Slug
		}
		if strings.TrimSpace(resource.H1) == "" ||
			strings.TrimSpace(resource.PrimaryKeyword) == "" ||
			strings.TrimSpace(resource.Intro) == "" ||
			!containsKeywordTerms(
				resource.MetaTitle+" "+resource.H1,
				resource.PrimaryKeyword,
			) {
			return fmt.Errorf(
				"validate public resource registry: %s has incomplete search-intent copy",
				label,
			)
		}
		if strings.TrimSpace(resource.Verdict.BestFor) == "" ||
			strings.TrimSpace(resource.Verdict.RequiredInput) == "" ||
			strings.TrimSpace(resource.Verdict.ProductFit) == "" ||
			strings.TrimSpace(resource.Problem) == "" {
			return fmt.Errorf(
				"validate public resource registry: %s has an incomplete quick verdict",
				label,
			)
		}
		if len(resource.Coverage) < 3 ||
			len(resource.Limitations) < 2 ||
			len(resource.FAQs) < 3 ||
			len(resource.RelatedSlugs) < 2 {
			return fmt.Errorf(
				"validate public resource registry: %s lacks standalone page depth",
				label,
			)
		}
		if strings.TrimSpace(resource.Snippet.Label) == "" ||
			strings.TrimSpace(resource.Snippet.Content) == "" ||
			strings.TrimSpace(resource.Snippet.SourcePath) == "" ||
			strings.TrimSpace(resource.CTALabel) == "" {
			return fmt.Errorf(
				"validate public resource registry: %s lacks repository evidence or CTA",
				label,
			)
		}
		for faqIndex, faq := range resource.FAQs {
			if strings.TrimSpace(faq.Question) == "" ||
				strings.TrimSpace(faq.Answer) == "" {
				return fmt.Errorf(
					"validate public resource registry: %s FAQ %d is incomplete",
					label,
					faqIndex+1,
				)
			}
		}
		for _, step := range provider.Steps {
			asset, assetExists := manifestAssets[step.ScreenshotID]
			if !assetExists ||
				asset.ReviewStatus != "approved" ||
				asset.PixelDimensions.Width <= 0 ||
				asset.PixelDimensions.Height <= 0 ||
				strings.TrimSpace(asset.Provenance.Attribution) == "" {
				return fmt.Errorf(
					"validate public resource registry: %s screenshot %q is not publication-ready",
					label,
					step.ScreenshotID,
				)
			}
		}
	}
	if len(providerExportCoverage) != len(providers) {
		return fmt.Errorf(
			"validate public resource registry: provider export coverage = %d; want %d",
			len(providerExportCoverage),
			len(providers),
		)
	}
	if analysisPageCount != 1 {
		return fmt.Errorf(
			"validate public resource registry: Netflix analysis page count = %d; want 1",
			analysisPageCount,
		)
	}
	for _, resource := range registry.Resources {
		relatedSeen := make(map[string]struct{}, len(resource.RelatedSlugs))
		for _, relatedSlug := range resource.RelatedSlugs {
			if relatedSlug == resource.Slug {
				return fmt.Errorf(
					"validate public resource registry: resource %q links to itself",
					resource.Slug,
				)
			}
			if _, exists := resourcesBySlug[relatedSlug]; !exists {
				return fmt.Errorf(
					"validate public resource registry: resource %q links to unknown resource %q",
					resource.Slug,
					relatedSlug,
				)
			}
			if _, duplicate := relatedSeen[relatedSlug]; duplicate {
				return fmt.Errorf(
					"validate public resource registry: resource %q repeats related resource %q",
					resource.Slug,
					relatedSlug,
				)
			}
			relatedSeen[relatedSlug] = struct{}{}
		}
	}
	return nil
}

func validateMetadata(label string, title string, description string) error {
	titleLength := utf8.RuneCountInString(title)
	descriptionLength := utf8.RuneCountInString(description)
	if titleLength < 50 || titleLength > 60 {
		return fmt.Errorf(
			"validate public resource registry: %s title length = %d; want 50 through 60",
			label,
			titleLength,
		)
	}
	if descriptionLength < 120 || descriptionLength > 155 {
		return fmt.Errorf(
			"validate public resource registry: %s description length = %d; want 120 through 155",
			label,
			descriptionLength,
		)
	}
	return nil
}

func containsKeywordTerms(searchSurface string, keyword string) bool {
	normalizedSurface := strings.ToLower(searchSurface)
	for _, keywordTerm := range strings.Fields(strings.ToLower(keyword)) {
		if !strings.Contains(normalizedSurface, keywordTerm) {
			return false
		}
	}
	return true
}

func renderPublicSite(
	origin string,
	registry resourceRegistry,
	applicationData applicationResourceData,
	manifest screenshotManifest,
) (Site, error) {
	resourceTemplate, resourceTemplateError := template.ParseFS(
		publicSiteSources,
		"templates/resource.html",
	)
	if resourceTemplateError != nil {
		return nil, fmt.Errorf("parse public resource template: %w", resourceTemplateError)
	}
	hubTemplate, hubTemplateError := template.ParseFS(
		publicSiteSources,
		"templates/resources-index.html",
	)
	if hubTemplateError != nil {
		return nil, fmt.Errorf("parse public resource hub template: %w", hubTemplateError)
	}
	english := applicationData.Strings[englishLocale]
	providers := make(map[string]applicationLocalizedProvider, len(english.Platforms))
	for _, provider := range english.Platforms {
		providers[provider.ID] = provider
	}
	providerDefinitions := make(map[string]applicationProviderDefinition, len(applicationData.ProviderRegistry))
	for _, provider := range applicationData.ProviderRegistry {
		providerDefinitions[provider.ID] = provider
	}
	manifestAssets := make(map[string]screenshotManifestRecord, len(manifest.Screenshots))
	for _, asset := range manifest.Screenshots {
		manifestAssets[asset.ID] = asset
	}
	resourcesBySlug := make(map[string]resourceDefinition, len(registry.Resources))
	for _, resource := range registry.Resources {
		resourcesBySlug[resource.Slug] = resource
	}
	significantUpdateDisplay, dateError := displayResourceDate(registry.SignificantUpdate)
	if dateError != nil {
		return nil, dateError
	}

	documents := make(map[string]publicDocument, len(registry.Resources)+3)
	paths := make([]string, 0, len(registry.Resources)+3)
	cards := make([]resourceCardData, 0, len(registry.Resources))
	for _, resource := range registry.Resources {
		provider := providers[resource.ProviderID]
		providerDefinition := providerDefinitions[resource.ProviderID]
		path := resourcePath(resource.Slug)
		card := resourceCardData{
			Path:            path,
			H1:              resource.H1,
			MetaDescription: resource.MetaDescription,
			ProviderName:    provider.Title,
			IconURL:         "/" + providerDefinition.IconSrc,
			BestFor:         resource.Verdict.BestFor,
			RequiredInput:   resource.Verdict.RequiredInput,
		}
		cards = append(cards, card)
		pageData, pageDataError := buildResourcePageData(
			origin,
			registry,
			resource,
			provider,
			applicationData.InstructionScreenshots[resource.ProviderID],
			manifestAssets,
			resourcesBySlug,
			providers,
			significantUpdateDisplay,
		)
		if pageDataError != nil {
			return nil, pageDataError
		}
		var renderedPage bytes.Buffer
		if executeError := resourceTemplate.Execute(&renderedPage, pageData); executeError != nil {
			return nil, fmt.Errorf("render public resource %q: %w", resource.Slug, executeError)
		}
		documents[path] = publicDocument{
			body:        renderedPage.Bytes(),
			contentType: "text/html; charset=utf-8",
		}
	}

	hubStructuredData, structuredDataError := buildHubStructuredData(
		origin,
		registry,
	)
	if structuredDataError != nil {
		return nil, structuredDataError
	}
	hubData := resourceHubPageData{
		MetaTitle:                registry.Hub.MetaTitle,
		MetaDescription:          registry.Hub.MetaDescription,
		CanonicalURL:             origin + resourcesIndexPath,
		StructuredData:           hubStructuredData,
		H1:                       registry.Hub.H1,
		Intro:                    registry.Hub.Intro,
		QuickVerdict:             registry.Hub.QuickVerdict,
		Resources:                cards,
		AuthorName:               registry.Author.Name,
		AuthorURL:                registry.Author.URL,
		SignificantUpdate:        registry.SignificantUpdate,
		SignificantUpdateDisplay: significantUpdateDisplay,
	}
	var renderedHub bytes.Buffer
	if executeError := hubTemplate.Execute(&renderedHub, hubData); executeError != nil {
		return nil, fmt.Errorf("render public resource hub: %w", executeError)
	}
	documents[resourcesIndexPath] = publicDocument{
		body:        renderedHub.Bytes(),
		contentType: "text/html; charset=utf-8",
	}

	sitemap, sitemapError := buildSitemap(origin, registry)
	if sitemapError != nil {
		return nil, sitemapError
	}
	documents[sitemapPath] = publicDocument{
		body:        sitemap,
		contentType: "application/xml; charset=utf-8",
	}
	documents[robotsPath] = publicDocument{
		body: []byte(
			"User-agent: *\n" +
				"Allow: /\n" +
				"Sitemap: " + origin + sitemapPath + "\n",
		),
		contentType: "text/plain; charset=utf-8",
	}

	paths = append(paths, resourcesIndexPath)
	for _, resource := range registry.Resources {
		paths = append(paths, resourcePath(resource.Slug))
	}
	paths = append(paths, sitemapPath, robotsPath)
	return &publicSite{
		paths:     paths,
		documents: documents,
	}, nil
}

func buildResourcePageData(
	origin string,
	registry resourceRegistry,
	resource resourceDefinition,
	provider applicationLocalizedProvider,
	screenshotAssets []applicationScreenshot,
	manifestAssets map[string]screenshotManifestRecord,
	resourcesBySlug map[string]resourceDefinition,
	providers map[string]applicationLocalizedProvider,
	significantUpdateDisplay string,
) (resourcePageData, error) {
	path := resourcePath(resource.Slug)
	steps, visuals, stepsTitle, stepsError := buildResourceWorkflow(
		resource,
		provider,
		screenshotAssets,
		manifestAssets,
	)
	if stepsError != nil {
		return resourcePageData{}, stepsError
	}
	relatedResources := make([]resourceCardData, 0, len(resource.RelatedSlugs))
	for _, relatedSlug := range resource.RelatedSlugs {
		related := resourcesBySlug[relatedSlug]
		relatedProvider := providers[related.ProviderID]
		relatedResources = append(relatedResources, resourceCardData{
			Path:         resourcePath(related.Slug),
			H1:           related.H1,
			ProviderName: relatedProvider.Title,
		})
	}
	officialReferences := make([]resourcePageReference, 0, len(provider.Refs))
	for _, reference := range provider.Refs {
		officialReferences = append(officialReferences, resourcePageReference{
			Label: reference.Label,
			URL:   reference.Href,
		})
	}
	providerNote := ""
	if provider.Note != nil {
		providerNote = *provider.Note
	}
	structuredData, structuredDataError := buildResourceStructuredData(
		origin,
		registry,
		resource,
		provider.Title,
	)
	if structuredDataError != nil {
		return resourcePageData{}, structuredDataError
	}
	ctaURL := "/#guide/" + resource.ProviderID
	if resource.Kind == "netflix-analysis" {
		ctaURL = "/#app/netflix"
	}
	return resourcePageData{
		MetaTitle:                resource.MetaTitle,
		MetaDescription:          resource.MetaDescription,
		CanonicalURL:             origin + path,
		StructuredData:           structuredData,
		H1:                       resource.H1,
		Intro:                    resource.Intro,
		ProviderName:             provider.Title,
		BestFor:                  resource.Verdict.BestFor,
		RequiredInput:            resource.Verdict.RequiredInput,
		ProductFit:               resource.Verdict.ProductFit,
		Problem:                  resource.Problem,
		StepsTitle:               stepsTitle,
		Steps:                    steps,
		Visuals:                  visuals,
		Coverage:                 slices.Clone(resource.Coverage),
		Limitations:              slices.Clone(resource.Limitations),
		SnippetLabel:             resource.Snippet.Label,
		SnippetContent:           resource.Snippet.Content,
		SnippetSourcePath:        resource.Snippet.SourcePath,
		SnippetSourceURL:         sourceRepository + resource.Snippet.SourcePath,
		OfficialReferences:       officialReferences,
		ProviderNote:             providerNote,
		FAQs:                     slices.Clone(resource.FAQs),
		RelatedResources:         relatedResources,
		AuthorName:               registry.Author.Name,
		AuthorURL:                registry.Author.URL,
		SignificantUpdate:        registry.SignificantUpdate,
		SignificantUpdateDisplay: significantUpdateDisplay,
		CTAURL:                   ctaURL,
		CTALabel:                 resource.CTALabel,
	}, nil
}

func buildResourceWorkflow(
	resource resourceDefinition,
	provider applicationLocalizedProvider,
	screenshotAssets []applicationScreenshot,
	manifestAssets map[string]screenshotManifestRecord,
) ([]resourcePageStep, []resourcePageVisual, string, error) {
	assetsByID := make(map[string]applicationScreenshot, len(screenshotAssets))
	for _, asset := range screenshotAssets {
		assetsByID[asset.ID] = asset
	}
	steps := make([]resourcePageStep, 0, max(len(provider.Steps), len(resource.WorkflowSteps)))
	if resource.Kind == "netflix-analysis" {
		for stepIndex, step := range resource.WorkflowSteps {
			steps = append(steps, resourcePageStep{
				Number: stepIndex + 1,
				Text:   step,
			})
		}
	} else {
		for stepIndex, step := range provider.Steps {
			asset, assetExists := assetsByID[step.ScreenshotID]
			if !assetExists {
				return nil, nil, "", fmt.Errorf(
					"build public resource %q: guide step references unknown screenshot %q",
					resource.Slug,
					step.ScreenshotID,
				)
			}
			steps = append(steps, resourcePageStep{
				Number:    stepIndex + 1,
				Text:      step.Text,
				ActionURL: asset.Href,
			})
		}
	}

	visuals := make([]resourcePageVisual, 0, len(screenshotAssets))
	visualSeen := make(map[string]struct{}, len(screenshotAssets))
	for _, step := range provider.Steps {
		if _, exists := visualSeen[step.ScreenshotID]; exists {
			continue
		}
		asset := assetsByID[step.ScreenshotID]
		manifestAsset := manifestAssets[step.ScreenshotID]
		visuals = append(visuals, resourcePageVisual{
			ImageURL:    "/" + asset.Src,
			ActionURL:   asset.Href,
			Alt:         step.Alt,
			Width:       manifestAsset.PixelDimensions.Width,
			Height:      manifestAsset.PixelDimensions.Height,
			Attribution: manifestAsset.Provenance.Attribution,
		})
		visualSeen[step.ScreenshotID] = struct{}{}
	}
	if resource.Kind == "netflix-analysis" {
		return steps, visuals, "How the private Netflix analysis works", nil
	}
	return steps, visuals, "How to complete the current provider export", nil
}

func buildResourceStructuredData(
	origin string,
	registry resourceRegistry,
	resource resourceDefinition,
	providerName string,
) (template.JS, error) {
	canonicalURL := origin + resourcePath(resource.Slug)
	faqEntities := make([]map[string]any, 0, len(resource.FAQs))
	for _, faq := range resource.FAQs {
		faqEntities = append(faqEntities, map[string]any{
			"@type": "Question",
			"name":  faq.Question,
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  faq.Answer,
			},
		})
	}
	document := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			map[string]any{
				"@type":            "Article",
				"headline":         resource.H1,
				"description":      resource.MetaDescription,
				"url":              canonicalURL,
				"mainEntityOfPage": canonicalURL,
				"datePublished":    registry.SignificantUpdate,
				"dateModified":     registry.SignificantUpdate,
				"author": map[string]any{
					"@type": "Person",
					"name":  registry.Author.Name,
					"url":   registry.Author.URL,
				},
				"publisher": map[string]any{
					"@type": "Organization",
					"name":  "Marco Polo Research Lab",
					"url":   "https://mprlab.com/",
				},
			},
			map[string]any{
				"@type": "BreadcrumbList",
				"itemListElement": []any{
					breadcrumbEntity(1, "Home", origin+"/"),
					breadcrumbEntity(2, "Resources", origin+resourcesIndexPath),
					breadcrumbEntity(3, providerName, canonicalURL),
				},
			},
			map[string]any{
				"@type":      "FAQPage",
				"mainEntity": faqEntities,
			},
		},
	}
	return marshalStructuredData(document, "resource "+resource.Slug)
}

func buildHubStructuredData(
	origin string,
	registry resourceRegistry,
) (template.JS, error) {
	hasPart := make([]map[string]any, 0, len(registry.Resources))
	for _, resource := range registry.Resources {
		hasPart = append(hasPart, map[string]any{
			"@type": "Article",
			"name":  resource.H1,
			"url":   origin + resourcePath(resource.Slug),
		})
	}
	document := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			map[string]any{
				"@type":        "CollectionPage",
				"name":         registry.Hub.H1,
				"description":  registry.Hub.MetaDescription,
				"url":          origin + resourcesIndexPath,
				"dateModified": registry.SignificantUpdate,
				"author": map[string]any{
					"@type": "Person",
					"name":  registry.Author.Name,
					"url":   registry.Author.URL,
				},
				"hasPart": hasPart,
			},
			map[string]any{
				"@type": "BreadcrumbList",
				"itemListElement": []any{
					breadcrumbEntity(1, "Home", origin+"/"),
					breadcrumbEntity(2, "Resources", origin+resourcesIndexPath),
				},
			},
		},
	}
	return marshalStructuredData(document, "resource hub")
}

func breadcrumbEntity(position int, name string, item string) map[string]any {
	return map[string]any{
		"@type":    "ListItem",
		"position": position,
		"name":     name,
		"item":     item,
	}
}

func marshalStructuredData(value any, label string) (template.JS, error) {
	encodedValue, marshalError := json.Marshal(value)
	if marshalError != nil {
		return "", fmt.Errorf("encode %s structured data: %w", label, marshalError)
	}
	if !json.Valid(encodedValue) {
		return "", fmt.Errorf("encode %s structured data: invalid JSON", label)
	}
	return template.JS(encodedValue), nil
}

func buildSitemap(origin string, registry resourceRegistry) ([]byte, error) {
	entries := make([]sitemapEntry, 0, len(registry.Resources)+2)
	entries = append(
		entries,
		sitemapEntry{
			Location:     origin + "/",
			LastModified: registry.SignificantUpdate,
		},
		sitemapEntry{
			Location:     origin + resourcesIndexPath,
			LastModified: registry.SignificantUpdate,
		},
	)
	for _, resource := range registry.Resources {
		entries = append(entries, sitemapEntry{
			Location:     origin + resourcePath(resource.Slug),
			LastModified: registry.SignificantUpdate,
		})
	}
	encodedSitemap, marshalError := xml.MarshalIndent(sitemapDocument{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  entries,
	}, "", "  ")
	if marshalError != nil {
		return nil, fmt.Errorf("encode public sitemap: %w", marshalError)
	}
	return append([]byte(xml.Header), append(encodedSitemap, '\n')...), nil
}

func displayResourceDate(value string) (string, error) {
	parsedDate, parseError := time.Parse(resourceDateLayout, value)
	if parseError != nil {
		return "", fmt.Errorf("format public resource date: %w", parseError)
	}
	return parsedDate.Format("January 2, 2006"), nil
}

func resourcePath(slug string) string {
	return resourcesIndexPath + slug + "/"
}
