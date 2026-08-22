//go:build e2e

// Package e2e drives the platform the way a client does: over HTTP, against
// the real server binary, a real Postgres and a real Redis, with the provider
// replaced by a fake whose payloads were captured from gitlab.com.
//
// Nothing here reaches into the code under test. The only in-process pieces are
// the fake provider and the assertions, which is what makes a failure here a
// statement about the product rather than about a mock.
//
//	make e2e
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

const (
	fakeGitLabToken = "glpat-e2e-fake-token"
	// postgresImage and redisImage match docker-compose.yml, so the suite
	// exercises the versions the project actually deploys against.
	postgresImage = "postgres:15-alpine"
	redisImage    = "redis:7-alpine"
)

// stack is the running system under test.
type stack struct {
	baseURL string
	fake    *fakegitlab.Server
	client  *http.Client
}

var sut *stack

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e harness:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run brings the stack up, runs the suite, and tears everything down even when
// a test panics — a leaked container would poison the next run.
func run(m *testing.M) (int, error) {
	var cleanups []func()
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		return 0, err
	}

	pgPort, err := freePort()
	if err != nil {
		return 0, err
	}
	redisPort, err := freePort()
	if err != nil {
		return 0, err
	}
	serverPort, err := freePort()
	if err != nil {
		return 0, err
	}

	suffix := strconv.Itoa(os.Getpid())
	pgName := "idp-e2e-pg-" + suffix
	redisName := "idp-e2e-redis-" + suffix

	stopPG, err := startContainer(pgName, postgresImage, pgPort, 5432,
		"-e", "POSTGRES_USER=postgres", "-e", "POSTGRES_PASSWORD=postgres", "-e", "POSTGRES_DB=idp_e2e")
	if err != nil {
		return 0, err
	}
	cleanups = append(cleanups, stopPG)

	stopRedis, err := startContainer(redisName, redisImage, redisPort, 6379)
	if err != nil {
		return 0, err
	}
	cleanups = append(cleanups, stopRedis)

	if err := waitFor(60*time.Second, func() error {
		return exec.Command("docker", "exec", pgName, "pg_isready", "-U", "postgres", "-q").Run()
	}); err != nil {
		return 0, fmt.Errorf("postgres never became ready: %w", err)
	}
	// pg_isready reports ready during the init phase too, before the
	// application database exists, so this retries rather than assuming.
	if err := waitFor(60*time.Second, func() error {
		out, err := exec.Command("docker", "exec", pgName, "psql", "-U", "postgres", "-d", "idp_e2e", "-q",
			"-c", `CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("create uuid-ossp extension: %w", err)
	}
	if err := waitFor(30*time.Second, func() error {
		return exec.Command("docker", "exec", redisName, "redis-cli", "ping").Run()
	}); err != nil {
		return 0, fmt.Errorf("redis never became ready: %w", err)
	}

	fake := fakegitlab.Default(fakeGitLabToken)
	fakeSrv := httptest.NewServer(fake.Handler())
	cleanups = append(cleanups, fakeSrv.Close)

	binary := filepath.Join(os.TempDir(), "idp-e2e-server-"+suffix)
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("build server: %v\n%s", err, out)
	}
	cleanups = append(cleanups, func() { _ = os.Remove(binary) })

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return 0, err
	}

	server := exec.Command(binary)
	// The server resolves `migrations/` relative to its working directory.
	server.Dir = repoRoot
	server.Env = []string{
		"PORT=" + strconv.Itoa(serverPort),
		"DB_HOST=127.0.0.1",
		"DB_PORT=" + strconv.Itoa(pgPort),
		"DB_USER=postgres",
		"DB_PASSWORD=postgres",
		"DB_NAME=idp_e2e",
		"DB_SSLMODE=disable",
		"REDIS_HOST=127.0.0.1",
		"REDIS_PORT=" + strconv.Itoa(redisPort),
		"JWT_SECRET=e2e-jwt-secret-e2e-jwt-secret-e2e",
		"ENCRYPTION_KEY=" + base64.StdEncoding.EncodeToString(key),
		// Point the GitLab client at the fake. This is the same setting a
		// self-hosted instance uses.
		"GITLAB_BASE_URL=" + fakeSrv.URL,
		// Webhook registration is skipped when the base URL looks unreachable
		// from the provider, and "localhost"/"127.0.0.1" are treated as exactly
		// that. The fake runs on this machine, so loopback IS reachable — the
		// IPv6 spelling is the one that says so without tripping the guard.
		"WEBHOOK_BASE_URL=http://[::1]:" + strconv.Itoa(serverPort),
		"LOG_LEVEL=error",
		// Keep .env from leaking a real provider token into the run.
		"GITHUB_TOKEN=",
		"GITLAB_TOKEN=",
		"ANTHROPIC_API_KEY=",
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	logFile, err := os.Create(filepath.Join(os.TempDir(), "idp-e2e-server-"+suffix+".log"))
	if err != nil {
		return 0, err
	}
	cleanups = append(cleanups, func() { _ = logFile.Close() })
	server.Stdout = logFile
	server.Stderr = logFile

	if err := server.Start(); err != nil {
		return 0, fmt.Errorf("start server: %w", err)
	}
	cleanups = append(cleanups, func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	if err := waitFor(60*time.Second, func() error {
		resp, err := http.Get(baseURL + "/api/v1/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health returned %d", resp.StatusCode)
		}
		return nil
	}); err != nil {
		logs, _ := os.ReadFile(logFile.Name())
		return 0, fmt.Errorf("server never became healthy: %w\n%s", err, tail(string(logs), 40))
	}

	sut = &stack{
		baseURL: baseURL,
		fake:    fake,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	fmt.Printf("e2e: server %s, fake gitlab %s, postgres :%d, redis :%d\n", baseURL, fakeSrv.URL, pgPort, redisPort)

	return m.Run(), nil
}

func startContainer(name, image string, hostPort, containerPort int, extra ...string) (func(), error) {
	_ = exec.Command("docker", "rm", "-f", name).Run()

	args := []string{"run", "-d", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", hostPort, containerPort)}
	args = append(args, extra...)
	args = append(args, image)

	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("start %s: %v\n%s", image, err, out)
	}
	return func() { _ = exec.Command("docker", "stop", name).Run() }, nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitFor(timeout time.Duration, check func() error) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if last = check(); last == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return last
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

type response struct {
	status int
	body   []byte
}

// decode unmarshals the body, failing the test with the raw payload on error —
// a mismatched shape is far easier to diagnose from the bytes than from a
// zero-valued struct.
func (r response) decode(t *testing.T, target any) {
	t.Helper()
	if err := json.Unmarshal(r.body, target); err != nil {
		t.Fatalf("decode response (%d): %v\nbody: %s", r.status, err, r.body)
	}
}

func (s *stack) do(t *testing.T, method, path, token string, body any, headers ...[2]string) response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, s.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}

	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{status: resp.StatusCode, body: data}
}

func (s *stack) mustDo(t *testing.T, method, path, token string, body any, wantStatus int) response {
	t.Helper()
	resp := s.do(t, method, path, token, body)
	if resp.status != wantStatus {
		t.Fatalf("%s %s = %d, want %d\nbody: %s", method, path, resp.status, wantStatus, resp.body)
	}
	return resp
}

// ── domain helpers ───────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type repositoryResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	URL          string  `json:"url"`
	SyncStatus   string  `json:"sync_status"`
	SyncError    string  `json:"sync_error"`
	LastSyncedAt *string `json:"last_synced_at"`
	Metadata     struct {
		ProviderID    string         `json:"provider_id"`
		OwnerName     string         `json:"owner_name"`
		DefaultBranch string         `json:"default_branch"`
		Languages     map[string]int `json:"languages"`
		Topics        []string       `json:"topics"`
		StarCount     int            `json:"star_count"`
		ForkCount     int            `json:"fork_count"`
		BranchCount   int            `json:"branch_count"`
		CommitCount   int            `json:"commit_count"`
		PRCount       int            `json:"pr_count"`
		HasCI         *bool          `json:"has_ci"`
		HasTests      *bool          `json:"has_tests"`
		CIEvidence    string         `json:"ci_evidence"`
		TestEvidence  string         `json:"test_evidence"`
	} `json:"metadata"`
	Scorecard *struct {
		Passing  int `json:"passing"`
		Failing  int `json:"failing"`
		Total    int `json:"total"`
		Verdicts []struct {
			CheckID string `json:"check_id"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
		} `json:"verdicts"`
	} `json:"scorecard"`
}

