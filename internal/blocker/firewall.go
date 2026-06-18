// =============================================================================
// Pacote blocker — Módulo de firewall do Open Turkey
// =============================================================================
//
// Este arquivo implementa o bloqueio de sites usando o iptables, que é o
// firewall embutido no kernel do Linux. Vamos entender como isso funciona:
//
// O QUE É O IPTABLES?
// --------------------
// O iptables é uma ferramenta que controla o tráfego de rede no Linux.
// Ele funciona como um "porteiro" que decide quais pacotes de dados podem
// entrar ou sair do seu computador. Pense nele como um segurança de balada:
// ele tem uma lista e só deixa passar quem está autorizado.
//
// COMO ORGANIZAMOS O BLOQUEIO?
// ----------------------------
// O iptables organiza suas regras em "tabelas" e "chains" (correntes/cadeias).
// Nós usamos a tabela padrão (filter) e criamos uma chain customizada chamada
// "OPEN-TURKEY". Isso é como criar uma lista separada só para o nosso app,
// em vez de misturar nossas regras com as do sistema.
//
// A chain OUTPUT controla tudo que SAI do computador. Quando você tenta
// acessar um site, seu computador envia pacotes para fora — e é aí que
// interceptamos. Nós inserimos um "salto" (jump) da chain OUTPUT para a
// nossa chain OPEN-TURKEY, fazendo com que todo tráfego de saída passe
// pelas nossas regras primeiro.
//
// O QUE SIGNIFICA "DROP"?
// -----------------------
// Quando uma regra tem o alvo DROP, o pacote é silenciosamente descartado.
// O computador nem avisa o servidor que tentou conectar. Do ponto de vista
// do usuário, o site simplesmente "não carrega" — fica esperando até dar
// timeout. Isso é diferente de REJECT, que responderia com um erro.
// Usamos DROP porque é mais difícil de perceber e contornar.
//
// POR QUE BLOQUEAMOS DNS-OVER-HTTPS (DoH)?
// -----------------------------------------
// Quando bloqueamos um site pelo /etc/hosts (outro módulo), o navegador
// usa o DNS do sistema para resolver nomes. Mas navegadores modernos como
// Chrome e Firefox podem usar DNS-over-HTTPS, que é uma forma de consultar
// DNS diretamente via HTTPS, ignorando o /etc/hosts. Para evitar isso,
// bloqueamos o acesso aos servidores DoH mais conhecidos (Cloudflare,
// Google, Quad9) na porta 443 (HTTPS).
//
// LIMITAÇÃO: APENAS IPv4
// ----------------------
// Este módulo usa apenas o comando "iptables", que controla somente o
// tráfego IPv4 (endereços como 192.168.1.1). Para bloquear IPv6
// (endereços como 2001:db8::1), seria necessário usar "ip6tables".
// Por simplicidade, não implementamos ip6tables nesta versão.
// Na prática, a maioria dos sites ainda funciona via IPv4, mas um
// usuário avançado poderia contornar o bloqueio usando IPv6.
// Uma versão futura deve adicionar suporte a ip6tables.
//
// =============================================================================
package blocker

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// chainName é o nome da nossa chain customizada no iptables.
// Usamos um nome único e descritivo para que qualquer pessoa que
// inspecione as regras do iptables saiba que essas regras pertencem
// ao Open Turkey. O hífen é permitido em nomes de chains.
const chainName = "OPEN-TURKEY"

// limitChainPrefix identifica chains usadas apenas para contar uso de blocos
// com limite diário. Elas não bloqueiam tráfego; só incrementam contadores.
const limitChainPrefix = "OT-LIMIT-"

// LimitTrackTarget descreve um bloco limitado que deve ter tráfego monitorado.
type LimitTrackTarget struct {
	BlockID int
	Domains []string
}

