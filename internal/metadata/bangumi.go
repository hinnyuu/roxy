// Package metadata Bangumi 适配器：在线 API 客户端（匿名 + 规范 UA，D-023）、
// Archive dump 导入器（D-022）与本地索引检索（FTS 前缀 + LIKE 兜底）。
// 设计见 docs/ARCHITECTURE.md §8。
package metadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
)

// ErrNotFound 条目不存在（404）。
var ErrNotFound = errors.New("metadata: not found")

// Subject 在线条目（v0/subjects/{id} 与搜索结果共用的宽松解码）。
type Subject struct {
	ID       int64   `json:"id"`
	Type     int     `json:"type"`
	Name     string  `json:"name"`
	NameCn   string  `json:"name_cn"`
	Platform string  `json:"platform"`
	Date     string  `json:"date"`
	Summary  string  `json:"summary"`
	NSFW     bool    `json:"nsfw"`
	Score    float64 `json:"score"`
	Rank     int     `json:"rank"`
}

// Episode 在线章节（v0/episodes 分页项）。
type Episode struct {
	ID        int64   `json:"id"`
	SubjectID int64   `json:"subject_id"`
	Name      string  `json:"name"`
	NameCn    string  `json:"name_cn"`
	Sort      float64 `json:"sort"`
	Type      int     `json:"type"`
	Airdate   string  `json:"airdate"`
}

// Client Bangumi v0 API 客户端：匿名可用（D-023），强制规范 UA，
// 可选 Bearer token（ROXY_BGM_TOKEN），匿名限流从严故内置最小请求间隔。
type Client struct {
	httpc   *http.Client
	baseURL string
	agent   string
	token   string

	mu       sync.Mutex
	lastCall time.Time
	minGap   time.Duration
}

func NewClient(baseURL, userAgent, token string) *Client {
	return &Client{
		httpc:   &http.Client{Timeout: 15 * time.Second},
		baseURL: strings.TrimSuffix(baseURL, "/"),
		agent:   userAgent,
		token:   token,
		minGap:  time.Second,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	c.mu.Lock()
	if wait := c.minGap - time.Since(c.lastCall); wait > 0 {
		unlock := make(chan struct{})
		go func() { time.AfterFunc(wait, func() { close(unlock) }) }()
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-unlock:
		}
		c.mu.Lock()
	}
	c.lastCall = time.Now()
	c.mu.Unlock()

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("bangumi request: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("bangumi: 限流（429）")
	case resp.StatusCode >= 300:
		return fmt.Errorf("bangumi: HTTP %d %s", resp.StatusCode, path)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("bangumi decode %s: %w", path, err)
		}
	}
	return nil
}

type searchResponse struct {
	Total int       `json:"total"`
	Data  []Subject `json:"data"`
}

// SearchSubjects 关键词搜索动画条目（POST /v0/search/subjects，实验性 API，
// 仅作候选生成——最终采信必须经 GetSubject 核验，D-019/§7.4 铁律）。
// 结果进 search_cache（TTL 7 天），同一问题不重复付费。
func (c *Client) SearchSubjects(ctx context.Context, cache *Cache, keyword string) ([]Subject, error) {
	key := "search:" + keyword
	if cache != nil {
		if hit, ok := cache.Get(ctx, "bangumi", key); ok {
			var out []Subject
			if json.Unmarshal([]byte(hit), &out) == nil {
				return out, nil
			}
		}
	}
	q := url.Values{"type": {"2"}, "response_group": {"small"}}
	var body = map[string]any{"keyword": keyword, "sort": "match"}
	var resp searchResponse
	if err := c.do(ctx, http.MethodPost, "/v0/search/subjects?"+q.Encode(), body, &resp); err != nil {
		return nil, err
	}
	if cache != nil {
		if b, err := json.Marshal(resp.Data); err == nil {
			cache.Put(ctx, "bangumi", key, string(b), 7*24*time.Hour)
		}
	}
	return resp.Data, nil
}

// GetSubject 条目详情（最终核验用）。
func (c *Client) GetSubject(ctx context.Context, cache *Cache, id int64) (*Subject, error) {
	key := fmt.Sprintf("subject:%d", id)
	if cache != nil {
		if hit, ok := cache.Get(ctx, "bangumi", key); ok {
			var s Subject
			if json.Unmarshal([]byte(hit), &s) == nil {
				return &s, nil
			}
		}
	}
	var s Subject
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/v0/subjects/%d", id), nil, &s); err != nil {
		return nil, err
	}
	if cache != nil {
		if b, err := json.Marshal(s); err == nil {
			cache.Put(ctx, "bangumi", key, string(b), 24*time.Hour)
		}
	}
	return &s, nil
}

type episodePage struct {
	Data  []Episode `json:"data"`
	Total int       `json:"total"`
}

// ListEpisodes 章节列表（分页 ≤200/页，全部拉取）。
func (c *Client) ListEpisodes(ctx context.Context, subjectID int64) ([]Episode, error) {
	var all []Episode
	for offset := 0; ; offset += 200 {
		q := url.Values{
			"subject_id": {fmt.Sprint(subjectID)},
			"limit":      {"200"},
			"offset":     {fmt.Sprint(offset)},
		}
		var page episodePage
		if err := c.do(ctx, http.MethodGet, "/v0/episodes?"+q.Encode(), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if len(page.Data) == 0 || len(all) >= page.Total {
			break
		}
	}
	return all, nil
}

// Cache search_cache 表读写（§7.4：搜索结果缓存入库）。
type Cache struct{ db *sql.DB }

func NewCache(db *sql.DB) *Cache { return &Cache{db: db} }

func (c *Cache) Get(ctx context.Context, source, query string) (string, bool) {
	var result string
	err := c.db.QueryRowContext(ctx,
		`SELECT result FROM search_cache WHERE source = ? AND query = ? AND expires_at > ?`,
		source, query, domain.Now()).Scan(&result)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("search_cache get", "err", err)
		}
		return "", false
	}
	return result, true
}

func (c *Cache) Put(ctx context.Context, source, query, result string, ttl time.Duration) {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO search_cache (query, source, result, expires_at) VALUES (?, ?, ?, ?)`,
		query, source, result, time.Now().UTC().Add(ttl).Format(time.RFC3339))
	if err != nil {
		slog.Warn("search_cache put", "err", err)
	}
}
