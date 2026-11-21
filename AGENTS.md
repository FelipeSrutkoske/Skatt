# Regras do Projeto (AGENTS.md)

Este arquivo define as diretrizes permanentes para qualquer agente ou colaborador trabalhando neste repositório. Atualize este documento sempre que o usuário solicitar novas regras.

## Comunicação e Documentação
- Sem emotes ou linguagem artificial com cara de IA ("slop") na comunicação, documentação ou interface.
- Evite criar arquivos `.md` desnecessários. Qualquer documento `.md` novo deve ser criado estritamente dentro do diretório `docs/` (exceções na raiz apenas para arquivos estruturais solicitados explicitamente, como `AGENTS.md`, `REQUIREMENTS.md` e `README.md`).

## Código
- Sem comentários no código-fonte (Go, TypeScript, CSS, SQL, scripts). O código deve ser claro e autoexplicativo por meio de nomes precisos e funções bem estruturadas.

## Design e Interface (UI)
- Adote um visual sóbrio, direto e utilitário (estilo planilha/ferramenta financeira limpa e densa em informação).
- Evite padrões visuais genéricos de IA:
  - Sem ícones decorativos espalhados por títulos, botões ou métricas.
  - Sem "card dentro de card" (evite caixas aninhadas e excesso de bordas/sombras).
  - Sem excesso de badges/pílulas coloridas de status (verde, vermelho, amarelo berrantes); prefira tipografia, alinhamento tabular (`tabular-nums`), hierarquia sutil e contraste neutro.

## Permissões e Execução de Comandos
- Peça permissão apenas para ações destrutivas (ex: apagar dados persistentes, `rm -rf`, reset/force-push no git, sobrescrever banco de produção).
- Comandos somente leitura e de inspeção (`find`, `ls`, `cat`, `git status`, `git diff`, `go test`, builds locais) podem ser executados diretamente sem pedir permissão.
