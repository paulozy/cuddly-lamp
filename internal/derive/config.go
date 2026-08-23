package derive

import (
	"bytes"
	"path"
	"strings"

	"github.com/goccy/go-yaml"
)

// Rule ids for the resource and consumption families. Stable strings — they are
// part of every fingerprint.
const (
	// RuleResourceComposeImage is a compose service whose image is a known
	// engine. High precision about the engine, zero about the instance.
	RuleResourceComposeImage = "resource.compose_image"
	// RuleResourceHelmSubchart is a Helm dependency on postgresql/redis/kafka.
	// Very strong evidence that the service uses that engine.
	RuleResourceHelmSubchart = "resource.helm_subchart"
	// RuleResourceDSN is a connection string with a real host, which is the only
	// evidence that can unify two repositories onto one resource.
	RuleResourceDSN = "resource.dsn"
	// RuleConsumeComposeDependsOn is a compose `depends_on` between two
	// application services.
	RuleConsumeComposeDependsOn = "consume.compose_depends_on"
	// RuleConsumeK8sServiceHost is a Kubernetes Service DNS name found in an
	// environment value. The best static signal there is — see phase 4.
	RuleConsumeK8sServiceHost = "consume.k8s_service_host"
	// RuleConsumeIngressHost is a public hostname matching an Ingress rule another
	// repository declares. Weaker than the in-cluster name, and it works only
	// because the host map is built from declarations rather than guessed.
	RuleConsumeIngressHost = "consume.ingress_host"
)

// engineImages maps a container image name to its engine.
//
// Identity here is unambiguous in a way almost nothing else in this domain is:
// `postgres:16` is a Postgres, full stop. What it does *not* tell us is which
// Postgres, which is why every finding from this rule is repository-scoped.
var engineImages = map[string]string{
	"postgres": EnginePostgreSQL, "postgresql": EnginePostgreSQL,
	"bitnami/postgresql": EnginePostgreSQL, "pgvector/pgvector": EnginePostgreSQL,
	"timescale/timescaledb": EnginePostgreSQL,
	"mysql":                 EngineMySQL, "bitnami/mysql": EngineMySQL,
	"mariadb": EngineMariaDB,
	"redis":   EngineRedis, "bitnami/redis": EngineRedis, "redis/redis-stack": EngineRedis,
	"valkey/valkey": EngineValkey,
	"mongo":         EngineMongoDB, "mongodb/mongodb-community-server": EngineMongoDB,
	"rabbitmq": EngineRabbitMQ, "bitnami/rabbitmq": EngineRabbitMQ,
	"confluentinc/cp-kafka": EngineKafka, "bitnami/kafka": EngineKafka,
	"apache/kafka": EngineKafka, "redpandadata/redpanda": EngineKafka,
	"elasticsearch": EngineElastic, "opensearchproject/opensearch": EngineOpenSearch,
	"clickhouse/clickhouse-server": EngineClickHouse,
	"cassandra":                    EngineCassandra,
	"memcached":                    EngineMemcached,
	"nats":                         EngineNATS,
	"minio/minio":                  EngineMinIO,
	"localstack/localstack":        EngineS3,
}

// engineForImage resolves a compose `image:` value to an engine.
//
// The registry host and the tag are both stripped, because
// `docker.io/library/postgres:16-alpine` and `postgres:16` are the same engine and
// a map keyed on the raw string would miss one of them.
func engineForImage(image string) (string, bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", false
	}
	// Strip the digest and then the tag. Digest first: a tag may contain no colon
	// but a digest always does, and cutting at the last colon would eat it.
	if idx := strings.Index(image, "@"); idx > 0 {
		image = image[:idx]
	}
	// A registry host is recognized by a dot or a port in the first segment,
	// which is Docker's own rule for telling a host from a namespace.
	if slash := strings.Index(image, "/"); slash > 0 {
		first := image[:slash]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			image = image[slash+1:]
		}
	}
	if idx := strings.LastIndex(image, ":"); idx > 0 && !strings.Contains(image[idx:], "/") {
		image = image[:idx]
	}
	image = strings.ToLower(strings.TrimPrefix(image, "library/"))

	if engine, ok := engineImages[image]; ok {
		return engine, true
	}
	// A bare name that matches an official image is the common case for an
	// image pulled from a mirror with a longer path.
	if engine, ok := engineImages[path.Base(image)]; ok {
		return engine, true
	}
	return "", false
}

// ── docker compose ───────────────────────────────────────────────────────────

