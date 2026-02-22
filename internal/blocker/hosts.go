// Package blocker implementa o bloqueio de sites no nível do sistema operacional.
//
// COMO FUNCIONA O BLOQUEIO VIA /etc/hosts:
//
// Quando você digita "facebook.com" no navegador, o sistema operacional precisa
// descobrir o endereço IP real daquele servidor (ex: 157.240.1.35). Para isso,
// ele faz uma "consulta DNS" — basicamente pergunta a um servidor de nomes
// "qual é o IP de facebook.com?".
//
// MAS, antes de fazer essa consulta na internet, o Linux SEMPRE verifica primeiro
// o arquivo /etc/hosts. Esse arquivo funciona como uma "lista telefônica local":
// se o domínio está lá, o sistema usa o IP definido no arquivo e NEM CONSULTA
// o DNS externo.
//
// O truque é simples: mapeamos os domínios que queremos bloquear para o endereço
// 0.0.0.0 (um endereço inválido/inexistente). Quando o navegador tenta acessar
// o site, ele recebe esse IP falso e a conexão falha imediatamente.
//
// POR QUE 0.0.0.0 E NÃO 127.0.0.1?
// O 127.0.0.1 (localhost) pode ter um servidor web rodando e mostraria uma página
// de erro confusa. O 0.0.0.0 simplesmente não existe como destino — a conexão
// falha instantaneamente, sem espera. É mais rápido e mais limpo.
//
// IMPORTANTE: Esse bloqueio afeta TODOS os programas do computador, não apenas
// o navegador. Apps, clientes de email, qualquer coisa que use DNS será bloqueada.
// Por isso é um método tão eficaz — não dá pra burlar trocando de navegador.
//
// COMO GERENCIAMOS NOSSA SEÇÃO:
// Para não bagunçar as entradas originais do /etc/hosts (que podem incluir
// configurações importantes do sistema), usamos marcadores especiais:
//
//	# BEGIN OPEN-TURKEY
//	0.0.0.0 facebook.com
//	0.0.0.0 www.facebook.com
//	# END OPEN-TURKEY
//
// Assim, quando precisamos remover o bloqueio, sabemos exatamente quais linhas
// são nossas e quais são do sistema. Nunca tocamos no que está fora dos marcadores.
package blocker

import (
	"fmt"
	"os"
	"strings"
)

// hostsPath é o caminho do arquivo de hosts do Linux.
// Esse arquivo existe em todo sistema Unix/Linux e é lido automaticamente
// pelo sistema operacional antes de qualquer consulta DNS.
const hostsPath = "/etc/hosts"

// beginMarker e endMarker delimitam a seção que o Open Turkey gerencia.
// Tudo que estiver ENTRE essas duas linhas foi escrito por nós e pode
// ser removido com segurança. Tudo FORA dessas linhas pertence ao sistema
// e NUNCA deve ser alterado.
const (
	beginMarker = "# BEGIN OPEN-TURKEY"
	endMarker   = "# END OPEN-TURKEY"
)

