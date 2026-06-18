// Package daemon implementa o loop principal do servi\u00e7o em segundo plano do Open Turkey.
//
// O daemon roda como um servi\u00e7o systemd e tem uma responsabilidade simples mas crucial:
// garantir que todas as camadas de bloqueio estejam sempre aplicadas corretamente.
//
// A cada 5 segundos, ele verifica se algu\u00e9m (ou algum programa) removeu ou alterou
// as regras de bloqueio \u2014 por exemplo, editando o /etc/hosts manualmente ou limpando
// as regras do iptables. Se detectar qualquer altera\u00e7\u00e3o, ele reaplica tudo.
//
// Al\u00e9m disso, o daemon tamb\u00e9m mata processos de aplicativos bloqueados a cada ciclo,
// impedindo que o usu\u00e1rio simplesmente abra o app enquanto o bloqueio est\u00e1 ativo.
//
// O daemon trata sinais SIGTERM e SIGINT para encerrar de forma limpa (graceful shutdown),
// sem deixar recursos pendurados.
package daemon

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/brunodcdo/open-turkey/internal/enforcer"
)

// Run \u00e9 a fun\u00e7\u00e3o principal do daemon. Ela inicia o loop de fiscaliza\u00e7\u00e3o
// que roda indefinidamente at\u00e9 receber um sinal de encerramento.
//
// Fluxo geral:
//  1. Abre o banco de dados no caminho padr\u00e3o
//  2. Configura o tratamento de sinais (SIGTERM e SIGINT)
//  3. Cria um ticker que dispara a cada 5 segundos
//  4. Executa um ciclo de fiscaliza\u00e7\u00e3o imediatamente ao iniciar
//  5. Entra no loop principal, onde alterna entre ticks e sinais
func Run() error {
	// Abrimos o banco de dados onde ficam armazenados os bloqueios ativos.
	// O caminho padr\u00e3o \u00e9 definido pelo pacote db (geralmente em /var/lib/open-turkey/).
	database, err := db.OpenDB(db.DefaultDBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	// Configuramos um canal para receber sinais do sistema operacional.
	// SIGTERM \u00e9 enviado pelo systemd ao parar o servi\u00e7o.
	// SIGINT \u00e9 enviado quando o usu\u00e1rio pressiona Ctrl+C (ex: em testes manuais).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// O ticker dispara a cada 5 segundos. Esse \u00e9 o "cora\u00e7\u00e3o" do daemon:
	// ele garante que a fiscaliza\u00e7\u00e3o acontece periodicamente, sem pausas longas
	// que permitiriam ao usu\u00e1rio burlar o bloqueio.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Println("daemon: iniciando loop de fiscaliza\u00e7\u00e3o do Open Turkey")

	// Executamos um ciclo imediatamente ao iniciar, sem esperar os primeiros
	// 5 segundos. Isso garante que, ao ligar o daemon, as regras j\u00e1 s\u00e3o
	// aplicadas na hora.
	if err := enforce(database); err != nil {
		// N\u00e3o interrompemos o daemon por causa de um erro no ciclo.
		// Apenas logamos e seguimos em frente \u2014 resili\u00eancia \u00e9 prioridade.
		log.Printf("daemon: erro no ciclo inicial de fiscaliza\u00e7\u00e3o: %v", err)
	}

	// Loop principal do daemon.
	// Usamos select para esperar por dois tipos de evento:
	//   - tick do ticker (hora de fiscalizar de novo)
	//   - sinal do SO (hora de encerrar)
	for {
		select {
		case <-ticker.C:
			// A cada 5 segundos, executamos um ciclo completo de fiscaliza\u00e7\u00e3o.
			// Se algo falhar, logamos o erro mas continuamos rodando.
			if err := enforce(database); err != nil {
				log.Printf("daemon: erro no ciclo de fiscaliza\u00e7\u00e3o: %v", err)
			}

		case sig := <-sigChan:
			// Recebemos um sinal de encerramento. Logamos e sa\u00edmos de forma limpa.
			log.Printf("daemon: sinal recebido (%v), encerrando graciosamente...", sig)
			return nil
		}
	}
}

// enforce executa um \u00fanico ciclo de fiscaliza\u00e7\u00e3o.
//
// O enforcer contabiliza uso de blocos limitados, decide quais blocos devem
// ficar apenas monitorados e reaplica as camadas de bloqueio quando a cota acaba
// ou quando o bloco n\u00e3o tem limite diário.
func enforce(database *db.DB) error {
	return enforcer.AccountAndApply(database, time.Now())
}
