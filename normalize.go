package main

import (
	"bytes"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// Transformer reutilizável para remoção de diacríticos Unicode (classe Mn)
	tRemoveMn = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

	// Pool de buffers para evitar alocações excessivas durante varreduras em larga escala
	bufferPool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}
)

// normalizeQuickASCII realiza conversão rápida para minúsculas se a string for estritamente ASCII.
// Retorna (string normalizada, bool indicando se é puramente ASCII).
func normalizeQuickASCII(s string) (string, bool) {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf {
			return "", false // Possui caracteres não-ASCII (acentos, multibyte)
		}
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
	}
	if !hasUpper {
		return s, true
	}
	// Se for apenas ASCII com maiúsculas, converte diretamente
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte(c + ('a' - 'A'))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

// normalizeUnicode remove acentos/diacríticos e converte para minúsculas usando transformador NFD
func normalizeUnicode(s string) string {
	// Primeiro converte para minúsculas
	lower := strings.ToLower(s)

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	wr := transform.NewWriter(buf, tRemoveMn)
	_, _ = wr.Write([]byte(lower))
	_ = wr.Close()

	return buf.String()
}

// NormalizeText normaliza uma string de acordo com o modo ("ampla" ou "exata").
// No modo "ampla": minúsculas e sem acentos ("Vitória" -> "vitoria").
// No modo "exata": mantém o texto original sem alterações.
func NormalizeText(s string, mode string) string {
	if mode == "exata" {
		return s
	}

	// Modo Amplo (padrão):
	// 1. Fast-path para strings puramente ASCII
	if normalized, ok := normalizeQuickASCII(s); ok {
		return normalized
	}

	// 2. Caminho Unicode com remoção de acentos e minúsculas
	return normalizeUnicode(s)
}

// NormalizeTerms normaliza uma lista de termos de acordo com o modo
func NormalizeTerms(terms []string, mode string) []string {
	res := make([]string, 0, len(terms))
	for _, term := range terms {
		trimmed := strings.TrimSpace(term)
		if trimmed != "" {
			res = append(res, NormalizeText(trimmed, mode))
		}
	}
	return res
}

// NormalizedRuneMap mapeia cada índice de caractere normalizado de volta para a posição (byteStart, byteEnd) na string original
type NormalizedRuneMap struct {
	OriginalText string
	NormText     string
	// normToOrigStart mapeia o índice de byte da string normalizada para o índice de byte na string original
	normToOrigStart []int
	// normToOrigEnd mapeia o índice de byte da string normalizada para o índice de byte final na string original
	normToOrigEnd []int
}

// BuildNormalizedMap constrói uma correspondência entre o texto original e o texto normalizado
func BuildNormalizedMap(s string, mode string) NormalizedRuneMap {
	if mode == "exata" {
		normText := s
		n := len(normText)
		starts := make([]int, n+1)
		ends := make([]int, n+1)
		for i := 0; i <= n; i++ {
			starts[i] = i
			ends[i] = i
		}
		return NormalizedRuneMap{
			OriginalText:    s,
			NormText:        normText,
			normToOrigStart: starts,
			normToOrigEnd:   ends,
		}
	}

	// Se for puramente ASCII, cada byte corresponde 1:1
	if _, ok := normalizeQuickASCII(s); ok {
		normText := strings.ToLower(s)
		n := len(normText)
		starts := make([]int, n+1)
		ends := make([]int, n+1)
		for i := 0; i <= n; i++ {
			starts[i] = i
			ends[i] = i
		}
		return NormalizedRuneMap{
			OriginalText:    s,
			NormText:        normText,
			normToOrigStart: starts,
			normToOrigEnd:   ends,
		}
	}

	// Caso Unicode: analisa rune a rune
	var normBuilder strings.Builder
	var starts []int
	var ends []int

	for origByteIdx, r := range s {
		origRuneLen := utf8.RuneLen(r)
		origEnd := origByteIdx + origRuneLen

		// Normaliza esta rune individual
		normR := NormalizeText(string(r), "ampla")
		for range []byte(normR) {
			starts = append(starts, origByteIdx)
			ends = append(ends, origEnd)
		}
		normBuilder.WriteString(normR)
	}

	// Adiciona sentinela de fim
	starts = append(starts, len(s))
	ends = append(ends, len(s))

	return NormalizedRuneMap{
		OriginalText:    s,
		NormText:        normBuilder.String(),
		normToOrigStart: starts,
		normToOrigEnd:   ends,
	}
}

// FindMatchIntervals localiza os intervalos (start, end) no texto ORIGINAL onde os termos aparecem
func FindMatchIntervals(text string, terms []string, mode string) []interval {
	if len(terms) == 0 || text == "" {
		return nil
	}

	normMap := BuildNormalizedMap(text, mode)
	var intervals []interval

	for _, term := range terms {
		normTerm := NormalizeText(term, mode)
		if normTerm == "" {
			continue
		}

		pos := 0
		for {
			idx := strings.Index(normMap.NormText[pos:], normTerm)
			if idx == -1 {
				break
			}
			normStart := pos + idx
			normEnd := normStart + len(normTerm)

			if normStart < len(normMap.normToOrigStart) && normEnd <= len(normMap.normToOrigEnd) {
				origStart := normMap.normToOrigStart[normStart]
				origEnd := normMap.normToOrigEnd[normEnd-1]
				if origEnd > origStart {
					intervals = append(intervals, interval{start: origStart, end: origEnd})
				}
			}

			pos = normStart + len(normTerm)
		}
	}

	return intervals
}