// registerOrg onboards a fresh organization and returns its admin token. Each
// test that needs isolation gets its own, since the first user in an
// organization is its admin.
func (s *stack) registerOrg(t *testing.T, slug string) string {
	t.Helper()
	resp := s.mustDo(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":             slug + "@e2e.example",
		"password":          "E2ePassw0rd!",
		"full_name":         "E2E " + slug,
		"organization_name": "E2E " + slug,
		"organization_slug": slug,
	}, http.StatusCreated)

	var token tokenResponse
	resp.decode(t, &token)
	if token.AccessToken == "" {
		t.Fatalf("register returned no access token: %s", resp.body)
	}
	return token.AccessToken
}

func (s *stack) createRepository(t *testing.T, token, url string) repositoryResponse {
	t.Helper()
	resp := s.mustDo(t, http.MethodPost, "/api/v1/repositories", token, map[string]any{
		"url":         url,
		"description": "e2e",
	}, http.StatusCreated)

	var repo repositoryResponse
	resp.decode(t, &repo)
	return repo
}

func (s *stack) getRepository(t *testing.T, token, id string) repositoryResponse {
	t.Helper()
	resp := s.mustDo(t, http.MethodGet, "/api/v1/repositories/"+id, token, nil, http.StatusOK)
	var repo repositoryResponse
	resp.decode(t, &repo)
	return repo
}

// waitForSync polls until the repository leaves the `idle`/`syncing` states.
// Sync runs in the worker, so the API returns before it finishes.
//
// It polls the list endpoint rather than the detail endpoint on purpose: the
// list query reads through, while the detail read is served from a Redis cache
// that the sync invalidates. Polling the authoritative read keeps this helper
// from depending on that invalidation — which the provider-error test asserts
// on directly instead.
func (s *stack) waitForSync(t *testing.T, token, id string) repositoryResponse {
	t.Helper()
	var repo repositoryResponse
	err := waitFor(45*time.Second, func() error {
		found, ok := s.findInList(t, token, id)
		if !ok {
			return fmt.Errorf("repository %s not in the list yet", id)
		}
		repo = found
		switch repo.SyncStatus {
		case "synced", "error":
			return nil
		default:
			return fmt.Errorf("sync_status = %q", repo.SyncStatus)
		}
	})
	if err != nil {
		t.Fatalf("repository %s never finished syncing: %v", id, err)
	}
	return repo
}

// findInList reads the enriched list, which is also where the scorecard and the
// coverage join are computed.
func (s *stack) findInList(t *testing.T, token, id string) (repositoryResponse, bool) {
	t.Helper()
	resp := s.mustDo(t, http.MethodGet, "/api/v1/repositories?limit=100", token, nil, http.StatusOK)
	var list struct {
		Items []repositoryResponse `json:"items"`
	}
	resp.decode(t, &list)
	for _, item := range list.Items {
		if item.ID == id {
			return item, true
		}
	}
	return repositoryResponse{}, false
}
