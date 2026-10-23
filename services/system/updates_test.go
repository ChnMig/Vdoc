package system

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"vdoc/domain/release"
)

type updateRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip updateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func updateResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func updateReleasesResponse(t *testing.T, releases []githubRelease) *http.Response {
	t.Helper()
	body, err := json.Marshal(releases)
	if err != nil {
		t.Fatal(err)
	}
	return updateResponse(string(body), http.StatusOK)
}

func TestCheckerSeparatesChannelsAndIgnoresUnpublishedTags(t *testing.T) {
	for _, test := range []struct {
		current string
		latest  string
		channel release.Channel
	}{
		{current: "v1.0.0", latest: "v1.2.0", channel: release.Stable},
		{current: "v1.0.0-beta.1", latest: "v1.3.0-beta.10", channel: release.Beta},
	} {
		t.Run(string(test.channel), func(t *testing.T) {
			checker := NewChecker()
			checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
				return updateReleasesResponse(t, []githubRelease{
					{TagName: "v1.3.0-beta.9", Prerelease: true},
					{TagName: "v1.2.0"},
					{TagName: "v1.3.0-beta.10", Prerelease: true},
					{TagName: "v9.0.0", Draft: true},
					{TagName: "v9.0.0-beta.1", Draft: true, Prerelease: true},
					{TagName: "v9.0.0-rc.1", Prerelease: true},
					{TagName: "v9.0.0-beta.1", Prerelease: false},
					{TagName: "v9.0.0", Prerelease: true},
					{TagName: "9.0.0"},
				}), nil
			})
			got, err := checker.Check(context.Background(), test.current)
			if err != nil {
				t.Fatal(err)
			}
			if got.CurrentVersion != test.current || got.Channel != test.channel || got.LatestVersion == nil || *got.LatestVersion != test.latest || !got.UpdateAvailable {
				t.Fatalf("unexpected channel result: %#v", got)
			}
		})
	}
}

func TestCheckerReportsNoReleaseSameNewerAndAheadVersions(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		latest  string
		update  bool
	}{
		{name: "no stable release", current: "v1.2.0"},
		{name: "no beta release", current: "v1.2.0-beta.2"},
		{name: "stable unchanged", current: "v1.2.0", latest: "v1.2.0"},
		{name: "beta unchanged", current: "v1.2.0-beta.2", latest: "v1.2.0-beta.2"},
		{name: "stable update", current: "v1.2.0", latest: "v1.3.0", update: true},
		{name: "beta update", current: "v1.2.0-beta.9", latest: "v1.2.0-beta.10", update: true},
		{name: "stable ahead", current: "v1.3.0", latest: "v1.2.0"},
		{name: "beta ahead", current: "v1.2.0-beta.11", latest: "v1.2.0-beta.10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checker := NewChecker()
			publishedAt := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
				items := []githubRelease{}
				if test.latest != "" {
					items = append(items, githubRelease{TagName: test.latest, Prerelease: release.ChannelOf(test.latest) == release.Beta, PublishedAt: &publishedAt})
				}
				return updateReleasesResponse(t, items), nil
			})
			got, err := checker.Check(context.Background(), test.current)
			if err != nil {
				t.Fatal(err)
			}
			if got.UpdateAvailable != test.update || got.CurrentVersion != test.current || got.Channel != release.ChannelOf(test.current) {
				t.Fatalf("unexpected update result: %#v", got)
			}
			if test.latest == "" {
				if got.LatestVersion != nil || got.ReleaseURL != nil || got.PublishedAt != nil {
					t.Fatalf("missing release has metadata: %#v", got)
				}
				return
			}
			if got.LatestVersion == nil || *got.LatestVersion != test.latest || got.ReleaseURL == nil || *got.ReleaseURL != releasesURL+test.latest || got.PublishedAt == nil || !got.PublishedAt.Equal(publishedAt) {
				t.Fatalf("wrong release metadata: %#v", got)
			}
		})
	}
}

func TestCheckerUsesTrustedBoundedRequest(t *testing.T) {
	checker := NewChecker()
	if checker.client.Timeout != checkTimeout || checker.client.CheckRedirect == nil || checker.client.CheckRedirect(nil, nil) == nil {
		t.Fatal("release HTTP client must have a timeout and reject redirects")
	}
	checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != releasesAPI+"?per_page=100&page=1" {
			t.Errorf("unexpected release request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Accept") != "application/vnd.github+json" || request.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || request.Header.Get("User-Agent") != "Vdoc-update-checker" {
			t.Errorf("missing release API headers: %#v", request.Header)
		}
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > checkTimeout {
			t.Errorf("request context is not bounded: %v, %v", deadline, ok)
		}
		return updateResponse(`[{"tag_name":"v1.2.0","html_url":"https://untrusted.example/install"}]`, http.StatusOK), nil
	})
	got, err := checker.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReleaseURL == nil || *got.ReleaseURL != "https://github.com/ChnMig/Vdoc-site/releases/tag/v1.2.0" {
		t.Fatalf("release URL was not constructed from trusted host: %#v", got.ReleaseURL)
	}
}

