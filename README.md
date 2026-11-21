# Skatt

Sistema local para conciliação de faturas de cartão de crédito compartilhado entre múltiplas pessoas, múltiplos bancos e múltiplos anos, com separação de compras por centavo exato, controle de saldo devedor acumulado, emissão de recibos e backup portátil em arquivo único.

---

## Contexto e Motivação

O compartilhamento de cartões de crédito entre familiares e terceiros acumula distorções ao longo de meses e anos: compras parceladas atravessam ciclos, estornos abatem valores parciais, uma mesma compra precisa ser dividida em proporções exatas e cobranças repetidas no mesmo dia e valor (como corridas de transporte) quebram importadores ingênuos que deduplicam por heurística.

O **Skatt** resolve o fechamento mensal de ponta a ponta em um ambiente 100% local, sem dependência de serviços em nuvem para armazenamento e sem exposição de dados financeiros fora da máquina do operador.

---

## Arquitetura e Decisões de Engenharia

O sistema é empacotado como um único binário Go que serve a API REST (`/api/*`) e a interface web compilada via `embed.FS`:

- **Backend (`Go 1.27`, `net/http`):** módulo `github.com/FelipeSrutkoske/Skatt`, roteamento padrão da biblioteca `net/http` e desligamento gracioso (`SIGINT`/`SIGTERM`).
- **Persistência (`modernc.org/sqlite`):** SQLite em Go puro (sem dependência de CGO), operando com `PRAGMA journal_mode = WAL`, `PRAGMA foreign_keys = ON` e migrações SQL versionadas e embarcadas no binário.
- **Frontend (`React 19`, `TypeScript`, `Vite`, `Tailwind CSS v4`):** SPA compilada para `internal/web/dist` e embutida diretamente no executável final, com fonte `Inter` empacotada localmente e fundo WebGL/GLSL de baixo contraste.
- **Isolamento de Dados (`DATA_DIR`):** todos os dados mutáveis (banco `finance.db`, extratos originais em `uploads/`, área temporária `staging/` e snapshots `backups/`) residem exclusivamente no diretório `./data`, ignorado pelo controle de versão.
- **Aritmética Financeira Exata:** todos os valores monetários são armazenados, somados e validados estritamente em centavos inteiros (`int64`), eliminando desvios de ponto flutuante.
- **Unicidade Posicional:** cada lançamento dentro de uma fatura preserva `line_index` sequencial (`UNIQUE(statement_id, line_index)`), garantindo que compras legítimas no mesmo estabelecimento, data e valor nunca sejam descartadas na importação.

---

## Capacidades do Sistema

### Pipeline de Importação em Duas Fases
1. **Extração Determinística Local (`POST /api/imports/preview`):**
   - Parsers dedicados para **Nubank** (CSV/PDF), **Inter** (CSV/PDF), **Mercado Pago** (PDF), **Itaú** (CSV/PDF) e **OFX** (`<STMTTRN>`), além de lançamento manual sem arquivo.
   - Processamento de PDFs via `pdftotext -layout` executado diretamente sem shell intermediário.
   - Detecção automática de linhas de pagamento de fatura (marcadas como ignoradas para não distorcer despesas), preservação de estornos negativos e extração de número de parcelas (`02/06`, `Parcela 2 de 6`).
   - Proteção contra duplicação no nível da fatura (`SHA-256` do arquivo e verificação de `banco + mês + ano`), oferecendo substituição limpa ou registro como cartão adicional.
2. **Desbloqueio de PDF em Memória (`POST /api/imports/preview/{stagingId}/unlock`):**
   - PDFs protegidos por senha solicitam a chave na pré-visualização, são lidos via `pdftotext -upw` em memória sem persistência ou registro em log e mantêm o arquivo original preservado em disco.
3. **Extração Assistida sob Consentimento (`POST /api/imports/preview/{stagingId}/ai-extract`):**
   - Nunca acionada automaticamente. Caso os parsers determinísticos não reconheçam o layout e haja camada de texto extraível, o operador escolhe explicitamente entre preenchimento manual ou envio via API Gemini (`gemini-2.5-flash` por padrão).
   - Antes de qualquer envio externo, CPF, sequências de cartão e CEP/endereços são mascarados localmente por tokens neutros.
4. **Revisão Obrigatória (`POST /api/imports/confirm`):**
   - Conferência em tempo real entre soma dos lançamentos e total declarado da fatura antes de efetivar a gravação no banco.

