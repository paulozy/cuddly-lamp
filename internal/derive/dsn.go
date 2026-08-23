package derive

import (
	"net/url"
	"strconv"
	"strings"
)

// Engine names, mirroring OpenTelemetry's `db.system.name` values so a resource
// derived from committed config and one observed in a trace describe the same
// thing under the same name.
const (
	EnginePostgreSQL = "postgresql"
	EngineMySQL      = "mysql"
	EngineMariaDB    = "mariadb"
	EngineRedis      = "redis"
	EngineValkey     = "valkey"
	EngineMongoDB    = "mongodb"
	EngineKafka      = "kafka"
	EngineRabbitMQ   = "rabbitmq"
	EngineElastic    = "elasticsearch"
	EngineOpenSearch = "opensearch"
	EngineClickHouse = "clickhouse"
	EngineCassandra  = "cassandra"
	EngineMemcached  = "memcached"
	EngineNATS       = "nats"
	EngineS3         = "s3"
	EngineMinIO      = "minio"
)

// Locator is a resource's identity, shaped like OTel's database semantic
// conventions.
//
// Host, Port and Namespace are all optional, and their absence is the whole
// point: `postgres:16` in a compose file proves the *engine* and says nothing
// about which instance. Only a locator with a real host can unify two
// repositories — see migration 035.
type Locator struct {
	Engine string `json:"engine"`
	// Host is server.address. Empty when the evidence was local-only.
	Host string `json:"host,omitempty"`
	// Port is server.port. nil rather than a default, because "the DSN omitted
	// the port" and "the DSN said 5432" are different facts, and inventing the
	// default would make two genuinely different locators look identical.
	Port *int `json:"port,omitempty"`
	// Namespace is db.namespace: the logical database inside the instance.
	Namespace string `json:"namespace,omitempty"`
}

// IsShared reports whether this locator identifies an instance, which is the
// only case in which two repositories may be unified onto one resource node.
func (l Locator) IsShared() bool {
	return l.Host != "" && !IsPlaceholderHost(l.Host)
}

// dsnSchemes maps a URL scheme to its engine. Several schemes per engine is the
// norm rather than the exception — `postgres://` and `postgresql://` are both
// idiomatic, and a parser that knew only one would miss half the DSNs in the wild.
var dsnSchemes = map[string]string{
	"postgres":      EnginePostgreSQL,
	"postgresql":    EnginePostgreSQL,
	"mysql":         EngineMySQL,
	"mariadb":       EngineMariaDB,
	"redis":         EngineRedis,
	"rediss":        EngineRedis,
	"valkey":        EngineValkey,
	"mongodb":       EngineMongoDB,
	"mongodb+srv":   EngineMongoDB,
	"amqp":          EngineRabbitMQ,
	"amqps":         EngineRabbitMQ,
	"kafka":         EngineKafka,
	"nats":          EngineNATS,
	"elasticsearch": EngineElastic,
	"opensearch":    EngineOpenSearch,
	"clickhouse":    EngineClickHouse,
	"cassandra":     EngineCassandra,
	"memcached":     EngineMemcached,
	"s3":            EngineS3,
}

// ParseDSN reads a connection string into a locator, discarding credentials.
//
// The credential discard is not incidental — it is the security property of this
// function. A DSN carries `user:pass@`, and `net/url` parses it into Userinfo;
// nothing here ever copies that anywhere. The locator has no field for it, so a
// credential cannot reach a column, a metadata blob or an evidence log even by
// mistake.
//
// A missing port stays nil rather than being filled in with the engine's default.
// Defaulting would make `postgres://db.prod/orders` and
// `postgres://db.prod:5432/orders` unify, which is *probably* right and not
// certainly right — and this table's whole discipline is refusing to unify on
// probably.
func ParseDSN(raw string) (Locator, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "://") {
		return Locator{}, false
	}

	// A broker DSN can list several endpoints (`kafka://a:9092,b:9092`). The comma
	// has to go before parsing, not after: net/url reads `9092,b:9092` as an
	// invalid port and falls back to treating the whole thing as the hostname,
	// which yields a locator that matches nothing. The first endpoint identifies
	// the cluster, which is all the locator needs.
	if scheme, rest, found := strings.Cut(raw, "://"); found {
		if idx := strings.IndexByte(rest, ','); idx >= 0 {
			raw = scheme + "://" + rest[:idx]
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return Locator{}, false
	}
	engine, known := dsnSchemes[strings.ToLower(parsed.Scheme)]
	if !known {
		return Locator{}, false
	}

	locator := Locator{Engine: engine}

	// Hostname() and Port() both drop the userinfo, which is exactly what we
	// want: the credential never enters the locator.
	locator.Host = strings.ToLower(parsed.Hostname())

	if portText := parsed.Port(); portText != "" {
		if port, err := strconv.Atoi(portText); err == nil && port > 0 && port <= 65535 {
			locator.Port = &port
		}
	}
	locator.Namespace = namespaceFromDSN(engine, parsed)
	return locator, locator.Engine != ""
}

// namespaceFromDSN extracts the logical database, whose spelling is per engine.
func namespaceFromDSN(engine string, parsed *url.URL) string {
	path := strings.TrimPrefix(parsed.Path, "/")
	switch engine {
	case EngineRabbitMQ:
		// AMQP's path segment is the vhost, which is the closest thing it has to
		// a logical namespace.
		return path
	case EngineRedis, EngineValkey:
		// Redis addresses databases by number. Keeping it means two services on
		// db 0 and db 3 of the same instance are two resources, which is correct.
		return path
	default:
		// Anything after a further slash is not part of the database name.
		if idx := strings.Index(path, "/"); idx >= 0 {
			path = path[:idx]
		}
		return path
	}
}

// placeholderHosts are values that legitimately point at nothing.
//
// Without this list the consumption edges of phase 4 turn to noise, because
// `.env.example` is made of placeholders. The distinction matters: a placeholder
// is not a failure to inspect, so it is discarded *without* recording evidence
// and without marking anything incomplete.
//
// Third-party hosts (`api.stripe.com`, `s3.amazonaws.com`) need no denylist —
// they match nothing in the internal index, so they produce nothing on their own.
var placeholderHosts = map[string]bool{
	"localhost": true, "127.0.0.1": true, "0.0.0.0": true, "::1": true,
	"host.docker.internal": true,
	"example.com":          true, "example.org": true, "example.net": true,
	"test.local": true, "changeme": true, "todo": true, "none": true,
}

// placeholderPrefixes catch the templating and fill-me-in conventions.
var placeholderPrefixes = []string{"changeme", "your-", "your_", "my-", "my_", "<", "${", "{{", "$(", "%"}

// IsPlaceholderHost reports whether a hostname legitimately points at nothing.
func IsPlaceholderHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	if placeholderHosts[host] {
		return true
	}
	for _, prefix := range placeholderPrefixes {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	// A template anywhere in the value makes it unresolvable, not just at the
	// front: `svc-${ENV}.internal` names no host we can join on.
	return strings.ContainsAny(host, "${}<>") || strings.Contains(host, "{{")
}
