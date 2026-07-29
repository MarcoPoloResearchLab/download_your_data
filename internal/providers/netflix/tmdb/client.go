package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

const (
	userAgent             = "download-your-data/1"
	maxRemoteTextBytes    = 16 * 1024
	maxRemoteDescription  = 128 * 1024
	maxGenres             = 32
	maxOriginCountries    = 32
	defaultRequestTimeout = 20 * time.Second
)

// ErrorCode is the closed machine-readable TMDB client failure.
type ErrorCode string

const (
	ErrorCanceled        ErrorCode = "canceled"
	ErrorInvalidRequest  ErrorCode = "invalid_request"
	ErrorUnauthorized    ErrorCode = "unauthorized"
	ErrorRateLimited     ErrorCode = "rate_limited"
	ErrorUnavailable     ErrorCode = "unavailable"
	ErrorInvalidResponse ErrorCode = "invalid_response"
)

// Error describes a TMDB failure without including a token or private title query.
type Error struct {
	code      ErrorCode
	operation string
	status    int
	cause     error
}

func (clientError *Error) Error() string {
	if clientError.status > 0 {
		return fmt.Sprintf(
			"TMDB %s failed with %s (HTTP %d)",
			clientError.operation,
			clientError.code,
			clientError.status,
		)
	}
	return fmt.Sprintf("TMDB %s failed with %s", clientError.operation, clientError.code)
}

func (clientError *Error) Unwrap() error {
	return clientError.cause
}

// Code returns the typed failure identity.
func (clientError *Error) Code() ErrorCode {
	return clientError.code
}

// HTTPStatus returns the remote status, or zero when no response was received.
func (clientError *Error) HTTPStatus() int {
	return clientError.status
}

// Candidate is one validated movie or series search result.
type Candidate struct {
	TMDBID        int64
	MediaType     netflix.MediaType
	Title         string
	OriginalTitle string
	Popularity    float64
}

// Details is one validated TMDB metadata response before Netflix domain construction.
type Details struct {
	TMDBID           int64
	MediaType        netflix.MediaType
	Genres           []string
	ReleaseDate      string
	RuntimeMinutes   *int
	OriginalLanguage string
	VoteAverage      *float64
	VoteCount        *int
	OriginCountries  []string
	Seasons          *int
	Episodes         *int
	MatchedTitle     string
	Description      string
}

// Client is the bounded Bearer-authenticated TMDB boundary.
type Client struct {
	httpClient  *http.Client
	token       ReadToken
	baseURL     string
	pacer       *requestPacer
	sleep       sleepFunction
	now         func() time.Time
	maxAttempts int
}

type clientOptions struct {
	httpClient      *http.Client
	baseURL         string
	requestInterval time.Duration
	sleep           sleepFunction
	now             func() time.Time
	maxAttempts     int
}

type sleepFunction func(context.Context, time.Duration) error

// NewClient creates the sole production client against TMDB's official HTTPS origin.
func NewClient(token ReadToken) (*Client, error) {
	return newClient(token, clientOptions{
		httpClient:      &http.Client{Timeout: defaultRequestTimeout},
		baseURL:         OfficialBaseURL,
		requestInterval: time.Second / product.TMDBRequestsPerSecond,
		sleep:           sleepWithContext,
		now:             time.Now,
		maxAttempts:     product.MaxTMDBAttempts,
	})
}

func newClient(token ReadToken, options clientOptions) (*Client, error) {
	if !token.valid() {
		return nil, ErrNotConfigured
	}
	if options.httpClient == nil {
		return nil, errors.New("create TMDB client: HTTP client is required")
	}
	baseURL, baseURLError := validateClientBaseURL(options.baseURL)
	if baseURLError != nil {
		return nil, baseURLError
	}
	if options.requestInterval < 0 {
		return nil, errors.New("create TMDB client: request interval must not be negative")
	}
	if options.sleep == nil {
		return nil, errors.New("create TMDB client: sleep function is required")
	}
	if options.now == nil {
		return nil, errors.New("create TMDB client: clock is required")
	}
	if options.maxAttempts <= 0 || options.maxAttempts > product.MaxTMDBAttempts {
		return nil, fmt.Errorf(
			"create TMDB client: attempts must be between 1 and %d",
			product.MaxTMDBAttempts,
		)
	}

	httpClient := *options.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		httpClient:  &httpClient,
		token:       token,
		baseURL:     baseURL,
		pacer:       newRequestPacer(options.requestInterval, options.now, options.sleep),
		sleep:       options.sleep,
		now:         options.now,
		maxAttempts: options.maxAttempts,
	}, nil
}

