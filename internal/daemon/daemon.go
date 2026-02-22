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

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/db"
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
// Essa fun\u00e7\u00e3o \u00e9 o n\u00facleo do daemon. Ela verifica se cada camada de bloqueio
// est\u00e1 corretamente aplicada e, caso n\u00e3o esteja, reaplica.
//
// As camadas verificadas s\u00e3o:
//   - /etc/hosts: bloqueia dom\u00ednios resolvendo para 127.0.0.1
//   - Firewall (iptables): bloqueia conex\u00f5es de rede para os dom\u00ednios
//   - Pol\u00edticas de navegador: impede acesso via Chrome/Firefox policies
//   - Processos: mata aplicativos bloqueados que estejam rodando
//
// Importante: se uma camada falhar, as outras ainda s\u00e3o verificadas.
// Isso garante m\u00e1xima resili\u00eancia \u2014 um erro no iptables n\u00e3o deve
// impedir o bloqueio via /etc/hosts, por exemplo.
func enforce(database *db.DB) error {
	// Buscamos todos os bloqueios ativos no banco de dados.
	// Um bloqueio est\u00e1 ativo se o hor\u00e1rio atual est\u00e1 dentro do per\u00edodo configurado.
	activeBlocks, err := database.GetActiveBlocks()
	if err != nil {
		return err
	}

	// Se n\u00e3o houver bloqueios ativos, garantimos que todas as camadas
	// est\u00e3o removidas (estado limpo). Isso evita que regras orf\u00e3s
	// fiquem penduradas ap\u00f3s o t\u00e9rmino de um bloqueio.
	if len(activeBlocks) == 0 {
		// Removemos cada camada individualmente para garantir estado limpo.
		blocker.RemoveHosts()
		blocker.RemoveFirewall()
		blocker.RemoveBrowserPolicies()
		return nil
	}

	// Coletamos todos os dom\u00ednios e aplicativos de todos os bloqueios ativos.
	// V\u00e1rios bloqueios podem estar ativos ao mesmo tempo (ex: "redes sociais"
	// e "jogos"), ent\u00e3o precisamos unificar as listas.
	var domains []string
	var apps []string

	for _, block := range activeBlocks {
		domains = append(domains, block.Sites...)
		apps = append(apps, block.Apps...)
	}

	// --- Camada 1: /etc/hosts ---
	// Verificamos se os dom\u00ednios j\u00e1 est\u00e3o mapeados para 127.0.0.1 no /etc/hosts.
	// Se algu\u00e9m editou o arquivo manualmente para remover as entradas, reaplicamos.
	if !blocker.IsHostsApplied(domains) {
		log.Println("daemon: /etc/hosts desatualizado, reaplicando bloqueios")
		if err := blocker.ApplyHosts(domains); err != nil {
			// Logamos o erro mas continuamos para as pr\u00f3ximas camadas.
			log.Printf("daemon: erro ao aplicar /etc/hosts: %v", err)
		}
	}

	// --- Camada 2: Firewall (iptables) ---
	// Verificamos se as regras de firewall est\u00e3o presentes.
	// Se algu\u00e9m executou "iptables -F" para limpar as regras, reaplicamos.
	if !blocker.IsFirewallApplied() {
		log.Println("daemon: regras de firewall ausentes, reaplicando")
		if err := blocker.ApplyFirewall(domains); err != nil {
			log.Printf("daemon: erro ao aplicar firewall: %v", err)
		}
	}

	// --- Camada 3: Pol\u00edticas de navegador ---
	// Verificamos se as pol\u00edticas do Chrome/Firefox est\u00e3o configuradas.
	// Essas pol\u00edticas impedem o acesso mesmo se o usu\u00e1rio usar DNS alternativo.
	if !blocker.IsBrowserPoliciesApplied(domains) {
		log.Println("daemon: pol\u00edticas de navegador desatualizadas, reaplicando")
		if err := blocker.ApplyBrowserPolicies(domains); err != nil {
			log.Printf("daemon: erro ao aplicar pol\u00edticas de navegador: %v", err)
		}
	}

	// --- Camada 4: Matar processos bloqueados ---
	// Procuramos por processos de aplicativos bloqueados e os encerramos.
	// Isso impede o uso de apps como jogos ou redes sociais no desktop.
	if len(apps) > 0 {
		killed, err := blocker.KillBlocked(apps)
		if err != nil {
			log.Printf("daemon: erro ao matar processos bloqueados: %v", err)
		}
		if killed > 0 {
			log.Printf("daemon: %d processos bloqueados encerrados", killed)
		}
	}

	return nil
}
