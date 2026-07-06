package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// assetHTTPClient downloads release assets (bundle + checksum). It is separate
// from the relay httpClient on purpose: that client caps the ENTIRE response at
// 15 seconds, which aborts any bundle download on a slow connection. Release
// assets may legitimately take minutes, so this client has no total timeout;
// stalls are still bounded per phase (dial, TLS handshake, response headers).
var assetHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

const assetDownloadAttempts = 3

// assetRetryDelay is a variable so tests can run the retry loop without sleeps.
var assetRetryDelay = time.Second

// assetBodyIdleTimeout bounds stalled body reads without imposing a total
// download timeout. Slow downloads can continue as long as bytes keep arriving.
var assetBodyIdleTimeout = 30 * time.Second

func downloadAssetFile(url, dest string) error {
	return downloadAssetFileWithVerify(url, dest, "")
}

func downloadAssetFileVerified(url, dest, checksumManifest string) error {
	return downloadAssetFileWithVerify(url, dest, checksumManifest)
}

func downloadAssetFileWithVerify(url, dest, checksumManifest string) error {
	var attemptErrors []string
	for attempt := 1; attempt <= assetDownloadAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(assetRetryDelay * time.Duration(attempt-1))
		}
		retryable, err := downloadAssetFileOnce(url, dest)
		if err == nil && checksumManifest != "" {
			if verifyErr := verifyFileChecksum(dest, checksumManifest); verifyErr != nil {
				// A 200 response with wrong bytes (proxy substitution, captive
				// portal, truncated cache) is transient from the next transport's
				// point of view: discard and re-download instead of giving up.
				_ = os.Remove(dest)
				retryable, err = true, verifyErr
			}
		}
		if err == nil {
			return nil
		}
		attemptErrors = append(attemptErrors, fmt.Sprintf("attempt %d: %s", attempt, err))
		if !retryable {
			break
		}
	}
	_ = os.Remove(dest)
	return fmt.Errorf("could not download release asset %s (%s)", url, strings.Join(attemptErrors, "; "))
}

func downloadAssetFileOnce(url, dest string) (retryable bool, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "ha-nova-installer")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := assetHTTPClient.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return retryable, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return false, err
	}
	written, copyErr := copyWithIdleTimeout(out, resp.Body, assetBodyIdleTimeout)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return true, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return false, closeErr
	}
	if written == 0 {
		_ = os.Remove(dest)
		return true, fmt.Errorf("download produced no data")
	}
	return false, nil
}

func copyWithIdleTimeout(dst io.Writer, src io.ReadCloser, timeout time.Duration) (int64, error) {
	if timeout <= 0 {
		return io.Copy(dst, src)
	}

	buf := make([]byte, 32*1024)
	var written int64
	for {
		type readResult struct {
			n   int
			err error
		}
		resultCh := make(chan readResult, 1)
		go func() {
			n, err := src.Read(buf)
			resultCh <- readResult{n: n, err: err}
		}()

		select {
		case result := <-resultCh:
			if result.n > 0 {
				nw, err := dst.Write(buf[:result.n])
				written += int64(nw)
				if err != nil {
					return written, err
				}
				if nw != result.n {
					return written, io.ErrShortWrite
				}
			}
			if result.err != nil {
				if result.err == io.EOF {
					return written, nil
				}
				return written, result.err
			}
		case <-time.After(timeout):
			_ = src.Close()
			return written, fmt.Errorf("download body idle timeout after %s", timeout)
		}
	}
}