// dohServersIPv4 contém os endereços IPv4 dos servidores DNS-over-HTTPS
// mais utilizados. Bloqueamos esses IPs na porta 443 (HTTPS) para impedir
// que navegadores façam consultas DNS criptografadas, o que contornaria
// nosso bloqueio via /etc/hosts.
//
// Por que esses especificamente?
// - Cloudflare (1.1.1.1, 1.0.0.1): usado pelo Firefox por padrão
// - Google (8.8.8.8, 8.8.4.4): usado pelo Chrome e Android
// - Quad9 (9.9.9.9, 149.112.112.112): alternativa popular focada em segurança
//
// Nota: endereços IPv6 desses provedores (como 2606:4700:4700::1111) NÃO
// são bloqueados aqui porque usamos apenas iptables (IPv4). Veja a
// limitação documentada no cabeçalho do arquivo.
var dohServersIPv4 = []string{
	// Cloudflare — provedor de DNS mais rápido do mundo, padrão do Firefox
	"1.1.1.1",
	"1.0.0.1",

	// Google — provedor de DNS mais popular do mundo
	"8.8.8.8",
	"8.8.4.4",

	// Quad9 — provedor focado em segurança e privacidade
	"9.9.9.9",
	"149.112.112.112",
}

// =============================================================================
// Funções públicas — a interface que o resto do Open Turkey usa
// =============================================================================

// ApplyFirewall configura o firewall para bloquear os domínios especificados.
//
// Como funciona passo a passo:
//  1. Cria a chain OPEN-TURKEY (se já não existir)
//  2. Limpa todas as regras anteriores da chain (para recomeçar do zero)
//  3. Garante que a chain OUTPUT "salta" para a nossa chain
//  4. Para cada domínio, resolve os IPs e adiciona regras DROP
//  5. Bloqueia os servidores DoH conhecidos
//
// Por que limpar e recriar? Porque a lista de sites bloqueados pode mudar.
// É mais simples e seguro recriar todas as regras do que tentar calcular
// a diferença entre o estado atual e o desejado.
func ApplyFirewall(domains []string) error {
	// --- Passo 1: Criar a chain customizada ---
	// O argumento -N cria uma nova chain. Se ela já existe, o iptables
	// retorna um erro, mas tudo bem — ignoramos esse erro silenciosamente.
	// É como tentar criar uma pasta que já existe: não faz mal.
	_ = runIptables("-N", chainName)

	// --- Passo 2: Limpar regras existentes ---
	// O argumento -F (flush) remove todas as regras da chain, mas mantém
	// a chain em si. Fazemos isso para garantir que partimos de um estado
	// limpo antes de adicionar as novas regras.
	if err := runIptables("-F", chainName); err != nil {
		return fmt.Errorf("erro ao limpar a chain %s: %w", chainName, err)
	}

	// --- Passo 3: Inserir o salto (jump) da OUTPUT para nossa chain ---
	// Primeiro verificamos se o salto já existe usando -C (check).
	// Se não existe, inserimos com -I (insert no topo, não append).
	// Usamos -I em vez de -A para que nossa regra seja avaliada ANTES
	// de qualquer outra regra na chain OUTPUT. Isso garante que nosso
	// bloqueio tem prioridade.
	if err := runIptables("-C", "OUTPUT", "-j", chainName); err != nil {
		// A regra de salto não existe ainda — vamos criá-la
		if err := runIptables("-I", "OUTPUT", "-j", chainName); err != nil {
			return fmt.Errorf("erro ao inserir salto para a chain %s na OUTPUT: %w", chainName, err)
		}
	}

	// --- Passo 4: Resolver domínios e adicionar regras DROP ---
	// Para cada domínio (ex: "youtube.com"), precisamos descobrir quais
	// endereços IP ele usa. Um domínio pode ter vários IPs (ex: o Google
	// tem dezenas). Bloqueamos todos eles.
	for _, domain := range domains {
		domain = NormalizarDominio(domain)
		if domain == "" {
			continue
		}

		// net.LookupHost faz uma consulta DNS e retorna todos os IPs
		// associados ao domínio. Isso é equivalente a rodar "nslookup" ou
		// "dig" no terminal.
		ips, err := net.LookupHost(domain)
		if err != nil {
			// Se não conseguimos resolver o domínio, não é um erro fatal.
			// O domínio pode estar temporariamente fora do ar, ou pode ser
			// um domínio inválido. Apenas pulamos e continuamos com os outros.
			// Em um app de produção, seria bom logar isso para debug.
			continue
		}

		for _, ip := range ips {
			// Filtramos apenas endereços IPv4. Endereços IPv6 contêm ":"
			// (ex: "2607:f8b0:4004::64") enquanto IPv4 não (ex: "142.250.80.46").
			// Como usamos apenas iptables (não ip6tables), ignoramos IPv6.
			if strings.Contains(ip, ":") {
				continue
			}

			// -A adiciona (append) a regra ao final da chain
			// -d especifica o IP de destino (destination)
			// -j DROP significa "descarte o pacote silenciosamente"
			//
			// Em linguagem humana: "todo pacote saindo do computador com
			// destino ao IP X deve ser descartado"
			if err := runIptables("-A", chainName, "-d", ip, "-j", "DROP"); err != nil {
				return fmt.Errorf("erro ao bloquear IP %s do domínio %s: %w", ip, domain, err)
			}
		}
	}

	// --- Passo 5: Bloquear servidores DNS-over-HTTPS ---
	if err := BlockDoH(); err != nil {
		return fmt.Errorf("erro ao bloquear servidores DoH: %w", err)
	}

	return nil
}

