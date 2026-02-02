package traefikstats

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type streamClient struct {
	endpoint string
	client   *http.Client
}

func newStreamClient(sidecarURL string) (*streamClient, error) {
	if strings.TrimSpace(sidecarURL) == "" {
		return nil, fmt.Errorf("sidecarURL is empty")
	}
	endpoint := strings.TrimRight(sidecarURL, "/") + "/ingest"
	return &streamClient{
		endpoint: endpoint,
		client:   &http.Client{},
	}, nil
}

func (c *streamClient) StreamEvents(ctx context.Context, events []event) error {
	reader, writer := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	writeErrCh := make(chan error, 1)
	go func() {
		buf := bufio.NewWriter(writer)
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		for _, evt := range events {
			if err := enc.Encode(evt); err != nil {
				_ = writer.CloseWithError(err)
				writeErrCh <- err
				return
			}
			if err := buf.Flush(); err != nil {
				_ = writer.CloseWithError(err)
				writeErrCh <- err
				return
			}
		}
		_ = writer.Close()
		writeErrCh <- nil
	}()

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := <-writeErrCh; err != nil {
		return err
	}
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
