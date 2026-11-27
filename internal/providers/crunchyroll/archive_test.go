package crunchyroll

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
)

const testHeader = "history_date,series_or_collection,title,crunchyroll_status,format,crunchyroll_url\n"
const testEpisode = "2026-05-10,Synthetic Series,Pilot,Watched,Episode,https://www.crunchyroll.com/watch/ABC123XYZ/pilot\n"
const testMovie = "2026-05-11,Synthetic Collection,Synthetic Movie,23m left,Movie,https://www.crunchyroll.com/watch/DEF456UVW/movie\n"

func TestParsePreservesHistoryEvidenceAndCanonicalIdentity(testContext *testing.T) {
	input := testHeader + testEpisode + testEpisode + testMovie
	archive := parseForTest(testContext, input)
	withSignature := parseForTest(testContext, "\ufeff"+strings.ReplaceAll(input, "\n", "\r\n"))
	if !reflect.DeepEqual(archive.Snapshot(), withSignature.Snapshot()) {
		testContext.Fatal("signature and CSV line endings changed the canonical dataset")
	}
	snapshot := archive.Snapshot()
	if snapshot.ActiveGeneration.ID != snapshot.Revision {
		testContext.Fatal("active source revision does not identify its generation")
	}
	if snapshot.ActiveGeneration.RecordCount != 3 || snapshot.ActiveGeneration.StartDate != "2026-05-10" || snapshot.ActiveGeneration.EndDate != "2026-05-11" || len(snapshot.Revision) != 64 {
		testContext.Fatalf("unexpected summary: %+v", snapshot)
	}
	records := archive.Records()
	episode := records[0].Snapshot()
	digest := sha256.Sum256([]byte("ABC123XYZ"))
	if episode.Provider != media.Crunchyroll || episode.DatePrecision != media.CalendarDate || episode.Completion != "unknown" || episode.RecordedSeconds != nil || episode.Source.Status != "Watched" || episode.Source.Collection != "Synthetic Series" || episode.Source.Format != "Episode" || episode.SeriesTitle != "Synthetic Series" || episode.SeasonNumber != 0 || episode.EpisodeIdentity != hex.EncodeToString(digest[:]) {
		testContext.Fatalf("source evidence changed: %+v", episode)
	}
	if records[1].Snapshot().ID == episode.ID || records[1].Snapshot().Source.Row != 3 {
		testContext.Fatal("duplicate visible rows lost their source ordinals")
	}
	records[0] = media.Record{}
	if archive.Records()[0].Snapshot().Title != "Pilot" {
		testContext.Fatal("records escaped mutable archive storage")
	}
	changed := parseForTest(testContext, testHeader+strings.Replace(testEpisode, "Watched", "7m left", 1))
	if changed.Snapshot().Revision == parseForTest(testContext, testHeader+testEpisode).Snapshot().Revision {
		testContext.Fatal("source status change retained an obsolete revision")
	}
}

func TestParseQuotedTextFormatsAndChangedSourceIdentity(testContext *testing.T) {
	var buffer strings.Builder
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(expectedHeader); err != nil {
		testContext.Fatal(err)
	}
	if err := writer.Write([]string{"2026-05-12", "Synthetic, Collection", "A \"quoted\" title", "23m", media.CrunchyrollTheatricalSpecial, "https://www.crunchyroll.com/watch/ABC123XYZ"}); err != nil {
		testContext.Fatal(err)
	}
	writer.Flush()
	archive := parseForTest(testContext, buffer.String())
	value := archive.Records()[0].Snapshot()
	if value.Title != "A \"quoted\" title" || value.Source.Collection != "Synthetic, Collection" || value.Source.Status != "23m" || value.Source.Format != media.CrunchyrollTheatricalSpecial || value.SeriesTitle != "" || value.EpisodeIdentity != "" {
		testContext.Fatalf("quoted source evidence changed: %+v", value)
	}
	original := parseForTest(testContext, testHeader+testMovie)
	for name, row := range map[string]string{
		"date":       strings.Replace(testMovie, "2026-05-11", "2026-05-12", 1),
		"collection": strings.Replace(testMovie, "Synthetic Collection", "Another Collection", 1),
		"title":      strings.Replace(testMovie, "Synthetic Movie", "Another Movie", 1),
		"status":     strings.Replace(testMovie, "23m left", "Watched", 1),
		"format":     strings.Replace(testMovie, ",Movie,", ",Theatrical special,", 1),
		"url":        strings.Replace(testMovie, "DEF456UVW", "DEF456UVX", 1),
	} {
		testContext.Run(name, func(testContext *testing.T) {
			revised := parseForTest(testContext, testHeader+row)
			if revised.Snapshot().Revision == original.Snapshot().Revision {
				testContext.Fatal("changed source content kept the previous revision")
			}
		})
	}
	forward := parseForTest(testContext, testHeader+testEpisode+testMovie)
	reverse := parseForTest(testContext, testHeader+testMovie+testEpisode)
	if forward.Snapshot().Revision == reverse.Snapshot().Revision {
		testContext.Fatal("source order did not participate in revision identity")
	}
}

