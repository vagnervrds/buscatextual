# BuscaTextual

O **BuscaTextual** é uma ferramenta de linha de comando (CLI) desenvolvida em Go cujo objetivo primário é proporcionar uma **busca por arquivos de forma extremamente rápida**, resultado que temos alcançado com excelência tanto em SSDs quanto em HDDs mecânicos de alta capacidade.

Permite encontrar arquivos por nome, por conteúdo ou por ambos os modos simultaneamente, com filtros inteligentes, paralelismo adaptativo e geração de relatórios auditáveis.

## Ideia do projeto

O projeto foi concebido para resolver a necessidade de localizar arquivos e dados com rapidez máxima e simplicidade operacional:

- Buscar arquivos pelo nome de forma instantânea através de índice local.
- Buscar ocorrências de texto dentro do conteúdo dos arquivos.
- Buscar recursivamente em subpastas com concorrência otimizada.
- Salvar relatórios estruturados em `.csv`, `.json` ou `.toml` (com `.csv` como padrão).
- Manter o código 100% aberto, transparente e fácil de auditar.

O projeto foi escrito em Go e está pronto para uso imediato.

## Liberdade de uso

Este projeto é de uso 100% livre para:

- Uso pessoal
- Estudo
- Modificação e derivação
- Redistribuição
- Uso comercial

Em resumo: você pode usar, estudar, modificar, vender, incorporar em outro projeto ou adaptar como quiser.

Veja a licença em [LICENSE](LICENSE).

## Desempenho e Performance

O projeto foi projetado com foco em velocidade máxima e eficiência no uso de recursos:

* **Busca e Indexação Ultrarrápidas**: O programa entrega busca e indexação na casa de **1 a 3 segundos**, mesmo em discos rígidos mecânicos de alta capacidade (testado com sucesso em **HD de 6TB**).
* **Paralelismo Adaptativo**: A calibragem automática de threads avalia a velocidade de resposta do hardware em tempo real para extrair o máximo de SSDs rápidos e evitar travamentos por movimentação excessiva da agulha de leitura (*disk thrashing*) em HDDs tradicionais.

## O que o programa faz

* **Interface Colorida (ANSI)**: Possui menus estilizados e destaca os termos buscados em vermelho negrito no terminal (tanto no caminho do arquivo quanto no trecho correspondente).
* **Busca Concorrente**: Realiza busca em disco usando múltiplos workers em paralelo para aproveitar o poder da CPU sem sobrecarregar a leitura física.
* **Banco de Dados Local (BoltDB)**: Indexa os caminhos e nomes de arquivos no banco local `buscatextual.db` para buscas instantâneas por nome, sem necessidade de reler todo o disco.
* **Autotuning por Disco**: Na primeira varredura em uma unidade de disco (ex.: `C:`, `D:`), o programa realiza um benchmark dinâmico (2, 4, 8 e 16 threads) para determinar a concorrência ideal e salva o perfil para indexações futuras.
* **Gravação Otimizada**: Indexação em segundo plano com escrita no BoltDB em lotes periódicos de 5.000 arquivos, garantindo baixo consumo de memória RAM e alta velocidade.
* **Navegação Passo a Passo**: Após a exibição dos resultados, permite abrir diretamente a pasta de cada item no Gerenciador de Arquivos pressionando `Enter` (ou sair pressionando `q`).
* **Relatórios Estruturados**: Salva relatórios detalhados com metadados da busca e ocorrências encontradas nos formatos **CSV**, **JSON** ou **TOML** (com a preferência salva no banco de dados e **CSV** definido como padrão).

## Arquivos incluídos no repositório

Este repositório inclui:

- Código-fonte em Go.
- `build.bat` para compilar o executável no Windows com facilidade.
- `buscatextual.exe` pronto para baixar e executar.

Assim, você pode escolher:

- Usar o executável pré-compilado `.exe` diretamente.
- Ou compilar a partir do código-fonte.

## Como usar

Você tem duas opções principais:

1. **Baixar e usar diretamente:** Baixe o arquivo `buscatextual.exe` disponível na raiz deste repositório e execute-o. Nenhuma instalação adicional é necessária.
2. **Compilar localmente:** Obtenha o código-fonte (via download ou `git clone`) e compile o executável você mesmo, garantindo total transparência sobre o código em execução.

O fluxo de uso é simples:

1. Informar a pasta base para a busca.
2. Escolher o tipo de busca (Nome, Conteúdo ou Ambos).
3. Definir se a busca será em todos os arquivos ou restrita a extensões específicas.
4. Informar os termos de busca separados por `;`.

Exemplo de termos:

```text
erro;cliente;pedido
```

Exemplo de extensões:

```text
.txt;.log;.go
```

## Como compilar

### Windows via batch

```bat
build.bat
```

### Go direto via terminal

```bash
go build -o buscatextual.exe .
```

## Requisitos para compilar

- Go 1.22 ou superior

## Exemplo de saída

```text
Encontrado: Arquivo: C:\dados\app.log | Linha: 120 | Trecho: erro de conexão
Encontrado: Arquivo: C:\dados\cliente_abc.txt | Correspondência no nome do arquivo
```

## Relatórios gerados

Os relatórios são salvos na pasta:

```text
resultados_busca/
```

Exemplo de nome de arquivo:

```text
resultado_busca_20260423_101115_161.csv
```

O relatório estruturado inclui:

* Data e hora de início da busca.
* Pasta base da busca.
* Modo de busca utilizado.
* Termos pesquisados e filtros de extensão aplicados.
* Lista detalhada de ocorrências contendo o caminho do arquivo, tipo de correspondência (nome ou conteúdo), linha e trecho encontrado.

## Código auditável

O objetivo do projeto não é esconder lógica nem criar complexidade desnecessária.

A estrutura foi mantida simples, direta e auditável para que qualquer pessoa possa:

- Ler e compreender o código rapidamente.
- Adaptar as rotinas para seu próprio fluxo de trabalho.
- Validar o processamento de dados realizado.
- Recompilar sem dependências externas pesadas.

## Publicação

Neste repositório no GitHub, mantemos publicados:

- O código-fonte completo.
- O script de automação de compilação `build.bat`.
- O executável `buscatextual.exe` pré-compilado.

Isso facilita tanto para quem busca uma ferramenta pronta para uso imediato quanto para desenvolvedores que desejam estudar ou customizar a solução.
