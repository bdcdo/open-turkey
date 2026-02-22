// control.go — Comandos de controle de blocos: start, stop, unlock, status.
//
// Este arquivo implementa os comandos que controlam o CICLO DE VIDA de um bloco:
//
//   open-turkey start <bloco>   → Ativa um bloco (começa a bloquear sites/apps)
//   open-turkey stop <bloco>    → Desativa um bloco (para de bloquear)
//   open-turkey unlock <bloco>  → Desbloqueia um bloco travado via desafio de digitação
//   open-turkey status          → Mostra o status de todos os blocos
//
// COMO FUNCIONA A ATIVAÇÃO DE UM BLOCO?
// -------------------------------------
// Quando o usuário executa "open-turkey start redes-sociais", o sistema:
//  1. Marca o bloco como ativo no banco de dados
//  2. Coleta TODOS os domínios de TODOS os blocos ativos (não só o novo)
//  3. Aplica as 4 camadas de bloqueio:
//     - /etc/hosts (bloqueia DNS local)
//     - Firewall iptables (bloqueia conexões de rede)
//     - Políticas de navegador (bloqueia no Firefox/Chrome/Chromium)
//     - Mata processos bloqueados (fecha apps como Discord, Slack, etc.)
//
// Por que reaplicar TODAS as camadas e não só a do bloco novo?
// Porque as camadas são "globais" — o /etc/hosts, por exemplo, tem UMA seção
// do Open Turkey com TODOS os domínios bloqueados. Não dá pra adicionar
// domínios incrementalmente sem risco de inconsistência. É mais seguro
// recriar tudo do zero a cada mudança.
//
// O QUE É O "LOCK" (TRAVA)?
// --------------------------
// Quando o usuário ativa um bloco com --lock, ele está dizendo: "eu sei que
// vou tentar me sabotar, então torna difícil desativar esse bloqueio".
// O bloco travado só pode ser desativado via o comando "unlock", que exige
// digitar uma string aleatória enorme (o "desafio de digitação"). Isso cria
// atrito suficiente para que o impulso de "preciso ver o Instagram" passe.
package cli

import (
	"fmt"
	"os"

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/brunodcdo/open-turkey/internal/lock"
	"github.com/spf13/cobra"
)

// ============================================================================
// startCmd — Ativar um bloco de bloqueio
// ============================================================================

