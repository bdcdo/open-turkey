package blocker

import (
	"reflect"
	"testing"
)

// TestNormalizarDominio cobre a normalização de entradas variadas para o host
// canônico que alimenta a geração de padrões de bloqueio. São casos de lógica
// pura — sem I/O nem root — que valem para todos os navegadores, inclusive o
// Brave, já que todos derivam dos mesmos padrões.
func TestNormalizarDominio(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  string
		esperado string
	}{
		{"dominio simples", "facebook.com", "facebook.com"},
		{"maiusculas viram minusculas", "Facebook.COM", "facebook.com"},
		{"espacos em volta", "  facebook.com  ", "facebook.com"},
		{"url completa extrai o host", "https://www.facebook.com/feed", "www.facebook.com"},
		{"caminho sem scheme e descartado", "example.com/foo/bar", "example.com"},
		{"porta e removida", "example.com:443", "example.com"},
		{"url com porta extrai host sem a porta", "https://example.com:8080/x", "example.com"},
		{"ponto final e removido", "example.com.", "example.com"},
		{"string vazia", "", ""},
		{"apenas espacos", "   ", ""},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			obtido := NormalizarDominio(c.entrada)
			if obtido != c.esperado {
				t.Errorf("NormalizarDominio(%q) = %q; esperado %q", c.entrada, obtido, c.esperado)
			}
		})
	}
}

// TestGerarPadroesURL valida a geração dos padrões *://.../* a partir dos
// domínios: os dois padrões por domínio (raiz e wildcard de subdomínio), o
// colapso do prefixo "www.", a deduplicação e o resultado ordenado.
func TestGerarPadroesURL(t *testing.T) {
	casos := []struct {
		nome     string
		dominios []string
		esperado []string
	}{
		{
			nome:     "dominio raiz gera os dois padroes",
			dominios: []string{"facebook.com"},
			esperado: []string{"*://*.facebook.com/*", "*://facebook.com/*"},
		},
		{
			nome:     "prefixo www colapsa no wildcard sem www",
			dominios: []string{"www.facebook.com"},
			esperado: []string{"*://*.facebook.com/*", "*://www.facebook.com/*"},
		},
		{
			nome:     "raiz e www compartilham e deduplicam o wildcard",
			dominios: []string{"facebook.com", "www.facebook.com"},
			esperado: []string{"*://*.facebook.com/*", "*://facebook.com/*", "*://www.facebook.com/*"},
		},
		{
			nome:     "dominio repetido nao duplica padroes",
			dominios: []string{"facebook.com", "facebook.com"},
			esperado: []string{"*://*.facebook.com/*", "*://facebook.com/*"},
		},
		{
			nome:     "entradas vazias sao ignoradas",
			dominios: []string{"", "   "},
			esperado: []string{},
		},
		{
			nome:     "sem dominios resulta em lista vazia",
			dominios: nil,
			esperado: []string{},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			obtido := gerarPadroesURL(c.dominios)
			if !reflect.DeepEqual(obtido, c.esperado) {
				t.Errorf("gerarPadroesURL(%v) = %v; esperado %v", c.dominios, obtido, c.esperado)
			}
		})
	}
}