// Identity returns the exact request and response contract used in cache keys.
func (client *Client) Identity() string {
	return ClientIdentity
}

// Search sends one derived title query and returns only movie and series candidates.
func (client *Client) Search(
	ctx context.Context,
	query string,
	locale Locale,
) ([]Candidate, error) {
	if contextError := validateRequestContext(ctx, "search"); contextError != nil {
		return nil, contextError
	}
	if queryError := validateQuery(query); queryError != nil {
		return nil, queryError
	}
	if !locale.valid() {
		return nil, newClientError(
			ErrorInvalidRequest,
			"search",
			0,
			ErrInvalidLocale,
		)
	}

	queryValues := url.Values{}
	queryValues.Set("include_adult", "false")
	queryValues.Set("language", locale.String())
	queryValues.Set("page", "1")
	queryValues.Set("query", query)
	endpoint := client.baseURL + "/search/multi?" + queryValues.Encode()

	var decoded searchResponse
	if requestError := client.getJSON(ctx, "search", endpoint, &decoded); requestError != nil {
		return nil, requestError
	}
	if len(decoded.Results) > product.MaxTMDBSearchCandidates {
		return nil, newClientError(
			ErrorInvalidResponse,
			"search",
			http.StatusOK,
			errors.New("candidate limit exceeded"),
		)
	}

	candidates := make([]Candidate, 0, len(decoded.Results))
	for _, entity := range decoded.Results {
		candidate, included, candidateError := decodeSearchCandidate(entity)
		if candidateError != nil {
			return nil, newClientError(
				ErrorInvalidResponse,
				"search",
				http.StatusOK,
				candidateError,
			)
		}
		if included {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}

// Details retrieves the complete accepted-candidate metadata snapshot.
func (client *Client) Details(
	ctx context.Context,
	candidate Candidate,
	locale Locale,
) (Details, error) {
	if contextError := validateRequestContext(ctx, "details"); contextError != nil {
		return Details{}, contextError
	}
	if candidateError := validateCandidate(candidate); candidateError != nil {
		return Details{}, newClientError(
			ErrorInvalidRequest,
			"details",
			0,
			candidateError,
		)
	}
	if !locale.valid() {
		return Details{}, newClientError(
			ErrorInvalidRequest,
			"details",
			0,
			ErrInvalidLocale,
		)
	}

	queryValues := url.Values{}
	queryValues.Set("language", locale.String())
	mediaPath := "movie"
	if candidate.MediaType == netflix.MediaTypeSeries {
		mediaPath = "tv"
	}
	endpoint := client.baseURL + "/" + mediaPath + "/" +
		strconv.FormatInt(candidate.TMDBID, 10) + "?" + queryValues.Encode()

	var decoded detailsResponse
	if requestError := client.getJSON(ctx, "details", endpoint, &decoded); requestError != nil {
		return Details{}, requestError
	}
	details, detailsError := decodeDetails(candidate, decoded)
	if detailsError != nil {
		return Details{}, newClientError(
			ErrorInvalidResponse,
			"details",
			http.StatusOK,
			detailsError,
		)
	}
	return details, nil
}

func (client *Client) getJSON(
	ctx context.Context,
	operation string,
	endpoint string,
	destination any,
) error {
	var lastError error
	for attempt := 0; attempt < client.maxAttempts; attempt++ {
		if paceError := client.pacer.Wait(ctx); paceError != nil {
			return newClientError(ErrorCanceled, operation, 0, paceError)
		}
		request, requestError := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if requestError != nil {
			return newClientError(ErrorInvalidRequest, operation, 0, requestError)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+client.token.value)
		request.Header.Set("User-Agent", userAgent)

		response, responseError := client.httpClient.Do(request)
		if responseError != nil {
			if ctx.Err() != nil {
				return newClientError(ErrorCanceled, operation, 0, ctx.Err())
			}
			lastError = newClientError(ErrorUnavailable, operation, 0, responseError)
			if attempt+1 < client.maxAttempts {
				if sleepError := client.sleep(ctx, retryBackoff(attempt)); sleepError != nil {
					return newClientError(ErrorCanceled, operation, 0, sleepError)
				}
				continue
			}
			return lastError
		}

		responseBody, readError := readBoundedResponse(response.Body)
		closeError := response.Body.Close()
		if readError != nil {
			return newClientError(ErrorInvalidResponse, operation, response.StatusCode, readError)
		}
		if closeError != nil {
			return newClientError(ErrorInvalidResponse, operation, response.StatusCode, closeError)
		}
		if response.StatusCode == http.StatusOK {
			if decodeError := json.Unmarshal(responseBody, destination); decodeError != nil {
				return newClientError(
					ErrorInvalidResponse,
					operation,
					response.StatusCode,
					decodeError,
				)
			}
			return nil
		}

		statusError := statusClientError(operation, response.StatusCode)
		lastError = statusError
		if !retryableStatus(response.StatusCode) || attempt+1 == client.maxAttempts {
			return statusError
		}
		retryDelay := retryBackoff(attempt)
		if response.StatusCode == http.StatusTooManyRequests {
			retryDelay = parseRetryAfter(response.Header.Get("Retry-After"), client.now())
		}
		if sleepError := client.sleep(ctx, retryDelay); sleepError != nil {
			return newClientError(ErrorCanceled, operation, response.StatusCode, sleepError)
		}
	}
	return lastError
}

type searchResponse struct {
	Results []searchEntity `json:"results"`
}

type searchEntity struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	Popularity    float64 `json:"popularity"`
}

type detailsResponse struct {
	ID                  int64             `json:"id"`
	Title               string            `json:"title"`
	Name                string            `json:"name"`
	Overview            string            `json:"overview"`
	Genres              []genreResponse   `json:"genres"`
	ReleaseDate         string            `json:"release_date"`
	FirstAirDate        string            `json:"first_air_date"`
	Runtime             *int              `json:"runtime"`
	EpisodeRunTime      []int             `json:"episode_run_time"`
	OriginalLanguage    string            `json:"original_language"`
	VoteAverage         *float64          `json:"vote_average"`
	VoteCount           *int              `json:"vote_count"`
	OriginCountry       []string          `json:"origin_country"`
	ProductionCountries []countryResponse `json:"production_countries"`
	NumberOfSeasons     *int              `json:"number_of_seasons"`
	NumberOfEpisodes    *int              `json:"number_of_episodes"`
}

type genreResponse struct {
	Name string `json:"name"`
}

type countryResponse struct {
	Code string `json:"iso_3166_1"`
}

func decodeSearchCandidate(entity searchEntity) (Candidate, bool, error) {
	candidate := Candidate{
		TMDBID:     entity.ID,
		Popularity: entity.Popularity,
	}
	switch entity.MediaType {
	case "movie":
		candidate.MediaType = netflix.MediaTypeMovie
		candidate.Title = entity.Title
		candidate.OriginalTitle = entity.OriginalTitle
	case "tv":
		candidate.MediaType = netflix.MediaTypeSeries
		candidate.Title = entity.Name
		candidate.OriginalTitle = entity.OriginalName
	default:
		return Candidate{}, false, nil
	}
	if candidateError := validateCandidate(candidate); candidateError != nil {
		return Candidate{}, false, candidateError
	}
	return candidate, true, nil
}

func validateCandidate(candidate Candidate) error {
	if candidate.TMDBID <= 0 {
		return errors.New("candidate TMDB ID must be positive")
	}
	if candidate.MediaType != netflix.MediaTypeMovie &&
		candidate.MediaType != netflix.MediaTypeSeries {
		return errors.New("candidate media type must be movie or series")
	}
	if textError := validateRemoteText(candidate.Title, true, false, maxRemoteTextBytes); textError != nil {
		return fmt.Errorf("candidate title: %w", textError)
	}
	if candidate.OriginalTitle != "" {
		if textError := validateRemoteText(
			candidate.OriginalTitle,
			false,
			false,
			maxRemoteTextBytes,
		); textError != nil {
			return fmt.Errorf("candidate original title: %w", textError)
		}
	}
	if math.IsNaN(candidate.Popularity) ||
		math.IsInf(candidate.Popularity, 0) ||
		candidate.Popularity < 0 {
		return errors.New("candidate popularity must be finite and nonnegative")
	}
	return nil
}

func decodeDetails(candidate Candidate, decoded detailsResponse) (Details, error) {
	if decoded.ID != candidate.TMDBID {
		return Details{}, errors.New("details TMDB ID does not match the requested candidate")
	}
	if len(decoded.Genres) > maxGenres {
		return Details{}, errors.New("genre limit exceeded")
	}
	genres := make([]string, len(decoded.Genres))
	for genreIndex, genre := range decoded.Genres {
		if textError := validateRemoteText(genre.Name, true, false, maxRemoteTextBytes); textError != nil {
			return Details{}, fmt.Errorf("genre %d: %w", genreIndex+1, textError)
		}
		genres[genreIndex] = genre.Name
	}

	details := Details{
		TMDBID:           candidate.TMDBID,
		MediaType:        candidate.MediaType,
		Genres:           genres,
		OriginalLanguage: decoded.OriginalLanguage,
		VoteAverage:      decoded.VoteAverage,
		VoteCount:        decoded.VoteCount,
		Description:      decoded.Overview,
	}
	if candidate.MediaType == netflix.MediaTypeMovie {
		details.MatchedTitle = decoded.Title
		details.ReleaseDate = decoded.ReleaseDate
		if decoded.Runtime != nil {
			if *decoded.Runtime <= 0 {
				return Details{}, errors.New("movie runtime must be positive when present")
			}
			runtimeMinutes := *decoded.Runtime
			details.RuntimeMinutes = &runtimeMinutes
		}
		if len(decoded.ProductionCountries) > maxOriginCountries {
			return Details{}, errors.New("origin-country limit exceeded")
		}
		details.OriginCountries = make([]string, len(decoded.ProductionCountries))
		for countryIndex, country := range decoded.ProductionCountries {
			details.OriginCountries[countryIndex] = country.Code
		}
	} else {
		details.MatchedTitle = decoded.Name
		details.ReleaseDate = decoded.FirstAirDate
		if len(decoded.EpisodeRunTime) > maxGenres {
			return Details{}, errors.New("episode-runtime limit exceeded")
		}
		for _, runtimeMinutes := range decoded.EpisodeRunTime {
			if runtimeMinutes <= 0 {
				return Details{}, errors.New(
					"episode runtimes must be positive when present",
				)
			}
		}
		if len(decoded.EpisodeRunTime) > 0 {
			runtimeMinutes := decoded.EpisodeRunTime[0]
			details.RuntimeMinutes = &runtimeMinutes
		}
		details.OriginCountries = decoded.OriginCountry
		if decoded.NumberOfSeasons != nil {
			if *decoded.NumberOfSeasons <= 0 {
				return Details{}, errors.New("season count must be positive when present")
			}
			seasons := *decoded.NumberOfSeasons
			details.Seasons = &seasons
		}
		if decoded.NumberOfEpisodes != nil {
			if *decoded.NumberOfEpisodes <= 0 {
				return Details{}, errors.New("episode count must be positive when present")
			}
			episodes := *decoded.NumberOfEpisodes
			details.Episodes = &episodes
		}
	}

	if detailsError := validateDetails(details); detailsError != nil {
		return Details{}, detailsError
	}
	return details, nil
}

func validateDetails(details Details) error {
	if textError := validateRemoteText(
		details.MatchedTitle,
		true,
		false,
		maxRemoteTextBytes,
	); textError != nil {
		return fmt.Errorf("matched title: %w", textError)
	}
	if details.Description != "" {
		if textError := validateRemoteText(
			details.Description,
			false,
			true,
			maxRemoteDescription,
		); textError != nil {
			return fmt.Errorf("description: %w", textError)
		}
	}
	if details.OriginalLanguage != "" {
		if textError := validateRemoteText(
			details.OriginalLanguage,
			false,
			false,
			maxRemoteTextBytes,
		); textError != nil {
			return fmt.Errorf("original language: %w", textError)
		}
	}
	if details.ReleaseDate != "" {
		if _, dateError := time.Parse("2006-01-02", details.ReleaseDate); dateError != nil {
			return errors.New("release date must use YYYY-MM-DD")
		}
	}
	if len(details.OriginCountries) > maxOriginCountries {
		return errors.New("origin-country limit exceeded")
	}
	seenCountries := make(map[string]struct{}, len(details.OriginCountries))
	for countryIndex, country := range details.OriginCountries {
		if textError := validateRemoteText(country, true, false, maxRemoteTextBytes); textError != nil {
			return fmt.Errorf("origin country %d: %w", countryIndex+1, textError)
		}
		if _, exists := seenCountries[country]; exists {
			return errors.New("duplicate origin country")
		}
		seenCountries[country] = struct{}{}
	}
	if (details.VoteAverage == nil) != (details.VoteCount == nil) {
		return errors.New("vote average and count must be present together")
	}
	if details.VoteAverage != nil &&
		(math.IsNaN(*details.VoteAverage) ||
			math.IsInf(*details.VoteAverage, 0) ||
			*details.VoteAverage < 0 ||
			*details.VoteAverage > 10) {
		return errors.New("vote average must be between zero and ten")
	}
	if details.VoteCount != nil && *details.VoteCount < 0 {
		return errors.New("vote count must not be negative")
	}
	return nil
}

func validateQuery(query string) error {
	if len(query) == 0 || len(query) > product.MaxTMDBQueryBytes {
		return newClientError(
			ErrorInvalidRequest,
			"search",
			0,
			errors.New("query length is outside the current bound"),
		)
	}
	if textError := validateRemoteText(
		query,
		true,
		false,
		product.MaxTMDBQueryBytes,
	); textError != nil {
		return newClientError(ErrorInvalidRequest, "search", 0, textError)
	}
	return nil
}

func validateRemoteText(
	value string,
	required bool,
	allowLineBreaks bool,
	maximumBytes int,
) error {
	if required && value == "" {
		return errors.New("value is required")
	}
	if len(value) > maximumBytes {
		return errors.New("value exceeds the byte limit")
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return errors.New("value must be trimmed UTF-8")
	}
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowLineBreaks && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		return errors.New("value contains a control character")
	}
	return nil
}

func validateRequestContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return newClientError(
			ErrorInvalidRequest,
			operation,
			0,
			errors.New("context is required"),
		)
	}
	if contextError := ctx.Err(); contextError != nil {
		return newClientError(ErrorCanceled, operation, 0, contextError)
	}
	return nil
}

func readBoundedResponse(responseBody io.Reader) ([]byte, error) {
	limitedReader := io.LimitReader(responseBody, product.MaxTMDBResponseBytes+1)
	responseBytes, readError := io.ReadAll(limitedReader)
	if readError != nil {
		return nil, readError
	}
	if int64(len(responseBytes)) > product.MaxTMDBResponseBytes {
		return nil, errors.New("response body exceeds the byte limit")
	}
	return responseBytes, nil
}

func statusClientError(operation string, status int) *Error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return newClientError(ErrorUnauthorized, operation, status, nil)
	case status == http.StatusTooManyRequests:
		return newClientError(ErrorRateLimited, operation, status, nil)
	case status >= 500:
		return newClientError(ErrorUnavailable, operation, status, nil)
	default:
		return newClientError(ErrorInvalidResponse, operation, status, nil)
	}
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests ||
		status == http.StatusRequestTimeout ||
		status == http.StatusInternalServerError ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func retryBackoff(attempt int) time.Duration {
	return 250 * time.Millisecond * time.Duration(1<<min(attempt, 5))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	maximumDelay := time.Duration(product.MaxTMDBRetryAfterSeconds) * time.Second
	trimmedValue := strings.TrimSpace(value)
	if seconds, parseError := strconv.Atoi(trimmedValue); parseError == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, maximumDelay)
	}
	if retryTime, parseError := http.ParseTime(trimmedValue); parseError == nil {
		delay := retryTime.Sub(now)
		if delay < 0 {
			return 0
		}
		return min(delay, maximumDelay)
	}
	return retryBackoff(0)
}

