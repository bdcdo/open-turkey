// daemon.go — Comando para iniciar o daemon do Open Turkey.
//
// O QUE É UM DAEMON?
// -------------------
// Um daemon (pronuncia-se "dimon") é um programa que roda em segundo plano
// no Linux, sem interação direta com o usuário. Pense nele como um "vigia"
// que fica acordado o tempo todo, mesmo quando ninguém está olhando.
//
// Exemplos famosos de daemons no Linux:
//   - sshd: daemon do SSH (permite conexões remotas)
//   - nginx: daemon do servidor web
//   - systemd: o "gerente" de todos os outros daemons
//
// POR QUE O OPEN TURKEY PRECISA DE UM DAEMON?
// --------------------------------------------
// Sem um daemon, o bloqueio seria facilmente burlável. O usuário poderia:
//   - Editar o /etc/hosts manualmente para remover nossos bloqueios
//   - Limpar as regras do iptables com "iptables -F"
//   - Deletar os arquivos de política dos navegadores
//   - Simplesmente reiniciar o computador para limpar tudo
//
// O daemon monitora continuamente todos esses pontos e reaplicar os bloqueios
// se detectar que alguém (ou alguma atualização do sistema) os removeu.
// É como um segurança que fica rondando 24h e tranca de novo qualquer
// porta que alguém tente abrir.
//
// COMO EXECUTAR?
// --------------
// Este comando normalmente NÃO é chamado diretamente pelo usuário.
// Ele é executado pelo systemd (o gerenciador de serviços do Linux)
// através de um arquivo de unidade (unit file) em /etc/systemd/system/.
//
// Exemplo de uso manual (para debug):
//   sudo open-turkey daemon
//
// Uso via systemd (o jeito correto):
//   sudo systemctl start open-turkey
//   sudo systemctl enable open-turkey   # inicia automaticamente no boot
package cli

import (
	"github.com/brunodcdo/open-turkey/internal/daemon"
	"github.com/spf13/cobra"
)

// daemonCmd inicia o daemon do Open Turkey.
//
// O daemon roda em loop infinito, monitorando e reaplicando os bloqueios.
// Ele só para quando recebe um sinal de encerramento (SIGTERM/SIGINT)
// ou quando o processo é morto externamente.
//
// Toda a lógica do daemon fica no pacote "internal/daemon", mantendo
// este arquivo enxuto. O papel deste comando é apenas servir de "ponte"
// entre o Cobra (framework de CLI) e o pacote daemon.
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Iniciar o daemon do Open Turkey (use via systemd)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// daemon.Run() inicia o loop principal do daemon.
		// Esta chamada é BLOQUEANTE — ela só retorna quando o daemon
		// é encerrado (por sinal ou erro fatal). Enquanto o daemon
		// estiver rodando, a execução fica "presa" aqui dentro.
		return daemon.Run()
	},
}

// init() registra o comando daemon como filho do rootCmd.
//
// Após este registro, o usuário pode executar:
//   open-turkey daemon
//
// O Go chama init() automaticamente quando o pacote é carregado.
// Como todos os arquivos do pacote "cli" são carregados juntos,
// cada arquivo pode ter seu próprio init() e todos serão executados.
func init() {
	rootCmd.AddCommand(daemonCmd)
}