// ApplyHosts recebe uma lista de domínios e bloqueia todos eles no /etc/hosts.
//
// O processo é:
//  1. Lê o conteúdo atual do /etc/hosts (para preservar as entradas do sistema)
//  2. Remove qualquer seção anterior do Open Turkey (evita duplicatas)
//  3. Monta uma nova seção com todos os domínios apontando para 0.0.0.0
//  4. Grava o arquivo de volta com as permissões corretas (0644)
//
// Para cada domínio, adicionamos DUAS entradas:
//   - 0.0.0.0 dominio.com       (versão sem www)
//   - 0.0.0.0 www.dominio.com   (versão com www)
//
// Isso é necessário porque "facebook.com" e "www.facebook.com" são tratados
// como domínios DIFERENTES pelo sistema. Se bloqueássemos só um, o usuário
// poderia acessar pelo outro.
func ApplyHosts(domains []string) error {
	// Passo 1: Ler o conteúdo atual do /etc/hosts.
	// Precisamos preservar tudo que já existe (entradas do sistema como
	// "127.0.0.1 localhost") e apenas adicionar/atualizar nossa seção.
	conteudo, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("erro ao ler o arquivo %s: %w", hostsPath, err)
	}

	// Passo 2: Remover qualquer seção anterior do Open Turkey.
	// Isso garante que não teremos entradas duplicadas caso o usuário
	// mude a lista de sites bloqueados.
	conteudoLimpo := removerSecaoOpenTurkey(string(conteudo))

	// Passo 3: Montar a nova seção com os domínios bloqueados.
	// Cada domínio ganha duas linhas: uma com e outra sem "www.".
	var secao strings.Builder
	secao.WriteString(beginMarker + "\n")

	for _, dominio := range domains {
		// Normaliza entradas do tipo "https://exemplo.com/" ou "exemplo.com/".
		dominio = NormalizarDominio(dominio)
		if dominio == "" {
			continue
		}

		// Sempre adicionamos o domínio como foi informado
		secao.WriteString(fmt.Sprintf("0.0.0.0 %s\n", dominio))

		// Agora tratamos a versão com/sem "www.":
		// Se o domínio JÁ começa com "www.", adicionamos também sem o "www."
		// Se NÃO começa com "www.", adicionamos a versão com "www."
		// Isso evita duplicar "www." (ex: "www.www.facebook.com")
		if strings.HasPrefix(dominio, "www.") {
			// O usuário passou "www.facebook.com", então adicionamos "facebook.com"
			semWWW := strings.TrimPrefix(dominio, "www.")
			secao.WriteString(fmt.Sprintf("0.0.0.0 %s\n", semWWW))
		} else {
			// O usuário passou "facebook.com", então adicionamos "www.facebook.com"
			secao.WriteString(fmt.Sprintf("0.0.0.0 www.%s\n", dominio))
		}
	}

	secao.WriteString(endMarker + "\n")

	// Passo 4: Juntar o conteúdo original (sem nossa seção antiga) com a nova seção.
	// Garantimos que há uma quebra de linha entre o conteúdo existente e nossa seção
	// para manter o arquivo organizado e legível.
	conteudoFinal := conteudoLimpo
	if !strings.HasSuffix(conteudoFinal, "\n") && conteudoFinal != "" {
		conteudoFinal += "\n"
	}
	conteudoFinal += secao.String()

	// Passo 5: Gravar o arquivo de volta.
	// Permissão 0644 significa:
	//   - Dono (root): pode ler e escrever (6 = 4+2)
	//   - Grupo: pode apenas ler (4)
	//   - Outros: pode apenas ler (4)
	// Essa é a permissão padrão do /etc/hosts na maioria das distribuições Linux.
	if err := os.WriteFile(hostsPath, []byte(conteudoFinal), 0644); err != nil {
		return fmt.Errorf("erro ao gravar o arquivo %s: %w", hostsPath, err)
	}

	return nil
}

// RemoveHosts remove todos os bloqueios do Open Turkey do arquivo /etc/hosts.
//
// Essa função é chamada quando o período de bloqueio termina ou quando o
// usuário desativa o bloqueador. Ela remove APENAS as linhas entre nossos
// marcadores, preservando todo o restante do arquivo.
func RemoveHosts() error {
	// Passo 1: Ler o conteúdo atual.
	conteudo, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("erro ao ler o arquivo %s: %w", hostsPath, err)
	}

	// Passo 2: Remover a seção do Open Turkey.
	conteudoLimpo := removerSecaoOpenTurkey(string(conteudo))

	// Passo 3: Gravar o arquivo de volta sem nossa seção.
	if err := os.WriteFile(hostsPath, []byte(conteudoLimpo), 0644); err != nil {
		return fmt.Errorf("erro ao gravar o arquivo %s: %w", hostsPath, err)
	}

	return nil
}

