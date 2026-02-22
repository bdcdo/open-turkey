// Package db fornece a camada de acesso ao banco de dados SQLite para o Open Turkey.
//
// Usamos o driver "modernc.org/sqlite" porque ele é uma implementação pura em Go,
// ou seja, não precisa de CGo nem de bibliotecas C instaladas no sistema.
// Isso facilita a compilação cruzada e a distribuição do binário final.
package db

import (
	"database/sql"
	"fmt"
	"strings"

	// Importamos o driver SQLite puro em Go. O underscore (_) na frente significa
	// que não vamos chamar funções desse pacote diretamente — ele se registra
	// sozinho como driver SQL quando é importado. É um padrão comum em Go.
	_ "modernc.org/sqlite"
)

// DefaultDBPath é o caminho padrão onde o banco de dados será armazenado.
// Usamos /var/lib/ porque é o diretório padrão no Linux para dados persistentes
// de aplicações (seguindo o Filesystem Hierarchy Standard — FHS).
const DefaultDBPath = "/var/lib/open-turkey/open-turkey.db"

// --------------------------------------------------------------------------
// Tipos (structs) — representam os dados que transitam entre o banco e o app
// --------------------------------------------------------------------------

// DB encapsula a conexão com o banco de dados.
// Encapsulamos em vez de expor *sql.DB diretamente para controlar quais
// operações são permitidas e manter a interface pública limpa.
type DB struct {
	conn *sql.DB
}

// Block representa um bloco na listagem geral.
// SiteCount e AppCount evitam carregar todos os domínios/apps só para contar.
type Block struct {
	ID        int
	Name      string
	SiteCount int
	AppCount  int
	CreatedAt string
}

// BlockDetail traz todas as informações de um bloco específico,
// incluindo as listas completas de sites e apps bloqueados.
type BlockDetail struct {
	ID        int
	Name      string
	Sites     []string
	Apps      []string
	Active    bool
	Locked    bool
	LockChars int
	CreatedAt string
}

// ActiveBlockDetail representa um bloco que está ativo no momento.
// É usado pelo daemon para saber o que bloquear.
type ActiveBlockDetail struct {
	BlockName string
	Sites     []string
	Apps      []string
	Locked    bool
	LockChars int
}

// --------------------------------------------------------------------------
// Migrations (esquema do banco)
// --------------------------------------------------------------------------

// migrations contém o SQL que cria as tabelas do banco.
// Colocamos tudo numa única string porque é simples e direto.
//
// Decisões de design do esquema:
//   - blocks: tabela central. O campo "name" é UNIQUE para evitar blocos duplicados.
//   - sites: domínios associados a um bloco. ON DELETE CASCADE garante que,
//     ao apagar um bloco, seus sites são removidos automaticamente.
//   - apps: processos associados a um bloco. Mesma lógica de CASCADE.
//   - active_blocks: registra quais blocos estão ativos no momento.
//     O campo "locked" indica se o usuário travou o bloco (não pode desativar fácil).
//     O campo "lock_chars" armazena quantos caracteres aleatórios o usuário
//     precisa digitar para destravar (mecanismo de "atrito" para evitar
//     desativação impulsiva). block_id é UNIQUE porque um bloco só pode
//     estar ativo uma vez.
const migrations = `
CREATE TABLE IF NOT EXISTS blocks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sites (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    block_id INTEGER NOT NULL,
    domain   TEXT NOT NULL,
    FOREIGN KEY (block_id) REFERENCES blocks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS apps (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    block_id     INTEGER NOT NULL,
    process_name TEXT NOT NULL,
    FOREIGN KEY (block_id) REFERENCES blocks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS active_blocks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    block_id     INTEGER UNIQUE NOT NULL,
    locked       BOOLEAN NOT NULL DEFAULT 0,
    lock_chars   INTEGER NOT NULL DEFAULT 0,
    activated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (block_id) REFERENCES blocks(id) ON DELETE CASCADE
);
`