func newClientError(
	code ErrorCode,
	operation string,
	status int,
	cause error,
) *Error {
	return &Error{
		code:      code,
		operation: operation,
		status:    status,
		cause:     cause,
	}
}

func validateClientBaseURL(value string) (string, error) {
	parsedURL, parseError := url.Parse(value)
	if parseError != nil ||
		parsedURL.Host == "" ||
		parsedURL.User != nil ||
		parsedURL.RawQuery != "" ||
		parsedURL.Fragment != "" {
		return "", errors.New("create TMDB client: invalid API base URL")
	}
	if value == OfficialBaseURL {
		if parsedURL.Scheme != "https" || parsedURL.Host != "api.themoviedb.org" {
			return "", errors.New("create TMDB client: official origin contract is invalid")
		}
		return value, nil
	}
	hostAddress := parsedURL.Hostname()
	if parsedURL.Scheme != "http" ||
		(hostAddress != "127.0.0.1" && hostAddress != "localhost" && hostAddress != "::1") {
		return "", errors.New("create TMDB client: injected origins must be loopback HTTP")
	}
	return strings.TrimRight(value, "/"), nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type requestPacer struct {
	mutex    sync.Mutex
	next     time.Time
	interval time.Duration
	now      func() time.Time
	sleep    sleepFunction
}

func newRequestPacer(
	interval time.Duration,
	now func() time.Time,
	sleep sleepFunction,
) *requestPacer {
	return &requestPacer{
		interval: interval,
		now:      now,
		sleep:    sleep,
	}
}

func (pacer *requestPacer) Wait(ctx context.Context) error {
	pacer.mutex.Lock()
	now := pacer.now()
	scheduled := now
	if pacer.next.After(now) {
		scheduled = pacer.next
	}
	pacer.next = scheduled.Add(pacer.interval)
	pacer.mutex.Unlock()
	delay := scheduled.Sub(now)
	if delay <= 0 {
		return nil
	}
	return pacer.sleep(ctx, delay)
}