// startCmd ativa um bloco cadastrado no banco de dados.
//
// Uso: open-turkey start <nome-do-bloco> [--lock] [--lock-chars N]
//
// Exemplos:
//   open-turkey start redes-sociais              → ativa sem trava
//   open-turkey start redes-sociais --lock       → ativa com trava (300 chars padrão)
//   open-turkey start redes-sociais --lock --lock-chars 500  → trava com 500 chars
//
// Flags:
//   --lock         Trava o bloco para que não possa ser desativado com "stop".
//                  O usuário precisará usar "unlock" com desafio de digitação.
//   --lock-chars   Quantidade de caracteres aleatórios do desafio de desbloqueio.
//                  Padrão: 300. Quanto mais, mais difícil de desbloquear.
var startCmd = &cobra.Command{
	Use:   "start [bloco]",
	Short: "Ativar um bloco de bloqueio",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Extraímos o nome do bloco do primeiro argumento.
		// O Cobra já garantiu que temos exatamente 1 argumento (ExactArgs(1)).
		nomeBLoco := args[0]

		// Lemos as flags --lock e --lock-chars.
		// cmd.Flags().GetBool/GetInt retornam o valor da flag e um possível erro
		// de parsing (improvável, pois o Cobra valida os tipos automaticamente).
		usarTrava, err := cmd.Flags().GetBool("lock")
		if err != nil {
			return fmt.Errorf("erro ao ler flag --lock: %w", err)
		}

		charsDesbloqueio, err := cmd.Flags().GetInt("lock-chars")
		if err != nil {
			return fmt.Errorf("erro ao ler flag --lock-chars: %w", err)
		}

		// --- Passo 1: Abrir o banco de dados ---
		// openDB() é uma função auxiliar definida em block.go que abre o banco
		// SQLite no caminho padrão (/var/lib/open-turkey/open-turkey.db).
		database, err := openDB()
		if err != nil {
			return err
		}
		// defer garante que o banco será fechado quando a função terminar,
		// mesmo se um erro ocorrer no meio do caminho. É o equivalente ao
		// "finally" de outras linguagens.
		defer database.Close()

		// --- Passo 2: Verificar se o bloco existe e tem sites/apps ---
		// Precisamos confirmar que o bloco existe antes de tentar ativá-lo.
		// Também verificamos se ele tem pelo menos um site ou app configurado,
		// pois ativar um bloco vazio não faz sentido.
		detalhe, err := database.GetBlock(nomeBLoco)
		if err != nil {
			return err
		}

		if len(detalhe.Sites) == 0 && len(detalhe.Apps) == 0 {
			return fmt.Errorf("bloco '%s' não tem sites nem apps configurados. Adicione com 'open-turkey block add-site' ou 'open-turkey block add-app'", nomeBLoco)
		}

		// --- Passo 3: Verificar se já está ativo ---
		// Se o bloco já está ativo, não faz sentido ativar de novo.
		// Informamos o usuário para evitar confusão.
		if detalhe.Active {
			return fmt.Errorf("bloco '%s' já está ativo", nomeBLoco)
		}

		// --- Passo 4: Ativar o bloco no banco de dados ---
		// Se --lock não foi passado, lockChars fica como 0 (sem trava).
		// Se --lock foi passado, usamos o valor de --lock-chars (padrão 300).
		lockChars := 0
		if usarTrava {
			lockChars = charsDesbloqueio
		}

		if err := database.ActivateBlock(nomeBLoco, usarTrava, lockChars); err != nil {
			return err
		}

		// --- Passo 5: Coletar TODOS os domínios e apps de TODOS os blocos ativos ---
		// Precisamos de todos os domínios porque as camadas de bloqueio são globais.
		// Se o bloco "redes-sociais" bloqueia facebook.com e o bloco "jogos" bloqueia
		// steam.com, precisamos aplicar AMBOS no /etc/hosts, firewall, etc.
		dominios, err := database.GetAllBlockedDomains()
		if err != nil {
			return fmt.Errorf("erro ao buscar domínios bloqueados: %w", err)
		}

		apps, err := database.GetAllBlockedApps()
		if err != nil {
			return fmt.Errorf("erro ao buscar apps bloqueados: %w", err)
		}

		// --- Passo 6: Aplicar as 4 camadas de bloqueio ---
		// Cada camada é um mecanismo diferente de bloqueio que funciona
		// independentemente dos outros. Juntas, tornam muito difícil burlar.

		// Camada 1: /etc/hosts — redireciona domínios para 0.0.0.0 (IP inválido).
		// É a primeira linha de defesa e afeta TODOS os programas do sistema.
		if err := blocker.ApplyHosts(dominios); err != nil {
			return fmt.Errorf("erro ao aplicar bloqueio no /etc/hosts: %w", err)
		}

		// Camada 2: iptables (firewall) — bloqueia pacotes de rede para os IPs dos sites.
		// Funciona mesmo se o usuário encontrar o IP real e tentar acessar diretamente.
		if err := blocker.ApplyFirewall(dominios); err != nil {
			return fmt.Errorf("erro ao aplicar bloqueio no firewall: %w", err)
		}

		// Camada 3: Políticas de navegador — bloqueia diretamente no Firefox/Chrome/Chromium.
		// O navegador mostra uma página "Bloqueado pela política da organização".
		if err := blocker.ApplyBrowserPolicies(dominios); err != nil {
			return fmt.Errorf("erro ao aplicar políticas de navegador: %w", err)
		}

		// Camada 4: Matar processos — encerra apps bloqueados que estejam rodando.
		// Usa SIGKILL (sinal 9) para garantir que o processo morra imediatamente.
		if len(apps) > 0 {
			blocker.KillBlocked(apps)
		}

		// --- Passo 7: Mensagem de sucesso ---
		// Informamos se o bloco foi ativado com ou sem trava.
		if usarTrava {
			fmt.Printf("Bloco '%s' ativado com sucesso [TRAVADO - %d chars para desbloquear]\n", nomeBLoco, lockChars)
		} else {
			fmt.Printf("Bloco '%s' ativado com sucesso\n", nomeBLoco)
		}

		return nil
	},
}

// ============================================================================
// stopCmd — Desativar um bloco de bloqueio
// ============================================================================

