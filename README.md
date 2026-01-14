# Verity

**Verificador de Factos com Inteligência Artificial**

![Verity](https://img.shields.io/badge/version-1.0.0-blue.svg)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8.svg)
![License](https://img.shields.io/badge/license-MIT-green.svg)

## 🎯 Sobre

Verity é um sistema de verificação de factos alimentado por IA que analisa texto, URLs e documentos para verificar a veracidade das afirmações. Utiliza modelos de linguagem avançados (GPT-4) combinados com pesquisa multi-fonte para fornecer pontuações de confiança detalhadas.

## ✨ Funcionalidades

- **Extração Automática de Afirmações** - A IA identifica afirmações verificáveis do texto
- **Pesquisa Multi-Fonte** - Cruza referências da Wikipédia, PubMed e outras fontes académicas
- **Pontuação de Confiança** - Pontuações detalhadas para cada afirmação (0-10)
- **Suporte Multi-formato** - Texto, URLs e ficheiros (PDF, DOCX, TXT)
- **Interface em Português** - UI completamente traduzida para português
- **API REST** - Integração fácil com outros sistemas

## 🚀 Início Rápido

### Pré-requisitos

- Go 1.24+
- Chave API OpenAI

### Instalação

```bash
# Clonar o repositório
git clone https://github.com/gdlf13/verity.git
cd verity

# Copiar configuração
cp verity.yaml.example verity.yaml

# Editar verity.yaml e adicionar a sua chave OpenAI
# openai_api_key: "sk-..."

# Compilar
go build -o verity ./cmd/verity

# Executar
./verity
```

### Aceder à Aplicação

Abra o browser em `http://localhost:8080`

## 📖 Utilização

### Interface Web

1. Introduza a sua chave API ou gere uma nova
2. Escolha o tipo de entrada (Texto, URL ou Ficheiro)
3. Cole o texto ou carregue o documento
4. Clique em "Verificar Agora"
5. Analise os resultados com pontuações de confiança

### API REST

```bash
# Verificar texto
curl -X POST http://localhost:8080/api/v1/verify \
  -H "Content-Type: application/json" \
  -H "X-API-Key: vrt_sua_chave" \
  -d '{"text": "O sol é uma estrela."}'

# Verificar URL
curl -X POST http://localhost:8080/api/v1/verify/url \
  -H "Content-Type: application/json" \
  -H "X-API-Key: vrt_sua_chave" \
  -d '{"url": "https://exemplo.com/artigo"}'
```

## 🏗️ Arquitetura

```
verity/
├── cmd/verity/          # Ponto de entrada da aplicação
├── internal/
│   ├── api/             # Handlers HTTP e rotas
│   ├── config/          # Configuração YAML
│   ├── database/        # Persistência SQLite
│   ├── llm/             # Integração OpenAI
│   ├── models/          # Estruturas de dados
│   ├── search/          # Clientes de pesquisa (Wikipedia, PubMed)
│   └── verify/          # Motor de verificação
├── web/static/          # Frontend (HTML/CSS/JS)
├── go.mod
├── go.sum
└── verity.yaml.example
```

## ⚙️ Configuração

Edite `verity.yaml`:

```yaml
server:
  port: 8080
  host: "0.0.0.0"

openai:
  api_key: "sk-..."
  model: "gpt-4o-mini"

database:
  driver: "sqlite"
  path: "./data/verity.db"

rate_limit:
  requests_per_minute: 60
```

## 🔒 Segurança

- Rate limiting por IP e chave API
- Validação de entrada
- Headers de segurança HTTP
- Sem armazenamento de dados sensíveis

## 📊 Fontes de Verificação

| Fonte | Tipo | Descrição |
|-------|------|-----------|
| Wikipedia | Enciclopédia | Conhecimento geral |
| PubMed | Académico | Artigos científicos e médicos |
| DuckDuckGo | Web | Pesquisa web geral |

## 🛠️ Desenvolvimento

```bash
# Executar em modo desenvolvimento
go run ./cmd/verity

# Executar testes
go test ./...

# Build para produção
CGO_ENABLED=1 go build -ldflags="-s -w" -o verity ./cmd/verity
```

## 📝 Licença

MIT License - veja [LICENSE](LICENSE) para detalhes.

## 🤝 Contribuir

Contribuições são bem-vindas! Por favor, abra uma issue ou pull request.

---

**Verity** — Verificação de factos IA de nível empresarial 🔍
