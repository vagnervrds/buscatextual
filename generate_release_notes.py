import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.request

# Configura saida padrao do console para UTF-8 sem falhas de encoding no Windows
if sys.stdout.encoding != "utf-8":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CONFIG_FILENAME = "release_ai_config.json"
CONFIG_EXAMPLE_FILENAME = "release_ai_config.example.json"


def get_git_remote_repo():
    """Detecta automaticamente o repositorio GitHub configurado no Git remoto (origin)."""
    try:
        cmd = ["git", "config", "--get", "remote.origin.url"]
        res = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
        if res.returncode == 0 and res.stdout.strip():
            url = res.stdout.strip()
            match = re.search(r"github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$", url)
            if match:
                return match.group(1)
    except Exception:
        pass
    return ""


def get_build_info():
    """Obtem informacoes do contador incremental e data a partir do build.json."""
    build_path = os.path.join(SCRIPT_DIR, "build.json")
    if os.path.exists(build_path):
        try:
            with open(build_path, "r", encoding="utf-8") as f:
                data = json.load(f)
                b_num = data.get("build")
                b_date = data.get("date", "")
                if b_num is not None:
                    return {"build": str(b_num).strip(), "date": str(b_date).strip()}
        except Exception:
            pass
    return {"build": "1", "date": ""}


def get_project_metadata():
    """Obtem metadados do projeto a partir de manifestos padrao (package.json, build.json)."""
    meta = {
        "name": "",
        "description": "",
        "version": "",
    }

    # 1. Carrega do package.json
    pkg_path = os.path.join(SCRIPT_DIR, "package.json")
    if os.path.exists(pkg_path):
        try:
            with open(pkg_path, "r", encoding="utf-8") as f:
                data = json.load(f)
                meta["name"] = str(data.get("name", "")).strip()
                meta["description"] = str(data.get("description", "")).strip()
                meta["version"] = str(data.get("version", "")).strip()
        except Exception:
            pass

    # 2. Informacoes de build
    b_info = get_build_info()
    meta["build"] = b_info["build"]
    meta["build_date"] = b_info["date"]

    if not meta["name"]:
        meta["name"] = os.path.basename(os.path.abspath(SCRIPT_DIR))
    if not meta["version"]:
        meta["version"] = meta["build"]

    return meta