func TestParseRejectsInvalidCSV(testContext *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"header only":        testHeader,
		"obsolete header":    strings.Replace(testHeader, "history_date", "date", 1) + testEpisode,
		"invalid utf8":       testHeader + string([]byte{0xff}) + testEpisode,
		"bad date":           testHeader + strings.Replace(testEpisode, "2026-05-10", "2026-02-30", 1),
		"year zero":          testHeader + strings.Replace(testEpisode, "2026-05-10", "0000-05-10", 1),
		"blank title":        testHeader + strings.Replace(testEpisode, ",Pilot,", ",,", 1),
		"unknown format":     testHeader + strings.Replace(testEpisode, ",Episode,", ",Series,", 1),
		"foreign URL":        testHeader + strings.Replace(testEpisode, "www.crunchyroll.com", "example.com", 1),
		"credential URL":     testHeader + strings.Replace(testEpisode, "https://", "https://user:password@", 1),
		"query URL":          testHeader + strings.Replace(testEpisode, "/pilot", "/pilot?token=secret", 1),
		"fragment URL":       testHeader + strings.Replace(testEpisode, "/pilot", "/pilot#fragment", 1),
		"empty fragment URL": testHeader + strings.Replace(testEpisode, "/pilot", "/pilot#", 1),
		"port URL":           testHeader + strings.Replace(testEpisode, ".com/", ".com:443/", 1),
		"bad watch ID":       testHeader + strings.Replace(testEpisode, "ABC123XYZ", "lowercase", 1),
		"dot link path":      testHeader + strings.Replace(testEpisode, "/pilot", "/%2e%2e", 1),
		"backslash path":     testHeader + strings.Replace(testEpisode, "/pilot", "/a%5Cb", 1),
		"oversized field":    testHeader + strings.Replace(testEpisode, ",Pilot,", ","+strings.Repeat("x", MaxFieldBytes+1)+",", 1),
		"nul status":         testHeader + strings.Replace(testEpisode, "Watched", "Watch\x00ed", 1),
		"malformed row":      testHeader + "2026-05-10,broken\n",
	}
	for name, input := range cases {
		testContext.Run(name, func(testContext *testing.T) {
			_, err := Parse(context.Background(), strings.NewReader(input))
			var invalid *InputError
			if !errors.As(err, &invalid) {
				testContext.Fatalf("expected input error, received %v", err)
			}
		})
	}
	testContext.Run("upload limit", func(testContext *testing.T) {
		_, err := Parse(context.Background(), strings.NewReader(strings.Repeat("x", MaxUploadBytes+1)))
		if !errors.Is(err, ErrTooLarge) {
			testContext.Fatalf("expected upload limit, received %v", err)
		}
	})
	testContext.Run("cancellation", func(testContext *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Parse(ctx, strings.NewReader(testHeader+testEpisode))
		if !errors.Is(err, context.Canceled) {
			testContext.Fatalf("expected cancellation, received %v", err)
		}
	})
}

func TestReplacePreservesActiveStateAcrossRetriesRestartAndDeletion(testContext *testing.T) {
	ctx := context.Background()
	root := rootForTest(testContext)
	empty, err := Read(ctx, root)
	if err != nil || empty.Snapshot().Revision != EmptyRevision || empty.Snapshot().ActiveGeneration != nil {
		testContext.Fatalf("empty provider: %+v, %v", empty.Snapshot(), err)
	}
	first := parseForTest(testContext, testHeader+testEpisode)
	second := parseForTest(testContext, testHeader+testMovie)
	snapshot, err := Replace(ctx, root, EmptyRevision, first)
	if err != nil || snapshot.Revision != first.Snapshot().Revision {
		testContext.Fatalf("initial import: %+v, %v", snapshot, err)
	}
	if _, err = Replace(ctx, root, EmptyRevision, first); !errors.Is(err, ErrStaleRevision) {
		testContext.Fatalf("stale identical request accepted: %v", err)
	}
	if _, err = Replace(ctx, root, snapshot.Revision, first); err != nil {
		testContext.Fatalf("identical current request rejected: %v", err)
	}
	if _, err = Replace(ctx, root, snapshot.Revision, Archive{}); err == nil {
		testContext.Fatal("invalid empty replacement accepted")
	}
	active, err := Read(ctx, root)
	if err != nil || !reflect.DeepEqual(active.Snapshot(), snapshot) {
		testContext.Fatalf("active source lost on restart: %v", err)
	}
	if err = Delete(ctx, root, EmptyRevision); !errors.Is(err, ErrStaleRevision) {
		testContext.Fatalf("stale deletion accepted: %v", err)
	}
	replaced, err := Replace(ctx, root, snapshot.Revision, second)
	if err != nil || replaced.Revision == snapshot.Revision {
		testContext.Fatalf("replacement failed: %v", err)
	}
	if err = Delete(ctx, root, replaced.Revision); err != nil {
		testContext.Fatal(err)
	}
	deleted, err := Read(ctx, root)
	if err != nil || deleted.Snapshot().Revision != EmptyRevision || len(deleted.Records()) != 0 {
		testContext.Fatalf("deletion retained history: %v", err)
	}
	if err = Delete(ctx, root, EmptyRevision); err != nil {
		testContext.Fatal(err)
	}
}

