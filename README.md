# IDP Backend (Internal Developer Platform com IA)

Plataforma unificada de desenvolvimento inteligente com Code Hub, Infra Hub, Architecture Hub, Deployment Hub, Observability Hub e Knowledge Base.

## 📋 Pré-requisitos

- Go 1.21+
- Docker & Docker Compose
- Git

## 🚀 Quick Start

### 1. Clone o repositório
```bash
git clone https://github.com/seu-org/idp-backend.git
cd idp-backend
```

### 2. Configure variáveis de ambiente
```bash
cp .env.example .env
```

### 3. Inicie dependências (PostgreSQL, Redis)
```bash
make docker-up
```

Aguarde alguns segundos para health checks completarem:
```bash
docker-compose ps
# Deve mostrar:
# postgres  ✓ (healthy)
# redis     ✓ (healthy)
```

### 4. Instale dependências Go
```bash
go mod download
go mod tidy
```

### 5. Rode o servidor
```bash
make dev
```

Você deve ver:
```
INFO    Starting IDP Backend    {"env": "development", "port": "8080"}
INFO    Connecting to database  {"host": "postgres", "dbname": "idp_dev"}
INFO    Database connection established
INFO    Running migrations      {"path": "migrations"}
INFO    Executing migration     {"file": "001_init_schema.sql"}
INFO    Migration completed     {"file": "001_init_schema.sql"}
INFO    Migrations completed successfully
INFO    Database ready
INFO    HTTP server listening   {"addr": ":3000"}
```

### 6. Teste o servidor
```bash
curl http://localhost:3000/health
# Resposta: {"status":"ok","service":"IDP Backend"}
```

## 📚 Documentação

- **[SETUP.md](docs/SETUP.md)** - Setup detalhado (banco, ambiente, etc)
- **[MIGRATIONS.md](docs/MIGRATIONS.md)** - Como criar e gerenciar migrations
- **[API.md](docs/API.md)** - Documentação de endpoints (em progresso)
- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** - Visão geral da arquitetura (em progresso)

## 🛠️ Comandos Úteis

### Development
```bash
make dev              # Inicia servidor em modo desenvolvimento
make build            # Compila binário
make run              # Executa binário compilado
```

### Testing
```bash
make test             # Roda testes
make test-coverage    # Testes com coverage report
make lint             # Roda linter
```

### Docker
```bash
make docker-up        # Inicia PostgreSQL + Redis
make docker-down      # Para os serviços
make docker-logs      # Mostra logs dos containers
```

### Utilities
```bash
make fmt              # Formata código (gofmt)
make mod-tidy         # Atualiza go.mod/go.sum
make clean            # Remove build artifacts
```

## 📁 Estrutura de Pastas

```
idp-backend/
├── cmd/server/              # Entry point
│   └── main.go
├── internal/
│   ├── api/                 # HTTP handlers e rotas
│   │   ├── handlers/        # Feature handlers (code, infra, etc)
│   │   ├── middleware/      # Logger, error handler, auth
│   │   └── routes.go        # Route registration
│   ├── config/              # Configuração
│   ├── storage/             # Database (migrations, queries)
│   ├── services/            # Business logic (a implementar)
│   ├── models/              # Data models (a implementar)
│   ├── integrations/        # External APIs (GitHub, Docker, etc)
│   └── utils/               # Logger, helpers
├── pkg/                     # Código reutilizável (a implementar)
├── migrations/              # SQL migrations
│   └── 001_init_schema.sql
├── tests/                   # Testes (a implementar)
├── docs/                    # Documentação
├── Makefile                 # Comandos de desenvolvimento
├── docker-compose.yml       # Dev environment
├── .env.example             # Variáveis de ambiente template
├── go.mod                   # Go module file
└── README.md                # Este arquivo
```

## 🗄️ Database

### Conectar ao PostgreSQL
```bash
docker-compose exec postgres psql -U postgres -d idp_dev
```

### Ver tabelas criadas
```sql
\dt
```

### Ver estrutura de uma tabela
```sql
\d repositories
```

### Conectar ao Redis
```bash
docker-compose exec redis redis-cli
```

## 🔒 Variáveis de Ambiente

Veja `.env.example` para todas as variáveis disponíveis:

- **PORT** - Porta do servidor (default: 3000)
- **ENV** - Ambiente (development/production)
- **DB_HOST, DB_USER, DB_PASSWORD, DB_NAME** - PostgreSQL
- **REDIS_HOST, REDIS_PORT** - Redis
- **ANTHROPIC_API_KEY** - Claude API key
- **GITHUB_TOKEN** - GitHub access token
- **LOG_LEVEL** - Nível de logging (debug/info/warn/error)

## 🚨 Troubleshooting

### PostgreSQL não conecta
```bash
# Verificar se containers estão rodando
docker-compose ps

# Se não, iniciar
make docker-up

# Se der erro, limpar e recomeçar
docker-compose down -v  # Remove volumes
docker-compose up -d
```

### Porta 8080 em uso
```bash
# Mudar porta em .env
PORT=3000
```

### Migrations falharam
```bash
# Ver logs do PostgreSQL
docker-compose logs postgres

# Conectar e verificar tabelas
docker-compose exec postgres psql -U postgres -d idp_dev -c "\dt"
```

## 📖 Próximos Passos

- [ ] Criar models (Repository, User, Webhook)
- [ ] Implementar handlers do Code Hub
- [ ] Integração com GitHub API
- [ ] Redis setup (cache, job queue)
- [ ] WebSocket para real-time updates
- [ ] AI integration (Claude API)

## 🤝 Contribuindo

Por favor, veja [CONTRIBUTING.md](docs/CONTRIBUTING.md) (a criar) para guidelines.

## 📄 License

MIT

## 📞 Contato

Para dúvidas ou sugestões, abra uma issue ou entre em contato com o time.

---

**Status**: 🚀 MVP Phase  
**Última atualização**: April 25, 2026