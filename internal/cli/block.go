// Este arquivo define os comandos CLI para gerenciar blocos no Open Turkey.
//
// Um "bloco" é um grupo nomeado de sites e aplicativos que o usuário deseja
// bloquear juntos. Por exemplo, um bloco "redes-sociais" pode conter os
// domínios facebook.com, instagram.com e os processos discord, telegram.
//
// Os comandos aqui são a interface entre o usuário e o banco de dados.
// Cada comando segue o mesmo padrão:
//   1. Abre o banco de dados (openDB)
//   2. Chama a função correspondente no pacote db
//   3. Exibe o resultado ou a mensagem de erro
//
// Todos os subcomandos ficam sob "open-turkey block":
//   - open-turkey block create [nome]
//   - open-turkey block add-site [bloco] [domínios...]
//   - open-turkey block remove-site [bloco] [domínios...]
//   - open-turkey block add-app [bloco] [processos...]
//   - open-turkey block remove-app [bloco] [processos...]
//   - open-turkey block list
//   - open-turkey block info [nome]
//   - open-turkey block remove [nome]
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/spf13/cobra"
)

// --------------------------------------------------------------------------
// Função auxiliar para abrir o banco de dados
// --------------------------------------------------------------------------

// openDB abre a conexão com o banco de dados SQLite.
// Antes de abrir, criamos o diretório pai caso não exista (os.MkdirAll).
// Isso é necessário porque o SQLite não cria diretórios automaticamente —
// ele só cria o arquivo .db, mas precisa que o diretório já exista.
//
// Usamos db.DefaultDBPath para manter o caminho centralizado no pacote db.
// Se no futuro quisermos permitir um caminho customizado (via flag),
// basta trocar essa constante por uma variável configurável.
func openDB() (*db.DB, error) {
	// filepath.Dir() extrai o diretório pai do caminho completo.
	// Exemplo: "/var/lib/open-turkey/open-turkey.db" → "/var/lib/open-turkey"
	dir := filepath.Dir(db.DefaultDBPath)

	// os.MkdirAll cria o diretório e todos os pais necessários.
	// Se o diretório já existe, não faz nada (é idempotente).
	// Permissão 0755: dono pode ler/escrever/executar, outros podem ler/executar.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório do banco de dados '%s': %w", dir, err)
	}

	return db.OpenDB(db.DefaultDBPath)
}

// --------------------------------------------------------------------------
// Comando pai: block
// --------------------------------------------------------------------------

// blockCmd é o comando pai de todos os subcomandos de blocos.
// Ele não faz nada sozinho — serve apenas como agrupador.
// Quando o usuário digita "open-turkey block" sem subcomando,
// o Cobra exibe a ajuda automaticamente.
var blockCmd = &cobra.Command{
	Use:   "block",
	Short: "Gerenciar blocos de sites e aplicativos",
}

// --------------------------------------------------------------------------
// Subcomando: block create
// --------------------------------------------------------------------------

