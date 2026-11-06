package productionprofile

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeploymentManifestMatchesTheProductionProfile(testContext *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	profile, err := Load(filepath.Join(repositoryRoot, "configs", "production.yml"))
	if err != nil {
		testContext.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(repositoryRoot, ".mprlab", "deploy", "resources.yml"))
	if err != nil {
		testContext.Fatal(err)
	}
	manifest := currentManifestContract(testContext, content)
	requireCurrentContractYAML(testContext, "owner", manifest["owner"], `download-your-data`)
	requireCurrentContractYAML(testContext, "release", manifest["release"], `scheme: semver`)
	lifecycle := currentContractMap(testContext, manifest["lifecycle"])
	requireCurrentContractYAML(testContext, "lifecycle.defaults", lifecycle["defaults"], `timeouts: {completion: 30m, readiness: 2m, request: 30s, shutdown: 30s, startup: 2m}`)
	operations := currentContractIndex(testContext, lifecycle["operations"], "name")
	deploymentPlan := operations["app-plan-deploy"]
	if deploymentPlan == nil {
		testContext.Fatal("deployment planning operation is missing")
	}
	requireCurrentContractYAML(testContext, "deployment plan polling", deploymentPlan["polling"], `{initial: 1s, maximum: 10s, increment: 1s, multiplier: 2, attempt_limit: 120}`)
	environment := currentContractMap(testContext, manifest["environment"])
	inputs := currentContractIndex(testContext, environment["private_inputs"], "id")
	for id, variable := range map[string]string{"download-your-data-tmdb-read-token": "DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN", "download-your-data-google-client-id": "DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID", "download-your-data-tauth-jwt-signing-key": "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY"} {
		if inputs[id]["variable"] != variable {
			testContext.Fatalf("native private binding %s changed", id)
		}
	}
	targets := currentContractMap(testContext, manifest["targets"])
	projects := currentContractIndex(testContext, currentContractMap(testContext, targets["docker_compose"])["projects"], "id")
	if len(projects) != 1 || projects["runtime"] == nil {
		testContext.Fatal("runtime project missing")
	}
	project := projects["runtime"]
	artifacts := currentContractMap(testContext, manifest["artifacts"])
	publications := currentContractMap(testContext, manifest["publications"])
	requireCurrentContractYAML(testContext, "artifacts.container_images", artifacts["container_images"], `[{id: api-image, build: {context: ., dockerfile: Dockerfile, target: api, platforms: [linux/amd64]}}]`)
	requireCurrentContractYAML(testContext, "publications.ghcr", publications["ghcr"], `images: [{id: api-image, artifact: api-image, repository: ghcr.io/marcopoloresearchlab/download-your-data}]`)
	services := currentContractIndex(testContext, project["services"], "id")
	if len(services) != 1 || services["download-your-data-api"] == nil {
		testContext.Fatal("runtime service count or identity changed")
	}
	service := services["download-your-data-api"]
	requireCurrentContractYAML(testContext, "service.image", service["image"], `publication: api-image`)
	requireCurrentContractYAML(testContext, "service.placement", service["placement"], `{group: gateway, cardinality: one}`)
	env := currentContractIndex(testContext, service["environment"], "name")
	literalBindings := map[string]string{"DOWNLOAD_YOUR_DATA_ADDRESS": "0.0.0.0:8787", "DOWNLOAD_YOUR_DATA_DATA_DIR": profile.Runtime.DataMount, "DOWNLOAD_YOUR_DATA_API_ORIGIN": profile.Browser.APIOrigin, "DOWNLOAD_YOUR_DATA_TAUTH_URL": profile.Browser.APIOrigin, "DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID": profile.Browser.TenantID, "DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME": profile.Session.SessionCookieName, "DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME": profile.Session.RefreshCookieName}
	for name, value := range literalBindings {
		requireCurrentContractValue(testContext, name, env[name], map[string]any{"name": name, "value": value})
	}
	for name, input := range map[string]string{"DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID": "download-your-data-google-client-id", "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY": "download-your-data-tauth-jwt-signing-key", "DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN": "download-your-data-tmdb-read-token"} {
		requireCurrentContractValue(testContext, name, env[name], map[string]any{"name": name, "input": input})
	}
	requireCurrentContractYAML(testContext, "DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN", env["DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN"], `{name: DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN, target: website, output: origin}`)
	requireCurrentContractValue(testContext, "service.mounts", service["mounts"], []any{map[string]any{"target": profile.Runtime.DataMount, "volume": "data", "read_only": false}})
	requireCurrentContractValue(testContext, "service.ports", service["ports"], []any{map[string]any{"container_port": profile.Runtime.ContainerPort}})
	requireCurrentContractYAML(testContext, "project.volumes", project["volumes"], `[{id: data, name: mprlab-download-your-data-data, retention: retain}]`)
	endpoints := currentContractIndex(testContext, service["endpoints"], "id")
	if len(endpoints) != 2 || endpoints["http"] == nil || endpoints["readiness"] == nil {
		testContext.Fatal("unexpected service endpoints")
	}
	expectedEndpoint := map[string]any{"scope": "same_host", "scheme": "http", "alias": "download-your-data-api", "port": profile.Runtime.ContainerPort}
	expectedHealth := map[string]any{"protocol": "http", "path": profile.Runtime.HealthPath, "expected_status": 200}
	runtimeOperations := []any{map[string]any{"name": "app-deploy", "restart_delay": "5s"}}
	requireCurrentContractValue(testContext, "http", endpoints["http"], map[string]any{"id": "http", "name": "download-your-data.http", "version": 1, "endpoint": expectedEndpoint, "health": expectedHealth, "operations": runtimeOperations})
	readinessID := currentContractMap(testContext, service["readiness"])["endpoint"].(string)
	readiness := endpoints[readinessID]
	requireCurrentContractValue(testContext, "service.readiness.health", readiness["health"], expectedHealth)
	if currentContractMap(testContext, readiness["endpoint"])["port"] != profile.Runtime.ContainerPort {
		testContext.Fatal("service readiness port changed")
	}
	const apiRouteID = "download-your-data-public-api"
	routes := currentContractIndex(testContext, currentContractMap(testContext, currentContractMap(testContext, targets["ingress"])["caddy"])["routes"], "id")
	if len(routes) != 1 || routes[apiRouteID] == nil {
		testContext.Fatal("unexpected API routes")
	}
	route := currentContractDeclaration(testContext, currentContractMap(testContext, currentContractMap(testContext, targets["ingress"])["caddy"])["routes"], apiRouteID)
	requireCurrentContractYAML(testContext, "route.hostname", route["hostname"], `dyd-api.mprlab.com`)
	requireCurrentContractYAML(testContext, "route.listener", route["listener"], `https`)
	requireCurrentContractYAML(testContext, "route.tls", route["tls"], `mode: automatic`)
	requireCurrentContractYAML(testContext, "route.operations", route["operations"], `[{name: app-deploy, timeouts: {startup: 1m, readiness: 5m}, polling: {initial: 1s, maximum: 10s, increment: 1s, multiplier: 2, attempt_limit: 200}}]`)
	requireCurrentContractYAML(testContext, "route.rate_limit", currentContractMap(testContext, route["access"])["rate_limit"], `{events: 300, window_seconds: 10}`)
	handlers := currentContractIndex(testContext, route["handlers"], "id")
	if len(handlers) != 3 {
		testContext.Fatal("public API handler count changed")
	}
	for id, values := range map[string][2]string{"shared-auth": {"/auth", "tauth.http"}, "shared-profile": {"/me", "tauth.http"}, "default": {"/", "download-your-data.http"}} {
		requireCurrentContractValue(testContext, "handler "+id, handlers[id], map[string]any{"id": id, "path_prefix": values[0], "upstream": values[1], "transport": map[string]any{"protocol": "http", "dial_timeout_seconds": 10, "response_header_timeout_seconds": 30, "read_timeout_seconds": 80}})
	}
	observations := currentContractIndex(testContext, currentContractMap(testContext, currentContractMap(testContext, manifest["lifecycle"])["observations"])["http"], "id")
	if len(observations) != 1 || observations["public-health"] == nil {
		testContext.Fatal("unexpected public observations")
	}
	health := currentContractDeclaration(testContext, currentContractMap(testContext, currentContractMap(testContext, manifest["lifecycle"])["observations"])["http"], "public-health")
	requireCurrentContractValue(testContext, "public health", health, map[string]any{"id": "public-health", "protocol": "http", "url": profile.Browser.APIOrigin + profile.Runtime.HealthPath, "expected_status": 200, "operations": runtimeOperations})
	requireCurrentContractYAML(testContext, "website publication", publications["github_pages"], `sites: [{id: website, artifact: website, repository: marcopoloresearchlab/download_your_data, branch: gh-pages, domain: dyd.mprlab.com, marker: {path: /.mprlab-release.json}}]`)
	requireCurrentContractYAML(testContext, "website source", artifacts["web_bundles"], `[{id: website, format: directory, source: {kind: container, context: ., dockerfile: Dockerfile, target: pages}}]`)
	requireCurrentContractValue(testContext, "website target", targets["github_pages"], map[string]any{"sites": []any{map[string]any{"id": "website", "publication": "website", "url": profile.Browser.PublicOrigin + "/", "operations": runtimeOperations}}})
	if profile.Session.CookieDomain != ".mprlab.com" {
		testContext.Fatal("production browser cookie domain changed")
	}
}

func currentContractMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected native declaration mapping, got %#v", value)
	}
	return result
}

func currentManifestContract(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := yaml.Unmarshal(content, &envelope); err != nil {
		t.Fatalf("parse selected manifest: %v", err)
	}
	if len(envelope) != 1 {
		t.Fatalf("unexpected manifest envelope: %#v", envelope)
	}
	actual := currentContractMap(t, envelope["mprlab_resources"])
	keys := make([]string, 0, len(actual))
	for key := range actual {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	expected := []string{"artifacts", "environment", "lifecycle", "owner", "publications", "release", "targets"}
	if !reflect.DeepEqual(keys, expected) {
		t.Fatalf("selected manifest root keys=%v want=%v", keys, expected)
	}
	return actual
}

func currentContractDeclaration(t *testing.T, value any, identity string) map[string]any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected explicit declaration list, got %#v", value)
	}
	var found map[string]any
	for _, item := range items {
		declaration := currentContractMap(t, item)
		if declaration["id"] == identity {
			if found != nil {
				t.Fatalf("duplicate declaration %s", identity)
			}
			found = declaration
		}
	}
	if found == nil {
		t.Fatalf("missing native declaration %s", identity)
	}
	return found
}

func currentContractIndex(t *testing.T, value any, identityKey string) map[string]map[string]any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected native declaration list, got %#v", value)
	}
	result := make(map[string]map[string]any, len(items))
	for _, item := range items {
		declaration := currentContractMap(t, item)
		identity, ok := declaration[identityKey].(string)
		if !ok || identity == "" || result[identity] != nil {
			t.Fatalf("invalid or duplicate %s in %#v", identityKey, value)
		}
		result[identity] = declaration
	}
	return result
}

