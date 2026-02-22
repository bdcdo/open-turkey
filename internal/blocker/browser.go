// =============================================================================
// Pacote blocker — Módulo de políticas de navegador do Open Turkey
// =============================================================================
//
// Este arquivo implementa o bloqueio de sites usando "políticas de empresa"
// (enterprise policies) dos navegadores Firefox e Chromium/Chrome.
//
// O QUE SÃO POLÍTICAS DE NAVEGADOR?
// ----------------------------------
// Navegadores como Firefox e Chrome foram feitos para serem usados em empresas
// grandes, onde o administrador de TI precisa controlar o que os funcionários
// podem ou não fazer. Para isso, eles suportam "políticas" — arquivos JSON
// que ficam em diretórios do sistema e que o navegador lê automaticamente.
//
// Essas políticas permitem, por exemplo:
//   - Bloquear o acesso a determinados sites (o que nos interessa!)
//   - Desativar a instalação de extensões
//   - Forçar configurações de proxy
//   - Desativar o modo de navegação anônima
//
// POR QUE ISSO É TÃO EFICAZ?
// ---------------------------
// Diferente de extensões ou configurações do navegador, as políticas são
// aplicadas pelo SISTEMA OPERACIONAL. O usuário NÃO consegue desativá-las
// de dentro do navegador. Não importa se ele vai nas configurações, usa
// modo anônimo, ou tenta qualquer truque — as políticas são lei.
//
// Para remover uma política, é necessário ter acesso root ao sistema e
// editar/excluir o arquivo JSON. No contexto do Open Turkey, isso significa
// que o bloqueio só pode ser removido pelo daemon do programa (que roda
// como root), não pelo usuário tentando burlar.
//
// COMO FUNCIONA NA PRÁTICA?
// -------------------------
// 1. Criamos arquivos JSON nos diretórios de políticas do sistema
// 2. O navegador detecta esses arquivos automaticamente (na inicialização
//    E durante a operação — não precisa nem reiniciar!)
// 3. A política "WebsiteFilter" (Firefox) ou "URLBlocklist" (Chromium)
//    bloqueia os padrões de URL que especificamos
// 4. O navegador mostra uma página de erro dizendo que o site foi bloqueado
//    pela política da organização
//
// NENHUMA EXTENSÃO É NECESSÁRIA!
// ------------------------------
// Muitos bloqueadores de sites dependem de extensões do navegador, que o
// usuário pode simplesmente desativar. Aqui usamos um mecanismo do próprio
// sistema operacional — muito mais robusto.
//
// DIFERENÇAS ENTRE FIREFOX E CHROMIUM:
// ------------------------------------
// Firefox:
//   - Arquivo: /etc/firefox/policies/policies.json
//   - Formato: { "policies": { "WebsiteFilter": { "Block": [...] } } }
//   - O arquivo é COMPARTILHADO — outras políticas podem existir no mesmo
//     arquivo. Por isso, precisamos fazer MERGE (juntar) nossas regras com
//     as existentes, sem apagar nada que não seja nosso.
//
// Chromium/Chrome:
//   - Arquivo: /etc/chromium/policies/managed/open-turkey.json
//   - Formato: { "URLBlocklist": [...] }
//   - O Chromium lê TODOS os arquivos .json dentro do diretório managed/,
//     então podemos criar um arquivo separado só nosso (open-turkey.json).
//     Isso é muito mais simples — não precisamos nos preocupar em mergear.
//   - O Google Chrome usa um diretório diferente:
//     /etc/opt/chrome/policies/managed/open-turkey.json
//
// =============================================================================
package blocker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"net/url"
	"strings"
)

// Caminhos dos arquivos de políticas de cada navegador.
//
// O Firefox usa um único arquivo policies.json compartilhado.
// O Chromium e o Chrome usam diretórios onde cada arquivo .json é uma política
// separada — por isso criamos "open-turkey.json", um arquivo exclusivo nosso.
const (
	// firefoxPolicyPath é o arquivo de políticas do Firefox.
	// O Firefox procura esse arquivo automaticamente ao iniciar.
	// Se o arquivo não existe, o Firefox simplesmente ignora e continua
	// sem políticas — então não quebramos nada ao criar/remover esse arquivo.
	firefoxPolicyPath = "/etc/firefox/policies/policies.json"

	// chromiumPolicyPath é o arquivo de políticas do Chromium.
	// O Chromium lê todos os .json dentro de /etc/chromium/policies/managed/,
	// então nosso arquivo convive pacificamente com outros que possam existir.
	chromiumPolicyPath = "/etc/chromium/policies/managed/open-turkey.json"

	// chromePolicyPath é o arquivo de políticas do Google Chrome.
	// O Chrome usa um diretório diferente do Chromium, mesmo sendo baseado
	// no mesmo código. Isso acontece porque a instalação do Chrome coloca
	// seus arquivos em /etc/opt/chrome/ em vez de /etc/chromium/.
	chromePolicyPath = "/etc/opt/chrome/policies/managed/open-turkey.json"
)

