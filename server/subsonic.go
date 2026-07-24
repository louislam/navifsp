package main

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/patrickmn/go-cache"
)

type subsonicClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
	cache    *cache.Cache
}

func newSubsonicClient(baseURL, username, password string) *subsonicClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	return &subsonicClient{
		baseURL:  baseURL,
		username: username,
		password: password,
		client:   &http.Client{Transport: transport},
		cache:    cache.New(10*time.Second, 30*time.Second),
	}
}

func (c *subsonicClient) buildURL(endpoint string, params map[string]string) string {
	salt := generateSalt()
	token := md5Password(c.password, salt)
	q := url.Values{}
	q.Set("u", c.username)
	q.Set("t", token)
	q.Set("s", salt)

	// The Subsonic protocol version implemented by the client (https://www.subsonic.org/pages/api.jsp#versions)
	q.Set("v", "1.16.1")

	// A unique string identifying the client application.
	q.Set("c", "NaviFSP")
	q.Set("f", "json")
	for k, v := range params {
		q.Set(k, v)
	}
	return c.baseURL + "/rest/" + endpoint + "?" + q.Encode()
}

func generateSalt() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func md5Password(password, salt string) string {
	h := md5.Sum([]byte(password + salt))
	return fmt.Sprintf("%x", h)
}

func (c *subsonicClient) doRequest(ctx context.Context, url string) ([]byte, error) {
	log.Printf("Request: %s", endpointName(url))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return body, nil
}

func (c *subsonicClient) doStream(ctx context.Context, url string) (io.ReadCloser, error) {
	log.Printf("Request Stream: %s", endpointName(url))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating stream request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream request failed: %w", err)
	}
	return resp.Body, nil
}

func endpointName(urlStr string) string {
	u, _ := url.Parse(urlStr)
	q := u.Query()
	q.Del("u")
	q.Del("t")
	q.Del("s")
	q.Del("v")
	q.Del("f")
	q.Del("c")
	return fmt.Sprintf("%s?%s", u.Path, q.Encode())
}

type artist struct {
	ID   string
	Name string
}

func (c *subsonicClient) getArtists(ctx context.Context) ([]artist, error) {
	const cacheKey = "artists"

	if cached, found := c.cache.Get(cacheKey); found {
		debugLog("getArtists: cache hit")
		return cached.([]artist), nil
	}

	debugLog("getArtists: cache miss, fetching from server")
	body, err := c.doRequest(ctx, c.buildURL("getArtists", nil))
	if err != nil {
		debugLog("getArtists: request error: %v", err)
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		debugLog("getArtists: unmarshal error: %v", err)
		return nil, fmt.Errorf("decoding artists: %w", err)
	}
	if resp.SubsonicResponse.Status == "failed" {
		debugLog("getArtists: subsonic API returned failed status")
		return nil, fmt.Errorf("subsonic API error")
	}
	if resp.SubsonicResponse.Artists == nil {
		debugLog("getArtists: no artists found")
		return nil, nil
	}
	var result []artist
	for _, idx := range resp.SubsonicResponse.Artists.Index {
		for _, a := range idx.Artist {
			result = append(result, artist{ID: a.ID, Name: a.Name})
		}
	}
	debugLog("getArtists: cached %d artists for 10s", len(result))
	c.cache.Set(cacheKey, result, cache.DefaultExpiration)
	return result, nil
}

type album struct {
	ID      string
	Name    string
	Created string
}

func (c *subsonicClient) getAlbums(ctx context.Context, artistID string) ([]album, error) {
	cacheKey := "albums:" + artistID

	if cached, found := c.cache.Get(cacheKey); found {
		debugLog("getAlbums(%s): cache hit", artistID)
		return cached.([]album), nil
	}

	debugLog("getAlbums(%s): cache miss, fetching from server", artistID)
	body, err := c.doRequest(ctx, c.buildURL("getArtist", map[string]string{"id": artistID}))
	if err != nil {
		debugLog("getAlbums(%s): request error: %v", artistID, err)
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		debugLog("getAlbums(%s): unmarshal error: %v", artistID, err)
		return nil, fmt.Errorf("decoding albums: %w", err)
	}
	if resp.SubsonicResponse.Status == "failed" {
		debugLog("getAlbums(%s): subsonic API returned failed status", artistID)
		return nil, fmt.Errorf("subsonic API error")
	}
	if resp.SubsonicResponse.Artist == nil {
		debugLog("getAlbums(%s): no albums found", artistID)
		return nil, nil
	}
	var result []album
	for _, a := range resp.SubsonicResponse.Artist.Album {
		if a.ArtistId != "" && a.ArtistId != artistID {
			debugLog("getAlbums(%s): skipping album %s (artistId=%s)", artistID, a.ID, a.ArtistId)
			continue
		}
		result = append(result, album{ID: a.ID, Name: a.Name, Created: a.Created})
	}
	debugLog("getAlbums(%s): cached %d albums for 10s", artistID, len(result))
	c.cache.Set(cacheKey, result, cache.DefaultExpiration)
	return result, nil
}

