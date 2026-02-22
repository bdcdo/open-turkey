// =============================================================================
// Pacote blocker — Modulo de encerramento de processos do Open Turkey
// =============================================================================
//
// Este arquivo implementa o "matador de processos" — a parte do Open Turkey
// que fecha programas bloqueados (como navegadores ou apps de distracoes).
// Vamos entender como isso funciona no Linux:
//
// O QUE E UM PROCESSO?
// --------------------
// Todo programa rodando no seu computador e um "processo". Quando voce abre
// o Firefox, o Linux cria um processo pra ele. Cada processo recebe um numero
// unico chamado PID (Process ID), que e como um RG — nenhum outro processo
// ativo tem o mesmo numero.
//
// O QUE E O /proc?
// -----------------
// O Linux tem um sistema de arquivos virtual chamado /proc. Ele nao existe de
// verdade no disco — o kernel (nucleo do sistema) gera essas informacoes em
// tempo real. Cada processo ativo tem uma pasta /proc/[PID]/ com varias
// informacoes sobre ele. E como se cada processo tivesse uma "ficha cadastral"
// que o kernel mantem atualizada.
//
// Dois arquivos nos interessam:
//
//   /proc/[PID]/comm    → O nome curto do processo (maximo 15 caracteres).
//                         Exemplo: "firefox", "chrome", "slack"
//
//   /proc/[PID]/cmdline → A linha de comando completa usada pra iniciar o
//                         processo. Exemplo: "/usr/bin/firefox --new-window".
//                         Os argumentos sao separados por bytes nulos (\0),
//                         nao por espacos — isso e uma particularidade do Linux.
//
// COMO "MATAMOS" UM PROCESSO?
// ----------------------------
// No Linux, processos se comunicam atraves de "sinais" (signals). Sao como
// mensagens especiais que o kernel entrega. Existem varios tipos:
//
//   SIGTERM (sinal 15) → "Por favor, encerre educadamente." O processo pode
//                         capturar esse sinal, salvar dados e sair com calma.
//
//   SIGKILL (sinal 9)  → "Morra AGORA." O processo NAO pode capturar, ignorar
//                         ou bloquear esse sinal. O kernel mata ele na hora,
//                         sem chance de fazer nada. E o "botao de emergencia".
//
// Nos usamos SIGKILL porque estamos lidando com programas que o usuario quer
// BLOQUEADOS. Nao faz sentido pedir educadamente pro Firefox fechar — o
// usuario pode ter uma aba aberta no site bloqueado e o navegador poderia
// simplesmente ignorar o pedido.
//
// POR QUE PRECISAMOS DE ROOT?
// ---------------------------
// Um usuario normal so pode matar processos que pertencem a ele. Mas como o
// Open Turkey roda como root (precisa disso pra editar /etc/hosts e o
// iptables), ele tambem tem permissao pra matar qualquer processo no sistema.
//
// CUIDADOS DE SEGURANCA:
// ----------------------
// - Nunca matamos o PID 1 (init/systemd) — ele e o "pai" de todos os
//   processos. Mata-lo derrubaria o sistema inteiro.
// - Nunca matamos nosso proprio processo — seria suicidio, o bloqueio pararia.
// - Processos podem desaparecer a qualquer momento (o usuario fechou, ou o
//   processo terminou sozinho). Isso causa "race conditions" — tentamos ler
//   a ficha de um processo que ja nao existe mais. Simplesmente ignoramos
//   esses erros e seguimos em frente.
// =============================================================================
package blocker

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// KillBlocked percorre todos os processos ativos no sistema e encerra (mata)
// aqueles cujos nomes batem com a lista fornecida.
//
// Parametros:
//   - processNames: lista de nomes de programas a serem mortos.
//     Exemplos: ["firefox", "chrome", "slack", "discord"]
//
// Retorno:
//   - int:   quantidade de processos que foram efetivamente mortos.
//   - error: erro caso nao consiga ler o diretorio /proc (se isso falhar,
//            algo esta muito errado no sistema).
//
// Exemplo de uso:
//
//	killed, err := blocker.KillBlocked([]string{"firefox", "chrome"})
//	fmt.Printf("Processos encerrados: %d\n", killed)
func KillBlocked(processNames []string) (int, error) {
	// Se a lista de processos esta vazia, nao temos nada pra fazer.
	// Retornamos 0 mortos e nenhum erro.
	if len(processNames) == 0 {
		return 0, nil
	}

	// Lemos o conteudo do diretorio /proc. Cada entrada pode ser:
	// - Um numero (ex: "1234") → e uma pasta de processo (PID)
	// - Um nome (ex: "cpuinfo", "meminfo") → e um arquivo do sistema, ignoramos
	entries, err := os.ReadDir("/proc")
	if err != nil {
		// Se nao conseguimos ler /proc, algo esta muito errado.
		// Pode ser que o sistema nao tenha /proc montado (muito raro no Linux).
		return 0, err
	}

	// Pegamos nosso proprio PID pra nao nos matarmos acidentalmente.
	// os.Getpid() retorna o PID do programa que esta rodando AGORA (o Open Turkey).
	selfPID := os.Getpid()

	// Contador de quantos processos matamos com sucesso.
	killed := 0

	// Percorremos cada entrada do /proc procurando por PIDs.
	for _, entry := range entries {
		// Tentamos converter o nome da entrada pra numero.
		// Se funcionar, e um PID. Se nao, e um arquivo do sistema e ignoramos.
		// Exemplo: "1234" vira o numero 1234, mas "cpuinfo" daria erro.
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			// Nao e um numero → nao e um PID → pula pro proximo.
			continue
		}

		// SEGURANCA: Nunca matamos o PID 1 (init/systemd).
		// O PID 1 e o primeiro processo que o kernel inicia. Ele e o "avo"
		// de todos os outros processos. Mata-lo causaria um kernel panic
		// ou desligaria o sistema.
		if pid == 1 {
			continue
		}

		// SEGURANCA: Nunca matamos nosso proprio processo.
		// Se nos matassemos, o Open Turkey pararia de funcionar e todos os
		// bloqueios que dependem de monitoramento continuo falhariam.
		if pid == selfPID {
			continue
		}

		// Verificamos se este processo bate com algum nome da lista de bloqueio.
		// Usamos duas estrategias de deteccao (explicadas abaixo).
		if matchesBlockedProcess(pid, processNames) {
			// Encontramos um processo bloqueado! Enviamos SIGKILL (sinal 9).
			//
			// syscall.Kill() pede ao kernel pra enviar um sinal a um processo.
			// SIGKILL e o sinal mais agressivo — mata o processo instantaneamente
			// e ele NAO pode interceptar ou ignorar esse sinal.
			err := syscall.Kill(pid, syscall.SIGKILL)
			if err != nil {
				// Se deu erro ao matar, o processo provavelmente ja morreu sozinho
				// entre a hora que lemos /proc e a hora que tentamos mata-lo.
				// Isso e uma "race condition" — situacao normal em sistemas com
				// muitos processos. Simplesmente ignoramos e continuamos.
				continue
			}
			// Processo morto com sucesso! Incrementamos o contador.
			killed++
		}
	}

	// Retornamos quantos processos matamos. Nao retornamos erro aqui porque
	// erros individuais de processos que desapareceram sao normais e ja foram
	// tratados acima.
	return killed, nil
}

