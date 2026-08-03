package productionprofile

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type deploymentManifest struct {
	Resources deploymentResources `yaml:"mprlab_resources"`
}

type deploymentResources struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Owner         string               `yaml:"owner"`
	Resources     []deploymentResource `yaml:"resources"`
}

type deploymentResource struct {
	Kind       string                   `yaml:"kind"`
	ID         string                   `yaml:"id"`
	Bindings   map[string]string        `yaml:"bindings"`
	Images     []deploymentImage        `yaml:"images"`
	Services   []deploymentService      `yaml:"services"`
	Volumes    []deploymentVolume       `yaml:"volumes"`
	Name       string                   `yaml:"name"`
	Version    int                      `yaml:"version"`
	Project    string                   `yaml:"project"`
	Service    string                   `yaml:"service"`
	Endpoint   deploymentEndpoint       `yaml:"endpoint"`
	Health     deploymentHealth         `yaml:"health"`
	Hostname   string                   `yaml:"hostname"`
	Listener   string                   `yaml:"listener"`
	Handlers   []deploymentHandler      `yaml:"handlers"`
	Protocol   string                   `yaml:"protocol"`
	URL        string                   `yaml:"url"`
	Repository string                   `yaml:"repository"`
	Branch     string                   `yaml:"branch"`
	Domain     string                   `yaml:"domain"`
	Source     deploymentSource         `yaml:"source"`
	Capability string                   `yaml:"capability"`
	Tenant     deploymentAuthentication `yaml:"tenant"`
	Access     deploymentAccess         `yaml:"access"`
	TLS        deploymentTLS            `yaml:"tls"`
	Verify     deploymentVerification   `yaml:"verification"`
	Expected   int                      `yaml:"expected_status"`
}

type deploymentImage struct {
	ID         string          `yaml:"id"`
	Repository string          `yaml:"repository"`
	Visibility string          `yaml:"visibility"`
	Build      deploymentBuild `yaml:"build"`
}

type deploymentBuild struct {
	Context    string   `yaml:"context"`
	Dockerfile string   `yaml:"dockerfile"`
	Target     string   `yaml:"target"`
	Platforms  []string `yaml:"platforms"`
}

type deploymentService struct {
	ID          string                     `yaml:"id"`
	Image       string                     `yaml:"image"`
	Placement   map[string]string          `yaml:"placement"`
	Environment map[string]deploymentValue `yaml:"environment"`
	Mounts      []deploymentMount          `yaml:"mounts"`
	Ports       []map[string]int           `yaml:"ports"`
	Readiness   deploymentHealth           `yaml:"readiness"`
}

type deploymentValue struct {
	Value    string `yaml:"value"`
	Resource string `yaml:"resource"`
	Output   string `yaml:"output"`
}

