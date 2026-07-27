package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

func cloudNoRedirectHTTPClient(source *http.Client, defaultTimeout time.Duration) *http.Client {
	var client http.Client
	if source != nil {
		client = *source
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultTimeout
	}
	client.Jar = nil
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func readCloudResponse(body io.Reader, maxBytes int64, op string) ([]byte, error) {
	limited := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		var cloudErr *CloudError
		if errors.As(err, &cloudErr) {
			return nil, err
		}
		return nil, newCloudError(CloudErrNetwork, op, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, newCloudError(CloudErrResponseTooLarge, op, nil)
	}
	return data, nil
}

func cloudRequestError(op string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return newCloudError(CloudErrTimeout, op, err)
	}
	return newCloudError(CloudErrNetwork, op, err)
}

func isHTTPRedirect(status int) bool {
	return status >= http.StatusMultipleChoices && status < http.StatusBadRequest
}
