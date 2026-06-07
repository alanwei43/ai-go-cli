package downloader

import (
	"io"
	"net/http"
	"os"
)

// Downloader 文件下载器
type Downloader struct {
	httpClient *http.Client
}

// New 创建新的下载器
func New() *Downloader {
	return &Downloader{
		httpClient: &http.Client{},
	}
}

// Download 下载文件到当前目录
func (d *Downloader) Download(url, filename string) error {
	resp, err := d.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}
