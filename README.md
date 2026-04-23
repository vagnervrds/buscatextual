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

- pede a pasta base da busca
- pergunta se a busca sera por nome, conteudo ou ambos
- permite buscar em todos os arquivos ou filtrar por extensao
- recebe termos separados por `;`
- faz a busca recursiva
- usa multiplos workers para aproveitar melhor o processador sem exagerar na leitura de disco
- mostra o andamento no terminal
- mostra cada ocorrencia encontrada durante a execucao
- cria uma subpasta `resultados_busca`
- cria o arquivo de relatorio no inicio da execucao com data, hora e milissegundos
- vai adicionando os resultados no `.txt` conforme encontra
- mostra o relatorio completo no final e espera `Enter` antes de fechar

## Arquivos incluidos no repositorio

Este repositorio inclui:

- codigo-fonte em Go
- `build.bat` para gerar o executavel no Windows
- `buscatextual.exe` pronto para baixar e usar

Assim, quem baixar do GitHub pode:

- usar o `.exe` direto
- ou compilar a partir do codigo

## Como usar o executavel

Baixe `buscatextual.exe` e execute.

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

Os relatorios ficam em:

```text
resultados_busca/
```

Nome de exemplo:

```text
resultado_busca_20260423_101115_161.txt
```

O relatorio inclui:

- data e hora de inicio
- pasta base
- modo de busca
- termos
- filtro de extensoes
- lista das ocorrencias encontradas

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
