package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/buildexecutor"
	"ideploy/internal/gitauth"
	"ideploy/internal/sharedserver"
)

type worker struct {
	id, token, base, root string
	client                *http.Client
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--probe" || os.Args[1] == "--probe-compose") {
		if err := buildexecutor.CheckReady(context.Background(), os.Args[1] == "--probe-compose"); err != nil {
			log.Fatal(err)
		}
		return
	}
	tokenFile, root := os.Getenv("IDEPLOY_WORKER_TOKEN_FILE"), os.Getenv("IDEPLOY_WORK_ROOT")
	if tokenFile == "" || root == "" {
		log.Fatal("worker token file and work root are required")
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		log.Fatal("cannot read worker token")
	}
	base := os.Getenv("IDEPLOY_SERVER_URL")
	if base == "" {
		base = "http://127.0.0.1:8765"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		log.Fatal("worker server URL must be local HTTP")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		log.Fatal(err)
	}
	w := worker{id: hex.EncodeToString(idBytes), token: strings.TrimSpace(string(data)), base: strings.TrimRight(base, "/"), root: root, client: &http.Client{Timeout: 25 * time.Second}}
	if len(w.token) < 32 {
		log.Fatal("worker token is too short")
	}
	for {
		if !buildexecutor.Probe(context.Background()).BuildxReady {
			time.Sleep(15 * time.Second)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var job sharedserver.RemoteReleaseJob
		status, err := w.call(ctx, "POST", "/v3/worker/releases/claim", map[string]string{"workerId": w.id}, &job, nil)
		cancel()
		if err != nil {
			log.Printf("claim unavailable: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		if status == http.StatusNoContent {
			time.Sleep(5 * time.Second)
			continue
		}
		w.execute(job)
	}
}

func (w *worker) execute(job sharedserver.RemoteReleaseJob) {
	ctx, cancel := context.WithCancel(context.Background())
	terminal, stage := "failed", "finished"
	var image, manifest string
	buildStarted := false
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.patch(ctx, job, "running", "", ""); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		interrupted := ctx.Err() != nil
		cancel()
		<-done
		reportCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if terminal == "artifact_ready" {
			for attempt := 0; attempt < 2; attempt++ {
				if err := w.patchArtifact(reportCtx, job, image, manifest); err == nil {
					return
				}
			}
			terminal, stage = "needs_reconcile", "checking"
		} else if interrupted && buildStarted {
			terminal, stage = "needs_reconcile", "checking"
		}
		if err := w.patch(reportCtx, job, terminal, stage, ""); err != nil {
			// A simultaneous cancellation can reject the original result.
			if buildStarted {
				err = w.patch(reportCtx, job, "needs_reconcile", "checking", "")
			} else {
				err = w.patch(reportCtx, job, "canceled", "finished", "")
			}
			if err != nil {
				log.Printf("release outcome could not be confirmed: %s", terminal)
			}
		}
	}()
	if err := w.patch(ctx, job, "running", "checking_out", ""); err != nil {
		return
	}
	headers := map[string]string{"X-iDeploy-Worker-ID": w.id, "X-iDeploy-Lease-Generation": fmtInt(job.LeaseGeneration)}
	var gitSecret struct {
		Credential string `json:"credential"`
	}
	status, gitErr := w.call(ctx, "GET", "/v3/worker/releases/"+job.ID+"/git-secret", nil, &gitSecret, headers)
	var gitEnvironment []string
	if gitErr == nil && gitSecret.Credential != "" {
		credential, decodeErr := gitauth.Decode(gitSecret.Credential)
		gitSecret.Credential = ""
		if decodeErr != nil {
			return
		}
		var cleanup func()
		gitEnvironment, cleanup, gitErr = gitauth.Environment(credential)
		if gitErr != nil {
			return
		}
		defer cleanup()
	} else if status != http.StatusNotFound {
		if ctx.Err() == nil {
			log.Print("Git credential unavailable")
		}
		return
	}
	checkout, err := buildexecutor.Prepare(ctx, buildexecutor.CheckoutInput{RepositoryURL: job.RepositoryURL, GitEnvironment: gitEnvironment, DeliveryCommit: job.DeliveryCommit, DeliveryDigest: job.DeliveryDigest, DeploymentID: job.DeploymentID, ReleaseID: job.ReleaseID, WorkRoot: w.root})
	if err != nil {
		log.Printf("checkout failed: %v", err)
		return
	}
	defer func() {
		if err := os.RemoveAll(checkout.Root); err != nil {
			log.Printf("release worktree cleanup failed: %v", err)
		}
	}()
	if err := w.patch(ctx, job, "running", "building", ""); err != nil {
		return
	}
	var secret struct {
		Password string `json:"password"`
	}
	if _, err := w.call(ctx, "GET", "/v3/worker/releases/"+job.ID+"/registry-secret", nil, &secret, headers); err != nil || secret.Password == "" {
		log.Print("registry credential unavailable")
		return
	}
	var buildSecrets struct {
		Secrets map[string]string `json:"secrets"`
	}
	if _, err := w.call(ctx, "GET", "/v3/worker/releases/"+job.ID+"/build-secrets", nil, &buildSecrets, headers); err != nil {
		log.Print("build credentials unavailable")
		return
	}
	auth := buildexecutor.RegistryAuth{Host: job.RegistryHost, Username: job.RegistryUser, Password: secret.Password}
	if err := w.patch(ctx, job, "running", "pushing", ""); err != nil {
		return
	}
	buildStarted = true
	if checkout.Document.WorkloadKind == "compose" {
		manifest, err = buildexecutor.BuildCompose(ctx, buildexecutor.ComposeBuildInput{Checkout: checkout, Version: job.Version, Registry: auth, Secrets: buildSecrets.Secrets})
		image = "compose:" + job.DeploymentID + ":" + job.Version
	} else {
		image, err = buildexecutor.BuildContainer(ctx, buildexecutor.ContainerBuildInput{Checkout: checkout, Version: job.Version, Registry: auth, Secrets: buildSecrets.Secrets})
	}
	secret.Password = ""
	if err != nil {
		log.Printf("build failed: %v", err)
		return
	}
	terminal = "artifact_ready"
}

func fmtInt(value int64) string { return strconv.FormatInt(value, 10) }

func (w *worker) patch(ctx context.Context, job sharedserver.RemoteReleaseJob, status, stage, image string) error {
	_, err := w.call(ctx, "PATCH", "/v3/worker/releases/"+job.ID, map[string]any{"workerId": w.id, "generation": job.LeaseGeneration, "status": status, "stage": stage, "imageRef": image}, nil, nil)
	return err
}

func (w *worker) patchArtifact(ctx context.Context, job sharedserver.RemoteReleaseJob, image, manifest string) error {
	_, err := w.call(ctx, "PATCH", "/v3/worker/releases/"+job.ID, map[string]any{"workerId": w.id, "generation": job.LeaseGeneration, "status": "artifact_ready", "stage": "artifact_ready", "imageRef": image, "manifest": manifest}, nil, nil)
	return err
}

func (w *worker) call(ctx context.Context, method, path string, input, output any, headers map[string]string) (int, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.base+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, errors.New("service rejected worker request")
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 10*1024*1024)).Decode(output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