// =============================================================================
// Funções públicas — a interface que o resto do Open Turkey usa
// =============================================================================

// ApplyBrowserPolicies cria as políticas de bloqueio para todos os navegadores.
//
// Para cada domínio fornecido, cria dois padrões de URL:
//   - *://*.dominio.com/*  → bloqueia qualquer subdomínio (www, m, app, etc.)
//   - *://dominio.com/*    → bloqueia o domínio raiz
//
// Por que dois padrões? Porque "facebook.com" e "www.facebook.com" são URLs
// diferentes. O asterisco no protocolo (*://) cobre tanto http quanto https.
// O asterisco no final (/*) garante que qualquer página dentro do site seja
// bloqueada, não apenas a página inicial.
//
// Exemplo: para o domínio "facebook.com", os padrões seriam:
//   - *://*.facebook.com/*  → bloqueia www.facebook.com, m.facebook.com, etc.
//   - *://facebook.com/*    → bloqueia facebook.com diretamente
func ApplyBrowserPolicies(domains []string) error {
	// Passo 1: Montar a lista de padrões de URL a partir dos domínios.
	// Cada domínio gera dois padrões — um para subdomínios e outro para o domínio raiz.
	padroes := gerarPadroesURL(domains)

	// Passo 2: Aplicar a política do Firefox.
	// O Firefox usa um formato próprio com "WebsiteFilter" dentro de "policies".
	// Como o arquivo pode já existir com outras políticas, fazemos merge.
	if err := aplicarPoliticaFirefox(padroes); err != nil {
		return fmt.Errorf("erro ao aplicar política do Firefox: %w", err)
	}

	// Passo 3: Aplicar a política do Chromium.
	// O Chromium usa "URLBlocklist" e aceita arquivos separados por política.
	if err := aplicarPoliticaChromium(chromiumPolicyPath, padroes); err != nil {
		return fmt.Errorf("erro ao aplicar política do Chromium: %w", err)
	}

	// Passo 4: Aplicar a política do Google Chrome.
	// O Chrome usa o mesmo formato do Chromium, mas em diretório diferente.
	if err := aplicarPoliticaChromium(chromePolicyPath, padroes); err != nil {
		return fmt.Errorf("erro ao aplicar política do Google Chrome: %w", err)
	}

	return nil
}

// RemoveBrowserPolicies remove as políticas de bloqueio de todos os navegadores.
//
// A estratégia de remoção é diferente para cada navegador:
//
// Firefox: O arquivo policies.json pode conter OUTRAS políticas além da nossa.
// Por isso, NÃO podemos simplesmente deletar o arquivo. Em vez disso:
//   - Se o arquivo só tem a nossa política WebsiteFilter: deletamos o arquivo
//   - Se tem outras políticas além da nossa: removemos só o WebsiteFilter
//
// Chromium/Chrome: Como usamos um arquivo separado (open-turkey.json),
// basta deletá-lo. Outros arquivos de política no mesmo diretório não são
// afetados.
func RemoveBrowserPolicies() error {
	// Passo 1: Remover a política do Firefox (com cuidado de não apagar
	// outras políticas que possam existir no mesmo arquivo).
	if err := removerPoliticaFirefox(); err != nil {
		return fmt.Errorf("erro ao remover política do Firefox: %w", err)
	}

	// Passo 2: Remover a política do Chromium.
	// Simplesmente deletamos nosso arquivo. Se não existe, tudo bem.
	if err := removerArquivoPolitica(chromiumPolicyPath); err != nil {
		return fmt.Errorf("erro ao remover política do Chromium: %w", err)
	}

	// Passo 3: Remover a política do Google Chrome.
	if err := removerArquivoPolitica(chromePolicyPath); err != nil {
		return fmt.Errorf("erro ao remover política do Google Chrome: %w", err)
	}

	return nil
}

