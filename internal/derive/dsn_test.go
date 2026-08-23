package derive

import (
	"encoding/json"
	"strings"
	"testing"
)

func port(p int) *int { return &p }

func portValue(p *int) string {
	if p == nil {
		return "nil"
	}
	return itoa(*p)
}

// ── the locator ──────────────────────────────────────────────────────────────

func TestParseDSN(t *testing.T) {
	tests := []struct {
		name          string
		dsn           string
		wantEngine    string
		wantHost      string
		wantPort      *int
		wantNamespace string
	}{
		{
			name: "postgres", dsn: "postgres://user:secret@db.prod.internal:5432/orders",
			wantEngine: EnginePostgreSQL, wantHost: "db.prod.internal", wantPort: port(5432), wantNamespace: "orders",
		},
		{
			// Both spellings are idiomatic and a parser that knew one would miss
			// half the DSNs in the wild.
			name: "postgresql spelling", dsn: "postgresql://db.prod.internal:5432/orders",
			wantEngine: EnginePostgreSQL, wantHost: "db.prod.internal", wantPort: port(5432), wantNamespace: "orders",
		},
		{
			// Redis addresses databases by number, so keeping it means db 0 and
			// db 3 of the same instance are two resources — which is correct.
			name: "redis with db number", dsn: "redis://cache.prod:6379/3",
			wantEngine: EngineRedis, wantHost: "cache.prod", wantPort: port(6379), wantNamespace: "3",
		},
		{
			// AMQP's path segment is the vhost, the closest thing it has to a
			// logical namespace.
			name: "amqp vhost", dsn: "amqp://guest:guest@rabbit.prod:5672/payments",
			wantEngine: EngineRabbitMQ, wantHost: "rabbit.prod", wantPort: port(5672), wantNamespace: "payments",
		},
		{
			name: "mongodb srv has no port", dsn: "mongodb+srv://cluster0.abcd.mongodb.net/orders",
			wantEngine: EngineMongoDB, wantHost: "cluster0.abcd.mongodb.net", wantPort: nil, wantNamespace: "orders",
		},
		{
			// The first broker identifies the cluster; treating the whole list as
			// one hostname would produce a locator that matches nothing.
			name: "kafka broker list keeps the first", dsn: "kafka://broker-1.prod:9092,broker-2.prod:9092",
			wantEngine: EngineKafka, wantHost: "broker-1.prod", wantPort: port(9092),
		},
		{
			name: "mysql", dsn: "mysql://root:root@mysql.prod:3306/billing",
			wantEngine: EngineMySQL, wantHost: "mysql.prod", wantPort: port(3306), wantNamespace: "billing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseDSN(tt.dsn)
			if !ok {
				t.Fatalf("ParseDSN(%q) ok = false, want true", tt.dsn)
			}
			if got.Engine != tt.wantEngine {
				t.Errorf("engine = %q, want %q", got.Engine, tt.wantEngine)
			}
			if got.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", got.Host, tt.wantHost)
			}
			if portValue(got.Port) != portValue(tt.wantPort) {
				t.Errorf("port = %s, want %s", portValue(got.Port), portValue(tt.wantPort))
			}
			if got.Namespace != tt.wantNamespace {
				t.Errorf("namespace = %q, want %q", got.Namespace, tt.wantNamespace)
			}
		})
	}
}

// The most important test in this phase. A DSN carries `user:pass@`, and the
// locator has no field for it — so a credential cannot reach a column, a metadata
// blob or an evidence log even by mistake. This asserts it on the serialized form,
// because that is what actually gets persisted.
func TestDSN_NeverPersistsCredentials(t *testing.T) {
	dsns := []string{
		"postgres://admin:sup3rs3cret@db.prod.internal:5432/orders",
		"amqp://guest:guest@rabbit.prod:5672/payments",
		"mongodb://root:hunter2@mongo.prod:27017/orders",
		"redis://:authtoken@cache.prod:6379/0",
		"mysql://svc_user:p%40ssw0rd@mysql.prod:3306/billing",
	}
	secrets := []string{"sup3rs3cret", "guest:guest", "hunter2", "authtoken", "p%40ssw0rd", "admin", "root", "svc_user"}

	for _, dsn := range dsns {
		t.Run(dsn, func(t *testing.T) {
			locator, ok := ParseDSN(dsn)
			if !ok {
				t.Fatalf("ParseDSN(%q) ok = false, want true", dsn)
			}
			encoded, err := json.Marshal(locator)
			if err != nil {
				t.Fatalf("marshal locator: %v", err)
			}
			serialized := string(encoded)
			for _, secret := range secrets {
				if strings.Contains(serialized, secret) {
					t.Errorf("serialized locator %s contains %q, want no credential material", serialized, secret)
				}
			}
			if strings.Contains(serialized, "@") {
				t.Errorf("serialized locator %s contains an @, want userinfo dropped entirely", serialized)
			}
		})
	}
}

