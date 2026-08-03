// Package frontend owns the static browser application source.
package frontend

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html"
	"io/fs"
)

//go:embed index.html application content images styles
var embeddedAssets embed.FS

// APIOriginMarker is replaced exactly once when the local application index is served.
const APIOriginMarker = "__DOWNLOAD_YOUR_DATA_API_ORIGIN__"

// PublicOriginMarker is replaced in public indexing metadata at the serving
// or static-site build boundary.
const PublicOriginMarker = "__DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN__"

// ContentSecurityPolicyMarker binds GitHub Pages to the same browser policy
// used by the embedded development surface.
const ContentSecurityPolicyMarker = "__DOWNLOAD_YOUR_DATA_CONTENT_SECURITY_POLICY__"

// Assets returns the complete browser application filesystem.
func Assets() fs.FS {
	return embeddedAssets
}

// RenderApplicationIndex binds the static application document to one exact
// public frontend and protected API origin.
func RenderApplicationIndex(
	publicOrigin string,
	apiOrigin string,
	tAuthOrigin string,
) ([]byte, error) {
	indexSource, readError := fs.ReadFile(Assets(), "index.html")
	if readError != nil {
		return nil, readError
	}
	if bytes.Count(indexSource, []byte(APIOriginMarker)) != 1 {
		return nil, errors.New("render application index: API origin marker must appear exactly once")
	}
	if bytes.Count(indexSource, []byte(PublicOriginMarker)) != 2 {
		return nil, errors.New("render application index: public origin marker must appear exactly twice")
	}
	if bytes.Count(indexSource, []byte(ContentSecurityPolicyMarker)) != 1 {
		return nil, errors.New("render application index: content security policy marker must appear exactly once")
	}
	renderedIndex := bytes.Replace(
		indexSource,
		[]byte(APIOriginMarker),
		[]byte(html.EscapeString(apiOrigin)),
		1,
	)
	renderedIndex = bytes.ReplaceAll(
		renderedIndex,
		[]byte(PublicOriginMarker),
		[]byte(html.EscapeString(publicOrigin)),
	)
	return bytes.Replace(
		renderedIndex,
		[]byte(ContentSecurityPolicyMarker),
		[]byte(html.EscapeString(MetaContentSecurityPolicy(apiOrigin, tAuthOrigin))),
		1,
	), nil
}

// ContentSecurityPolicy returns the exact HTTP response-header policy.
func ContentSecurityPolicy(apiOrigin string, tAuthOrigin string) string {
	return fmt.Sprintf(
		"default-src 'self'; base-uri 'self'; connect-src 'self' %s %s https://accounts.google.com; font-src 'self'; form-action 'self' %s; frame-ancestors 'none'; frame-src https://accounts.google.com; img-src 'self' data: https://lh3.googleusercontent.com; object-src 'none'; script-src 'self' https://cdn.jsdelivr.net https://accounts.google.com; style-src 'self' https://cdn.jsdelivr.net https://accounts.google.com 'unsafe-inline'",
		apiOrigin,
		tAuthOrigin,
		tAuthOrigin,
	)
}

// MetaContentSecurityPolicy returns the GitHub Pages policy. The meta delivery
// mechanism does not support frame-ancestors; the API response header retains
// that directive.
func MetaContentSecurityPolicy(apiOrigin string, tAuthOrigin string) string {
	return fmt.Sprintf(
		"default-src 'self'; base-uri 'self'; connect-src 'self' %s %s https://accounts.google.com; font-src 'self'; form-action 'self' %s; frame-src https://accounts.google.com; img-src 'self' data: https://lh3.googleusercontent.com; object-src 'none'; script-src 'self' https://cdn.jsdelivr.net https://accounts.google.com; style-src 'self' https://cdn.jsdelivr.net https://accounts.google.com 'unsafe-inline'",
		apiOrigin,
		tAuthOrigin,
		tAuthOrigin,
	)
}