// RemoveFirewall desfaz completamente o bloqueio do firewall.
//
// A ordem importa aqui! Precisamos:
//  1. Primeiro remover o salto da OUTPUT (senão a OUTPUT aponta para uma chain inexistente)
//  2. Depois limpar as regras da nossa chain
//  3. Por último, deletar a chain em si
//
// O iptables não permite deletar uma chain que ainda tem regras ou que
// é referenciada por outra chain. Por isso a ordem é crucial.
//
// Esta função é idempotente: pode ser chamada várias vezes sem erro,
// mesmo que o firewall já tenha sido removido. Isso é importante para
// robustez — se o programa crashar, podemos chamar RemoveFirewall na
// próxima execução sem nos preocupar com o estado anterior.
func RemoveFirewall() error {
	// --- Passo 1: Remover o salto da OUTPUT para nossa chain ---
	// -D (delete) remove a regra especificada. Se a regra não existe,
	// o iptables retorna erro, mas ignoramos — pode ser que o firewall
	// já tenha sido removido antes.
	_ = runIptables("-D", "OUTPUT", "-j", chainName)

	// --- Passo 2: Limpar todas as regras da chain ---
	// Precisamos fazer isso ANTES de deletar a chain, porque o iptables
	// se recusa a deletar uma chain que ainda contém regras.
	_ = runIptables("-F", chainName)

	// --- Passo 3: Deletar a chain ---
	// -X deleta uma chain customizada. Chains embutidas (INPUT, OUTPUT,
	// FORWARD) não podem ser deletadas, mas a nossa OPEN-TURKEY pode.
	_ = runIptables("-X", chainName)

	// Não retornamos erros porque qualquer falha aqui provavelmente
	// significa que a chain já não existia, o que é o estado desejado.
	return nil
}

// ApplyLimitTracking instala chains de contagem para blocos com limite diário.
func ApplyLimitTracking(targets []LimitTrackTarget) error {
	if len(targets) == 0 {
		return RemoveLimitTracking()
	}

	desired := make(map[string]bool)
	for _, target := range targets {
		if target.BlockID <= 0 {
			continue
		}

		chain := limitChainName(target.BlockID)
		desired[chain] = true

		_ = runIptables("-N", chain)
		if err := runIptables("-F", chain); err != nil {
			return fmt.Errorf("erro ao limpar a chain %s: %w", chain, err)
		}
		if err := ensureOutputJump(chain); err != nil {
			return err
		}

		for _, ip := range resolveDomainsIPv4(target.Domains) {
			if err := runIptables("-A", chain, "-d", ip, "-j", "RETURN"); err != nil {
				return fmt.Errorf("erro ao adicionar regra de contagem para %s: %w", ip, err)
			}
		}
	}

	existing, err := listLimitChains()
	if err != nil {
		return err
	}
	for _, chain := range existing {
		if !desired[chain] {
			removeLimitChain(chain)
		}
	}

	return nil
}