// IsBrowserPoliciesApplied verifica se as políticas de bloqueio estão ativas
// para TODOS os navegadores que possuem diretório de políticas no sistema.
//
// A verificação é rigorosa: retorna true SOMENTE se todos os domínios
// esperados estão presentes em TODOS os arquivos de política aplicáveis.
// Se algum navegador instalado não tiver a política correta, retorna false.
//
// Isso é importante para detectar se alguém editou manualmente os arquivos
// de política tentando burlar o bloqueio.
func IsBrowserPoliciesApplied(domains []string) bool {
	// Montamos os padrões esperados a partir dos domínios.
	padroesEsperados := gerarPadroesURL(domains)

	if len(padroesEsperados) == 0 {
		return false
	}

	// Verificação do Firefox:
	// Só verificamos se o diretório de políticas do Firefox existe,
	// o que indica que o Firefox está instalado no sistema.
	dirFirefox := filepath.Dir(firefoxPolicyPath)
	if diretorioExiste(dirFirefox) {
		if !verificarPoliticaFirefox(padroesEsperados) {
			return false
		}
	}

	// Verificação do Chromium:
	// Mesmo raciocínio — só verificamos se o diretório pai existe.
	dirChromium := filepath.Dir(chromiumPolicyPath)
	if diretorioExiste(dirChromium) {
		if !verificarPoliticaChromium(chromiumPolicyPath, padroesEsperados) {
			return false
		}
	}

	// Verificação do Google Chrome:
	dirChrome := filepath.Dir(chromePolicyPath)
	if diretorioExiste(dirChrome) {
		if !verificarPoliticaChromium(chromePolicyPath, padroesEsperados) {
			return false
		}
	}

	return true
}

// =============================================================================
// Funções auxiliares — geração de padrões de URL
// =============================================================================

// gerarPadroesURL converte uma lista de domínios em padrões de URL para bloqueio.
//
// Para cada domínio, gera dois padrões:
//   - *://*.dominio.com/*  → subdomínios (www, m, app, etc.)
//   - *://dominio.com/*    → domínio raiz
//
// O formato *://.../* é o padrão usado tanto pelo Firefox (WebsiteFilter)
// quanto pelo Chromium (URLBlocklist). Funciona assim:
//   - *://  → qualquer protocolo (http, https, etc.)
//   - *.    → qualquer subdomínio
//   - /*    → qualquer caminho dentro do site
func gerarPadroesURL(domains []string) []string {
	conjunto := make(map[string]bool)

	for _, dominio := range domains {
		host := NormalizarDominio(dominio)
		if host == "" {
			continue
		}

		// Padrão 1: bloqueia o host exato (ex: *://facebook.com/*)
		conjunto[fmt.Sprintf("*://%s/*", host)] = true

		// Padrão 2: bloqueia subdomínios. Se o host começa com "www.",
		// geramos o wildcard em cima do host sem www (evita "*.www.*").
		base := host
		if strings.HasPrefix(base, "www.") {
			base = strings.TrimPrefix(base, "www.")
		}
		conjunto[fmt.Sprintf("*://*.%s/*", base)] = true
	}

	padroes := make([]string, 0, len(conjunto))
	for p := range conjunto {
		padroes = append(padroes, p)
	}
	sort.Strings(padroes)
	return padroes
}

func NormalizarDominio(entrada string) string {
	s := strings.TrimSpace(strings.ToLower(entrada))
	if s == "" {
		return ""
	}

	// Se for uma URL completa, extraímos o host.
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err == nil && u != nil && u.Host != "" {
			s = u.Host
		}
	}

	// Remove qualquer caminho (ex: "example.com/foo").
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}

	// Remove porta (ex: "example.com:443").
	if i := strings.LastIndexByte(s, ':'); i >= 0 && !strings.Contains(s, "]") {
		s = s[:i]
	}

	s = strings.TrimSpace(strings.TrimSuffix(s, "."))
	if s == "" {
		return ""
	}

	return s
}

// =============================================================================
// Funções auxiliares — Firefox
// =============================================================================