// stopCmd desativa um bloco que está ativo.
//
// Uso: open-turkey stop <nome-do-bloco>
//
// IMPORTANTE: Se o bloco estiver travado (--lock), o "stop" será recusado.
// O usuário precisará usar "open-turkey unlock <bloco>" para desbloquear
// primeiro. Isso é intencional — a trava existe para impedir desativação impulsiva.
//
// Após desativar, o sistema reaplicar as camadas com os domínios restantes
// (de outros blocos ativos). Se nenhum bloco permanecer ativo, TODAS as
// camadas de bloqueio são removidas completamente.
var stopCmd = &cobra.Command{
	Use:   "stop [bloco]",
	Short: "Desativar um bloco de bloqueio",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nomeBLoco := args[0]

		// --- Passo 1: Abrir o banco de dados ---
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		// --- Passo 2: Verificar se o bloco está ativo ---
		// Não faz sentido desativar algo que não está ativo.
		ativo, err := database.IsBlockActive(nomeBLoco)
		if err != nil {
			return err
		}
		if !ativo {
			return fmt.Errorf("bloco '%s' não está ativo", nomeBLoco)
		}

		// --- Passo 3: Verificar se o bloco está travado ---
		// Se está travado, o usuário NÃO pode usar "stop". Precisa usar "unlock"
		// que exige digitar uma string aleatória enorme (desafio de digitação).
		// Isso é o mecanismo de "atrito" que impede desativação por impulso.
		travado, err := database.IsBlockLocked(nomeBLoco)
		if err != nil {
			return err
		}
		if travado {
			return fmt.Errorf("bloco '%s' está travado. Use 'open-turkey unlock' primeiro", nomeBLoco)
		}

		// --- Passo 4: Desativar o bloco no banco de dados ---
		if err := database.DeactivateBlock(nomeBLoco); err != nil {
			return err
		}

		// --- Passo 5: Reaplicar ou remover as camadas de bloqueio ---
		// Após desativar um bloco, precisamos atualizar as camadas de bloqueio.
		// Existem dois cenários possíveis:
		//
		// Cenário A: Ainda há outros blocos ativos.
		//   → Reaplicamos TODAS as camadas com os domínios/apps restantes.
		//
		// Cenário B: Não há mais nenhum bloco ativo.
		//   → Removemos TODAS as camadas de bloqueio completamente.
		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}

		fmt.Printf("Bloco '%s' desativado com sucesso\n", nomeBLoco)
		return nil
	},
}

// ============================================================================
// unlockCmd — Desbloquear um bloco travado via desafio de digitação
// ============================================================================

// unlockCmd permite desativar um bloco que foi ativado com --lock.
//
// Uso: open-turkey unlock <nome-do-bloco>
//
// O fluxo é:
//  1. Verificar se o bloco está ativo e travado
//  2. Gerar uma string aleatória com N caracteres (definido na ativação)
//  3. O usuário precisa digitar a string EXATAMENTE igual
//  4. Se acertar: o bloco é desativado
//  5. Se errar: o bloqueio permanece
//
// Este comando é a ÚNICA forma de desativar um bloco travado. Foi projetado
// para ser chato e demorado de propósito — o objetivo é que o impulso de
// desbloquear passe antes do usuário terminar de digitar.
var unlockCmd = &cobra.Command{
	Use:   "unlock [bloco]",
	Short: "Desbloquear um bloco travado via desafio de digitação",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nomeBLoco := args[0]

		// --- Passo 1: Abrir o banco de dados ---
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		// --- Passo 2: Verificar se o bloco está ativo ---
		// Não faz sentido desbloquear algo que não está ativo.
		ativo, err := database.IsBlockActive(nomeBLoco)
		if err != nil {
			return err
		}
		if !ativo {
			return fmt.Errorf("bloco '%s' não está ativo", nomeBLoco)
		}

		// --- Passo 3: Verificar se o bloco está realmente travado ---
		// Se não está travado, o usuário pode simplesmente usar "stop".
		travado, err := database.IsBlockLocked(nomeBLoco)
		if err != nil {
			return err
		}
		if !travado {
			return fmt.Errorf("bloco '%s' não está travado. Use 'open-turkey stop' para desativar", nomeBLoco)
		}

		// --- Passo 4: Obter o número de caracteres do desafio ---
		// O valor de lock_chars foi definido quando o bloco foi ativado com --lock.
		// É armazenado no banco para que saibamos quantos caracteres gerar.
		detalhe, err := database.GetBlock(nomeBLoco)
		if err != nil {
			return err
		}

		// --- Passo 5: Executar o desafio de digitação ---
		// RunChallenge gera uma string aleatória, mostra para o usuário,
		// lê o que ele digitou e compara caractere por caractere.
		// Retorna true se o desafio foi completado com sucesso.
		sucesso, err := lock.RunChallenge(detalhe.LockChars)
		if err != nil {
			return fmt.Errorf("erro ao executar desafio de desbloqueio: %w", err)
		}

		// --- Passo 6: Se falhou, o bloqueio permanece ---
		if !sucesso {
			fmt.Println("Desbloqueio falhou. O bloco permanece ativo e travado.")
			os.Exit(1)
		}

		// --- Passo 7: Desafio bem-sucedido — desativar o bloco ---
		if err := database.DeactivateBlock(nomeBLoco); err != nil {
			return err
		}

		// --- Passo 8: Reaplicar ou remover camadas (mesma lógica do stop) ---
		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}

		fmt.Printf("Bloco '%s' desbloqueado e desativado com sucesso\n", nomeBLoco)
		return nil
	},
}

