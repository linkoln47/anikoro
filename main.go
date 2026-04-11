package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	malAuthorizeURL  = "https://myanimelist.net/v1/oauth2/authorize"
	malTokenURL      = "https://myanimelist.net/v1/oauth2/token"
	malAnimeListURL  = "https://api.myanimelist.net/v2/users/@me/animelist"
	tokenFileName    = ".mal_token.json"
	detailsCacheName = ".mal_anime_details_cache.json"
	seriesOutputName = "series_list.txt"
	moviesOutputName = "movies_list.txt"
)

var (
	tokenFilePath    = appFilePath(tokenFileName)
	detailsCachePath = appFilePath(detailsCacheName)
	seriesOutputPath = appFilePath(seriesOutputName)
	moviesOutputPath = appFilePath(moviesOutputName)
)

type animeListResponse struct {
	Data []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
		ListStatus struct {
			Score              int `json:"score"`
			NumEpisodesWatched int `json:"num_episodes_watched"`
		} `json:"list_status"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

type malToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type animeDetailsResponse struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	MediaType    string `json:"media_type"`
	RelatedAnime []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
	} `json:"related_anime"`
}

type animeDetailsCacheItem struct {
	RelatedIDs []int     `json:"related_ids"`
	MediaType  string    `json:"media_type"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type animeDetailsInfo struct {
	RelatedIDs []int
	MediaType  string
}

type groupedView struct {
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
}

func main() {
	clientID := firstNonEmpty(
		strings.TrimSpace(os.Getenv("MAL_CLIENT_ID")),
	)
	clientSecret := firstNonEmpty(
		strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET")),
	)
	redirectURI := firstNonEmpty(
		strings.TrimSpace(os.Getenv("MAL_REDIRECT_URI")),
	)
	if redirectURI == "" {
		redirectURI = "http://localhost:8085/callback"
	}

	if envToken := strings.TrimSpace(os.Getenv("MAL_ACCESS_TOKEN")); envToken != "" {
		if err := printCompletedAnime(envToken); err != nil {
			fmt.Printf("request error: %v\n", err)
		}
		return
	}

	if clientID == "" {
		fmt.Println("set MAL_CLIENT_ID (and optionally MAL_CLIENT_SECRET) to run OAuth flow")
		return
	}

	token, err := ensureToken(clientID, clientSecret, redirectURI)
	if err != nil {
		fmt.Printf("auth error: %v\n", err)
		return
	}

	if err := printCompletedAnime(token.AccessToken); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "401") && token.RefreshToken != "" {
			refreshed, refreshErr := refreshAccessToken(clientID, clientSecret, token.RefreshToken)
			if refreshErr == nil {
				_ = saveToken(refreshed)
				if retryErr := printCompletedAnime(refreshed.AccessToken); retryErr == nil {
					return
				}
			}
		}
		fmt.Printf("request error: %v\n", err)
	}
}

func ensureToken(clientID, clientSecret, redirectURI string) (*malToken, error) {
	if token, err := loadTokenFromFile(); err == nil {
		if token.AccessToken != "" && time.Now().Before(token.ExpiresAt) {
			return token, nil
		}
		if token.RefreshToken != "" {
			refreshed, refreshErr := refreshAccessToken(clientID, clientSecret, token.RefreshToken)
			if refreshErr == nil {
				_ = saveToken(refreshed)
				return refreshed, nil
			}
		}
	}

	code, verifier, err := authorizeWithLocalCallback(clientID, redirectURI)
	if err != nil {
		return nil, err
	}

	token, err := exchangeCodeForToken(clientID, clientSecret, redirectURI, code, verifier)
	if err != nil {
		return nil, err
	}
	if err := saveToken(token); err != nil {
		fmt.Printf("warning: cannot save token file: %v\n", err)
	}
	return token, nil
}

