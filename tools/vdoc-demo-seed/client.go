package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func send[T any](ctx context.Context, client apiClient, req request) (T, error) {
	var empty T
	var body io.Reader = http.NoBody
	if req.Body != nil {
		encoded, err := json.Marshal(req.Body)
		if err != nil {
			return empty, fmt.Errorf("marshal %s %s: %w", req.Method, req.Path, err)
		}
		body = bytes.NewReader(encoded)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, client.baseURL+req.Path, body)
	if err != nil {
		return empty, fmt.Errorf("build %s %s: %w", req.Method, req.Path, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Token != "" {
		httpReq.Header.Set("Authorization", req.Token)
	}
	resp, err := client.http.Do(httpReq)
	if err != nil {
		return empty, fmt.Errorf("send %s %s: %w", req.Method, req.Path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, fmt.Errorf("read %s %s: %w", req.Method, req.Path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("%s %s HTTP %d", req.Method, req.Path, resp.StatusCode)
	}
	value, err := decodeEnvelope[T](data)
	if err != nil {
		return empty, fmt.Errorf("decode %s %s: %w", req.Method, req.Path, err)
	}
	return value, nil
}

func decodeEnvelope[T any](body []byte) (T, error) {
	var empty T
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return empty, fmt.Errorf("decode envelope: %w", err)
	}
	if env.Code != 200 || env.Status != "OK" {
		return empty, fmt.Errorf("api envelope %d %s: %s", env.Code, env.Status, env.Message)
	}
	var value T
	if err := json.Unmarshal(env.Detail, &value); err != nil {
		return empty, fmt.Errorf("decode detail: %w", err)
	}
	return value, nil
}