// ComposeService is one service as a compose file declares it.
type ComposeService struct {
	Name string `json:"name"`
	// Image is empty for a service built from a Dockerfile, which is how an
	// application service is normally spelled — and is exactly what makes it
	// distinguishable from an engine.
	Image     string   `json:"image,omitempty"`
	Engine    string   `json:"engine,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	// Environment holds the service's declared env values, the input to both the
	// DSN rule here and the consumption rules of phase 4.
	Environment map[string]string `json:"environment,omitempty"`
}

// ComposeFile is a shallow reading of a compose file.
type ComposeFile struct {
	Path     string           `json:"path"`
	Services []ComposeService `json:"services,omitempty"`
	// IsTestCompose marks `docker-compose.test.yml` and friends. A compose file
	// that exists to stub a dependency in CI produces the same text as one that
	// consumes the real thing, so which file a finding came from is the only
	// signal available for judging it — see phase 4.
	IsTestCompose bool `json:"is_test_compose"`
}

type composeDoc struct {
	Services map[string]struct {
		Image     string `yaml:"image"`
		Build     any    `yaml:"build"`
		DependsOn any    `yaml:"depends_on"`
		// Environment is `any` because compose accepts both a map and a list of
		// `KEY=value` strings, and both are common.
		Environment any `yaml:"environment"`
	} `yaml:"services"`
}

// IsComposeFile reports whether a path is a compose file.
func IsComposeFile(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
		return false
	}
	return strings.HasPrefix(base, "docker-compose") || strings.HasPrefix(base, "compose")
}

// isTestComposePath reports whether a compose file describes a test or CI stack.
func isTestComposePath(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	for _, marker := range []string{".test.", ".tests.", ".ci.", ".e2e.", "-test.", "-ci."} {
		if strings.Contains(base, marker) {
			return true
		}
	}
	return false
}

// ParseCompose reads a compose file's services, images, dependencies and
// environment.
func ParseCompose(filePath string, content []byte) (ComposeFile, error) {
	var doc composeDoc
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return ComposeFile{}, err
	}

	file := ComposeFile{Path: filePath, IsTestCompose: isTestComposePath(filePath)}
	for name, service := range doc.Services {
		entry := ComposeService{
			Name:        name,
			Image:       strings.TrimSpace(service.Image),
			DependsOn:   composeDependsOn(service.DependsOn),
			Environment: environmentMap(service.Environment),
		}
		if engine, ok := engineForImage(entry.Image); ok {
			entry.Engine = engine
		}
		file.Services = append(file.Services, entry)
	}
	sortServices(file.Services)
	return file, nil
}

// composeDependsOn handles both spellings: the short list form and the long map
// form with conditions.
func composeDependsOn(raw any) []string {
	switch value := raw.(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if name, ok := item.(string); ok && name != "" {
				out = append(out, name)
			}
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(value))
		for name := range value {
			if name != "" {
				out = append(out, name)
			}
		}
		sortStrings(out)
		return out
	default:
		return nil
	}
}

// environmentMap normalizes compose's two environment spellings — a mapping, and
// a list of `KEY=value` strings — into one map.
func environmentMap(raw any) map[string]string {
	out := map[string]string{}
	switch value := raw.(type) {
	case map[string]any:
		for key, item := range value {
			out[key] = scalarString(item)
		}
	case []any:
		for _, item := range value {
			entry, ok := item.(string)
			if !ok {
				continue
			}
			key, val, found := strings.Cut(entry, "=")
			if !found {
				// `KEY` with no value passes the variable through from the host.
				// It names an engine at best and identifies nothing, so it is
				// recorded with an empty value and dropped by the caller.
				out[strings.TrimSpace(entry)] = ""
				continue
			}
			out[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ── helm ─────────────────────────────────────────────────────────────────────

// HelmDependency is one subchart a Chart.yaml declares.
type HelmDependency struct {
	Name   string `json:"name"`
	Alias  string `json:"alias,omitempty"`
	Engine string `json:"engine,omitempty"`
	// Condition is the values key that turns the subchart on. A dependency behind
	// a condition is still recorded: the condition's default lives in values.yaml
	// and may be overridden per environment, so treating it as off would hide a
	// real dependency.
	Condition string `json:"condition,omitempty"`
}

type chartDoc struct {
	Name         string `yaml:"name"`
	Dependencies []struct {
		Name       string `yaml:"name"`
		Alias      string `yaml:"alias"`
		Condition  string `yaml:"condition"`
		Repository string `yaml:"repository"`
	} `yaml:"dependencies"`
}

// subchartEngines maps a well-known subchart name to its engine.
var subchartEngines = map[string]string{
	"postgresql": EnginePostgreSQL, "postgres": EnginePostgreSQL,
	"postgresql-ha": EnginePostgreSQL, "cloudnative-pg": EnginePostgreSQL,
	"mysql": EngineMySQL, "mariadb": EngineMariaDB,
	"redis": EngineRedis, "redis-cluster": EngineRedis, "valkey": EngineValkey,
	"mongodb": EngineMongoDB,
	"kafka":   EngineKafka, "rabbitmq": EngineRabbitMQ,
	"elasticsearch": EngineElastic, "opensearch": EngineOpenSearch,
	"clickhouse": EngineClickHouse, "cassandra": EngineCassandra,
	"memcached": EngineMemcached, "nats": EngineNATS, "minio": EngineMinIO,
}

// IsHelmChart reports whether a path is a Helm Chart.yaml.
func IsHelmChart(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	return base == "chart.yaml" || base == "chart.yml"
}

// ParseHelmChart reads a Chart.yaml's dependencies.
//
// A subchart named `postgresql` is very strong evidence that the service uses
// Postgres — but it says nothing about which instance, so the finding stays
// repository-scoped like a compose image.
func ParseHelmChart(filePath string, content []byte) ([]HelmDependency, error) {
	var doc chartDoc
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	var out []HelmDependency
	for _, dep := range doc.Dependencies {
		name := strings.ToLower(strings.TrimSpace(dep.Name))
		engine, known := subchartEngines[name]
		if !known {
			// A subchart that is not a known engine is another service's chart, a
			// sidecar, or a library chart. Guessing from the name is exactly the
			// tier-3 naming heuristic this plan keeps out of v1.
			continue
		}
		out = append(out, HelmDependency{
			Name: dep.Name, Alias: dep.Alias, Engine: engine, Condition: dep.Condition,
		})
	}
	return out, nil
}

// ── kubernetes manifests ─────────────────────────────────────────────────────

// K8sManifest is a shallow reading of one or more Kubernetes documents.
type K8sManifest struct {
	Path string `json:"path"`
	// ServiceNames are the `Service` objects this repository declares. They are
	// the input to phase 4's strongest rule: in-cluster DNS is a *contract*, not a
	// convention, so a hostname matching one of these matches a declaration.
	ServiceNames []string `json:"service_names,omitempty"`
	// IngressHosts are the public hostnames this repository claims.
	IngressHosts []string `json:"ingress_hosts,omitempty"`
	// Environment is every env value found in a container spec.
	Environment map[string]string `json:"environment,omitempty"`
}

type k8sDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Rules []struct {
			Host string `yaml:"host"`
		} `yaml:"rules"`
		Template struct {
			Spec struct {
				Containers     []k8sContainer `yaml:"containers"`
				InitContainers []k8sContainer `yaml:"initContainers"`
			} `yaml:"spec"`
		} `yaml:"template"`
		// A bare Pod puts containers directly on spec.
		Containers []k8sContainer `yaml:"containers"`
	} `yaml:"spec"`
}

type k8sContainer struct {
	Env []struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	} `yaml:"env"`
}

// IsK8sManifest reports whether a path is worth reading as a Kubernetes manifest.
//
// The filter is by location rather than by content, because reading every YAML in
// a repository to find out would cost hundreds of requests. The consequence — a
// manifest outside these directories is missed — is accepted: most repositories
// that ship manifests keep them in one of these places, and the alternative is
// unaffordable.
func IsK8sManifest(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
		return false
	}
	if IsComposeFile(filePath) || IsHelmChart(filePath) {
		return false
	}
	dir := strings.ToLower(path.Dir(filePath))
	for _, segment := range strings.Split(dir, "/") {
		switch segment {
		case "k8s", "kubernetes", "manifests", "deploy", "deployment", "deployments", "kustomize", "overlays", "base":
			return true
		}
	}
	return false
}

// ParseK8sManifest reads Service names, ingress hosts and container env from a
// possibly multi-document YAML file.
//
// Templated manifests are the norm in real repositories — Helm and Kustomize both
// leave `{{ }}` or patch markers in the committed file — and no attempt is made to
// resolve them. What parses, parses; the rest is skipped, and a templated value is
// filtered out downstream by the placeholder rules rather than guessed at.
func ParseK8sManifest(filePath string, content []byte) (K8sManifest, error) {
	manifest := K8sManifest{Path: filePath}

	env := map[string]string{}
	var firstErr error
	// Documents are split by hand rather than with yaml.NewDecoder, because the
	// decoder parses the whole stream before yielding the first document: one
	// templated document at the end would lose every document before it. Splitting
	// first is what makes "keeps what parsed" true rather than aspirational.
	for _, document := range splitYAMLDocuments(content) {
		var doc k8sDoc
		if err := yaml.Unmarshal(document, &doc); err != nil {
			// A Helm template is the usual cause. The error is reported so the
			// caller can mark the fact incomplete, but the other documents stand.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		switch strings.ToLower(doc.Kind) {
		case "service":
			if name := strings.TrimSpace(doc.Metadata.Name); name != "" && !IsPlaceholderHost(name) {
				manifest.ServiceNames = append(manifest.ServiceNames, name)
			}
		case "ingress":
			for _, rule := range doc.Spec.Rules {
				if host := strings.TrimSpace(rule.Host); host != "" && !IsPlaceholderHost(host) {
					manifest.IngressHosts = append(manifest.IngressHosts, host)
				}
			}
		}

		containers := doc.Spec.Template.Spec.Containers
		containers = append(containers, doc.Spec.Template.Spec.InitContainers...)
		containers = append(containers, doc.Spec.Containers...)
		for _, container := range containers {
			for _, entry := range container.Env {
				if entry.Name != "" && entry.Value != "" {
					env[entry.Name] = entry.Value
				}
			}
		}
	}

	if len(env) > 0 {
		manifest.Environment = env
	}
	sortStrings(manifest.ServiceNames)
	sortStrings(manifest.IngressHosts)
	return manifest, firstErr
}

// splitYAMLDocuments splits a multi-document YAML stream on its `---` separators.
//
// The separator only counts at the start of a line with nothing else on it, which
// is what keeps a `---` inside a block scalar or a Markdown string from cutting a
// document in half.
func splitYAMLDocuments(content []byte) [][]byte {
	lines := bytes.Split(content, []byte("\n"))
	var documents [][]byte
	current := make([][]byte, 0, len(lines))

	flush := func() {
		joined := bytes.TrimSpace(bytes.Join(current, []byte("\n")))
		if len(joined) > 0 {
			documents = append(documents, joined)
		}
		current = current[:0]
	}
	for _, line := range lines {
		if trimmed := bytes.TrimRight(line, " \t\r"); bytes.Equal(trimmed, []byte("---")) {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return documents
}

// ── dotenv ───────────────────────────────────────────────────────────────────

// IsDotEnvExample reports whether a path is a committed environment template.
//
// Only the *example* files are read. A committed `.env` with real values would be
// a secret leak we should not be encouraging, and the example is where the shape
// of the configuration actually lives.
func IsDotEnvExample(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	switch base {
	case ".env.example", ".env.sample", ".env.template", ".env.dist", "env.example", ".env.defaults":
		return true
	}
	return false
}

// ParseDotEnv reads `KEY=value` pairs, ignoring comments and blanks.
func ParseDotEnv(content []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ── values.yaml ──────────────────────────────────────────────────────────────

// IsHelmValues reports whether a path is a Helm values file.
//
// values.yaml is plain YAML, not a template, so it parses — but the values in it
// may be defaults nobody runs in production. It is therefore used only for
// hostnames that do not look like defaults, and never to unify a resource on its
// own.
func IsHelmValues(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	return strings.HasPrefix(base, "values") &&
		(strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml"))
}

// FlattenYAMLStrings reads a YAML document into dotted-path string leaves.
//
// Flattening rather than typing it is deliberate: a values file has no schema we
// control, and the only thing wanted from it is "every string that might be a DSN
// or a hostname". A typed struct would have to guess at the shape and would miss
// anything it guessed wrong.
func FlattenYAMLStrings(content []byte) (map[string]string, error) {
	var root any
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, err
	}
	out := map[string]string{}
	flatten("", root, out, 0)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// maxFlattenDepth bounds the walk. A values file nested deeper than this is
// configuration for something we are not modelling.
const maxFlattenDepth = 12

func flatten(prefix string, node any, out map[string]string, depth int) {
	if depth > maxFlattenDepth {
		return
	}
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			flatten(join(prefix, key), child, out, depth+1)
		}
	case map[any]any:
		for key, child := range value {
			flatten(join(prefix, scalarString(key)), child, out, depth+1)
		}
	case []any:
		for i, child := range value {
			flatten(join(prefix, itoa(i)), child, out, depth+1)
		}
	case string:
		if prefix != "" && value != "" {
			out[prefix] = value
		}
	}
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