// blockCreateCmd cria um novo bloco com o nome fornecido.
// O nome deve ser único — se já existir um bloco com o mesmo nome,
// o banco de dados retornará erro.
//
// Uso: open-turkey block create redes-sociais
var blockCreateCmd = &cobra.Command{
	Use:   "create [nome]",
	Short: "Criar um novo bloco",
	// cobra.ExactArgs(1) garante que o usuário passe exatamente 1 argumento.
	// Se passar 0 ou mais de 1, o Cobra exibe uma mensagem de erro automática.
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// args[0] contém o nome do bloco que o usuário digitou.
		name := args[0]

		// Abrimos o banco de dados.
		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		// defer garante que o banco será fechado ao sair da função,
		// mesmo que ocorra um erro no meio do caminho.
		defer database.Close()

		// Chamamos a função do pacote db para criar o bloco.
		if err := database.CreateBlock(name); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Bloco '%s' criado com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Subcomando: block add-site
// --------------------------------------------------------------------------

// blockAddSiteCmd adiciona um ou mais domínios (sites) a um bloco existente.
// O primeiro argumento é o nome do bloco, e os demais são os domínios.
//
// Uso: open-turkey block add-site redes-sociais facebook.com instagram.com
var blockAddSiteCmd = &cobra.Command{
	Use:   "add-site [bloco] [domínios...]",
	Short: "Adicionar sites a um bloco",
	// cobra.MinimumNArgs(2) exige pelo menos 2 argumentos:
	// o nome do bloco + pelo menos 1 domínio.
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		// O primeiro argumento é o nome do bloco.
		name := args[0]
		// Os argumentos restantes são os domínios a serem adicionados.
		// args[1:] cria uma fatia (slice) do segundo elemento em diante.
		// Normalizamos cada domínio para garantir formato consistente no banco.
		domains := make([]string, len(args[1:]))
		for i, d := range args[1:] {
			domains[i] = blocker.NormalizarDominio(d)
		}

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := database.AddSites(name, domains); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		ativo, err := database.IsBlockActive(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		if ativo {
			if err := reaplicarOuRemoverCamadas(database); err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Sites adicionados ao bloco '%s' com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Subcomando: block remove-site
// --------------------------------------------------------------------------

// blockRemoveSiteCmd remove um ou mais domínios (sites) de um bloco existente.
//
// Uso: open-turkey block remove-site redes-sociais facebook.com instagram.com
var blockRemoveSiteCmd = &cobra.Command{
	Use:   "remove-site [bloco] [domínios...]",
	Short: "Remover sites de um bloco",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		// Normalizamos cada domínio para garantir match com o que está no banco.
		domains := make([]string, len(args[1:]))
		for i, d := range args[1:] {
			domains[i] = blocker.NormalizarDominio(d)
		}

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := database.RemoveSites(name, domains); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		ativo, err := database.IsBlockActive(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		if ativo {
			// Se o bloco ficou vazio, desativamos para evitar um "ativo sem nada"
			// (e para garantir que as camadas sejam removidas se não restar nada ativo).
			detalhe, err := database.GetBlock(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}

			if len(detalhe.Sites) == 0 && len(detalhe.Apps) == 0 {
				if err := database.DeactivateBlock(name); err != nil {
					fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
					os.Exit(1)
				}
			}

			if err := reaplicarOuRemoverCamadas(database); err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Sites removidos do bloco '%s' com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Subcomando: block add-app
// --------------------------------------------------------------------------

// blockAddAppCmd adiciona um ou mais nomes de processos (apps) a um bloco existente.
// O primeiro argumento é o nome do bloco, e os demais são os nomes dos processos.
//
// Uso: open-turkey block add-app redes-sociais discord telegram
var blockAddAppCmd = &cobra.Command{
	Use:   "add-app [bloco] [processos...]",
	Short: "Adicionar aplicativos a um bloco",
	// Mesma lógica do add-site: precisa do nome do bloco + pelo menos 1 processo.
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		// Os nomes dos processos que serão bloqueados (ex: "discord", "telegram").
		processNames := args[1:]

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := database.AddApps(name, processNames); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		ativo, err := database.IsBlockActive(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		if ativo {
			if err := reaplicarOuRemoverCamadas(database); err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Aplicativos adicionados ao bloco '%s' com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Subcomando: block remove-app
// --------------------------------------------------------------------------

// blockRemoveAppCmd remove um ou mais nomes de processos (apps) de um bloco existente.
//
// Uso: open-turkey block remove-app redes-sociais discord telegram
var blockRemoveAppCmd = &cobra.Command{
	Use:   "remove-app [bloco] [processos...]",
	Short: "Remover aplicativos de um bloco",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		processNames := args[1:]

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := database.RemoveApps(name, processNames); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		ativo, err := database.IsBlockActive(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		if ativo {
			detalhe, err := database.GetBlock(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}

			if len(detalhe.Sites) == 0 && len(detalhe.Apps) == 0 {
				if err := database.DeactivateBlock(name); err != nil {
					fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
					os.Exit(1)
				}
			}

			if err := reaplicarOuRemoverCamadas(database); err != nil {
				fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Aplicativos removidos do bloco '%s' com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Subcomando: block list
// --------------------------------------------------------------------------

// blockListCmd lista todos os blocos cadastrados no banco de dados.
// Exibe uma tabela formatada com o nome do bloco, quantidade de sites,
// quantidade de apps e a data de criação.
//
// Uso: open-turkey block list
var blockListCmd = &cobra.Command{
	Use:   "list",
	Short: "Listar todos os blocos",
	Run: func(cmd *cobra.Command, args []string) {
		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		// ListBlocks retorna uma slice (lista) de blocos com contagens.
		blocks, err := database.ListBlocks()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		// Se não há blocos cadastrados, exibimos uma mensagem amigável.
		if len(blocks) == 0 {
			fmt.Println("Nenhum bloco cadastrado.")
			return
		}

		// Imprimimos o cabeçalho da tabela.
		// %-20s = string alinhada à esquerda com 20 caracteres de largura.
		// %-6s  = string alinhada à esquerda com 6 caracteres.
		// Esse formato garante que as colunas fiquem alinhadas visualmente.
		fmt.Printf("%-20s %-6s %-6s %s\n", "BLOCO", "SITES", "APPS", "CRIADO EM")

		// Iteramos sobre cada bloco e imprimimos uma linha da tabela.
		for _, b := range blocks {
			// %-20s para o nome do bloco (alinhado à esquerda).
			// %-6d para os números (alinhados à esquerda, formato inteiro).
			// %s para a data de criação.
			fmt.Printf("%-20s %-6d %-6d %s\n", b.Name, b.SiteCount, b.AppCount, b.CreatedAt)
		}
	},
}

// --------------------------------------------------------------------------
// Subcomando: block info
// --------------------------------------------------------------------------

// blockInfoCmd exibe os detalhes completos de um bloco específico.
// Mostra o nome, data de criação, status de ativação, e as listas
// de sites e aplicativos bloqueados.
//
// Uso: open-turkey block info redes-sociais
var blockInfoCmd = &cobra.Command{
	Use:   "info [nome]",
	Short: "Mostrar detalhes de um bloco",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		// GetBlock retorna um ponteiro para BlockDetail com todas as informações.
		block, err := database.GetBlock(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		// Exibimos as informações básicas do bloco.
		fmt.Printf("Bloco: %s\n", block.Name)
		fmt.Printf("Criado em: %s\n", block.CreatedAt)

		// Montamos a linha de status baseada nos campos Active e Locked.
		// Um bloco pode estar:
		//   - Inativo (não está na tabela active_blocks)
		//   - Ativo (está ativo, mas sem trava)
		//   - Ativo e Travado (precisa digitar N caracteres para destravar)
		if block.Active {
			if block.Locked {
				// Quando travado, mostramos quantos caracteres são necessários
				// para destravar. Isso informa o nível de "atrito" configurado.
				fmt.Printf("Status: Ativo (Travado - %d caracteres)\n", block.LockChars)
			} else {
				fmt.Printf("Status: Ativo\n")
			}
		} else {
			fmt.Printf("Status: Inativo\n")
		}

		// Exibimos a lista de sites bloqueados.
		// Separamos com uma linha em branco para melhorar a legibilidade.
		fmt.Println()
		if len(block.Sites) > 0 {
			fmt.Println("Sites bloqueados:")
			for _, site := range block.Sites {
				fmt.Printf("  - %s\n", site)
			}
		} else {
			fmt.Println("Sites bloqueados: nenhum")
		}

		// Exibimos a lista de aplicativos bloqueados.
		fmt.Println()
		if len(block.Apps) > 0 {
			fmt.Println("Aplicativos bloqueados:")
			for _, app := range block.Apps {
				fmt.Printf("  - %s\n", app)
			}
		} else {
			fmt.Println("Aplicativos bloqueados: nenhum")
		}
	},
}

// --------------------------------------------------------------------------
// Subcomando: block remove
// --------------------------------------------------------------------------

// blockRemoveCmd remove um bloco pelo nome.
// Se o bloco estiver ativo, a remoção é impedida pelo banco de dados —
// o usuário precisa desativar o bloco antes de removê-lo.
//
// Uso: open-turkey block remove redes-sociais
var blockRemoveCmd = &cobra.Command{
	Use:   "remove [nome]",
	Short: "Remover um bloco",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		database, err := openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := database.RemoveBlock(name); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Bloco '%s' removido com sucesso.\n", name)
	},
}

// --------------------------------------------------------------------------
// Registro dos comandos (init)
// --------------------------------------------------------------------------

// init() é uma função especial do Go que roda automaticamente quando o pacote
// é importado. Usamos ela para registrar os subcomandos na árvore de comandos
// do Cobra. A ordem de registro não importa — o Cobra organiza tudo internamente.
//
// Hierarquia resultante:
//   open-turkey (rootCmd)
//   └── block (blockCmd)
//       ├── create   (blockCreateCmd)
//       ├── add-site (blockAddSiteCmd)
//       ├── remove-site (blockRemoveSiteCmd)
//       ├── add-app  (blockAddAppCmd)
//       ├── remove-app  (blockRemoveAppCmd)
//       ├── list     (blockListCmd)
//       ├── info     (blockInfoCmd)
//       └── remove   (blockRemoveCmd)
func init() {
	// Adicionamos o comando "block" como filho do comando raiz.
	rootCmd.AddCommand(blockCmd)

	// Adicionamos todos os subcomandos como filhos de "block".
	blockCmd.AddCommand(blockCreateCmd)
	blockCmd.AddCommand(blockAddSiteCmd)
	blockCmd.AddCommand(blockRemoveSiteCmd)
	blockCmd.AddCommand(blockAddAppCmd)
	blockCmd.AddCommand(blockRemoveAppCmd)
	blockCmd.AddCommand(blockListCmd)
	blockCmd.AddCommand(blockInfoCmd)
	blockCmd.AddCommand(blockRemoveCmd)
}