func authorizeWithLocalCallback(clientID, redirectURI string) (code string, verifier string, err error) {
	verifier, err = randomURLSafe(64)
	if err != nil {
		return "", "", err
	}
	state, err := randomURLSafe(24)
	if err != nil {
		return "", "", err
	}

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return "", "", fmt.Errorf("invalid MAL_REDIRECT_URI: %w", err)
	}
	if redirectURL.Host == "" {
		return "", "", errors.New("MAL_REDIRECT_URI must include host")
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Addr: redirectURL.Host}
	mux := http.NewServeMux()
	srv.Handler = mux

	mux.HandleFunc(redirectURL.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errCh <- errors.New("state mismatch"):
			default:
			}
			return
		}
		codeVal := q.Get("code")
		if codeVal == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- errors.New("missing code in callback"):
			default:
			}
			return
		}
		_, _ = w.Write([]byte("Authorization completed. You can return to terminal."))
		select {
		case codeCh <- codeVal:
		default:
		}
	})

	go func() {
		if listenErr := srv.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			select {
			case errCh <- listenErr:
			default:
			}
		}
	}()

	authURL, err := buildAuthURL(clientID, redirectURI, state, verifier)
	if err != nil {
		_ = srv.Shutdown(context.Background())
		return "", "", err
	}

	fmt.Println("Open this URL to authorize:")
	fmt.Println(authURL)
	tryOpenBrowser(authURL)

	select {
	case code = <-codeCh:
		_ = srv.Shutdown(context.Background())
		return code, verifier, nil
	case listenErr := <-errCh:
		_ = srv.Shutdown(context.Background())
		return "", "", listenErr
	case <-time.After(3 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return "", "", errors.New("authorization timeout")
	}
}

func buildAuthURL(clientID, redirectURI, state, codeChallenge string) (string, error) {
	u, err := url.Parse(malAuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "plain")
	q.Set("redirect_uri", redirectURI)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func exchangeCodeForToken(clientID, clientSecret, redirectURI, code, verifier string) (*malToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(body))
	}

	var tok malToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func refreshAccessToken(clientID, clientSecret, refreshToken string) (*malToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh endpoint %d: %s", resp.StatusCode, string(body))
	}

	var tok malToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func loadTokenFromFile() (*malToken, error) {
	b, err := os.ReadFile(tokenFilePath)
	if err != nil {
		return nil, err
	}
	var tok malToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("empty access_token in token file")
	}
	return &tok, nil
}

func saveToken(token *malToken) error {
	b, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFilePath, b, 0o600)
}