// --------------------------------------------------------------------------
// Abertura e fechamento do banco
// --------------------------------------------------------------------------

// OpenDB abre (ou cria) o banco de dados SQLite no caminho fornecido.
//
// Passos importantes que acontecem aqui:
//  1. Abrimos a conexão usando o driver "sqlite" (registrado pelo modernc.org/sqlite).
//  2. Ativamos foreign keys — por padrão o SQLite NÃO aplica chaves estrangeiras!
//     Sem esse PRAGMA, o ON DELETE CASCADE não funcionaria.
//  3. Ativamos o modo WAL (Write-Ahead Logging) — isso permite leituras e escritas
//     simultâneas sem travamentos. É muito melhor que o modo padrão (journal)
//     para aplicações que leem e escrevem ao mesmo tempo (ex: daemon + CLI).
//  4. Rodamos as migrations para garantir que as tabelas existam.
func OpenDB(dbPath string) (*DB, error) {
	// "sqlite" é o nome do driver registrado pelo modernc.org/sqlite.
	// O dbPath é o caminho do arquivo .db no disco.
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir banco de dados em '%s': %w", dbPath, err)
	}

	// Verificamos se a conexão realmente funciona.
	// sql.Open() não conecta de verdade — ele só valida os parâmetros.
	// Ping() força uma conexão real.
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro ao conectar ao banco de dados: %w", err)
	}

	// Ativamos chaves estrangeiras. Sem isso, o SQLite ignora FOREIGN KEY
	// e o CASCADE não funciona. Esse é um dos "pegadinhas" mais comuns do SQLite.
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro ao ativar chaves estrangeiras: %w", err)
	}

	// WAL (Write-Ahead Logging) permite que leituras aconteçam enquanto
	// uma escrita está em andamento. No modo padrão (journal), o banco
	// trava completamente durante escritas.
	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro ao ativar modo WAL: %w", err)
	}

	// Rodamos as migrations para criar as tabelas caso não existam.
	// "IF NOT EXISTS" garante que rodar isso várias vezes é seguro (idempotente).
	if _, err := conn.Exec(migrations); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro ao criar tabelas do banco: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close fecha a conexão com o banco de dados.
// Sempre chame Close() quando terminar de usar o banco (defer db.Close()).
func (d *DB) Close() error {
	if err := d.conn.Close(); err != nil {
		return fmt.Errorf("erro ao fechar banco de dados: %w", err)
	}
	return nil
}

// --------------------------------------------------------------------------
// CRUD de blocos
// --------------------------------------------------------------------------

// CreateBlock cria um novo bloco com o nome fornecido.
// O nome precisa ser único — se já existir, o banco retorna erro.
func (d *DB) CreateBlock(name string) error {
	_, err := d.conn.Exec("INSERT INTO blocks (name) VALUES (?)", name)
	if err != nil {
		// Se o erro for de UNIQUE constraint, damos uma mensagem mais clara.
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("já existe um bloco com o nome '%s'", name)
		}
		return fmt.Errorf("erro ao criar bloco '%s': %w", name, err)
	}
	return nil
}

