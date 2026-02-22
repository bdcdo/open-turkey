# Open Turkey

Bloqueador de produtividade para Linux via linha de comando (CLI) + serviço `systemd`.

O Open Turkey organiza o bloqueio em **blocos** (grupos nomeados) de:
- **Sites** (domínios, ex: `youtube.com`)
- **Apps** (nomes de processos, ex: `discord`, `telegram-desktop`)

Quando você **ativa** um bloco, o daemon garante continuamente que as camadas de bloqueio estejam aplicadas.

## Instalação

Pré-requisitos:
- Linux com `systemd`
- `iptables` disponível

Instalação (recomendado):
```bash
sudo ./install.sh
```

Isso:
- compila o binário
- instala `open-turkey` (wrapper) e `open-turkey-bin` (binário real) em `/usr/local/bin`
- cria a regra em `/etc/sudoers.d/open-turkey` para rodar sem senha
- instala e inicia o serviço `systemd` `open-turkey.service`
- cria o banco em `/var/lib/open-turkey/open-turkey.db`

## Conceitos

- **Bloco**: conjunto de sites/apps que você quer bloquear junto.
- **Ativo**: bloco em vigor (o sistema está bloqueando).
- **Trava (`--lock`)**: quando ativado, impede desativação com `stop`. O único jeito de desativar é `unlock` (desafio de digitação).

## Comandos principais

### 1) Criar e configurar um bloco

```bash
open-turkey block create redes-sociais
open-turkey block add-site redes-sociais instagram.com x.com facebook.com
open-turkey block add-app  redes-sociais discord telegram
```

Ver detalhes:
```bash
open-turkey block info redes-sociais
```

Listar todos os blocos:
```bash
open-turkey block list
```

### 2) Ativar / desativar

Ativar:
```bash
open-turkey start redes-sociais
```

Ativar com trava:
```bash
open-turkey start redes-sociais --lock
```

Ver status:
```bash
open-turkey status
```

Desativar (se não estiver travado):
```bash
open-turkey stop redes-sociais
```

Desbloquear um bloco travado (isso **desativa** o bloco):
```bash
open-turkey unlock redes-sociais
```

### 3) Como remover um site da lista de links bloqueados

Para remover um domínio de um bloco:
```bash
open-turkey block remove-site redes-sociais instagram.com
```

Notas importantes:
- Se o bloco estiver **ativo**, o Open Turkey reaplica as camadas para o desbloqueio ter efeito imediatamente.
- Se o bloco estiver **ativo e travado**, você **não consegue** remover sites/apps dele. Primeiro faça `open-turkey unlock <bloco>` (que desativa), depois ajuste e por fim ative novamente.

Remover um app (processo) de um bloco:
```bash
open-turkey block remove-app redes-sociais discord
```

Remover um bloco inteiro (precisa estar inativo):
```bash
open-turkey block remove redes-sociais
```

## Serviço (daemon) e logs

O daemon é gerenciado por `systemd`:
```bash
systemctl status open-turkey
systemctl restart open-turkey
journalctl -u open-turkey -f
```

## Desinstalação

```bash
sudo make uninstall
```