### Classificação Ágil e Divisão por Centavo
- **Painel Anual (12 Meses):** visão anual (`2024`, `2025`, `2026`) com filtro em switch por banco e grade simétrica dos 12 meses exibindo totais, barra proporcional por participante e atalhos diretos para abertura ou importação no período.
- **Atribuição em 1 Clique:** seletor segmentado colorido por pessoa com atualização otimista instantânea e escolha personalizada de cor por participante.
- **Divisão Múltipla (`splits`):** permite repartir um único lançamento entre múltiplas pessoas por valor exato em reais ou divisão igualitária com distribuição determinística de centavo residual. O backend garante `SUM(splits.amount_cents) == transaction.amount_cents` e impede participante repetido na mesma compra.
- **Aprendizado de Regras (`assignment_rules`):** atribuições individuais registram o estabelecimento normalizado (sem sufixo de parcela) para sugerir automaticamente o responsável em faturas e parcelas futuras.

### Controle de Dívida, Recibos e Backup
- **Extrato por Pessoa:** consolidação histórica e mensal com cálculo de saldo acumulado (`total gasto - total pago`) e status mensal (`sem_gastos`, `pendente`, `pago_parcial`, `pago`).
- **Recibos:** disponibilidade mensal pré-calculada por pessoa/ano/banco, renderização HTML com `@media print` para PDF, resumo em texto plano para cópia rápida e envio direto por e-mail via SMTP configurável (`STARTTLS`, `TLS` ou sem criptografia). Recibos de banco único exibem apenas os itens e subtotal do banco; recibos consolidados abatem pagamentos do mês e permitem incluir saldo anterior.
- **Backup e Restauração (`.zip`):** exportação consistente usando `VACUUM INTO` empacotando `finance.db`, arquivos originais (`uploads/`) e `manifest.json`. Por padrão (`include_secrets=false`), `smtp_pass` e `ai_api_key` são removidos do snapshot exportado. A restauração valida contra *zip-slip*, verifica `PRAGMA integrity_check`, salva cópia pré-restauração em `data/backups/` e preserva credenciais locais quando o pacote importado não inclui segredos.

---

## Estrutura do Repositório

```text
.
├── cmd/server/            Ponto de entrada HTTP e gerenciamento de ciclo de vida
├── internal/
│   ├── ai/                Cliente HTTP Gemini sob demanda com mascaramento defensivo
│   ├── api/               Handlers REST, validação de payload e fallback SPA
│   ├── backup/            Exportação consistente (VACUUM INTO) e restauração segura
│   ├── db/                Conexão SQLite, PRAGMAs e migrações embarcadas
│   ├── domain/            Entidades de domínio e aritmética monetária em centavos
│   ├── parser/            Parsers CSV/PDF/OFX, normalização, PII e staging
│   ├── receipt/           Templates HTML, texto plano e cliente SMTP
│   ├── store/             Regras transacionais, ledger, dashboard e settings
│   └── web/               Assets estáticos compilados embutidos via embed.FS
├── frontend/              Aplicação React + TypeScript + Vite + Tailwind CSS
├── docs/                  Especificação técnica e plano de implementação
├── Dockerfile             Build multi-stage (Node -> Go -> Debian slim + poppler-utils)
└── docker-compose.yml     Orquestração local na porta 8080 com volume ./data
```

---

## Execução e Desenvolvimento

### Via Docker Compose (Recomendado)

```bash
docker compose up --build
```

A aplicação ficará disponível em `http://localhost:8080`, com todos os dados persistidos localmente em `./data`.

### Execução Local (Sem Docker)

Requisitos: `Go 1.27+`, `Node.js 22+` e `poppler-utils` (`pdftotext` disponível no `PATH`).

```bash
cd frontend && npm ci && npm run build && cd ..
go run ./cmd/server
```

Variáveis de ambiente suportadas:
- `PORT`: porta HTTP de escuta (padrão: `8080`).
- `DATA_DIR`: diretório de persistência local (padrão: `./data`).

### Testes e Verificação

Backend (suíte completa com detector de data race e análise estática):

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
```

Frontend (testes unitários e verificação estrita de tipos):

```bash
cd frontend
npm test -- --run
npx tsc --noEmit
```

---

## Propriedade e Direitos Autorais

Copyright (c) 2026 Felipe Srutkoske. Todos os direitos reservados.

Este repositório é disponibilizado exclusivamente para uso pessoal do autor e consulta técnica. Nenhuma licença é concedida para cópia, reprodução, redistribuição, modificação, sublicenciamento ou uso comercial, total ou parcial, sem autorização prévia e expressa por escrito do autor.
