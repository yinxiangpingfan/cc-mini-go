package client

import (
	"net/http"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

type ChatCompletionClient struct {
	baseUrl    string
	apiKey     string
	httpClient *http.Client
}

func Init(baseUrl string, apiKey string) (*ChatCompletionClient, error) {
	//确认baseurl合法
	if strings.HasPrefix(baseUrl, "http://") || strings.HasPrefix(baseUrl, "https://") {
	} else {
		return nil, errors.ErrInvalidBaseUrl
	}
	return &ChatCompletionClient{
		baseUrl:    baseUrl,
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}