// AddSites adiciona uma lista de domínios a um bloco existente.
//
// Usamos uma transação (tx) para garantir que ou TODOS os domínios são
// inseridos, ou NENHUM é. Isso evita estados inconsistentes — por exemplo,
// se o terceiro domínio falhar, os dois primeiros são desfeitos (rollback).
func (d *DB) AddSites(blockName string, domains []string) error {
	// Primeiro, buscamos o ID do bloco pelo nome.
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	// Iniciamos uma transação.
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	// defer tx.Rollback() é seguro mesmo se tx.Commit() já foi chamado.
	// Se Commit() foi bem-sucedido, Rollback() simplesmente não faz nada.
	defer tx.Rollback()

	// Preparamos o statement uma vez e reutilizamos para cada domínio.
	// Isso é mais eficiente do que executar INSERT separadamente para cada um.
	stmt, err := tx.Prepare("INSERT INTO sites (block_id, domain) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("erro ao preparar inserção de sites: %w", err)
	}
	defer stmt.Close()

	for _, domain := range domains {
		if _, err := stmt.Exec(blockID, domain); err != nil {
			return fmt.Errorf("erro ao adicionar domínio '%s' ao bloco '%s': %w", domain, blockName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("erro ao salvar sites do bloco '%s': %w", blockName, err)
	}

	return nil
}

// AddApps adiciona uma lista de nomes de processos a um bloco existente.
// A lógica é idêntica à de AddSites — usamos transação pelo mesmo motivo.
func (d *DB) AddApps(blockName string, processNames []string) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO apps (block_id, process_name) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("erro ao preparar inserção de apps: %w", err)
	}
	defer stmt.Close()

	for _, name := range processNames {
		if _, err := stmt.Exec(blockID, name); err != nil {
			return fmt.Errorf("erro ao adicionar processo '%s' ao bloco '%s': %w", name, blockName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("erro ao salvar apps do bloco '%s': %w", blockName, err)
	}

	return nil
}

// RemoveSites remove uma lista de domínios de um bloco existente.
//
// A operação é idempotente: se um domínio não existir no bloco, ele é ignorado.
func (d *DB) RemoveSites(blockName string, domains []string) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM sites WHERE block_id = ? AND domain = ?")
	if err != nil {
		return fmt.Errorf("erro ao preparar remoção de sites: %w", err)
	}
	defer stmt.Close()

	for _, domain := range domains {
		result, err := stmt.Exec(blockID, domain)
		if err != nil {
			return fmt.Errorf("erro ao remover domínio '%s' do bloco '%s': %w", domain, blockName, err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("erro ao verificar remoção do domínio '%s': %w", domain, err)
		}
		if rowsAffected == 0 {
			return fmt.Errorf("domínio '%s' não encontrado no bloco '%s'", domain, blockName)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("erro ao salvar remoção de sites do bloco '%s': %w", blockName, err)
	}

	return nil
}

// RemoveApps remove uma lista de nomes de processos de um bloco existente.
//
// A operação é idempotente: se um processo não existir no bloco, ele é ignorado.
func (d *DB) RemoveApps(blockName string, processNames []string) error {
	blockID, err := d.getBlockID(blockName)
	if err != nil {
		return err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM apps WHERE block_id = ? AND process_name = ?")
	if err != nil {
		return fmt.Errorf("erro ao preparar remoção de apps: %w", err)
	}
	defer stmt.Close()

	for _, name := range processNames {
		result, err := stmt.Exec(blockID, name)
		if err != nil {
			return fmt.Errorf("erro ao remover processo '%s' do bloco '%s': %w", name, blockName, err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("erro ao verificar remoção do processo '%s': %w", name, err)
		}
		if rowsAffected == 0 {
			return fmt.Errorf("processo '%s' não encontrado no bloco '%s'", name, blockName)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("erro ao salvar remoção de apps do bloco '%s': %w", blockName, err)
	}

	return nil
}

// ListBlocks retorna todos os blocos cadastrados com a contagem de sites e apps.
//
// Usamos LEFT JOIN + COUNT(DISTINCT ...) para buscar tudo numa única query.
// LEFT JOIN garante que blocos sem sites ou apps também apareçam (com contagem 0).
// DISTINCT evita contagem duplicada quando há múltiplos joins.
func (d *DB) ListBlocks() ([]Block, error) {
	query := `
		SELECT
			b.id,
			b.name,
			COUNT(DISTINCT s.id) AS site_count,
			COUNT(DISTINCT a.id) AS app_count,
			b.created_at
		FROM blocks b
		LEFT JOIN sites s ON s.block_id = b.id
		LEFT JOIN apps  a ON a.block_id = b.id
		GROUP BY b.id
		ORDER BY b.name
	`

	rows, err := d.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("erro ao listar blocos: %w", err)
	}
	// defer rows.Close() é obrigatório para liberar os recursos do cursor.
	// Sem isso, a conexão pode ficar "presa" e causar deadlocks.
	defer rows.Close()

	var blocks []Block
	for rows.Next() {
		var b Block
		if err := rows.Scan(&b.ID, &b.Name, &b.SiteCount, &b.AppCount, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("erro ao ler dados do bloco: %w", err)
		}
		blocks = append(blocks, b)
	}

	// rows.Err() verifica se houve algum erro durante a iteração.
	// É diferente do erro retornado por rows.Next() — cobre erros de rede, etc.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar blocos: %w", err)
	}

	return blocks, nil
}

// GetBlock retorna os detalhes completos de um bloco: sites, apps, estado de ativação.
func (d *DB) GetBlock(name string) (*BlockDetail, error) {
	// Buscamos os dados básicos do bloco + informações de ativação (se houver).
	// LEFT JOIN com active_blocks porque o bloco pode não estar ativo.
	var detail BlockDetail
	err := d.conn.QueryRow(`
		SELECT
			b.id,
			b.name,
			b.created_at,
			CASE WHEN ab.id IS NOT NULL THEN 1 ELSE 0 END AS active,
			COALESCE(ab.locked, 0) AS locked,
			COALESCE(ab.lock_chars, 0) AS lock_chars
		FROM blocks b
		LEFT JOIN active_blocks ab ON ab.block_id = b.id
		WHERE b.name = ?
	`, name).Scan(
		&detail.ID,
		&detail.Name,
		&detail.CreatedAt,
		&detail.Active,
		&detail.Locked,
		&detail.LockChars,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("bloco '%s' não encontrado", name)
	}
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar bloco '%s': %w", name, err)
	}

	// Buscamos os sites do bloco.
	detail.Sites, err = d.getBlockSites(detail.ID)
	if err != nil {
		return nil, err
	}

	// Buscamos os apps do bloco.
	detail.Apps, err = d.getBlockApps(detail.ID)
	if err != nil {
		return nil, err
	}

	return &detail, nil
}

// RemoveBlock apaga um bloco pelo nome.
// Se o bloco estiver ativo, a remoção é impedida — o usuário precisa
// desativar antes de remover. Isso evita remoção acidental enquanto
// o bloqueio está em vigor.
func (d *DB) RemoveBlock(name string) error {
	// Verificamos se o bloco está ativo antes de tentar remover.
	active, err := d.IsBlockActive(name)
	if err != nil {
		return err
	}
	if active {
		return fmt.Errorf("não é possível remover o bloco '%s' porque ele está ativo. Desative-o primeiro", name)
	}

	result, err := d.conn.Exec("DELETE FROM blocks WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("erro ao remover bloco '%s': %w", name, err)
	}

	// Verificamos se alguma linha foi realmente apagada.
	// Se RowsAffected() == 0, o bloco não existia.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("erro ao verificar remoção do bloco '%s': %w", name, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("bloco '%s' não encontrado", name)
	}

	return nil
}

// --------------------------------------------------------------------------
// Ativação e desativação de blocos
// --------------------------------------------------------------------------

// ActivateBlock marca um bloco como ativo. Isso faz com que os sites e apps
// desse bloco passem a ser bloqueados pelo daemon.
//
// Parâmetros:
//   - locked: se true, o bloco não pode ser desativado facilmente.
//   - lockChars: quantos caracteres aleatórios o usuário precisa digitar
//     para desbloquear. Quanto mais caracteres, mais "atrito" para desativar.
func (d *DB) ActivateBlock(name string, locked bool, lockChars int) error {
	blockID, err := d.getBlockID(name)
	if err != nil {
		return err
	}

	_, err = d.conn.Exec(
		"INSERT INTO active_blocks (block_id, locked, lock_chars) VALUES (?, ?, ?)",
		blockID, locked, lockChars,
	)
	if err != nil {
		// Se o bloco já está ativo, block_id UNIQUE vai causar erro.
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("bloco '%s' já está ativo", name)
		}
		return fmt.Errorf("erro ao ativar bloco '%s': %w", name, err)
	}

	return nil
}

// DeactivateBlock remove o bloco da tabela de blocos ativos.
// Se o bloco não estiver ativo, retorna erro.
func (d *DB) DeactivateBlock(name string) error {
	blockID, err := d.getBlockID(name)
	if err != nil {
		return err
	}

	result, err := d.conn.Exec("DELETE FROM active_blocks WHERE block_id = ?", blockID)
	if err != nil {
		return fmt.Errorf("erro ao desativar bloco '%s': %w", name, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("erro ao verificar desativação do bloco '%s': %w", name, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("bloco '%s' não está ativo", name)
	}

	return nil
}

// IsBlockActive verifica se um bloco está ativo no momento.
func (d *DB) IsBlockActive(name string) (bool, error) {
	var count int
	err := d.conn.QueryRow(`
		SELECT COUNT(*) FROM active_blocks ab
		JOIN blocks b ON b.id = ab.block_id
		WHERE b.name = ?
	`, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("erro ao verificar se bloco '%s' está ativo: %w", name, err)
	}
	return count > 0, nil
}

// IsBlockLocked verifica se um bloco ativo está travado.
// Se o bloco não estiver ativo, retorna false (não está travado).
func (d *DB) IsBlockLocked(name string) (bool, error) {
	var locked bool
	err := d.conn.QueryRow(`
		SELECT COALESCE(ab.locked, 0) FROM active_blocks ab
		JOIN blocks b ON b.id = ab.block_id
		WHERE b.name = ?
	`, name).Scan(&locked)
	if err == sql.ErrNoRows {
		// Se não está ativo, não está travado.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("erro ao verificar se bloco '%s' está travado: %w", name, err)
	}
	return locked, nil
}

// --------------------------------------------------------------------------
// Consultas para o daemon (bloqueios ativos)
// --------------------------------------------------------------------------

// GetActiveBlocks retorna todos os blocos ativos com seus sites e apps.
// O daemon usa isso para saber o que bloquear.
func (d *DB) GetActiveBlocks() ([]ActiveBlockDetail, error) {
	rows, err := d.conn.Query(`
		SELECT b.id, b.name, ab.locked, ab.lock_chars
		FROM active_blocks ab
		JOIN blocks b ON b.id = ab.block_id
	`)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar blocos ativos: %w", err)
	}
	defer rows.Close()

	var activeBlocks []ActiveBlockDetail
	for rows.Next() {
		var blockID int
		var abd ActiveBlockDetail

		if err := rows.Scan(&blockID, &abd.BlockName, &abd.Locked, &abd.LockChars); err != nil {
			return nil, fmt.Errorf("erro ao ler bloco ativo: %w", err)
		}

		// Para cada bloco ativo, buscamos seus sites e apps.
		abd.Sites, err = d.getBlockSites(blockID)
		if err != nil {
			return nil, err
		}

		abd.Apps, err = d.getBlockApps(blockID)
		if err != nil {
			return nil, err
		}

		activeBlocks = append(activeBlocks, abd)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar blocos ativos: %w", err)
	}

	return activeBlocks, nil
}

// GetAllBlockedDomains retorna todos os domínios de todos os blocos ativos.
// O daemon usa isso para atualizar o /etc/hosts ou o firewall.
// Retornamos uma lista "achatada" (flat) porque o daemon não precisa
// saber a qual bloco cada domínio pertence — só precisa da lista completa.
func (d *DB) GetAllBlockedDomains() ([]string, error) {
	rows, err := d.conn.Query(`
		SELECT DISTINCT s.domain
		FROM sites s
		JOIN active_blocks ab ON ab.block_id = s.block_id
		ORDER BY s.domain
	`)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar domínios bloqueados: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("erro ao ler domínio bloqueado: %w", err)
		}
		domains = append(domains, domain)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar domínios bloqueados: %w", err)
	}

	return domains, nil
}