type deploymentMount struct {
	Volume   string `yaml:"volume"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
}

type deploymentVolume struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	Retention string `yaml:"retention"`
}

type deploymentEndpoint struct {
	Scope  string `yaml:"scope"`
	Scheme string `yaml:"scheme"`
	Alias  string `yaml:"alias"`
	Port   int    `yaml:"port"`
}

type deploymentHealth struct {
	Protocol       string `yaml:"protocol"`
	Port           int    `yaml:"port"`
	Path           string `yaml:"path"`
	ExpectedStatus int    `yaml:"expected_status"`
}

type deploymentHandler struct {
	ID         string              `yaml:"id"`
	PathPrefix string              `yaml:"path_prefix"`
	Upstream   string              `yaml:"upstream"`
	Transport  deploymentTransport `yaml:"transport"`
}

type deploymentTransport struct {
	Protocol                     string `yaml:"protocol"`
	DialTimeoutSeconds           int    `yaml:"dial_timeout_seconds"`
	ResponseHeaderTimeoutSeconds int    `yaml:"response_header_timeout_seconds"`
	ReadTimeoutSeconds           int    `yaml:"read_timeout_seconds"`
}

type deploymentAccess struct {
	RateLimit deploymentRateLimit `yaml:"rate_limit"`
}

type deploymentRateLimit struct {
	Events        int `yaml:"events"`
	WindowSeconds int `yaml:"window_seconds"`
}

type deploymentTLS struct {
	Mode string `yaml:"mode"`
}

type deploymentVerification struct {
	Path string `yaml:"path"`
}

type deploymentSource struct {
	Kind       string `yaml:"kind"`
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
	Target     string `yaml:"target"`
}

type deploymentAuthentication struct {
	ID                string              `yaml:"id"`
	DisplayName       string              `yaml:"display_name"`
	Origins           []string            `yaml:"origins"`
	GoogleWebClientID deploymentReference `yaml:"google_web_client_id"`
	JWTSigningKey     deploymentReference `yaml:"jwt_signing_key"`
	Cookie            deploymentCookie    `yaml:"cookie"`
}

type deploymentReference struct {
	Resource string `yaml:"resource"`
	Output   string `yaml:"output"`
}

type deploymentCookie struct {
	Domain      string `yaml:"domain"`
	SessionName string `yaml:"session_name"`
	RefreshName string `yaml:"refresh_name"`
}

func TestDeploymentManifestMatchesTheProductionProfile(testContext *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	profile, profileError := Load(filepath.Join(repositoryRoot, "configs", "production.yml"))
	if profileError != nil {
		testContext.Fatalf("load production profile: %v", profileError)
	}
	encodedManifest, readError := os.ReadFile(
		filepath.Join(repositoryRoot, ".mprlab", "deploy", "resources.yml"),
	)
	if readError != nil {
		testContext.Fatalf("read deployment manifest: %v", readError)
	}
	var manifest deploymentManifest
	if decodeError := yaml.Unmarshal(encodedManifest, &manifest); decodeError != nil {
		testContext.Fatalf("decode deployment manifest: %v", decodeError)
	}
	if manifest.Resources.SchemaVersion != 3 || manifest.Resources.Owner != "download-your-data" {
		testContext.Fatalf("deployment manifest envelope drifted: %+v", manifest.Resources)
	}
	if len(manifest.Resources.Resources) != 7 {
		testContext.Fatalf("deployment resource count = %d; want 7", len(manifest.Resources.Resources))
	}

	private := requireDeploymentResource(testContext, manifest, "private_values", "private")
	if len(private.Bindings) != 2 ||
		private.Bindings["google-web-client-id"] != "DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID" ||
		private.Bindings["tauth-jwt-signing-key"] != "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY" {
		testContext.Fatalf("private-value bindings drifted: %+v", private.Bindings)
	}

	runtime := requireDeploymentResource(testContext, manifest, "compose_project", "runtime")
	if len(runtime.Images) != 1 || len(runtime.Services) != 1 || len(runtime.Volumes) != 1 {
		testContext.Fatalf("runtime topology drifted: %+v", runtime)
	}
	image := runtime.Images[0]
	if image.Repository != "ghcr.io/marcopoloresearchlab/download-your-data" ||
		image.Visibility != "public" ||
		image.Build.Context != "." ||
		image.Build.Dockerfile != "Dockerfile" ||
		image.Build.Target != "api" ||
		len(image.Build.Platforms) != 1 ||
		image.Build.Platforms[0] != "linux/amd64" {
		testContext.Fatalf("production API image contract drifted: %+v", image)
	}
	service := runtime.Services[0]
	if service.ID != "download-your-data-api" || service.Image != "api-image" ||
		service.Placement["group"] != "gateway" ||
		service.Placement["cardinality"] != "one" ||
		service.Environment["DOWNLOAD_YOUR_DATA_ADDRESS"].Value != "0.0.0.0:8787" ||
		service.Environment["DOWNLOAD_YOUR_DATA_DATA_DIR"].Value != profile.Runtime.DataMount ||
		service.Readiness.Protocol != "http" ||
		service.Readiness.Port != profile.Runtime.ContainerPort ||
		service.Readiness.Path != profile.Runtime.HealthPath ||
		service.Readiness.ExpectedStatus != 200 ||
		len(service.Mounts) != 1 ||
		service.Mounts[0] != (deploymentMount{Volume: "data", Target: profile.Runtime.DataMount}) ||
		len(service.Ports) != 1 || service.Ports[0]["container_port"] != profile.Runtime.ContainerPort {
		testContext.Fatalf("production API service contract drifted: %+v", service)
	}
	if runtime.Volumes[0] != (deploymentVolume{ID: "data", Name: "mprlab-download-your-data-data", Retention: "retain"}) {
		testContext.Fatalf("retained data volume drifted: %+v", runtime.Volumes[0])
	}
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN", "website", "origin")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_API_ORIGIN", "public-api", "origin")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_TAUTH_URL", "public-api", "origin")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID", "authentication", "tenant-id")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID", "authentication", "google-web-client-id")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY", "authentication", "jwt-signing-key")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME", "authentication", "session-cookie-name")
	assertEnvironmentReference(testContext, service, "DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME", "authentication", "refresh-cookie-name")

	capability := requireDeploymentResource(testContext, manifest, "runtime_capability", "http")
	if capability.Name != "download-your-data.http" || capability.Version != 1 ||
		capability.Project != "runtime" || capability.Service != service.ID ||
		capability.Endpoint.Scope != "same_host" ||
		capability.Endpoint.Scheme != "http" ||
		capability.Endpoint.Alias != service.ID ||
		capability.Endpoint.Port != profile.Runtime.ContainerPort ||
		capability.Health.Protocol != "http" ||
		capability.Health.Path != profile.Runtime.HealthPath ||
		capability.Health.ExpectedStatus != 200 {
		testContext.Fatalf("runtime capability drifted: %+v", capability)
	}

	publicAPI := requireDeploymentResource(testContext, manifest, "caddy_route", "public-api")
	if publicAPI.Hostname != "dyd-api.mprlab.com" ||
		publicAPI.Listener != "https" || publicAPI.TLS.Mode != "automatic" ||
		publicAPI.Access.RateLimit != (deploymentRateLimit{Events: 300, WindowSeconds: 10}) ||
		len(publicAPI.Handlers) != 3 ||
		publicAPI.Handlers[0] != expectedDeploymentHandler("shared-auth", "/auth", "tauth.http") ||
		publicAPI.Handlers[1] != expectedDeploymentHandler("shared-profile", "/me", "tauth.http") ||
		publicAPI.Handlers[2] != expectedDeploymentHandler("default", "/", "download-your-data.http") {
		testContext.Fatalf("public API route drifted: %+v", publicAPI)
	}

	health := requireDeploymentResource(testContext, manifest, "health_check", "public-health")
	if health.Protocol != "http" ||
		health.URL != profile.Browser.APIOrigin+profile.Runtime.HealthPath ||
		health.Expected != 200 {
		testContext.Fatalf("public health contract drifted: %+v", health)
	}

	website := requireDeploymentResource(testContext, manifest, "github_pages", "website")
	if website.Repository != "marcopoloresearchlab/download_your_data" ||
		website.Branch != "gh-pages" ||
		website.Domain != "dyd.mprlab.com" ||
		website.URL != profile.Browser.PublicOrigin+"/" ||
		website.Source != (deploymentSource{Kind: "container", Context: ".", Dockerfile: "Dockerfile", Target: "pages"}) ||
		website.Verify.Path != "/.mprlab-release.json" {
		testContext.Fatalf("GitHub Pages contract drifted: %+v", website)
	}

	authentication := requireDeploymentResource(testContext, manifest, "tauth_tenant", "authentication")
	if authentication.Capability != "tauth.tenants" || authentication.Version != 1 ||
		authentication.Tenant.ID != profile.Browser.TenantID ||
		len(authentication.Tenant.Origins) != 1 ||
		authentication.Tenant.Origins[0] != profile.Browser.PublicOrigin ||
		authentication.Tenant.Cookie.Domain != profile.Session.CookieDomain ||
		authentication.Tenant.Cookie.SessionName != profile.Session.SessionCookieName ||
		authentication.Tenant.Cookie.RefreshName != profile.Session.RefreshCookieName ||
		authentication.Tenant.GoogleWebClientID != (deploymentReference{Resource: "private", Output: "google-web-client-id"}) ||
		authentication.Tenant.JWTSigningKey != (deploymentReference{Resource: "private", Output: "tauth-jwt-signing-key"}) {
		testContext.Fatalf("TAuth tenant contract drifted: %+v", authentication)
	}
}

func expectedDeploymentHandler(id string, pathPrefix string, upstream string) deploymentHandler {
	return deploymentHandler{
		ID:         id,
		PathPrefix: pathPrefix,
		Upstream:   upstream,
		Transport: deploymentTransport{
			Protocol:                     "http",
			DialTimeoutSeconds:           10,
			ResponseHeaderTimeoutSeconds: 30,
			ReadTimeoutSeconds:           80,
		},
	}
}

func requireDeploymentResource(
	testContext *testing.T,
	manifest deploymentManifest,
	kind string,
	id string,
) deploymentResource {
	testContext.Helper()
	for _, resource := range manifest.Resources.Resources {
		if resource.Kind == kind && resource.ID == id {
			return resource
		}
	}
	testContext.Fatalf("deployment resource %s/%s is missing", kind, id)
	return deploymentResource{}
}

func assertEnvironmentReference(
	testContext *testing.T,
	service deploymentService,
	name string,
	resource string,
	output string,
) {
	testContext.Helper()
	value, exists := service.Environment[name]
	if !exists || value != (deploymentValue{Resource: resource, Output: output}) {
		testContext.Fatalf("environment %s = %+v; want %s/%s", name, value, resource, output)
	}
}