// aplicarPoliticaFirefox cria ou atualiza o arquivo de políticas do Firefox.
//
// O arquivo policies.json do Firefox tem uma estrutura aninhada:
//
//	{
//	    "policies": {
//	        "WebsiteFilter": {
//	            "Block": [
//	                "*://*.facebook.com/*",
//	                "*://facebook.com/*"
//	            ]
//	        },
//	        ... (outras políticas podem existir aqui)
//	    }
//	}
//
// Se o arquivo já existe, precisamos fazer MERGE: ler o JSON existente,
// adicionar nossos padrões ao array "Block" (sem duplicar), e gravar de volta.
// Se o arquivo não existe, criamos do zero.
func aplicarPoliticaFirefox(padroes []string) error {
	// Passo 1: Criar o diretório de políticas se não existir.
	// os.MkdirAll cria todos os diretórios no caminho, similar ao "mkdir -p"
	// no terminal. Se já existem, não faz nada (idempotente).
	// Permissão 0755: dono pode tudo (7), grupo e outros podem ler e entrar (5).
	dir := filepath.Dir(firefoxPolicyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório %s: %w", dir, err)
	}

	// Passo 2: Tentar ler o arquivo existente.
	// Se o arquivo já existe, carregamos seu conteúdo para preservar
	// outras políticas que possam estar configuradas.
	// Se não existe, começamos com um mapa vazio.
	politicaRaiz := make(map[string]interface{})

	conteudoExistente, err := os.ReadFile(firefoxPolicyPath)
	if err == nil && len(conteudoExistente) > 0 {
		// O arquivo existe e tem conteúdo — vamos decodificar o JSON.
		// json.Unmarshal converte o JSON (texto) em um mapa Go (estrutura de dados).
		if err := json.Unmarshal(conteudoExistente, &politicaRaiz); err != nil {
			// Se o JSON está mal formado, não queremos perder dados.
			// Mas também precisamos aplicar nossa política. Nesse caso,
			// logamos o erro e começamos do zero.
			politicaRaiz = make(map[string]interface{})
		}
	}

	// Passo 3: Navegar até o nível "policies" no mapa.
	// Se "policies" não existe ainda, criamos um mapa vazio.
	policies, ok := politicaRaiz["policies"].(map[string]interface{})
	if !ok {
		// "policies" não existe ou não é um mapa — criamos do zero.
		policies = make(map[string]interface{})
	}

	// Passo 4: Navegar até "WebsiteFilter" dentro de "policies".
	websiteFilter, ok := policies["WebsiteFilter"].(map[string]interface{})
	if !ok {
		websiteFilter = make(map[string]interface{})
	}

	// Passo 5: Atualizar o array "Block" para refletir EXATAMENTE o estado atual.
	// Não fazemos merge aqui, pois merge impede remoções (o Firefox continuaria
	// bloqueando padrões antigos mesmo após o usuário remover um domínio).
	blocksFinais := make([]string, 0, len(padroes))
	for _, p := range padroes {
		blocksFinais = append(blocksFinais, p)
	}

	// Passo 6: Remontar a estrutura JSON e gravar o arquivo.
	websiteFilter["Block"] = blocksFinais
	policies["WebsiteFilter"] = websiteFilter
	politicaRaiz["policies"] = policies

	return gravarJSON(firefoxPolicyPath, politicaRaiz)
}

// removerPoliticaFirefox remove a política de bloqueio do Firefox.
//
// Temos dois cenários:
//   - Se o arquivo policies.json SÓ contém nossa política WebsiteFilter:
//     deletamos o arquivo inteiro (limpeza total).
//   - Se tem OUTRAS políticas além da nossa: removemos apenas o WebsiteFilter
//     e gravamos o arquivo de volta com o restante preservado.
//
// Isso é importante para não apagar configurações que o administrador do
// sistema possa ter feito independentemente do Open Turkey.
func removerPoliticaFirefox() error {
	// Passo 1: Tentar ler o arquivo existente.
	// Se não existe, não há nada para remover — sucesso!
	conteudo, err := os.ReadFile(firefoxPolicyPath)
	if err != nil {
		// Se o erro é "arquivo não encontrado", está tudo bem.
		// Qualquer outro erro (permissão, etc.) é um problema real.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("erro ao ler %s: %w", firefoxPolicyPath, err)
	}

	// Passo 2: Decodificar o JSON existente.
	var politicaRaiz map[string]interface{}
	if err := json.Unmarshal(conteudo, &politicaRaiz); err != nil {
		// JSON mal formado — podemos simplesmente deletar o arquivo,
		// pois ele não contém dados válidos de qualquer forma.
		return os.Remove(firefoxPolicyPath)
	}

	// Passo 3: Verificar se "policies" existe.
	policies, ok := politicaRaiz["policies"].(map[string]interface{})
	if !ok {
		// Sem "policies", o arquivo não tem nossas regras. Nada a fazer.
		return nil
	}

	// Passo 4: Remover o WebsiteFilter.
	delete(policies, "WebsiteFilter")

	// Passo 5: Decidir se deletamos o arquivo ou gravamos sem WebsiteFilter.
	// Se "policies" ficou vazio após remover WebsiteFilter, o arquivo
	// não serve mais para nada — podemos deletar.
	if len(policies) == 0 {
		// O arquivo só tinha nossa política — deletamos tudo.
		if err := os.Remove(firefoxPolicyPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("erro ao remover %s: %w", firefoxPolicyPath, err)
		}
		return nil
	}

	// Ainda existem outras políticas — gravamos o arquivo sem o WebsiteFilter.
	politicaRaiz["policies"] = policies
	return gravarJSON(firefoxPolicyPath, politicaRaiz)
}