// A missing port stays nil rather than being filled in with the engine's default.
// Defaulting would make `postgres://db.prod/orders` and
// `postgres://db.prod:5432/orders` unify, which is *probably* right and not
// certainly right — and refusing to unify on probably is this table's discipline.
func TestParseDSN_MissingPortStaysNil(t *testing.T) {
	got, ok := ParseDSN("postgres://db.prod.internal/orders")
	if !ok {
		t.Fatal("ParseDSN() ok = false, want true")
	}
	if got.Port != nil {
		t.Errorf("port = %d, want nil rather than an invented default", *got.Port)
	}
}

func TestParseDSN_RejectsNonDSN(t *testing.T) {
	tests := []string{
		"", "just-a-value", "5432", "true",
		// A scheme we do not recognize must not be guessed at.
		"ftp://files.prod/data",
		"https://api.stripe.com",
		// No scheme at all.
		"db.prod.internal:5432",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, ok := ParseDSN(raw); ok {
				t.Errorf("ParseDSN(%q) ok = true, want false", raw)
			}
		})
	}
}

// A DSN pointing at localhost parses fine but identifies no instance, so it can
// never unify two repositories.
func TestLocator_IsSharedRefusesPlaceholderHosts(t *testing.T) {
	tests := []struct {
		dsn  string
		want bool
	}{
		{dsn: "postgres://db.prod.internal:5432/orders", want: true},
		{dsn: "postgres://localhost:5432/orders", want: false},
		{dsn: "postgres://127.0.0.1:5432/orders", want: false},
		{dsn: "postgres://host.docker.internal:5432/orders", want: false},
		{dsn: "postgres://changeme:5432/orders", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.dsn, func(t *testing.T) {
			locator, ok := ParseDSN(tt.dsn)
			if !ok {
				t.Fatalf("ParseDSN(%q) ok = false, want true", tt.dsn)
			}
			if got := locator.IsShared(); got != tt.want {
				t.Errorf("IsShared() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── the placeholder denylist ─────────────────────────────────────────────────

// Without this list the consumption edges of phase 4 turn to noise, because
// `.env.example` is made of placeholders.
func TestIsPlaceholderHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "0.0.0.0", want: true},
		{host: "::1", want: true},
		{host: "host.docker.internal", want: true},
		{host: "example.com", want: true},
		{host: "test.local", want: true},
		{host: "changeme", want: true},
		{host: "your-host.com", want: true},
		{host: "my-service", want: true},
		{host: "<your-host>", want: true},
		{host: "${DB_HOST}", want: true},
		{host: "{{ .Values.host }}", want: true},
		{host: "svc-${ENV}.internal", want: true},
		{host: "", want: true},
		// Real hosts, including third-party ones. Third parties need no denylist:
		// they match nothing in the internal index, so they produce nothing.
		{host: "orders-api", want: false},
		{host: "db.prod.internal", want: false},
		{host: "api.stripe.com", want: false},
		{host: "orders.prod.svc.cluster.local", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := IsPlaceholderHost(tt.host); got != tt.want {
				t.Errorf("IsPlaceholderHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// The first DNS label is the Service name someone actually declared, which is what
// makes the phase 4 match a match against a declaration rather than a guess.
func TestServiceLabel(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{host: "orders-api", want: "orders-api"},
		{host: "orders-api:8080", want: "orders-api"},
		{host: "orders.prod.svc.cluster.local", want: "orders"},
		{host: "orders.default.svc.cluster.local:8080", want: "orders"},
		{host: "Orders-API", want: "orders-api"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := ServiceLabel(tt.host); got != tt.want {
				t.Errorf("ServiceLabel(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}