def load_config():
    """Carrega as configuracoes a partir do arquivo JSON, metadados ou variaveis de ambiente."""
    meta = get_project_metadata()
    detected_repo = get_git_remote_repo()

    config = {
        "app_name": meta.get("name", "Application"),
        "app_description": meta.get("description", ""),
        "github_repo": detected_repo,
        "api_url": "https://api.openai.com/v1/chat/completions",
        "api_key": "",
        "model_name": "gemini-2.5-flash",
        "temperature": 0.3,
        "timeout_seconds": 30,
        "commit_limit": 30,
        "cleanup_keep_releases": 3,
        "tag_prefix": "Build-",
        "asset_paths": [],
        "custom_prompt": "",
    }

    config_paths = [
        os.path.join(SCRIPT_DIR, CONFIG_FILENAME),
        os.path.join(os.getcwd(), CONFIG_FILENAME),
    ]

    config_found = False
    for path in config_paths:
        if os.path.exists(path):
            try:
                with open(path, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    config.update(data)
                    config_found = True
                    break
            except Exception as e:
                print(f"[Aviso] Erro ao ler '{path}': {e}")

    # Permite sobrescrever via variaveis de ambiente
    if os.getenv("APP_NAME"):
        config["app_name"] = os.getenv("APP_NAME")
    if os.getenv("APP_DESCRIPTION"):
        config["app_description"] = os.getenv("APP_DESCRIPTION")
    if os.getenv("GITHUB_REPO"):
        config["github_repo"] = os.getenv("GITHUB_REPO")
    if os.getenv("AI_API_URL"):
        config["api_url"] = os.getenv("AI_API_URL")
    if os.getenv("AI_API_KEY"):
        config["api_key"] = os.getenv("AI_API_KEY")
    if os.getenv("AI_MODEL_NAME"):
        config["model_name"] = os.getenv("AI_MODEL_NAME")

    if not config_found and not config.get("api_key"):
        print(f"[Info] Arquivo '{CONFIG_FILENAME}' nao encontrado. Copie '{CONFIG_EXAMPLE_FILENAME}' para customizar.")

    return config


def get_git_commits(limit=30):
    """Obtem o historico recente de commits do Git, priorizando commits desde a ultima tag."""
    try:
        tags_cmd = ["git", "describe", "--tags", "--abbrev=0"]
        tag_res = subprocess.run(tags_cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
        if tag_res.returncode == 0 and tag_res.stdout.strip():
            last_tag = tag_res.stdout.strip()
            cmd = ["git", "log", f"{last_tag}..HEAD", "--pretty=format:* %s (%ad)", "--date=short"]
            result = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
            if result.returncode == 0 and result.stdout.strip():
                return result.stdout.strip()

        cmd = [
            "git",
            "log",
            f"-{limit}",
            "--pretty=format:* %s (%ad)",
            "--date=short",
        ]
        result = subprocess.run(
            cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    except Exception as e:
        print(f"[Aviso] Nao foi possivel obter commits do git: {e}")
    return ""


def get_version(config):
    """Obtem a versao do projeto."""
    meta = get_project_metadata()
    return meta.get("version", "1.0.0")


def generate_notes_with_ai(version_label, commits, config):
    """Envia os commits para a IA gerar notas de lancamento formatadas."""
    api_url = config.get("api_url")
    api_key = config.get("api_key", "").strip()
    model_name = config.get("model_name", "gemini-2.5-flash")
    temperature = config.get("temperature", 0.3)
    timeout = config.get("timeout_seconds", 30)

    if not api_key or api_key in ("SEU_API_KEY_AQUI", "YOUR_API_KEY_HERE"):
        raise ValueError("Chave de API nao configurada no release_ai_config.json")

    app_name = config.get("app_name", "BuscaTextual")
    app_desc = config.get("app_description", "").strip()

    custom_prompt = config.get("custom_prompt", "").strip()
    if custom_prompt:
        try:
            prompt = custom_prompt.format(
                app_name=app_name,
                app_description=app_desc,
                version=version_label,
                commits=commits,
            )
        except Exception:
            prompt = custom_prompt
    else:
        app_context = f"o aplicativo **{app_name}**"
        if app_desc:
            app_context += f" ({app_desc})"
        else:
            app_context += " (um buscador ultrarrápido de arquivos e conteúdos em Go para Windows, com terminal interativo e dashboard web moderno)"

        prompt = f"""Você é um especialista em engenharia de software criando Release Notes (Notas de Lançamento) elegantes, empolgantes e profissionais para {app_context}.

Abaixo estão as alterações e commits mais recentes do projeto:
{commits}

Instruções para as Release Notes da versão **{version_label}**:
1. Crie uma introdução curta e envolvente destacando as principais conquistas e o foco desta versão.
2. Agrupe as mudanças de forma inteligente e descritiva em seções claras com emojis (por exemplo:
   - 🚀 Novidades e Funcionalidades
   - 🎨 Interface Web & Experiência do Usuário
   - ⚡ Performance, Estabilidade e Correções
   - 🛠️ Melhorias Técnicas e Refatorações
3. Não liste commits mecanicamente; sintetize o valor real que cada mudança traz para quem usa o sistema.
4. Use formatação limpa e moderna em Markdown (negrito para pontos-chave, listas com marcadores organizadas).
5. Linguagem em português do Brasil, tom profissional, amigável e direto ao ponto.
6. Retorne APENAS o conteúdo em Markdown formatado (sem blocos ```markdown envolvendo todo o texto)."""

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}",
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    }

    payload = {
        "model": model_name,
        "messages": [
            {
                "role": "system",
                "content": "Você é um gerador técnico de release notes objetivo e preciso.",
            },
            {"role": "user", "content": prompt},
        ],
        "temperature": temperature,
    }

    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(api_url, data=data, headers=headers)

    with urllib.request.urlopen(req, timeout=timeout) as resp:
        res = json.loads(resp.read().decode("utf-8"))
        content = res["choices"][0]["message"]["content"].strip()
        if content.startswith("```markdown"):
            content = content[len("```markdown") :].strip()
        elif content.startswith("```"):
            content = content[3:].strip()
        if content.endswith("```"):
            content = content[:-3].strip()
        return content


def get_available_assets(config):
    """Identifica quais arquivos binarios/assets definidos na config estao disponiveis."""
    asset_paths = config.get("asset_paths", [])
    if not asset_paths:
        default_exe = os.path.join(SCRIPT_DIR, "buscatextual.exe")
        if os.path.exists(default_exe):
            return [default_exe]

    valid_assets = []
    for rel_path in asset_paths:
        full_path = os.path.join(SCRIPT_DIR, rel_path)
        if os.path.exists(full_path):
            valid_assets.append(full_path)
    return valid_assets


def check_gh_installed():
    """Verifica se o utilitario GitHub CLI (gh) esta disponivel no sistema."""
    return shutil.which("gh") is not None


def check_release_exists(tag, repo=None):
    """Verifica se a release ja existe no GitHub."""
    cmd = ["gh", "release", "view", tag]
    if repo:
        cmd.extend(["--repo", repo])
    res = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
    return res.returncode == 0


def commit_pending_release_files(build_num=None):
    """Garante que build.json e o executavel gerado sejam commitados antes do push."""
    try:
        files_to_check = ["build.json", "buscatextual.exe"]
        status_cmd = ["git", "status", "--porcelain"] + files_to_check
        res = subprocess.run(status_cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
        if res.returncode == 0 and res.stdout.strip():
            print("\n[Git] Modificacoes detectadas em arquivos de release (build.json/buscatextual.exe).")
            print("[Git] Criando commit automatico de release...")
            subprocess.run(["git", "add"] + files_to_check, capture_output=True, cwd=SCRIPT_DIR)
            ver_str = f"build {build_num}" if build_num else "new build"
            commit_msg = f"chore(release): bump {ver_str}"
            commit_res = subprocess.run(["git", "commit", "-m", commit_msg], capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
            if commit_res.returncode == 0:
                print(f"[OK] Commit automatico criado: {commit_msg}")
            else:
                err_msg = (commit_res.stderr or commit_res.stdout or "").strip()
                if err_msg:
                    print(f"[Aviso] Nao foi possivel criar o commit: {err_msg}")
    except Exception as e:
        print(f"[Aviso] Falha ao verificar/commitar arquivos de release: {e}")


def push_git_commits(build_num=None):
    """Envia os commits locais para o repositorio remoto no GitHub antes de publicar a release."""
    commit_pending_release_files(build_num=build_num)
    print("\n[Git] Enviando commits locais para o GitHub (git push)...")
    res = subprocess.run(["git", "push"], capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
    output = ((res.stdout or "") + "\n" + (res.stderr or "")).strip()

    if res.returncode == 0:
        print("[OK] Commits enviados para o GitHub com sucesso!")
        if output:
            for line in output.splitlines():
                print(f"  {line}")
        return True

    # Se falhou por falta de upstream no branch atual, tenta configurar
    branch_res = subprocess.run(["git", "rev-parse", "--abbrev-ref", "HEAD"], capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
    current_branch = branch_res.stdout.strip() if branch_res.returncode == 0 else "master"

    res_upstream = subprocess.run(["git", "push", "-u", "origin", current_branch], capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
    upstream_output = ((res_upstream.stdout or "") + "\n" + (res_upstream.stderr or "")).strip()

    if res_upstream.returncode == 0:
        print(f"[OK] Commits enviados para origin/{current_branch} com sucesso!")
        if upstream_output:
            for line in upstream_output.splitlines():
                print(f"  {line}")
        return True
    else:
        err = upstream_output or output
        print(f"[Aviso] Nao foi possivel executar o git push automaticamente:\n{err}")
        return False


def publish_github_release(tag, title, notes_path, config, draft=False, prerelease=False, do_push=True, build_num=None):
    """Cria ou atualiza a release no GitHub e faz upload dos arquivos binarios usando gh CLI."""
    if not check_gh_installed():
        print("[Erro] O utilitario GitHub CLI ('gh') nao foi encontrado no sistema.")
        print("Instale o GitHub CLI ou verifique o PATH: https://cli.github.com/")
        return False

    if do_push:
        push_git_commits(build_num=build_num)

    repo = config.get("github_repo", "").strip()
    assets = get_available_assets(config)

    already_exists = check_release_exists(tag, repo=repo)

    if already_exists:
        print(f"\nA release '{tag}' ja existe no GitHub. Atualizando notas e anexos...")
    else:
        print(f"\nPublicando nova Release '{tag}' no GitHub...")

    if repo:
        print(f"Repositorio: {repo}")
    if assets:
        print("Assets encontrados para anexo:")
        for a in assets:
            print(f"  - {os.path.relpath(a, SCRIPT_DIR)}")
    else:
        print("[Info] Nenhum arquivo binario anexado.")

    try:
        if already_exists:
            # Atualiza titulo e notas da release existente
            edit_cmd = ["gh", "release", "edit", tag, "--title", title, "--notes-file", notes_path]
            if repo:
                edit_cmd.extend(["--repo", repo])
            if draft:
                edit_cmd.append("--draft")
            if prerelease:
                edit_cmd.append("--prerelease")

            res_edit = subprocess.run(edit_cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
            if res_edit.returncode != 0:
                print(f"[Erro] Falha ao atualizar release no GitHub:\n{res_edit.stderr.strip()}")
                return False

            # Faz upload/substituicao dos assets
            if assets:
                upload_cmd = ["gh", "release", "upload", tag] + assets + ["--clobber"]
                if repo:
                    upload_cmd.extend(["--repo", repo])
                res_up = subprocess.run(upload_cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
                if res_up.returncode != 0:
                    print(f"[Aviso] Falha ao fazer upload de assets:\n{res_up.stderr.strip()}")

            print(f"[OK] Release {tag} atualizada com sucesso no GitHub!")
            return True
        else:
            # Cria nova release
            cmd = ["gh", "release", "create", tag]
            cmd.extend(assets)
            cmd.extend(["--title", title, "--notes-file", notes_path])

            if repo:
                cmd.extend(["--repo", repo])
            if draft:
                cmd.append("--draft")
            if prerelease:
                cmd.append("--prerelease")

            res = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
            if res.returncode == 0:
                print(f"[OK] Release {tag} publicada com sucesso no GitHub!")
                if res.stdout.strip():
                    print(f"URL: {res.stdout.strip()}")
                return True
            else:
                print(f"[Erro] Falha ao criar release no GitHub:\n{res.stderr.strip()}")
                return False
    except Exception as e:
        print(f"[Erro] Excecao ao executar operacao de release no GitHub: {e}")
        return False


def cleanup_old_releases(keep=3, repo=None):
    """Remove releases antigas mantendo apenas as N mais recentes."""
    if not check_gh_installed():
        return

    print(f"\nVerificando releases no GitHub para manter apenas as {keep} ultimas...")
    try:
        cmd = ["gh", "release", "list", "--limit", "100", "--json", "tagName"]
        if repo:
            cmd.extend(["--repo", repo])

        res = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
        if res.returncode == 0 and res.stdout.strip():
            releases = json.loads(res.stdout)
            if len(releases) > keep:
                to_delete = releases[keep:]
                for rel in to_delete:
                    tag = rel.get("tagName")
                    if tag:
                        print(f"Removendo release antiga: {tag}...")
                        del_cmd = ["gh", "release", "delete", tag, "--yes", "--cleanup-tag"]
                        if repo:
                            del_cmd.extend(["--repo", repo])
                        subprocess.run(del_cmd, capture_output=True, cwd=SCRIPT_DIR)
                print(f"[OK] Limpeza concluida! Mantidas as {keep} releases mais recentes.")
            else:
                print(f"[OK] Total de releases ({len(releases)}) ja esta dentro do limite (<= {keep}).")
    except Exception as e:
        print(f"[Aviso] Falha na limpeza de releases antigas: {e}")


def main():
    parser = argparse.ArgumentParser(description="Gerador de Release Notes com IA e publicador de releases no GitHub.")
    parser.add_argument("--publish", "-p", action="store_true", help="Gera notas e publica a release no GitHub com assets.")
    parser.add_argument("--tag", type=str, help="Tag da release (ex: v1.0.0). Se omitido, usa a versao do manifesto.")
    parser.add_argument("--title", type=str, help="Titulo da release.")
    parser.add_argument("--repo", type=str, help="Repositorio no GitHub no formato 'usuario/repo' (ex: usuario/projeto).")
    parser.add_argument("--draft", action="store_true", help="Publica como rascunho (draft).")
    parser.add_argument("--prerelease", action="store_true", help="Publica como pre-release.")
    parser.add_argument("--cleanup-only", action="store_true", help="Apenas executa a limpeza de releases antigas.")
    parser.add_argument("--no-cleanup", action="store_true", help="Nao executa limpeza de releases antigas apos publicar.")
    parser.add_argument("--no-push", action="store_true", help="Nao executa git push antes de publicar a release.")

    args = parser.parse_args()
    config = load_config()

    if args.repo:
        config["github_repo"] = args.repo

    repo = config.get("github_repo")
    cleanup_keep = config.get("cleanup_keep_releases", 3)
    commit_limit = config.get("commit_limit", 30)

    if args.cleanup_only:
        cleanup_old_releases(cleanup_keep, repo=repo)
        return

    meta = get_project_metadata()
    build_num = meta.get("build", "1")
    build_date = meta.get("build_date", "")

    tag_prefix = config.get("tag_prefix", "Build-")
    app_name = config.get("app_name", "Application")

    # Tag no Git/GitHub: ex: Build-1
    tag = args.tag or f"{tag_prefix}{build_num}"
    # Titulo da release: ex: MD Reader - Build 1 (2026-08-27 17:32:43)
    title = args.title or (f"{app_name} - Build {build_num} ({build_date})" if build_date else f"{app_name} - Build {build_num}")

    version_label = f"Build {build_num}"
    if build_date:
        version_label += f" ({build_date})"

    commits = get_git_commits(commit_limit)

    print(f"Gerando Release Notes com IA para {version_label} ({app_name})...")

    notes = ""
    try:
        if commits:
            notes = generate_notes_with_ai(version_label, commits, config)
            print("[OK] Release Notes geradas pela IA com sucesso!")
        else:
            notes = f"Release oficial do {app_name} - {version_label}"
    except Exception as e:
        print(f"[Aviso] Falha ao conectar a IA ({e}). Usando fallback automatico.")
        if commits:
            notes = f"### {app_name} - {version_label}\n\n**Commits recentes:**\n{commits}"
        else:
            notes = f"Release oficial do {app_name} - {version_label}"

    output_file = os.path.join(SCRIPT_DIR, "release_notes.txt")
    with open(output_file, "w", encoding="utf-8") as f:
        f.write(notes)

    print(f"\n--- Previa das Release Notes ({version_label}) ---")
    print(notes)
    print("----------------------------------------\n")
    print(f"[OK] Arquivo salvo em: {output_file}")

    # Publicacao no GitHub
    should_publish = args.publish

    if not should_publish and sys.stdin.isatty():
        try:
            choice = input(f"Deseja publicar a release {tag} no GitHub agora? (s/N): ").strip().lower()
            if choice in ("s", "sim", "y", "yes"):
                should_publish = True
        except (EOFError, KeyboardInterrupt):
            pass

    if should_publish:
        published = publish_github_release(
            tag=tag,
            title=title,
            notes_path=output_file,
            config=config,
            draft=args.draft,
            prerelease=args.prerelease,
            do_push=not args.no_push,
            build_num=build_num,
        )
        if published and not args.no_cleanup:
            cleanup_old_releases(cleanup_keep, repo=repo)
    else:
        print("\nPara publicar esta release no GitHub com os binarios anexados, execute:")
        print(f"  python generate_release_notes.py --publish")


if __name__ == "__main__":
    main()