// RemoveLimitTracking remove todas as chains de contagem de uso diário.
func RemoveLimitTracking() error {
	chains, err := listLimitChains()
	if err != nil {
		return err
	}
	for _, chain := range chains {
		removeLimitChain(chain)
	}
	return nil
}

// ReadLimitTrackingCounters soma os pacotes observados por bloco limitado.
func ReadLimitTrackingCounters() (map[int]uint64, error) {
	counters := make(map[int]uint64)

	chains, err := listLimitChains()
	if err != nil {
		return counters, err
	}

	for _, chain := range chains {
		blockID, ok := parseLimitChainID(chain)
		if !ok {
			continue
		}

		output, err := runIptablesOutput("-L", chain, "-v", "-x", "-n")
		if err != nil {
			return counters, err
		}
		counters[blockID] = parsePacketCounter(output)
	}

	return counters, nil
}

// IsFirewallApplied verifica se o firewall do Open Turkey está ativo.
//
// Para considerar o firewall como "aplicado", duas condições precisam
// ser verdadeiras:
//  1. A chain OPEN-TURKEY deve existir
//  2. A chain deve conter pelo menos uma regra (não estar vazia)
//
// Uma chain vazia significaria que criamos a estrutura mas não bloqueamos
// nada — não conta como firewall ativo.
func IsFirewallApplied() bool {
	// -L lista as regras de uma chain específica. Se a chain não existe,
	// o comando falha. Usamos -n para não resolver IPs para nomes (mais rápido).
	output, err := runIptablesOutput("-L", chainName, "-n")
	if err != nil {
		// Se deu erro, a chain provavelmente não existe
		return false
	}

	// A saída do "iptables -L CHAIN" sempre começa com 2 linhas de cabeçalho:
	//   Chain OPEN-TURKEY (1 references)
	//   target     prot opt source               destination
	//
	// Se houver regras, elas aparecem a partir da 3ª linha.
	// Então, se tivermos mais de 2 linhas, a chain tem regras.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return len(lines) > 2
}

// BlockDoH bloqueia o acesso aos servidores DNS-over-HTTPS mais conhecidos.
//
// DNS-over-HTTPS (DoH) é um protocolo que permite fazer consultas DNS
// usando HTTPS (porta 443). Navegadores modernos usam isso para
// "escapar" do DNS do sistema operacional, o que invalidaria nosso
// bloqueio via /etc/hosts.
//
// Aqui bloqueamos especificamente a porta 443 (HTTPS) desses servidores.
// Não bloqueamos TODO o tráfego para esses IPs porque isso quebraria
// a resolução DNS normal (porta 53), que ainda é necessária para os
// sites que NÃO estão bloqueados funcionarem.
//
// ATENÇÃO: Bloquear o DNS do Google (8.8.8.8) e Cloudflare (1.1.1.1)
// pode causar efeitos colaterais se o usuário os usa como DNS padrão.
// No entanto, como bloqueamos apenas a porta 443 (HTTPS), a resolução
// DNS normal na porta 53 (UDP/TCP) continua funcionando normalmente.
func BlockDoH() error {
	for _, ip := range dohServersIPv4 {
		// -A OPEN-TURKEY: adiciona à nossa chain
		// -d <ip>: destino é o servidor DoH
		// -p tcp: protocolo TCP (HTTPS usa TCP)
		// --dport 443: porta de destino 443 (HTTPS)
		// -j DROP: descarta silenciosamente
		//
		// Em linguagem humana: "se o computador tentar acessar esse IP
		// na porta 443 via TCP, descarte o pacote"
		err := runIptables("-A", chainName, "-d", ip, "-p", "tcp", "--dport", "443", "-j", "DROP")
		if err != nil {
			return fmt.Errorf("erro ao bloquear servidor DoH %s: %w", ip, err)
		}
	}

	return nil
}

