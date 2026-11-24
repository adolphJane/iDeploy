package sharedworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/server"
)

const RuntimePolicyAPIVersion = 3

type Client struct {
	http  *http.Client
	token string
}

type HTTPStatusError struct {
	Code    int
	Message string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("共享服务返回 %d：%s", e.Code, e.Message)
}

func NewClient(connection server.Connection, token string) (*Client, error) {
	if token == "" {
		return nil, errors.New("此设备尚未取得共享服务访问令牌")
	}
	return &Client{http: server.SharedHTTPClient(connection), token: token}, nil
}

func NewPairingClient(connection server.Connection) *Client {
	return &Client{http: server.SharedHTTPClient(connection)}
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://shared.local"+path, body)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resp, err = c.http.Do(req)
		if err == nil || method != http.MethodGet || attempt >= 2 ||
			(!errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF)) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
		}
	}
	if err != nil {
		return fmt.Errorf("共享服务 SSH 通道不可用: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &HTTPStatusError{Code: resp.StatusCode, Message: strings.TrimSpace(string(message))}
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(output)
}

type WorkspaceInfo struct {
	APIVersion int    `json:"apiVersion"`
	Name       string `json:"name"`
	Revision   int64  `json:"revision"`
}

type RemoteReleaseJob struct {
	ID             string `json:"id"`
	RequestID      string `json:"requestId"`
	DeploymentID   string `json:"deploymentId"`
	ReleaseID      string `json:"releaseId"`
	DeliveryCommit string `json:"deliveryCommit"`
	DeliveryDigest string `json:"deliveryDigest"`
	RepositoryURL  string `json:"repositoryUrl"`
	Version        string `json:"version"`
	RegistryID     string `json:"registryId"`
	RegistryHost   string `json:"registryHost"`
	RegistryUser   string `json:"registryUser"`
	ImageRef       string `json:"imageRef"`
	Manifest       string `json:"manifest"`
	Status         string `json:"status"`
	Stage          string `json:"stage"`
}

func (c *Client) CreateRemoteRelease(ctx context.Context, input RemoteReleaseJob) (RemoteReleaseJob, error) {
	var result RemoteReleaseJob
	err := c.request(ctx, http.MethodPost, "/v3/releases", input, &result)
	return result, err
}

func (c *Client) RemoteRelease(ctx context.Context, id string) (RemoteReleaseJob, error) {
	var result RemoteReleaseJob
	err := c.request(ctx, http.MethodGet, "/v3/releases/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (c *Client) RemoteReleaseByRequest(ctx context.Context, requestID string) (RemoteReleaseJob, error) {
	var result RemoteReleaseJob
	err := c.request(ctx, http.MethodGet, "/v3/releases/by-request/"+url.PathEscape(requestID), nil, &result)
	return result, err
}

func (c *Client) CancelRemoteRelease(ctx context.Context, requestID string) error {
	return c.request(ctx, http.MethodDelete, "/v3/releases/by-request/"+url.PathEscape(requestID), nil, nil)
}

type RemoteResource struct {
	ID       string          `json:"id"`
	Revision int64           `json:"revision"`
	Deleted  bool            `json:"deleted"`
	Data     json.RawMessage `json:"data"`
}

func (c *Client) Resources(ctx context.Context, kind string) ([]RemoteResource, error) {
	var items []RemoteResource
	err := c.request(ctx, http.MethodGet, "/v1/resources/"+url.PathEscape(kind), nil, &items)
	return items, err
}

func (c *Client) Workspace(ctx context.Context) (WorkspaceInfo, error) {
	var value WorkspaceInfo
	err := c.request(ctx, http.MethodGet, "/v1/workspace", nil, &value)
	if err == nil && value.APIVersion < 2 {
		err = errors.New("共享服务版本过旧，请先升级服务端")
	}
	return value, err
}

func (c *Client) AcquireLease(ctx context.Context, resourceID, requestID string) error {
	return c.request(ctx, http.MethodPost, "/v2/leases/"+url.PathEscape(resourceID), map[string]any{"requestId": requestID, "runtimePolicyProtocol": 1}, nil)
}

func (c *Client) RuntimePolicy(ctx context.Context, deploymentID string) (deployment.RuntimePolicy, error) {
	var value deployment.RuntimePolicy
	err := c.request(ctx, http.MethodGet, "/v3/runtime-policies/"+url.PathEscape(deploymentID), nil, &value)
	var status *HTTPStatusError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		return deployment.RuntimePolicy{Mode: deployment.RuntimeObserve}, nil
	}
	return value, err
}

func (c *Client) ReleaseLease(ctx context.Context, resourceID, requestID string) error {
	return c.request(ctx, http.MethodDelete, "/v2/leases/"+url.PathEscape(resourceID)+"?requestId="+url.QueryEscape(requestID), nil, nil)
}

type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Revoked bool   `json:"revoked"`
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var items []Device
	err := c.request(ctx, http.MethodGet, "/v1/devices", nil, &items)
	return items, err
}