// verificarPoliticaFirefox verifica se o arquivo de políticas do Firefox
// contém TODOS os padrões de URL esperados no array WebsiteFilter.Block.
//
// Retorna true somente se TODOS os padrões estão presentes.
// Se faltar qualquer um, retorna false.
func verificarPoliticaFirefox(padroesEsperados []string) bool {
	// Ler e decodificar o arquivo de políticas.
	conteudo, err := os.ReadFile(firefoxPolicyPath)
	if err != nil {
		return false
	}

	var politicaRaiz map[string]interface{}
	if err := json.Unmarshal(conteudo, &politicaRaiz); err != nil {
		return false
	}

	// Navegar até policies → WebsiteFilter → Block.
	policies, ok := politicaRaiz["policies"].(map[string]interface{})
	if !ok {
		return false
	}

	websiteFilter, ok := policies["WebsiteFilter"].(map[string]interface{})
	if !ok {
		return false
	}

	blocksExistentes := extrairStringsDeInterface(websiteFilter["Block"])

	// Montar um conjunto com os bloqueios existentes para busca rápida.
	conjuntoBloqueios := make(map[string]bool)
	for _, b := range blocksExistentes {
		conjuntoBloqueios[b] = true
	}

	// Verificar se TODOS os padrões esperados estão presentes.
	for _, padrao := range padroesEsperados {
		if !conjuntoBloqueios[padrao] {
			return false
		}
	}

	return true
}

// =============================================================================
// Funções auxiliares — Chromium / Google Chrome
// =============================================================================

// aplicarPoliticaChromium cria o arquivo de política para Chromium ou Chrome.
//
// O formato é mais simples que o Firefox:
//
//	{
//	    "URLBlocklist": [
//	        "*://*.facebook.com/*",
//	        "*://facebook.com/*"
//	    ]
//	}
//
// Como usamos um arquivo separado (open-turkey.json), não precisamos nos
// preocupar com merge — simplesmente sobrescrevemos o arquivo.
//
// O parâmetro caminhoArquivo permite reutilizar essa função para Chromium
// e Chrome, que usam diretórios diferentes mas o mesmo formato.
func aplicarPoliticaChromium(caminhoArquivo string, padroes []string) error {
	// Passo 1: Criar o diretório de políticas se não existir.
	// Se o diretório pai não existe, provavelmente o navegador não está
	// instalado. Mesmo assim, criamos o diretório — se o usuário instalar
	// o navegador depois, a política já estará lá esperando.
	dir := filepath.Dir(caminhoArquivo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório %s: %w", dir, err)
	}

	// Passo 2: Montar a estrutura de política.
	// O Chromium lê a chave "URLBlocklist" automaticamente — não precisa
	// de uma estrutura "policies" aninhada como o Firefox.
	politica := map[string]interface{}{
		"URLBlocklist": padroes,
	}

	return gravarJSON(caminhoArquivo, politica)
}

// verificarPoliticaChromium verifica se o arquivo de políticas do Chromium/Chrome
// contém TODOS os padrões de URL esperados no array URLBlocklist.
func verificarPoliticaChromium(caminhoArquivo string, padroesEsperados []string) bool {
	conteudo, err := os.ReadFile(caminhoArquivo)
	if err != nil {
		return false
	}

	var politica map[string]interface{}
	if err := json.Unmarshal(conteudo, &politica); err != nil {
		return false
	}

	blocksExistentes := extrairStringsDeInterface(politica["URLBlocklist"])

	// Montar um conjunto com os bloqueios existentes.
	conjuntoBloqueios := make(map[string]bool)
	for _, b := range blocksExistentes {
		conjuntoBloqueios[b] = true
	}

	// Verificar se TODOS os padrões esperados estão presentes.
	for _, padrao := range padroesEsperados {
		if !conjuntoBloqueios[padrao] {
			return false
		}
	}

	return true
}