func TestCheckerRejectsUnsupportedDeploymentWithoutNetwork(t *testing.T) {
	checker := NewChecker()
	checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported deployment must not contact release API")
		return nil, nil
	})
	for _, current := range []string{"dev", "v1.2.0-dirty", "v1.2.0-rc.1", "1.2.0", ""} {
		if got, err := checker.Check(context.Background(), current); !errors.Is(err, ErrUnsupportedVersion) || !reflect.DeepEqual(got, UpdateResult{}) {
			t.Errorf("Check(%q) = %#v, %v; want unsupported error and no success result", current, got, err)
		}
	}
}

type failingUpdateBody struct{}

func (failingUpdateBody) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (failingUpdateBody) Close() error             { return nil }

func TestCheckerFailuresReturnErrorsAndAreNotCached(t *testing.T) {
	for _, test := range []struct {
		name     string
		response func() (*http.Response, error)
		contains string
	}{
		{name: "network", response: func() (*http.Response, error) { return nil, errors.New("network unavailable") }, contains: "network unavailable"},
		{name: "forbidden", response: func() (*http.Response, error) {
			return updateResponse(`{"message":"rate limited"}`, http.StatusForbidden), nil
		}, contains: "HTTP 403"},
		{name: "server error", response: func() (*http.Response, error) {
			return updateResponse("unavailable", http.StatusServiceUnavailable), nil
		}, contains: "HTTP 503"},
		{name: "malformed JSON", response: func() (*http.Response, error) { return updateResponse("[", http.StatusOK), nil }, contains: "decode release API"},
		{name: "trailing JSON", response: func() (*http.Response, error) { return updateResponse("[] {}", http.StatusOK), nil }, contains: "decode release API"},
		{name: "null list", response: func() (*http.Response, error) { return updateResponse("null", http.StatusOK), nil }, contains: "invalid release API list"},
		{name: "object instead of list", response: func() (*http.Response, error) { return updateResponse("{}", http.StatusOK), nil }, contains: "decode release API"},
		{name: "invalid timestamp", response: func() (*http.Response, error) {
			return updateResponse(`[{"tag_name":"v1.2.0","published_at":"yesterday"}]`, http.StatusOK), nil
		}, contains: "decode release API"},
		{name: "oversize body", response: func() (*http.Response, error) {
			return updateResponse(strings.Repeat(" ", maxBodySize+1), http.StatusOK), nil
		}, contains: "response is too large"},
		{name: "too many list entries", response: func() (*http.Response, error) {
			return updateReleasesResponse(t, make([]githubRelease, pageSize+1)), nil
		}, contains: "invalid release API list"},
		{name: "body read", response: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failingUpdateBody{}}, nil
		}, contains: "body read failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checker := NewChecker()
			calls := 0
			checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return test.response()
				}
				return updateResponse(`[{"tag_name":"v1.2.0"}]`, http.StatusOK), nil
			})
			got, err := checker.Check(context.Background(), "v1.0.0")
			if err == nil || !strings.Contains(err.Error(), test.contains) || !reflect.DeepEqual(got, UpdateResult{}) {
				t.Fatalf("failure reported as success: result=%#v error=%v", got, err)
			}
			got, err = checker.Check(context.Background(), "v1.0.0")
			if err != nil || !got.UpdateAvailable || calls != 2 {
				t.Fatalf("failed response was cached: result=%#v error=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestCheckerCachesByChannelAndRefreshesExpiredCache(t *testing.T) {
	checker := NewChecker()
	calls := 0
	betaTag := "v1.2.0-beta.10"
	publishedAt := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return updateReleasesResponse(t, []githubRelease{
			{TagName: betaTag, Prerelease: true, PublishedAt: &publishedAt},
			{TagName: "v1.2.0", PublishedAt: &publishedAt},
		}), nil
	})
	first, err := checker.Check(context.Background(), "v1.2.0-beta.9")
	if err != nil || !first.UpdateAvailable {
		t.Fatalf("initial check failed: %#v, %v", first, err)
	}
	*first.LatestVersion = "caller mutation"
	*first.ReleaseURL = "https://untrusted.example"
	*first.PublishedAt = publishedAt.Add(24 * time.Hour)
	second, err := checker.Check(context.Background(), "v1.2.0-beta.10")
	if err != nil || second.UpdateAvailable || second.CurrentVersion != "v1.2.0-beta.10" || second.LatestVersion == nil || *second.LatestVersion != betaTag || second.PublishedAt == nil || !second.PublishedAt.Equal(publishedAt) || calls != 1 {
		t.Fatalf("cache leaked caller state or stale current version: %#v, %v, calls=%d", second, err, calls)
	}
	if stable, err := checker.Check(context.Background(), "v1.1.0"); err != nil || stable.LatestVersion == nil || *stable.LatestVersion != "v1.2.0" || calls != 2 {
		t.Fatalf("channel cache leaked: %#v, %v, calls=%d", stable, err, calls)
	}
	checker.mu.Lock()
	entry := checker.cache[release.Beta]
	entry.expiresAt = time.Now().Add(-time.Second)
	checker.cache[release.Beta] = entry
	checker.mu.Unlock()
	betaTag = "v1.2.0-beta.11"
	refreshed, err := checker.Check(context.Background(), "v1.2.0-beta.10")
	if err != nil || !refreshed.UpdateAvailable || refreshed.LatestVersion == nil || *refreshed.LatestVersion != betaTag || calls != 3 {
		t.Fatalf("expired cache did not refresh: %#v, %v, calls=%d", refreshed, err, calls)
	}
}

