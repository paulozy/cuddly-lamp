# Testing: GitHub Integration (Sync)

## Pré-requisitos

- Docker em execução
- Um GitHub Personal Access Token com escopos `repo` e `admin:repo_hook`
- [ngrok](https://ngrok.com/download) instalado (necessário para registro de webhook no GitHub)

---

## 1. Expor o servidor com ngrok

O GitHub precisa de uma URL pública para enviar eventos de webhook. Com o servidor rodando em `localhost:3000`, execute em um terminal separado:

```bash
ngrok http 3000
```

Copie a URL HTTPS exibida (ex: `https://abc123.ngrok-free.app`) — ela muda a cada reinício do ngrok.

---

## 2. Configurar o `.env`

```bash
cp .env.example .env
```

Adicione/preencha no `.env`:

```env
GITHUB_TOKEN=ghp_seu_token_aqui
WEBHOOK_BASE_URL=https://abc123.ngrok-free.app   # URL do ngrok, sem barra no final
```

> **Sem `GITHUB_TOKEN`**: o worker aceita os jobs mas falha com `ErrUnauthorized` e marca `sync_status = "error"`.
> **`WEBHOOK_BASE_URL` com localhost**: o registro do webhook no GitHub é pulado automaticamente (GitHub rejeita URLs locais). O sync de metadata funciona normalmente.
> **Sem `WEBHOOK_BASE_URL`**: o sync de metadata funciona, mas o webhook não é registrado no GitHub.

---

## 3. Subir banco e Redis

```bash
make docker-up
```

Aguarde os healthchecks passarem antes de continuar.

---

## 4. Rodar o servidor

```bash
make dev
```

---

## 5. Fluxo de teste completo

### 5.1 Registrar um usuário

```bash
curl -s -X POST http://localhost:3000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"senha123"}' | jq
```

### 5.2 Login — capturar o access token

```bash
TOKEN=$(curl -s -X POST http://localhost:3000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"senha123"}' \
  | jq -r '.access_token')

echo "TOKEN: $TOKEN"
```

### 5.3 Criar um repositório (dispara sync automático)

```bash
REPO=$(curl -s -X POST http://localhost:3000/api/v1/repositories \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://github.com/paulozy/idp-with-ai-backend"}')

echo $REPO | jq
REPO_ID=$(echo $REPO | jq -r '.id')
echo "REPO_ID: $REPO_ID"
```

### 5.4 Verificar o sync no banco

Aguarde alguns segundos para o worker processar o job e então consulte:

```bash
docker exec idp-postgres psql -U postgres -d idp_dev -c "
  SELECT id, name, sync_status, last_synced_at,
         metadata->>'branch_count' AS branches,
         metadata->>'commit_count' AS commits,
         metadata->>'languages'    AS languages
  FROM repositories
  WHERE id = '$REPO_ID';
"
```

**Esperado:** `sync_status = synced`, campos de metadata preenchidos.

### 5.5 Verificar o webhook registrado no banco

```bash
docker exec idp-postgres psql -U postgres -d idp_dev -c "
  SELECT repository_id, webhook_url, provider_webhook_id, is_active
  FROM webhook_configs
  WHERE repository_id = '$REPO_ID';
"
```

**Esperado:** uma linha com `is_active = true` e `provider_webhook_id` preenchido (ID do webhook criado no GitHub).

Você também pode confirmar no GitHub em **Settings → Webhooks** do repositório.

---

## 6. Testar o webhook endpoint

> Este passo requer que o sync tenha criado um `WebhookConfig` no banco (só acontece quando `WEBHOOK_BASE_URL` aponta para uma URL pública).

### 6.1 Buscar o secret do webhook

```bash
SECRET=$(docker exec idp-postgres psql -U postgres -d idp_dev -t -c \
  "SELECT secret FROM webhook_configs WHERE repository_id = '$REPO_ID';" \
  | tr -d ' \n')

echo "SECRET: $SECRET"
```

### 6.2 Enviar um evento push com assinatura válida

```bash
BODY='{"ref":"refs/heads/main","repository":{"id":123}}'
SIG="sha256=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"
DELIVERY_ID="test-delivery-$(date +%s)"

curl -s -X POST "http://localhost:3000/api/v1/webhooks/github/$REPO_ID" \
  -H "X-Hub-Signature-256: $SIG" \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: $DELIVERY_ID" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

**Esperado:** HTTP 202 (Accepted).

### 6.3 Testar idempotência (mesmo delivery ID)

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST "http://localhost:3000/api/v1/webhooks/github/$REPO_ID" \
  -H "X-Hub-Signature-256: $SIG" \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: $DELIVERY_ID" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

**Esperado:** HTTP 200 (já processado, retorno idempotente).

### 6.4 Testar assinatura inválida

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST "http://localhost:3000/api/v1/webhooks/github/$REPO_ID" \
  -H "X-Hub-Signature-256: sha256=invalida" \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: delivery-invalida" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

**Esperado:** HTTP 401.

### 6.5 Verificar os eventos recebidos no banco

```bash
docker exec idp-postgres psql -U postgres -d idp_dev -c "
  SELECT id, event_type, status, delivery_id, created_at
  FROM webhooks
  WHERE repository_id = '$REPO_ID'
  ORDER BY created_at DESC
  LIMIT 5;
"
```

---

## Sem Redis (modo degradado)

Com Redis indisponível, o servidor ainda funciona:
- Jobs são logados como `WARN: Noop enqueuer: job dropped`
- `sync_status` permanece `idle` (nenhum sync acontece)
- Endpoint de webhook retorna 202 mas o job não é processado