// GetAllBlockedApps retorna todos os nomes de processos de todos os blocos ativos.
// O daemon usa isso para matar processos que o usuário quer bloquear.
func (d *DB) GetAllBlockedApps() ([]string, error) {
	rows, err := d.conn.Query(`
		SELECT DISTINCT a.process_name
		FROM apps a
		JOIN active_blocks ab ON ab.block_id = a.block_id
		ORDER BY a.process_name
	`)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar apps bloqueados: %w", err)
	}
	defer rows.Close()

	var apps []string
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			return nil, fmt.Errorf("erro ao ler app bloqueado: %w", err)
		}
		apps = append(apps, app)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar apps bloqueados: %w", err)
	}

	return apps, nil
}

// --------------------------------------------------------------------------
// Manutenção de dados
// --------------------------------------------------------------------------

// SiteRow representa uma linha da tabela sites (ID + domínio).
type SiteRow struct {
	ID     int
	Domain string
}

// GetAllSitesRaw retorna todos os sites de todos os blocos (ID + domínio).
// Usado para migração/normalização de dados existentes.
func (d *DB) GetAllSitesRaw() ([]SiteRow, error) {
	rows, err := d.conn.Query("SELECT id, domain FROM sites")
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar sites: %w", err)
	}
	defer rows.Close()

	var sites []SiteRow
	for rows.Next() {
		var s SiteRow
		if err := rows.Scan(&s.ID, &s.Domain); err != nil {
			return nil, fmt.Errorf("erro ao ler site: %w", err)
		}
		sites = append(sites, s)
	}
	return sites, rows.Err()
}