func TestCheckerCachesAnEmptyReleaseList(t *testing.T) {
	checker := NewChecker()
	calls := 0
	checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return updateResponse("[]", http.StatusOK), nil
	})
	for range 2 {
		got, err := checker.Check(context.Background(), "v1.0.0-beta.1")
		if err != nil || got.LatestVersion != nil || got.UpdateAvailable {
			t.Fatalf("unexpected empty-release result: %#v, %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("empty release list was not cached: calls=%d", calls)
	}
}

func TestCheckerCancellationAndTimeoutAreFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		context  func() (context.Context, context.CancelFunc)
		expected error
	}{
		{name: "already canceled", context: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, expected: context.Canceled},
		{name: "deadline elapsed", context: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}, expected: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			checker := NewChecker()
			checker.cache[release.Stable] = cachedRelease{latest: &githubRelease{TagName: "v1.2.0"}, expiresAt: time.Now().Add(time.Minute)}
			checker.client.Transport = updateRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("already canceled check must not contact the network")
				return nil, nil
			})
			ctx, cancel := test.context()
			defer cancel()
			if got, err := checker.Check(ctx, "v1.0.0"); !errors.Is(err, test.expected) || !reflect.DeepEqual(got, UpdateResult{}) {
				t.Fatalf("cancellation returned cached success: %#v, %v", got, err)
			}
		})
	}
	t.Run("canceled during request", func(t *testing.T) {
		checker := NewChecker()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			cancel()
			return nil, request.Context().Err()
		})
		if got, err := checker.Check(ctx, "v1.0.0"); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, UpdateResult{}) {
			t.Fatalf("request cancellation reported success: %#v, %v", got, err)
		}
	})
	t.Run("HTTP client timeout", func(t *testing.T) {
		checker := NewChecker()
		checker.client.Timeout = time.Nanosecond
		checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		if got, err := checker.Check(context.Background(), "v1.0.0"); !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(got, UpdateResult{}) {
			t.Fatalf("request timeout reported success: %#v, %v", got, err)
		}
	})
}

func TestCheckerFindsLatestAcrossPages(t *testing.T) {
	checker := NewChecker()
	pages := []int{}
	checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
		page, err := strconv.Atoi(request.URL.Query().Get("page"))
		if err != nil || request.URL.Query().Get("per_page") != "100" {
			t.Fatalf("invalid pagination query: %s", request.URL)
		}
		pages = append(pages, page)
		if page == 1 {
			items := make([]githubRelease, pageSize)
			items[0] = githubRelease{TagName: "v1.2.0-beta.9", Prerelease: true}
			return updateReleasesResponse(t, items), nil
		}
		if page != 2 {
			t.Fatalf("unexpected page %d", page)
		}
		return updateReleasesResponse(t, []githubRelease{{TagName: "v1.2.0-beta.10", Prerelease: true}}), nil
	})
	got, err := checker.Check(context.Background(), "v1.2.0-beta.8")
	if err != nil || got.LatestVersion == nil || *got.LatestVersion != "v1.2.0-beta.10" || !reflect.DeepEqual(pages, []int{1, 2}) {
		t.Fatalf("did not find highest release across pages: %#v, %v, pages=%v", got, err, pages)
	}
}

func TestCheckerPaginationLimitAndPartialFailureDoNotCacheSuccess(t *testing.T) {
	for _, test := range []struct {
		name     string
		failPage int
		contains string
	}{
		{name: "pagination limit", failPage: 0, contains: "pagination limit exceeded"},
		{name: "second page failure", failPage: 2, contains: "HTTP 503"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checker := NewChecker()
			calls := 0
			checker.client.Transport = updateRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				page, _ := strconv.Atoi(request.URL.Query().Get("page"))
				if page == test.failPage {
					return updateResponse("unavailable", http.StatusServiceUnavailable), nil
				}
				items := make([]githubRelease, pageSize)
				items[0] = githubRelease{TagName: "v1.2.0"}
				return updateReleasesResponse(t, items), nil
			})
			got, err := checker.Check(context.Background(), "v1.0.0")
			if err == nil || !strings.Contains(err.Error(), test.contains) || !reflect.DeepEqual(got, UpdateResult{}) || len(checker.cache) != 0 {
				t.Fatalf("partial list reported/cached as success: %#v, %v, cache=%#v", got, err, checker.cache)
			}
			wantCalls := maxPages
			if test.failPage != 0 {
				wantCalls = test.failPage
			}
			if calls != wantCalls {
				t.Fatalf("unexpected page count: got %d, want %d", calls, wantCalls)
			}
		})
	}
}