func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	return c.request(ctx, http.MethodDelete, "/v1/devices/"+url.PathEscape(id), nil, nil)
}

type ImportReceipt struct {
	BatchID  string `json:"batchId"`
	Digest   string `json:"digest"`
	Count    int    `json:"count"`
	Replayed bool   `json:"replayed"`
}

func (c *Client) Import(ctx context.Context, batchID string, records []Record) (ImportReceipt, error) {
	var receipt ImportReceipt
	err := c.request(ctx, http.MethodPost, "/v2/imports", map[string]any{"batchId": batchID, "records": records}, &receipt)
	return receipt, err
}

func (c *Client) ImportReceipt(ctx context.Context, batchID string) (ImportReceipt, error) {
	var receipt ImportReceipt
	err := c.request(ctx, http.MethodGet, "/v2/imports/"+url.PathEscape(batchID), nil, &receipt)
	return receipt, err
}

type ChangePage struct {
	Items      []Change `json:"items"`
	NextCursor int64    `json:"nextCursor"`
	HasMore    bool     `json:"hasMore"`
}

type ResourceMutation struct {
	Kind             string          `json:"kind"`
	ID               string          `json:"id"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Deleted          bool            `json:"deleted"`
	Data             json.RawMessage `json:"data,omitempty"`
}

type SecretMutation struct {
	ID               string   `json:"id"`
	ExpectedRevision int64    `json:"expectedRevision"`
	Envelope         Envelope `json:"envelope"`
}

func (c *Client) WriteBatch(ctx context.Context, requestID string, resources []ResourceMutation, secrets []SecretMutation) error {
	return c.request(ctx, http.MethodPost, "/v2/batches", map[string]any{"requestId": requestID, "resources": resources, "secrets": secrets}, nil)
}

func (c *Client) Changes(ctx context.Context, after int64) (ChangePage, error) {
	var page ChangePage
	err := c.request(ctx, http.MethodGet, "/v2/changes?after="+strconv.FormatInt(after, 10), nil, &page)
	return page, err
}

func (c *Client) GetVault(ctx context.Context) (VaultConfig, error) {
	var config VaultConfig
	err := c.request(ctx, http.MethodGet, "/v2/vault", nil, &config)
	return config, err
}

func (c *Client) CreateVault(ctx context.Context, config VaultConfig) error {
	return c.request(ctx, http.MethodPost, "/v2/vault", config, nil)
}

func (c *Client) PutSecret(ctx context.Context, id string, envelope Envelope, expectedRevision int64) error {
	return c.request(ctx, http.MethodPut, "/v2/secrets/"+url.PathEscape(id), map[string]any{"expectedRevision": expectedRevision, "envelope": envelope}, nil)
}

type RemoteSecret struct {
	ID       string   `json:"id"`
	Revision int64    `json:"revision"`
	Envelope Envelope `json:"envelope"`
}

func (c *Client) Secrets(ctx context.Context) ([]RemoteSecret, error) {
	var values []RemoteSecret
	err := c.request(ctx, http.MethodGet, "/v2/secrets", nil, &values)
	return values, err
}

func (c *Client) NewPairing(ctx context.Context) (string, string, error) {
	var result struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expiresAt"`
	}
	err := c.request(ctx, http.MethodPost, "/v2/pairings", map[string]string{}, &result)
	return result.Code, result.ExpiresAt, err
}

func (c *Client) RedeemPairing(ctx context.Context, code, name string) (string, string, error) {
	var result struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	err := c.request(ctx, http.MethodPost, "/v2/pairings/redeem", map[string]string{"code": code, "name": name}, &result)
	return result.ID, result.Token, err
}
