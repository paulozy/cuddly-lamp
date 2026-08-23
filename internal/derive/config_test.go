package derive

import (
	"testing"
)

func serviceNamed(t *testing.T, file ComposeFile, name string) ComposeService {
	t.Helper()
	for _, service := range file.Services {
		if service.Name == name {
			return service
		}
	}
	t.Fatalf("service %q not found in %+v", name, file.Services)
	return ComposeService{}
}

// ── image → engine ───────────────────────────────────────────────────────────

// `postgres:16` is a Postgres, full stop. What it does not say is *which*
// Postgres, which is why every finding from this rule is repository-scoped.
func TestEngineForImage(t *testing.T) {
	tests := []struct {
		image string
		want  string
		ok    bool
	}{
		{image: "postgres:16", want: EnginePostgreSQL, ok: true},
		{image: "postgres:15-alpine", want: EnginePostgreSQL, ok: true},
		{image: "bitnami/postgresql:16", want: EnginePostgreSQL, ok: true},
		{image: "pgvector/pgvector:pg16", want: EnginePostgreSQL, ok: true},
		{image: "redis:7-alpine", want: EngineRedis, ok: true},
		{image: "rabbitmq:3-management", want: EngineRabbitMQ, ok: true},
		{image: "confluentinc/cp-kafka:7.5.0", want: EngineKafka, ok: true},
		{image: "minio/minio:latest", want: EngineMinIO, ok: true},
		// The registry host and the tag both have to be stripped, or a map keyed
		// on the raw string misses the same engine spelled two ways.
		{image: "docker.io/library/postgres:16", want: EnginePostgreSQL, ok: true},
		{image: "registry.internal:5000/postgres:16", want: EnginePostgreSQL, ok: true},
		{image: "postgres@sha256:abc123", want: EnginePostgreSQL, ok: true},
		// An application image is not an engine, which is what keeps a service
		// out of the resource inventory.
		{image: "myorg/checkout-api:1.2.0", ok: false},
		{image: "node:20", ok: false},
		{image: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got, ok := engineForImage(tt.image)
			if ok != tt.ok {
				t.Fatalf("engineForImage(%q) ok = %v, want %v", tt.image, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("engine = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── docker compose ───────────────────────────────────────────────────────────

const composeYAML = `services:
  api:
    build: .
    depends_on:
      - db
      - worker
    environment:
      DATABASE_URL: postgres://user:secret@db:5432/app
      ORDERS_API_URL: http://orders-api:8080
  worker:
    build: ./worker
    environment:
      - REDIS_URL=redis://cache:6379/0
      - LOG_LEVEL
  db:
    image: postgres:16
  cache:
    image: redis:7-alpine
`

func TestParseCompose(t *testing.T) {
	file, err := ParseCompose("docker-compose.yml", []byte(composeYAML))
	if err != nil {
		t.Fatalf("ParseCompose() error = %v, want nil", err)
	}
	if len(file.Services) != 4 {
		t.Fatalf("services = %d, want 4", len(file.Services))
	}

	// A service built from a Dockerfile has no image, which is exactly how an
	// application service is distinguishable from an engine.
	api := serviceNamed(t, file, "api")
	if api.Engine != "" {
		t.Errorf("api engine = %q, want empty for a built service", api.Engine)
	}
	if len(api.DependsOn) != 2 {
		t.Errorf("api depends_on = %v, want two entries", api.DependsOn)
	}
	if api.Environment["DATABASE_URL"] == "" {
		t.Errorf("api environment = %v, want the DATABASE_URL", api.Environment)
	}

	db := serviceNamed(t, file, "db")
	if db.Engine != EnginePostgreSQL {
		t.Errorf("db engine = %q, want %q", db.Engine, EnginePostgreSQL)
	}
}

// compose accepts both environment spellings — a mapping and a list of
// `KEY=value` strings — and both are common, so a reader that knew one would
// silently lose half the real files.
func TestParseCompose_BothEnvironmentSpellings(t *testing.T) {
	file, err := ParseCompose("docker-compose.yml", []byte(composeYAML))
	if err != nil {
		t.Fatalf("ParseCompose() error = %v, want nil", err)
	}
	worker := serviceNamed(t, file, "worker")
	if worker.Environment["REDIS_URL"] != "redis://cache:6379/0" {
		t.Errorf("worker REDIS_URL = %q, want the list-form value", worker.Environment["REDIS_URL"])
	}
	// `KEY` with no value passes the host's variable through: it identifies
	// nothing, so it is recorded empty and dropped downstream.
	if value, present := worker.Environment["LOG_LEVEL"]; !present || value != "" {
		t.Errorf("worker LOG_LEVEL = %q (present %v), want present and empty", value, present)
	}
}

// The long `depends_on` form with conditions is what a real production compose
// file uses, so both shapes have to read the same.
func TestParseCompose_LongDependsOnForm(t *testing.T) {
	content := `services:
  api:
    build: .
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_started
  db:
    image: postgres:16
  cache:
    image: redis:7
`
	file, err := ParseCompose("docker-compose.yml", []byte(content))
	if err != nil {
		t.Fatalf("ParseCompose() error = %v, want nil", err)
	}
	api := serviceNamed(t, file, "api")
	if len(api.DependsOn) != 2 {
		t.Errorf("depends_on = %v, want db and cache", api.DependsOn)
	}
}

// Which compose file a finding came from is the only signal available for judging
// it, because a repository that mocks B in its tests produces the same text as one
// that calls B.
func TestParseCompose_MarksTestComposeFiles(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "docker-compose.yml", want: false},
		{path: "docker-compose.yaml", want: false},
		{path: "compose.yml", want: false},
		{path: "docker-compose.prod.yml", want: false},
		{path: "docker-compose.test.yml", want: true},
		{path: "docker-compose.ci.yml", want: true},
		{path: "docker-compose.e2e.yaml", want: true},
		{path: "compose-test.yml", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			file, err := ParseCompose(tt.path, []byte("services:\n  api:\n    build: .\n"))
			if err != nil {
				t.Fatalf("ParseCompose() error = %v, want nil", err)
			}
			if file.IsTestCompose != tt.want {
				t.Errorf("IsTestCompose = %v, want %v", file.IsTestCompose, tt.want)
			}
		})
	}
}

func TestParseCompose_MalformedIsAnErrorNotAPanic(t *testing.T) {
	if _, err := ParseCompose("docker-compose.yml", []byte("services:\n  api:\n   - bad: [")); err == nil {
		t.Error("ParseCompose() error = nil, want a parse error")
	}
}

func TestIsComposeFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "docker-compose.yml", want: true},
		{path: "compose.yaml", want: true},
		{path: "deploy/docker-compose.prod.yml", want: true},
		{path: "k8s/deployment.yaml", want: false},
		{path: "Chart.yaml", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsComposeFile(tt.path); got != tt.want {
				t.Errorf("IsComposeFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ── helm ─────────────────────────────────────────────────────────────────────

func TestParseHelmChart(t *testing.T) {
	content := `apiVersion: v2
name: checkout
dependencies:
  - name: postgresql
    version: 15.x.x
    repository: https://charts.bitnami.com/bitnami
  - name: redis
    version: 18.x.x
    condition: redis.enabled
    alias: sessions
  - name: common
    version: 2.x.x
`
	deps, err := ParseHelmChart("Chart.yaml", []byte(content))
	if err != nil {
		t.Fatalf("ParseHelmChart() error = %v, want nil", err)
	}
	// `common` is a library chart, not an engine. Guessing an engine from an
	// unknown subchart name is exactly the naming heuristic v1 keeps out.
	if len(deps) != 2 {
		t.Fatalf("dependencies = %+v, want only the two known engines", deps)
	}

	byEngine := map[string]HelmDependency{}
	for _, dep := range deps {
		byEngine[dep.Engine] = dep
	}
	if _, ok := byEngine[EnginePostgreSQL]; !ok {
		t.Errorf("dependencies = %+v, want a postgresql", deps)
	}
	redis, ok := byEngine[EngineRedis]
	if !ok {
		t.Fatalf("dependencies = %+v, want a redis", deps)
	}
	if redis.Alias != "sessions" {
		t.Errorf("alias = %q, want sessions", redis.Alias)
	}
	// A conditioned dependency is still recorded: the condition's default lives in
	// values.yaml and may be overridden per environment, so treating it as off
	// would hide a real dependency.
	if redis.Condition != "redis.enabled" {
		t.Errorf("condition = %q, want redis.enabled", redis.Condition)
	}
}

func TestParseHelmChart_MalformedIsAnErrorNotAPanic(t *testing.T) {
	if _, err := ParseHelmChart("Chart.yaml", []byte("dependencies:\n  - name: [")); err == nil {
		t.Error("ParseHelmChart() error = nil, want a parse error")
	}
}

// ── kubernetes manifests ─────────────────────────────────────────────────────

const k8sMultiDoc = `apiVersion: v1
kind: Service
metadata:
  name: orders-api
spec:
  ports:
    - port: 8080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders-api
spec:
  template:
    spec:
      initContainers:
        - name: migrate
          env:
            - name: DATABASE_URL
              value: postgres://svc:pw@db.prod.internal:5432/orders
      containers:
        - name: api
          env:
            - name: PAYMENTS_HOST
              value: payments.prod.svc.cluster.local
            - name: LOG_LEVEL
              value: info
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: orders-ingress
spec:
  rules:
    - host: orders.acme.example
`

// A Service and a Deployment in the same file is the normal layout, so multi-doc
// has to work or almost nothing is read.
func TestParseK8sManifest_MultiDocument(t *testing.T) {
	manifest, err := ParseK8sManifest("k8s/orders.yaml", []byte(k8sMultiDoc))
	if err != nil {
		t.Fatalf("ParseK8sManifest() error = %v, want nil", err)
	}
	if len(manifest.ServiceNames) != 1 || manifest.ServiceNames[0] != "orders-api" {
		t.Errorf("service_names = %v, want [orders-api]", manifest.ServiceNames)
	}
	if len(manifest.IngressHosts) != 1 || manifest.IngressHosts[0] != "orders.acme.example" {
		t.Errorf("ingress_hosts = %v, want the ingress rule host", manifest.IngressHosts)
	}
	// initContainers count: a migration job's DATABASE_URL is as real a dependency
	// as the app container's.
	if manifest.Environment["DATABASE_URL"] == "" {
		t.Errorf("environment = %v, want the initContainer's DATABASE_URL", manifest.Environment)
	}
	if manifest.Environment["PAYMENTS_HOST"] != "payments.prod.svc.cluster.local" {
		t.Errorf("PAYMENTS_HOST = %q, want the in-cluster host", manifest.Environment["PAYMENTS_HOST"])
	}
}

// Templated manifests are the norm — Helm and Kustomize both leave markers in the
// committed file. What parses is kept; the error only withdraws the authority to
// delete, it does not lose the documents that did parse.
func TestParseK8sManifest_KeepsWhatParsedBeforeATemplate(t *testing.T) {
	content := `apiVersion: v1
kind: Service
metadata:
  name: orders-api
---
{{- if .Values.ingress.enabled }}
kind: Ingress
  bad: [indent
{{- end }}
`
	manifest, err := ParseK8sManifest("k8s/orders.yaml", []byte(content))
	if err == nil {
		t.Error("ParseK8sManifest() error = nil, want the template to report as unparseable")
	}
	if len(manifest.ServiceNames) != 1 || manifest.ServiceNames[0] != "orders-api" {
		t.Errorf("service_names = %v, want the Service that did parse to survive", manifest.ServiceNames)
	}
}

func TestIsK8sManifest(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "k8s/deployment.yaml", want: true},
		{path: "kubernetes/service.yml", want: true},
		{path: "deploy/overlays/prod/patch.yaml", want: true},
		{path: "manifests/orders.yaml", want: true},
		// A compose file and a Chart.yaml have their own readers; letting them
		// match here would parse them twice under the wrong rules.
		{path: "k8s/docker-compose.yml", want: false},
		{path: "deploy/Chart.yaml", want: false},
		// Outside the conventional directories a manifest is missed, and that is
		// accepted: reading every YAML in a repository to find out would cost
		// hundreds of requests per sync.
		{path: "src/config.yaml", want: false},
		{path: "README.md", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsK8sManifest(tt.path); got != tt.want {
				t.Errorf("IsK8sManifest(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ── dotenv and values ────────────────────────────────────────────────────────

func TestParseDotEnv(t *testing.T) {
	content := `# database
DATABASE_URL=postgres://user:pw@localhost:5432/app
export REDIS_URL="redis://localhost:6379/0"

ORDERS_API_URL='http://orders-api:8080'
EMPTY=
NOT_A_PAIR
`
	env := ParseDotEnv([]byte(content))
	if env["DATABASE_URL"] != "postgres://user:pw@localhost:5432/app" {
		t.Errorf("DATABASE_URL = %q, want the raw value", env["DATABASE_URL"])
	}
	// `export ` prefixes and both quote styles are common in committed examples.
	if env["REDIS_URL"] != "redis://localhost:6379/0" {
		t.Errorf("REDIS_URL = %q, want the unquoted value", env["REDIS_URL"])
	}
	if env["ORDERS_API_URL"] != "http://orders-api:8080" {
		t.Errorf("ORDERS_API_URL = %q, want the unquoted value", env["ORDERS_API_URL"])
	}
	if _, present := env["NOT_A_PAIR"]; present {
		t.Errorf("environment = %v, want a line with no = to be skipped", env)
	}
}

func TestIsDotEnvExample(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: ".env.example", want: true},
		{path: ".env.sample", want: true},
		{path: "config/.env.template", want: true},
		// A committed `.env` with real values would be a secret leak we should not
		// encourage, and the example is where the shape actually lives.
		{path: ".env", want: false},
		{path: ".env.local", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsDotEnvExample(tt.path); got != tt.want {
				t.Errorf("IsDotEnvExample(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// A values file has no schema we control, so flattening beats typing it: the only
// thing wanted is "every string that might be a DSN or a hostname", and a typed
// struct would miss whatever shape it guessed wrong.
func TestFlattenYAMLStrings(t *testing.T) {
	content := `global:
  imageRegistry: docker.io
postgresql:
  auth:
    database: orders
externalDatabase:
  host: db.prod.internal
  port: 5432
env:
  - name: ORDERS_API_URL
    value: http://orders-api:8080
`
	values, err := FlattenYAMLStrings([]byte(content))
	if err != nil {
		t.Fatalf("FlattenYAMLStrings() error = %v, want nil", err)
	}
	if values["externalDatabase.host"] != "db.prod.internal" {
		t.Errorf("externalDatabase.host = %q, want db.prod.internal", values["externalDatabase.host"])
	}
	if values["env.0.value"] != "http://orders-api:8080" {
		t.Errorf("env.0.value = %q, want the url", values["env.0.value"])
	}
	// A non-string leaf is not a hostname or a DSN, so it is simply absent.
	if _, present := values["externalDatabase.port"]; present {
		t.Errorf("values = %v, want numeric leaves omitted", values)
	}
}

func TestIsHelmValues(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "values.yaml", want: true},
		{path: "chart/values-prod.yaml", want: true},
		{path: "deploy/values.yml", want: true},
		{path: "config.yaml", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsHelmValues(tt.path); got != tt.want {
				t.Errorf("IsHelmValues(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
