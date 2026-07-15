# BuscaTextual

CLI em Go para encontrar arquivos por nome, por conteudo, ou pelos dois modos ao mesmo tempo.

Foi feito para resolver um problema simples de forma direta: informar uma pasta, escolher como buscar, filtrar por extensao se quiser, e gerar um relatorio auditavel com os resultados.

## Ideia do projeto

Este projeto e uma ideia simples para resolver um problema simples:

- buscar arquivos pelo nome
- buscar ocorrencias dentro do conteudo
- buscar recursivamente em subpastas
- salvar tudo em `.txt`
- deixar o codigo aberto e facil de auditar

O projeto foi escrito em Go e esta pronto para uso.

## Liberdade de uso

Este projeto e de uso 100% livre para:

- uso pessoal
- estudo
- modificacao e derivacao
- redistribuicao
- uso comercial

Em resumo: voce pode usar, estudar, modificar, vender, incorporar em outro projeto ou adaptar como quiser.

Veja a licenca em [LICENSE](LICENSE).

## O que o programa faz

* **Interface Colorida (ANSI)**: Possui menus estilizados e destaca os termos buscados em vermelho negrito no terminal (tanto no caminho do arquivo quanto no trecho correspondente).
* **Busca Concorrente**: Realiza busca em disco usando múltiplos workers em paralelo para aproveitar a CPU sem sobrecarregar a leitura física.
* **Banco de Dados Local (BoltDB)**: Indexa os caminhos e nomes de arquivos no banco `buscatextual.db` para buscas instantâneas de nomes sem ler o disco.
* **Autotuning por Disco**: No primeiro crawler em uma unidade de disco (ex: `C:`, `D:`), o programa realiza um benchmark dinâmico (2, 4, 8 e 16 threads) para encontrar a concorrência ótima e salva o perfil no banco para indexações futuras.
* **Gravação Otimizada**: Indexação em segundo plano e escrita no BoltDB em lotes periódicos de 5.000 arquivos, mantendo a RAM baixa e velocidade alta.
* **Abertura Passo a Passo**: Após a busca, permite iterar e abrir as pastas dos resultados diretamente no Gerenciador de Arquivos pressionando `Enter` (ou sair digitando `q`).
* **Relatórios Estruturados**: Salva relatórios detalhados contendo dados de metadados e resultados estruturados em formato **TOML** na pasta `resultados_busca/`.

## Arquivos incluidos no repositorio

Este repositorio inclui:

- codigo-fonte em Go
- `build.bat` para gerar o executavel no Windows
- `buscatextual.exe` pronto para baixar e usar

Assim, quem baixar do GitHub pode:

- usar o `.exe` direto
- ou compilar a partir do codigo

## Como usar

Você tem duas opções principais:

1. **Baixar e usar direto:** Baixe o arquivo `buscatextual.exe` disponível na raiz deste repositório e execute-o com dois cliques. Nenhuma instalação é necessária.
2. **Compilar localmente:** Baixe o código fonte (ou faça o clone do repositório) e compile o executável você mesmo, garantindo total transparência do código que está rodando.

O fluxo sera:

1. informar a pasta para busca
2. escolher o tipo de busca
3. escolher se busca em todos os arquivos ou por extensoes
4. informar os termos separados por `;`

Exemplo de termos:

```text
erro;cliente;pedido
```

Exemplo de extensoes:

```text
.txt;.log;.go
```

## Como compilar

### Windows com batch

```bat
build.bat
```

### Go direto

```bash
go build -o buscatextual.exe .
```

## Requisitos para compilar

- Go 1.22 ou superior

## Exemplo de saida

```text
Encontrado: Arquivo: C:\dados\app.log | Linha: 120 | Trecho: erro de conexao
Encontrado: Arquivo: C:\dados\cliente_abc.txt | Correspondencia no nome do arquivo
```

## Relatorio gerado

Os relatórios ficam salvos em:

```text
resultados_busca/
```

Nome de exemplo:

```text
resultado_busca_20260423_101115_161.toml
```

O relatório estruturado em formato **TOML** inclui:

* Data e hora de início.
* Pasta base da busca.
* Modo de busca.
* Termos de busca e filtros de extensão utilizados.
* Lista de ocorrências com caminho do arquivo, tipo de correspondência (nome ou conteúdo), linha e o trecho de texto.

## Codigo auditavel

O objetivo aqui nao e esconder logica nem criar complexidade desnecessaria.

O projeto foi mantido simples, direto e auditavel para que qualquer pessoa possa:

- ler o codigo rapidamente
- adaptar para seu proprio fluxo
- validar o que esta sendo feito
- recompilar sem dependencia pesada

## Publicacao

Se este repositorio estiver no GitHub, o ideal e manter publicados:

- codigo-fonte
- `build.bat`
- `buscatextual.exe`

Isso facilita tanto para quem quer so usar quanto para quem quer estudar ou modificar.