func randomURLSafe(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("length must be > 0")
	}
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(raw)
	if len(s) > length {
		s = s[:length]
	}
	return s, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func tryOpenBrowser(link string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	case "darwin":
		cmd = exec.Command("open", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	_ = cmd.Start()
}

func printCompletedAnime(token string) error {
	fmt.Println("Starting MAL sync...")

	u, err := url.Parse(malAnimeListURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("status", "completed")
	q.Set("limit", "100")
	q.Set("fields", "list_status")
	u.RawQuery = q.Encode()

	type animeEntry struct {
		ID                 int
		Title              string
		Score              int
		NumEpisodesWatched int
	}
	var allEntries []animeEntry

	nextURL := u.String()
	page := 1
	for nextURL != "" {
		fmt.Printf("Requesting animelist page %d: %s\n", page, nextURL)
		req, err := http.NewRequest(http.MethodGet, nextURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("MAL API returned %d: %s", resp.StatusCode, string(body))
		}

		var parsed animeListResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		for _, item := range parsed.Data {
			allEntries = append(allEntries, animeEntry{
				ID:                 item.Node.ID,
				Title:              item.Node.Title,
				Score:              item.ListStatus.Score,
				NumEpisodesWatched: item.ListStatus.NumEpisodesWatched,
			})
		}
		fmt.Printf("Received %d entries on page %d\n", len(parsed.Data), page)
		nextURL = parsed.Paging.Next
		page++
	}

	if len(allEntries) == 0 {
		fmt.Println("No completed anime found.")
		return nil
	}

	cache, err := loadDetailsCache()
	if err != nil {
		fmt.Printf("warning: cannot load details cache: %v\n", err)
		cache = map[int]animeDetailsCacheItem{}
	}

	parent := make([]int, len(allEntries))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	idToIndexes := make(map[int][]int)
	for i, entry := range allEntries {
		if entry.ID != 0 {
			idToIndexes[entry.ID] = append(idToIndexes[entry.ID], i)
		}
	}

	detailsMap := make(map[int]animeDetailsInfo)
	for i, entry := range allEntries {
		fmt.Printf("Resolving details for id=%d (%s)\n", entry.ID, entry.Title)
		details, err := fetchAnimeDetails(token, entry.ID, cache)
		if err != nil {
			return err
		}
		detailsMap[entry.ID] = details
		for _, relID := range details.RelatedIDs {
			for _, j := range idToIndexes[relID] {
				union(i, j)
			}
		}
	}
	if err := saveDetailsCache(cache); err != nil {
		fmt.Printf("warning: cannot save details cache: %v\n", err)
	}

	type grouped struct {
		DisplayTitle       string
		NumEpisodesWatched int
		TotalScore         int
		ItemsCount         int
		Titles             map[string]struct{}
		HasMovie           bool
		HasNonMovie        bool
		IsIsolatedMovie    bool
	}
	groups := make(map[int]*grouped)
	for i, entry := range allEntries {
		root := find(i)
		g := groups[root]
		if g == nil {
			g = &grouped{
				DisplayTitle: entry.Title,
				Titles:       make(map[string]struct{}),
			}
			groups[root] = g
		}
		g.NumEpisodesWatched += entry.NumEpisodesWatched
		g.TotalScore += entry.Score
		g.ItemsCount++
		g.Titles[entry.Title] = struct{}{}

		details := detailsMap[entry.ID]
		if details.MediaType == "movie" {
			g.HasMovie = true
		} else {
			g.HasNonMovie = true
		}
	}

	var seriesGroups []groupedView
	var movieGroups []groupedView
	for root, g := range groups {
		avgScore := 0.0
		if g.ItemsCount > 0 {
			avgScore = float64(g.TotalScore) / float64(g.ItemsCount)
		}

		g.IsIsolatedMovie = false
		if g.ItemsCount == 1 && g.HasMovie && !g.HasNonMovie {
			entry := allEntries[root]
			hasLinkInsideList := false
			for _, relID := range detailsMap[entry.ID].RelatedIDs {
				if len(idToIndexes[relID]) > 0 {
					hasLinkInsideList = true
					break
				}
			}
			g.IsIsolatedMovie = !hasLinkInsideList
		}

		view := groupedView{
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       len(g.Titles),
			AvgScore:           avgScore,
			WatchedEpisodesSum: g.NumEpisodesWatched,
		}
		if g.IsIsolatedMovie {
			movieGroups = append(movieGroups, view)
		} else {
			seriesGroups = append(seriesGroups, view)
		}
	}

	sort.Slice(seriesGroups, func(i, j int) bool {
		if seriesGroups[i].WatchedEpisodesSum == seriesGroups[j].WatchedEpisodesSum {
			return seriesGroups[i].DisplayTitle < seriesGroups[j].DisplayTitle
		}
		return seriesGroups[i].WatchedEpisodesSum > seriesGroups[j].WatchedEpisodesSum
	})
	sort.Slice(movieGroups, func(i, j int) bool {
		if movieGroups[i].WatchedEpisodesSum == movieGroups[j].WatchedEpisodesSum {
			return movieGroups[i].DisplayTitle < movieGroups[j].DisplayTitle
		}
		return movieGroups[i].WatchedEpisodesSum > movieGroups[j].WatchedEpisodesSum
	})

	seriesText := renderGroupList("Series and merged entries", seriesGroups)
	moviesText := renderGroupList("Standalone movies", movieGroups)

	if err := writeFileWithChangeLog(seriesOutputPath, []byte(seriesText), 0o644, "Output file"); err != nil {
		return fmt.Errorf("cannot write %s: %w", seriesOutputPath, err)
	}

	if err := writeFileWithChangeLog(moviesOutputPath, []byte(moviesText), 0o644, "Output file"); err != nil {
		return fmt.Errorf("cannot write %s: %w", moviesOutputPath, err)
	}
	fmt.Println("Sync completed.")
	return nil
}

func fetchAnimeDetails(token string, animeID int, cache map[int]animeDetailsCacheItem) (animeDetailsInfo, error) {
	if animeID == 0 {
		return animeDetailsInfo{}, nil
	}
	if v, ok := cache[animeID]; ok {
		fmt.Printf("Cache hit for anime id=%d\n", animeID)
		return animeDetailsInfo{RelatedIDs: v.RelatedIDs, MediaType: v.MediaType}, nil
	}
	fmt.Printf("Cache miss for anime id=%d\n", animeID)

	detailsURL := fmt.Sprintf("https://api.myanimelist.net/v2/anime/%d?fields=related_anime,media_type", animeID)

	var details animeDetailsResponse
	maxAttempts := 4
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("Requesting details id=%d attempt %d/%d\n", animeID, attempt, maxAttempts)
		req, err := http.NewRequest(http.MethodGet, detailsURL, nil)
		if err != nil {
			return animeDetailsInfo{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if attempt == maxAttempts {
				fmt.Printf("warning: related_anime request failed for id=%d after %d attempts: %v\n", animeID, maxAttempts, err)
				cache[animeID] = animeDetailsCacheItem{
					RelatedIDs: nil,
					MediaType:  "",
					UpdatedAt:  time.Now(),
				}
				return animeDetailsInfo{}, nil
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			if err := json.Unmarshal(body, &details); err != nil {
				return animeDetailsInfo{}, err
			}
			fmt.Printf("Details fetched id=%d media_type=%s related=%d\n", animeID, details.MediaType, len(details.RelatedAnime))
			break
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt == maxAttempts {
				fmt.Printf("warning: related_anime endpoint %d for id=%d after %d attempts; skipping\n", resp.StatusCode, animeID, maxAttempts)
				cache[animeID] = animeDetailsCacheItem{
					RelatedIDs: nil,
					MediaType:  "",
					UpdatedAt:  time.Now(),
				}
				return animeDetailsInfo{}, nil
			}
			time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
			continue
		}

		return animeDetailsInfo{}, fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	ids := make([]int, 0, len(details.RelatedAnime))
	for _, rel := range details.RelatedAnime {
		if rel.Node.ID != 0 {
			ids = append(ids, rel.Node.ID)
		}
	}
	cache[animeID] = animeDetailsCacheItem{
		RelatedIDs: ids,
		MediaType:  details.MediaType,
		UpdatedAt:  time.Now(),
	}
	fmt.Printf("Cache updated for anime id=%d\n", animeID)
	return animeDetailsInfo{RelatedIDs: ids, MediaType: details.MediaType}, nil
}

func loadDetailsCache() (map[int]animeDetailsCacheItem, error) {
	b, err := os.ReadFile(detailsCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Cache file %s not found, a new cache will be created\n", detailsCachePath)
			return map[int]animeDetailsCacheItem{}, nil
		}
		return nil, err
	}
	fmt.Printf("Cache file %s loaded\n", detailsCachePath)
	var cache map[int]animeDetailsCacheItem
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, err
	}
	if cache == nil {
		cache = map[int]animeDetailsCacheItem{}
	}
	return cache, nil
}

func saveDetailsCache(cache map[int]animeDetailsCacheItem) error {
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeFileWithChangeLog(detailsCachePath, b, 0o644, "Cache file")
}

func renderGroupList(header string, groups []groupedView) string {
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")
	if len(groups) == 0 {
		sb.WriteString("No entries.\n")
		return sb.String()
	}
	for i, g := range groups {
		sb.WriteString(fmt.Sprintf("%d. %s | merged: %d | score: %.2f | episodes: %d\n",
			i+1, g.DisplayTitle, g.MergedTitles, g.AvgScore, g.WatchedEpisodesSum))
	}
	return sb.String()
}

func writeFileWithChangeLog(path string, newContent []byte, perm os.FileMode, label string) error {
	oldContent, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("%s %s does not exist, creating new file\n", label, path)
			return os.WriteFile(path, newContent, perm)
		}
		return err
	}

	if string(oldContent) == string(newContent) {
		fmt.Printf("%s %s exists, no changes (0)\n", label, path)
		return os.WriteFile(path, newContent, perm)
	}

	added, removed := countLineChanges(string(oldContent), string(newContent))
	fmt.Printf("%s %s exists, overwriting with changes: +%d / -%d\n", label, path, added, removed)
	return os.WriteFile(path, newContent, perm)
}

func countLineChanges(oldText, newText string) (added int, removed int) {
	oldLines := normalizeLines(oldText)
	newLines := normalizeLines(newText)

	oldCount := make(map[string]int)
	newCount := make(map[string]int)
	for _, line := range oldLines {
		oldCount[line]++
	}
	for _, line := range newLines {
		newCount[line]++
	}

	for line, count := range newCount {
		if count > oldCount[line] {
			added += count - oldCount[line]
		}
	}
	for line, count := range oldCount {
		if count > newCount[line] {
			removed += count - newCount[line]
		}
	}
	return added, removed
}

func normalizeLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func appFilePath(name string) string {
	baseDir := strings.TrimSpace(os.Getenv("MAL_DATA_DIR"))
	if baseDir == "" {
		_, sourceFile, _, ok := runtime.Caller(0)
		if ok {
			baseDir = filepath.Dir(sourceFile)
		}
	}
	if baseDir == "" {
		wd, err := os.Getwd()
		if err == nil {
			baseDir = wd
		}
	}
	if baseDir == "" {
		return name
	}
	return filepath.Join(baseDir, name)
}