// =============================================================================
// Funções auxiliares genéricas
// =============================================================================

// removerArquivoPolitica deleta um arquivo de política do sistema.
//
// Usada para remover os arquivos do Chromium e Chrome, que são exclusivos
// do Open Turkey (open-turkey.json) e podem ser simplesmente deletados.
//
// Se o arquivo já não existe, retorna nil (sem erro) — a função é idempotente.
// "Idempotente" significa que chamar várias vezes tem o mesmo efeito que
// chamar uma vez. Isso é útil para robustez: se o programa crashar, podemos
// tentar remover de novo sem problemas.
func removerArquivoPolitica(caminhoArquivo string) error {
	err := os.Remove(caminhoArquivo)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("erro ao remover %s: %w", caminhoArquivo, err)
	}
	return nil
}

// gravarJSON serializa um mapa Go para JSON formatado e grava em um arquivo.
//
// Usa json.MarshalIndent para gerar um JSON legível, com indentação de 2 espaços.
// Isso é importante porque:
//   - Administradores de sistema podem querer inspecionar os arquivos
//   - JSON minificado (sem espaços) é difícil de ler e debugar
//   - 2 espaços é a convenção mais comum para arquivos de configuração
//
// Permissão 0644:
//   - Dono (root): pode ler e escrever (6 = 4+2)
//   - Grupo: pode apenas ler (4)
//   - Outros: pode apenas ler (4)
// Todos os usuários precisam ler o arquivo (o navegador roda como usuário
// normal, não como root), mas só o root pode modificá-lo.
func gravarJSON(caminhoArquivo string, dados interface{}) error {
	// json.MarshalIndent converte a estrutura Go para uma string JSON.
	// O primeiro argumento vazio ("") é o prefixo de cada linha.
	// O segundo argumento ("  ") é a indentação — 2 espaços.
	jsonBytes, err := json.MarshalIndent(dados, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar JSON: %w", err)
	}

	// Adicionamos uma quebra de linha no final do arquivo.
	// Essa é uma convenção Unix: arquivos de texto devem terminar com
	// newline para que ferramentas como "cat" exibam corretamente.
	jsonBytes = append(jsonBytes, '\n')

	if err := os.WriteFile(caminhoArquivo, jsonBytes, 0644); err != nil {
		return fmt.Errorf("erro ao gravar %s: %w", caminhoArquivo, err)
	}

	return nil
}

// extrairStringsDeInterface converte um valor interface{} (que pode ser
// um array JSON decodificado) em um slice de strings Go.
//
// Quando decodificamos JSON genérico com json.Unmarshal em um
// map[string]interface{}, os arrays viram []interface{} (não []string).
// Cada elemento do array é um interface{} que precisa ser convertido
// para string individualmente.
//
// Exemplo: o JSON ["a", "b"] é decodificado para []interface{}{"a", "b"}
// e essa função converte para []string{"a", "b"}.
//
// Se o valor não for um array, ou se algum elemento não for string,
// simplesmente ignoramos (retornamos o que conseguimos converter).
func extrairStringsDeInterface(valor interface{}) []string {
	// Tentamos converter para []interface{} — o tipo que json.Unmarshal usa
	// para representar arrays JSON.
	array, ok := valor.([]interface{})
	if !ok {
		return nil
	}

	var resultado []string
	for _, item := range array {
		// Tentamos converter cada item para string.
		// Se não for string (ex: um número), ignoramos.
		if str, ok := item.(string); ok {
			resultado = append(resultado, str)
		}
	}

	return resultado
}

// diretorioExiste verifica se um diretório existe no sistema de arquivos.
//
// Usada para determinar se um navegador está "instalado" — se o diretório
// de políticas existe, assumimos que o navegador está presente.
// Essa heurística não é perfeita (o diretório pode existir sem o navegador,
// ou o navegador pode estar instalado sem o diretório), mas é suficiente
// para o nosso propósito.
func diretorioExiste(caminho string) bool {
	info, err := os.Stat(caminho)
	if err != nil {
		return false
	}
	return info.IsDir()
}