// matchesBlockedProcess verifica se um processo (identificado pelo PID) tem
// um nome que bate com algum dos nomes na lista de bloqueio.
//
// Usamos DUAS estrategias de deteccao pra maximizar a chance de pegar o
// processo certo:
//
//  1. /proc/[pid]/comm — Nome curto do processo (rapido e simples).
//     Funciona bem pra programas com nomes curtos como "firefox" ou "chrome".
//     Limitacao: maximo de 15 caracteres. "firefox-esr" aparece como "firefox-esr",
//     mas nomes muito longos seriam cortados.
//
//  2. /proc/[pid]/cmdline — Linha de comando completa (mais detalhada).
//     Contem o caminho completo e todos os argumentos. Util quando o nome no
//     comm nao bate, mas o executavel completo sim.
//     Exemplo: comm pode ser "python3", mas cmdline revela
//     "/usr/bin/python3 /opt/slack/slack".
func matchesBlockedProcess(pid int, processNames []string) bool {
	// Montamos o caminho base pra esse PID: "/proc/1234"
	pidPath := "/proc/" + strconv.Itoa(pid)

	// --- ESTRATEGIA 1: Checar /proc/[pid]/comm ---
	//
	// O arquivo comm contem apenas o nome do executavel, sem caminho e sem
	// argumentos. E o jeito mais rapido de identificar um processo.
	// Exemplo de conteudo: "firefox\n" (vem com uma quebra de linha no final)
	commBytes, err := os.ReadFile(pidPath + "/comm")
	if err == nil {
		// strings.TrimSpace remove espacos e quebras de linha do inicio e fim.
		// "firefox\n" vira "firefox".
		comm := strings.TrimSpace(string(commBytes))

		// Comparamos com cada nome da lista usando EqualFold, que ignora
		// maiusculas/minusculas. Assim "Firefox", "FIREFOX" e "firefox"
		// sao todos reconhecidos.
		for _, name := range processNames {
			if strings.EqualFold(comm, name) {
				return true
			}
		}
	}
	// Se deu erro ao ler comm, o processo pode ter desaparecido.
	// Nao e problema — tentamos a proxima estrategia.

	// --- ESTRATEGIA 2: Checar /proc/[pid]/cmdline ---
	//
	// O arquivo cmdline contem a linha de comando completa usada pra iniciar
	// o processo. Os argumentos sao separados por bytes nulos (\0), nao por
	// espacos. Isso e uma convencao do kernel Linux.
	//
	// Exemplo (representando \0 como |):
	//   /usr/bin/firefox|--new-window|https://google.com
	//
	// Cada pedaco entre os \0 e um "argumento" do comando.
	cmdlineBytes, err := os.ReadFile(pidPath + "/cmdline")
	if err != nil {
		// Se nao conseguimos ler o cmdline, o processo desapareceu.
		// Nao podemos confirmar nem negar — retornamos false (nao e match).
		return false
	}

	// Convertemos os bytes pra string e separamos pelos bytes nulos (\0).
	// Resultado: ["usr/bin/firefox", "--new-window", "https://google.com"]
	cmdline := string(cmdlineBytes)
	args := strings.Split(cmdline, "\x00")

	// Verificamos cada argumento da linha de comando contra cada nome da lista.
	// Usamos Contains (contem) ao inves de EqualFold (igual) porque o caminho
	// completo pode ser "/usr/bin/firefox" e queremos encontrar "firefox" dentro dele.
	//
	// Convertemos tudo pra minusculo pra a comparacao ser case-insensitive.
	for _, arg := range args {
		// Argumentos vazios podem aparecer (especialmente o ultimo, apos o \0 final).
		// Pulamos eles pra evitar falsos positivos.
		if arg == "" {
			continue
		}
		argLower := strings.ToLower(arg)
		for _, name := range processNames {
			if strings.Contains(argLower, strings.ToLower(name)) {
				return true
			}
		}
	}

	// Nenhuma estrategia encontrou match — esse processo nao esta na lista.
	return false
}