// =============================================================================
// Funções auxiliares (helpers) — uso interno do pacote
// =============================================================================

func ensureOutputJump(chain string) error {
	if err := runIptables("-C", "OUTPUT", "-j", chain); err == nil {
		return nil
	}
	if err := runIptables("-I", "OUTPUT", "-j", chain); err != nil {
		return fmt.Errorf("erro ao inserir salto para a chain %s na OUTPUT: %w", chain, err)
	}
	return nil
}

func removeLimitChain(chain string) {
	_ = runIptables("-D", "OUTPUT", "-j", chain)
	_ = runIptables("-F", chain)
	_ = runIptables("-X", chain)
}

func listLimitChains() ([]string, error) {
	output, err := runIptablesOutput("-S")
	if err != nil {
		return nil, err
	}

	var chains []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "-N" && strings.HasPrefix(fields[1], limitChainPrefix) {
			chains = append(chains, fields[1])
		}
	}
	return chains, nil
}

func limitChainName(blockID int) string {
	return fmt.Sprintf("%s%d", limitChainPrefix, blockID)
}

func parseLimitChainID(chain string) (int, bool) {
	if !strings.HasPrefix(chain, limitChainPrefix) {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(chain, limitChainPrefix))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func resolveDomainsIPv4(domains []string) []string {
	seen := make(map[string]bool)
	var ips []string

	for _, domain := range domains {
		domain = NormalizarDominio(domain)
		if domain == "" {
			continue
		}

		resolved, err := net.LookupHost(domain)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			if strings.Contains(ip, ":") || seen[ip] {
				continue
			}
			seen[ip] = true
			ips = append(ips, ip)
		}
	}

	return ips
}

func parsePacketCounter(output string) uint64 {
	var total uint64
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		packets, err := strconv.ParseUint(fields[0], 10, 64)
		if err == nil {
			total += packets
		}
	}
	return total
}

// runIptables executa um comando iptables com os argumentos fornecidos.
//
// Por que encapsulamos isso em uma função separada?
//  1. Evita repetição de código (DRY - Don't Repeat Yourself)
//  2. Centraliza o tratamento de erros
//  3. Captura a saída de erro (stderr) para mensagens mais úteis
//
// O iptables requer permissões de root (superusuário) para funcionar.
// Se o Open Turkey não estiver rodando como root, todos os comandos
// vão falhar. O daemon do Open Turkey deve ser iniciado com sudo.
func runIptables(args ...string) error {
	cmd := exec.Command("iptables", args...)

	// Capturamos stderr porque é onde o iptables escreve suas mensagens
	// de erro. stdout geralmente fica vazio para comandos que modificam
	// regras. Sem capturar stderr, só saberíamos que deu erro, mas não
	// o motivo.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Montamos uma mensagem de erro que inclui:
		// - O comando completo que foi executado (para debug)
		// - A mensagem de erro do iptables (stderr)
		// - O erro do Go (código de saída, etc.)
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return fmt.Errorf(
				"falha ao executar iptables %s: %s (erro: %w)",
				strings.Join(args, " "),
				stderrStr,
				err,
			)
		}
		return fmt.Errorf("falha ao executar iptables %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

// runIptablesOutput executa um comando iptables e retorna a saída padrão (stdout).
//
// Esta variante é usada quando precisamos LER informações do iptables,
// como listar regras existentes. A diferença para runIptables é que aqui
// capturamos e retornamos o stdout em vez de apenas verificar se houve erro.
func runIptablesOutput(args ...string) (string, error) {
	cmd := exec.Command("iptables", args...)

	// CombinedOutput captura tanto stdout quanto stderr juntos.
	// Usamos isso porque, em caso de erro, queremos a mensagem do stderr,
	// e em caso de sucesso, queremos o stdout com a listagem das regras.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"falha ao executar iptables %s: %s (erro: %w)",
			strings.Join(args, " "),
			strings.TrimSpace(string(output)),
			err,
		)
	}

	return string(output), nil
}