// IsHostsApplied verifica se TODOS os domínios da lista estão bloqueados no /etc/hosts.
//
// Retorna true SOMENTE se:
//  1. Os marcadores BEGIN/END do Open Turkey existem no arquivo
//  2. TODOS os domínios esperados estão presentes na seção (tanto com quanto sem "www.")
//
// Se faltar QUALQUER domínio, retorna false. Isso é importante para detectar
// se alguém editou manualmente o /etc/hosts tentando burlar o bloqueio.
func IsHostsApplied(domains []string) bool {
	// Tentamos ler o arquivo. Se falhar (ex: sem permissão), consideramos
	// que o bloqueio NÃO está aplicado, pois não temos como confirmar.
	conteudo, err := os.ReadFile(hostsPath)
	if err != nil {
		return false
	}

	texto := string(conteudo)

	// Verificação 1: Os marcadores existem?
	// Se não existem, significa que o Open Turkey nunca escreveu nesse arquivo
	// ou alguém removeu nossa seção manualmente.
	if !strings.Contains(texto, beginMarker) || !strings.Contains(texto, endMarker) {
		return false
	}

	// Verificação 2: Extrair apenas o conteúdo da nossa seção.
	// Precisamos verificar os domínios DENTRO da seção, não no arquivo inteiro,
	// porque o usuário pode ter entradas próprias com esses domínios fora da
	// nossa seção (improvável, mas possível).
	secao := extrairSecaoOpenTurkey(texto)

	// Verificação 3: Cada domínio esperado está presente?
	for _, dominio := range domains {
		dominio = NormalizarDominio(dominio)
		if dominio == "" {
			continue
		}

		// Verificamos se a linha "0.0.0.0 dominio" existe na seção.
		// Usamos a linha completa para evitar falsos positivos
		// (ex: "facebook.com.br" casando com "facebook.com").
		entradaPrincipal := fmt.Sprintf("0.0.0.0 %s", dominio)
		if !strings.Contains(secao, entradaPrincipal) {
			return false
		}

		// Verificamos também a versão alternativa (com ou sem www.)
		if strings.HasPrefix(dominio, "www.") {
			semWWW := strings.TrimPrefix(dominio, "www.")
			entradaAlternativa := fmt.Sprintf("0.0.0.0 %s", semWWW)
			if !strings.Contains(secao, entradaAlternativa) {
				return false
			}
		} else {
			entradaComWWW := fmt.Sprintf("0.0.0.0 www.%s", dominio)
			if !strings.Contains(secao, entradaComWWW) {
				return false
			}
		}
	}

	return true
}

// removerSecaoOpenTurkey remove tudo entre os marcadores BEGIN e END (inclusive).
//
// Essa é uma função auxiliar usada tanto por ApplyHosts (para limpar antes de
// reescrever) quanto por RemoveHosts (para remover o bloqueio).
//
// O algoritmo percorre o arquivo linha por linha:
//   - Enquanto estiver FORA dos marcadores, mantém a linha
//   - Quando encontra o BEGIN, começa a pular linhas
//   - Quando encontra o END, para de pular (e pula o próprio END também)
//   - Tudo que sobrou é o conteúdo original do sistema, intocado
func removerSecaoOpenTurkey(conteudo string) string {
	linhas := strings.Split(conteudo, "\n")
	var resultado []string

	// dentroSecao funciona como uma "chave liga/desliga":
	// false = estamos fora da seção do Open Turkey (manter a linha)
	// true = estamos dentro da seção (pular a linha)
	dentroSecao := false

	for _, linha := range linhas {
		// Encontramos o início da nossa seção — a partir daqui, pulamos tudo
		if strings.TrimSpace(linha) == beginMarker {
			dentroSecao = true
			continue
		}

		// Encontramos o fim da nossa seção — voltamos a manter as linhas
		if strings.TrimSpace(linha) == endMarker {
			dentroSecao = false
			continue
		}

		// Se estamos fora da seção, mantemos a linha normalmente
		if !dentroSecao {
			resultado = append(resultado, linha)
		}
	}

	// Juntamos as linhas de volta em um texto único.
	// Removemos linhas em branco extras do final para manter o arquivo limpo.
	textoFinal := strings.Join(resultado, "\n")
	textoFinal = strings.TrimRight(textoFinal, "\n")

	// Se o arquivo original não estava vazio, garantimos que termina com
	// uma quebra de linha (convenção Unix — arquivos de texto devem terminar
	// com newline para que ferramentas como "cat" funcionem corretamente).
	if textoFinal != "" {
		textoFinal += "\n"
	}

	return textoFinal
}

// extrairSecaoOpenTurkey retorna apenas o conteúdo entre os marcadores.
//
// Usada por IsHostsApplied para verificar quais domínios estão bloqueados
// sem se confundir com entradas fora da nossa seção.
func extrairSecaoOpenTurkey(conteudo string) string {
	linhas := strings.Split(conteudo, "\n")
	var secao []string
	dentroSecao := false

	for _, linha := range linhas {
		if strings.TrimSpace(linha) == beginMarker {
			dentroSecao = true
			continue
		}
		if strings.TrimSpace(linha) == endMarker {
			break // Encontrou o fim, não precisa continuar
		}
		if dentroSecao {
			secao = append(secao, linha)
		}
	}

	return strings.Join(secao, "\n")
}