func requireCurrentContractYAML(t *testing.T, path string, actual any, expectedText string) {
	t.Helper()
	var expected any
	if err := yaml.Unmarshal([]byte(expectedText), &expected); err != nil {
		t.Fatalf("parse expected %s: %v", path, err)
	}
	requireCurrentContractValue(t, path, actual, expected)
}

func requireCurrentContractValue(t *testing.T, path string, actual, expected any) {
	t.Helper()
	switch wanted := expected.(type) {
	case map[string]any:
		mapping, ok := actual.(map[string]any)
		if !ok {
			t.Fatalf("%s requires a mapping, got %#v", path, actual)
		}
		if !strings.HasSuffix(path, ".assets[]") && len(mapping) != len(wanted) {
			t.Fatalf("%s has unexpected fields: %#v", path, actual)
		}
		for key, value := range wanted {
			observed, exists := mapping[key]
			if !exists {
				t.Fatalf("%s lacks %s", path, key)
			}
			requireCurrentContractValue(t, path+"."+key, observed, value)
		}
	case []any:
		items, ok := actual.([]any)
		if !ok || len(items) != len(wanted) {
			t.Fatalf("%s list=%#v want=%#v", path, actual, wanted)
		}
		for index, value := range wanted {
			observed := items[index]
			if declaration, isMapping := value.(map[string]any); isMapping {
				identityKey := "id"
				if _, exists := declaration[identityKey]; !exists {
					identityKey = "name"
				}
				if identity, exists := declaration[identityKey]; exists {
					matches := 0
					for _, candidate := range items {
						if entry, valid := candidate.(map[string]any); valid && entry[identityKey] == identity {
							observed = candidate
							matches++
						}
					}
					if matches != 1 {
						t.Fatalf("%s requires one %s=%v, got %d", path, identityKey, identity, matches)
					}
				}
			}
			requireCurrentContractValue(t, path+"[]", observed, value)
		}
	default:
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s=%#v want=%#v", path, actual, expected)
		}
	}
}