type song struct {
	ID          string
	Path        string
	Size        int64
	ContentType string
	Created     string
}

func (c *subsonicClient) getSongs(ctx context.Context, albumID string) ([]song, error) {
	cacheKey := "songs:" + albumID

	if cached, found := c.cache.Get(cacheKey); found {
		debugLog("getSongs(%s): cache hit", albumID)
		return cached.([]song), nil
	}

	debugLog("getSongs(%s): cache miss, fetching from server", albumID)
	body, err := c.doRequest(ctx, c.buildURL("getAlbum", map[string]string{"id": albumID}))
	if err != nil {
		debugLog("getSongs(%s): request error: %v", albumID, err)
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		debugLog("getSongs(%s): unmarshal error: %v", albumID, err)
		return nil, fmt.Errorf("decoding songs: %w", err)
	}
	if resp.SubsonicResponse.Status == "failed" {
		debugLog("getSongs(%s): subsonic API returned failed status", albumID)
		return nil, fmt.Errorf("subsonic API error")
	}
	if resp.SubsonicResponse.Album == nil {
		debugLog("getSongs(%s): no songs found", albumID)
		return nil, nil
	}
	var result []song
	for _, s := range resp.SubsonicResponse.Album.Song {
		result = append(result, song{
			ID:          s.ID,
			Path:        s.Path,
			Size:        s.Size,
			ContentType: s.ContentType,
			Created:     s.Created,
		})
	}
	debugLog("getSongs(%s): cached %d songs for 10s", albumID, len(result))
	c.cache.Set(cacheKey, result, cache.DefaultExpiration)
	return result, nil
}

func (c *subsonicClient) getSong(ctx context.Context, songID string) (*song, error) {
	body, err := c.doRequest(ctx, c.buildURL("getSong", map[string]string{"id": songID}))
	if err != nil {
		return nil, err
	}
	var resp subsonicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding song: %w", err)
	}
	if resp.SubsonicResponse.Status == "failed" {
		return nil, fmt.Errorf("subsonic API error")
	}
	if resp.SubsonicResponse.Song == nil {
		return nil, fmt.Errorf("song not found: %s", songID)
	}
	s := resp.SubsonicResponse.Song
	return &song{ID: s.ID, Path: s.Path, Size: s.Size, ContentType: s.ContentType, Created: s.Created}, nil
}

func (c *subsonicClient) streamSong(ctx context.Context, songID string) (io.ReadCloser, error) {
	return c.doStream(ctx, c.buildURL("stream", map[string]string{"id": songID}))
}

func (c *subsonicClient) getCoverArt(ctx context.Context, id string) (io.ReadCloser, error) {
	return c.doStream(ctx, c.buildURL("getCoverArt", map[string]string{"id": id}))
}

func (c *subsonicClient) getCoverArtSize(ctx context.Context, id string) (int64, error) {
	u := c.buildURL("getCoverArt", map[string]string{"id": id})
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return 0, fmt.Errorf("creating HEAD request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HEAD request failed: %w", err)
	}
	defer resp.Body.Close()
	return resp.ContentLength, nil
}

func (c *subsonicClient) streamSongRange(ctx context.Context, songID string, start, end int64) (io.ReadCloser, error) {
	debugLog("Navidrome stream %s bytes=%d-%d", songID, start, end)
	u := c.buildURL("stream", map[string]string{"id": songID})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating range request: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("range request failed: %w", err)
	}
	return resp.Body, nil
}