func TestPersistenceRejectsForeignStateAndWriterContention(testContext *testing.T) {
	ctx := context.Background()
	root := rootForTest(testContext)
	archive := parseForTest(testContext, testHeader+testEpisode)
	if _, err := Replace(ctx, root, EmptyRevision, archive); err != nil {
		testContext.Fatal(err)
	}
	leasePath, err := root.File(providerLeasePath)
	if err != nil {
		testContext.Fatal(err)
	}
	handle, err := os.OpenFile(leasePath.Path(), os.O_RDWR, 0)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handle.Close()
	if err = syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		testContext.Fatal(err)
	}
	if _, err = Replace(ctx, root, archive.Snapshot().Revision, archive); !errors.Is(err, ErrBusy) {
		testContext.Fatalf("concurrent writer accepted: %v", err)
	}
	if err = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN); err != nil {
		testContext.Fatal(err)
	}
	state, err := root.File(providerStatePath)
	if err != nil {
		testContext.Fatal(err)
	}
	if err = state.Replace(func(writer io.Writer) error {
		_, writeError := io.WriteString(writer, `{"contract":"obsolete","rows":[]}`)
		return writeError
	}); err != nil {
		testContext.Fatal(err)
	}
	if _, err = Read(ctx, root); err == nil {
		testContext.Fatal("foreign persistence accepted")
	}
}

func TestPersistenceRejectsMalformedCurrentState(testContext *testing.T) {
	archive := parseForTest(testContext, testHeader+testEpisode)
	validState := libraryState{Contract: storeContract, Revision: archive.Snapshot().Revision, Rows: archive.rows}
	stateBytes, err := json.Marshal(validState)
	if err != nil {
		testContext.Fatal(err)
	}
	cases := map[string][]byte{
		"source revision mismatch": []byte(strings.Replace(string(stateBytes), archive.Snapshot().Revision, strings.Repeat("0", 64), 1)),
		"unknown property":         []byte(strings.TrimSuffix(string(stateBytes), "}") + `,"extra":true}`),
		"trailing document":        append(append([]byte(nil), stateBytes...), []byte("{}")...),
		"invalid utf8":             []byte{0xff},
		"null rows":                []byte(`{"contract":"` + storeContract + `","revision":"empty","rows":null}`),
		"bad empty revision":       []byte(`{"contract":"` + storeContract + `","revision":"other","rows":[]}`),
		"source row invalid":       []byte(strings.Replace(string(stateBytes), "2026-05-10", "invalid", 1)),
	}
	for name, encoded := range cases {
		testContext.Run(name, func(testContext *testing.T) {
			root := rootForTest(testContext)
			file, err := root.File(providerStatePath)
			if err != nil {
				testContext.Fatal(err)
			}
			if err = file.Replace(func(writer io.Writer) error {
				_, writeError := writer.Write(encoded)
				return writeError
			}); err != nil {
				testContext.Fatal(err)
			}
			if _, err = Read(context.Background(), root); err == nil {
				testContext.Fatal("malformed source persistence accepted")
			}
		})
	}
	testContext.Run("canceled replacement preserves active", func(testContext *testing.T) {
		root := rootForTest(testContext)
		if _, err := Replace(context.Background(), root, EmptyRevision, archive); err != nil {
			testContext.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		second := parseForTest(testContext, testHeader+testMovie)
		if _, err := Replace(ctx, root, archive.Snapshot().Revision, second); !errors.Is(err, context.Canceled) {
			testContext.Fatalf("canceled replacement accepted: %v", err)
		}
		active, err := Read(context.Background(), root)
		if err != nil || active.Snapshot().Revision != archive.Snapshot().Revision {
			testContext.Fatalf("canceled replacement changed source: %v", err)
		}
	})
}

func parseForTest(testContext *testing.T, input string) Archive {
	testContext.Helper()
	archive, err := Parse(context.Background(), strings.NewReader(input))
	if err != nil {
		testContext.Fatal(err)
	}
	return archive
}

func rootForTest(testContext *testing.T) privatepath.Root {
	testContext.Helper()
	root, err := privatepath.NewRoot(testContext.TempDir() + "/data")
	if err != nil {
		testContext.Fatal(err)
	}
	return root
}