// ============================================================================
// statusCmd — Mostrar status de todos os blocos
// ============================================================================

// statusCmd exibe um resumo do estado atual de todos os blocos cadastrados.
//
// Uso: open-turkey status
//
// Exemplo de saída:
//
//   === Status do Open Turkey ===
//
//   Blocos ativos:
//     redes-sociais [TRAVADO - 300 chars]
//     jogos
//
//   Blocos inativos:
//     trabalho
//
//   Total: 3 blocos (2 ativos, 1 inativo)
//
// Este comando é útil para ter uma visão geral rápida sem precisar
// inspecionar cada bloco individualmente.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Mostrar status de todos os blocos",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --- Passo 1: Abrir o banco de dados ---
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		// --- Passo 2: Buscar todos os blocos cadastrados ---
		// ListBlocks retorna todos os blocos com contagem de sites/apps.
		blocos, err := database.ListBlocks()
		if err != nil {
			return err
		}

		// Se não há blocos cadastrados, avisamos o usuário.
		if len(blocos) == 0 {
			fmt.Println("Nenhum bloco cadastrado. Crie um com 'open-turkey block create'.")
			return nil
		}

		// --- Passo 3: Buscar informações de ativação de cada bloco ---
		// Precisamos saber quais blocos estão ativos e se estão travados.
		// Para isso, consultamos GetBlock para cada bloco, que traz o status
		// completo (ativo, travado, lock_chars).
		//
		// Separamos os blocos em duas listas: ativos e inativos.
		// Cada bloco ativo vira uma string formatada com ou sem "[TRAVADO]".
		var blocosAtivos []string
		var blocosInativos []string

		for _, bloco := range blocos {
			// GetBlock retorna detalhes completos incluindo Active, Locked, LockChars.
			detalhe, err := database.GetBlock(bloco.Name)
			if err != nil {
				return err
			}

			if detalhe.Active {
				// Se está ativo, formatamos com informação de trava (se houver).
				if detalhe.Locked {
					blocosAtivos = append(blocosAtivos, fmt.Sprintf("  %s [TRAVADO - %d chars]", detalhe.Name, detalhe.LockChars))
				} else {
					blocosAtivos = append(blocosAtivos, fmt.Sprintf("  %s", detalhe.Name))
				}
			} else {
				blocosInativos = append(blocosInativos, fmt.Sprintf("  %s", detalhe.Name))
			}
		}

		// --- Passo 4: Imprimir o relatório formatado ---
		fmt.Println("=== Status do Open Turkey ===")
		fmt.Println()

		// Seção de blocos ativos.
		fmt.Println("Blocos ativos:")
		if len(blocosAtivos) == 0 {
			fmt.Println("  (nenhum)")
		} else {
			for _, linha := range blocosAtivos {
				fmt.Println(linha)
			}
		}

		fmt.Println()

		// Seção de blocos inativos.
		fmt.Println("Blocos inativos:")
		if len(blocosInativos) == 0 {
			fmt.Println("  (nenhum)")
		} else {
			for _, linha := range blocosInativos {
				fmt.Println(linha)
			}
		}

		fmt.Println()

		// Linha de totais.
		totalAtivos := len(blocosAtivos)
		totalInativos := len(blocosInativos)
		total := totalAtivos + totalInativos
		fmt.Printf("Total: %d blocos (%d ativos, %d inativo)\n", total, totalAtivos, totalInativos)

		return nil
	},
}