// UpdateSiteDomain atualiza o domínio de um site pelo ID.
func (d *DB) UpdateSiteDomain(id int, domain string) error {
	_, err := d.conn.Exec("UPDATE sites SET domain = ? WHERE id = ?", domain, id)
	return err
}

// --------------------------------------------------------------------------
// Funções auxiliares (privadas)
// --------------------------------------------------------------------------

// getBlockID busca o ID de um bloco pelo nome.
// É uma função auxiliar usada por várias outras funções que precisam
// converter o nome (que o usuário fornece) em ID (que o banco usa internamente).
func (d *DB) getBlockID(name string) (int, error) {
	var id int
	err := d.conn.QueryRow("SELECT id FROM blocks WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("bloco '%s' não encontrado", name)
	}
	if err != nil {
		return 0, fmt.Errorf("erro ao buscar bloco '%s': %w", name, err)
	}
	return id, nil
}

// getBlockSites retorna todos os domínios de um bloco pelo ID.
func (d *DB) getBlockSites(blockID int) ([]string, error) {
	rows, err := d.conn.Query("SELECT domain FROM sites WHERE block_id = ? ORDER BY domain", blockID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar sites do bloco: %w", err)
	}
	defer rows.Close()

	var sites []string
	for rows.Next() {
		var site string
		if err := rows.Scan(&site); err != nil {
			return nil, fmt.Errorf("erro ao ler site: %w", err)
		}
		sites = append(sites, site)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar sites: %w", err)
	}

	return sites, nil
}

// getBlockApps retorna todos os nomes de processos de um bloco pelo ID.
func (d *DB) getBlockApps(blockID int) ([]string, error) {
	rows, err := d.conn.Query("SELECT process_name FROM apps WHERE block_id = ? ORDER BY process_name", blockID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar apps do bloco: %w", err)
	}
	defer rows.Close()

	var apps []string
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			return nil, fmt.Errorf("erro ao ler app: %w", err)
		}
		apps = append(apps, app)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar apps: %w", err)
	}

	return apps, nil
}
