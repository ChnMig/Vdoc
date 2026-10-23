package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"vdoc/domain/release"
)

const (
	releasesAPI  = "https://api.github.com/repos/ChnMig/Vdoc-site/releases"
	releasesURL  = "https://github.com/ChnMig/Vdoc-site/releases/tag/"
	pageSize     = 100
	maxPages     = 10
	maxBodySize  = 4 << 20
	checkTimeout = 10 * time.Second
	cacheTTL     = 2 * time.Minute
)

var ErrUnsupportedVersion = errors.New("deployment is not a published beta or stable version")

type UpdateResult struct {
	CurrentVersion  string
	Channel         release.Channel
	LatestVersion   *string
	UpdateAvailable bool
	ReleaseURL      *string
	PublishedAt     *time.Time
}

type githubRelease struct {
	TagName     string     `json:"tag_name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt *time.Time `json:"published_at"`
}

type cachedRelease struct {
	latest    *githubRelease
	expiresAt time.Time
}

type Checker struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[release.Channel]cachedRelease
}

func NewChecker() *Checker {
	return &Checker{
		client: &http.Client{
			Timeout: checkTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("release API redirects are not allowed")
			},
		},
		cache: make(map[release.Channel]cachedRelease),
	}
}

func (checker *Checker) Check(ctx context.Context, currentVersion string) (UpdateResult, error) {
	current, ok := release.Parse(currentVersion)
	if !ok {
		return UpdateResult{}, ErrUnsupportedVersion
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return UpdateResult{}, err
	}
	latest, err := checker.latest(ctx, current.Channel)
	if err != nil {
		return UpdateResult{}, err
	}
	result := UpdateResult{CurrentVersion: currentVersion, Channel: current.Channel}
	if latest != nil {
		latestVersion, _ := release.Parse(latest.TagName)
		result.LatestVersion = &latest.TagName
		url := releasesURL + latest.TagName
		result.ReleaseURL = &url
		result.PublishedAt = latest.PublishedAt
		result.UpdateAvailable = latestVersion.Compare(current) > 0
	}
	return result, nil
}

func (checker *Checker) latest(ctx context.Context, channel release.Channel) (*githubRelease, error) {
	checker.mu.Lock()
	cached, exists := checker.cache[channel]
	checker.mu.Unlock()
	if exists && time.Now().Before(cached.expiresAt) {
		return cloneRelease(cached.latest), nil
	}
	var latest *githubRelease
	var latestVersion release.Version
	for page := 1; page <= maxPages; page++ {
		releases, err := checker.fetchPage(ctx, page)
		if err != nil {
			return nil, err
		}
		for _, candidate := range releases {
			version, ok := release.Parse(candidate.TagName)
			if !ok || candidate.Draft || version.Channel != channel || candidate.Prerelease != (channel == release.Beta) {
				continue
			}
			if latest == nil || version.Compare(latestVersion) > 0 {
				latest = cloneRelease(&candidate)
				latestVersion = version
			}
		}
		if len(releases) < pageSize {
			checker.mu.Lock()
			checker.cache[channel] = cachedRelease{latest: cloneRelease(latest), expiresAt: time.Now().Add(cacheTTL)}
			checker.mu.Unlock()
			return latest, nil
		}
	}
	return nil, errors.New("release API pagination limit exceeded")
}

func (checker *Checker) fetchPage(ctx context.Context, page int) ([]githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI+"?per_page="+strconv.Itoa(pageSize)+"&page="+strconv.Itoa(page), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Vdoc-update-checker")
	response, err := checker.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("check release API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release API returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("read release API: %w", err)
	}
	if len(body) > maxBodySize {
		return nil, errors.New("release API response is too large")
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decode release API: %w", err)
	}
	if releases == nil || len(releases) > pageSize {
		return nil, errors.New("invalid release API list")
	}
	return releases, nil
}

func cloneRelease(value *githubRelease) *githubRelease {
	if value == nil {
		return nil
	}
	clone := *value
	if value.PublishedAt != nil {
		publishedAt := *value.PublishedAt
		clone.PublishedAt = &publishedAt
	}
	return &clone
}