// ============================================================================
// Funções auxiliares
// ============================================================================

// reaplicarOuRemoverCamadas atualiza as camadas de bloqueio após uma desativação.
//
// Esta função centraliza a lógica comum entre stopCmd e unlockCmd:
// após desativar um bloco, precisamos decidir se reaplicamos as camadas
// com os domínios restantes ou se removemos tudo.
//
// Dois cenários possíveis:
//
//  1. Ainda existem blocos ativos: reaplicamos todas as 4 camadas com os
//     domínios/apps dos blocos que permaneceram ativos. Isso garante que
//     os outros bloqueios continuem funcionando.
//
//  2. Nenhum bloco ativo restante: removemos todas as camadas completamente.
//     O sistema volta ao estado "livre" — nenhum site ou app bloqueado.
func reaplicarOuRemoverCamadas(database *db.DB) error {
	// Buscamos os domínios e apps restantes (dos blocos que ainda estão ativos).
	dominios, err := database.GetAllBlockedDomains()
	if err != nil {
		return fmt.Errorf("erro ao buscar domínios bloqueados restantes: %w", err)
	}

	apps, err := database.GetAllBlockedApps()
	if err != nil {
		return fmt.Errorf("erro ao buscar apps bloqueados restantes: %w", err)
	}

	// Buscamos os blocos ativos para saber se ainda há algum.
	blocosAtivos, err := database.GetActiveBlocks()
	if err != nil {
		return fmt.Errorf("erro ao buscar blocos ativos: %w", err)
	}

	if len(blocosAtivos) > 0 {
		// Cenário 1: Ainda há blocos ativos — reaplicar todas as camadas.
		// Recriamos tudo do zero com os domínios restantes para garantir
		// consistência. É mais seguro que tentar remover domínios individualmente.

		if err := blocker.ApplyHosts(dominios); err != nil {
			return fmt.Errorf("erro ao reaplicar bloqueio no /etc/hosts: %w", err)
		}

		if err := blocker.ApplyFirewall(dominios); err != nil {
			return fmt.Errorf("erro ao reaplicar bloqueio no firewall: %w", err)
		}

		if err := blocker.ApplyBrowserPolicies(dominios); err != nil {
			return fmt.Errorf("erro ao reaplicar políticas de navegador: %w", err)
		}

		// Matar processos dos blocos que ainda estão ativos.
		if len(apps) > 0 {
			blocker.KillBlocked(apps)
		}
	} else {
		// Cenário 2: Nenhum bloco ativo — remover todas as camadas.
		// O sistema volta ao estado "limpo" — sem bloqueios.

		if err := blocker.RemoveHosts(); err != nil {
			return fmt.Errorf("erro ao remover bloqueio do /etc/hosts: %w", err)
		}

		if err := blocker.RemoveFirewall(); err != nil {
			return fmt.Errorf("erro ao remover bloqueio do firewall: %w", err)
		}

		if err := blocker.RemoveBrowserPolicies(); err != nil {
			return fmt.Errorf("erro ao remover políticas de navegador: %w", err)
		}
	}

	return nil
}

// ============================================================================
// init() — Registra os comandos no Cobra
// ============================================================================

// init() é chamada automaticamente pelo Go quando o pacote é importado.
// Aqui registramos nossos comandos como filhos do rootCmd (comando raiz),
// tornando-os disponíveis como subcomandos:
//   open-turkey start ...
//   open-turkey stop ...
//   open-turkey unlock ...
//   open-turkey status
//
// Também configuramos as flags do startCmd aqui, pois o Cobra exige que
// as flags sejam registradas antes da execução do comando.
func init() {
	// Registramos as flags do startCmd.
	// Flags().BoolP e Flags().IntP criam flags com nome longo e curto.
	// Neste caso, só usamos nome longo (sem atalho de uma letra).
	startCmd.Flags().Bool("lock", false, "Travar o bloco (requer desafio para desativar)")
	startCmd.Flags().Int("lock-chars", lock.DefaultLockChars, "Quantidade de caracteres do desafio de desbloqueio")

	// Adicionamos todos os comandos como filhos do comando raiz.
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(unlockCmd)
	rootCmd.AddCommand(statusCmd)
}
