package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Release 表示 GitHub Release 信息
type Release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

// Asset 表示 Release 产物
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Client 是 GitHub API 客户端
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient 创建新的 GitHub API 客户端
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "https://api.github.com",
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

// ListReleases 获取仓库的 release 列表
func (c *Client) ListReleases(repository string, limit int) ([]Release, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.baseURL, repository, limit)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 请求失败: %s", resp.Status)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	return releases, nil
}

// GetReleaseByVersion 获取指定版本的 release
func (c *Client) GetReleaseByVersion(repository, version string) (*Release, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.baseURL, repository, version)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("版本 %s 不存在", version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 请求失败: %s", resp.Status)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "gh-release-downloader")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
}
