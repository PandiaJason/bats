package policy

import (
	"fmt"
	"strings"
)

// Verdict is the three-tier decision of the WAND policy engine.
//
// Three outcomes:
//   - BLOCK:     Immediately deny. Action is unambiguously destructive.
//   - CHALLENGE: Risky but potentially legitimate. Agent must halt and
//                request explicit user re-approval before proceeding.
//   - ALLOW:     No dangerous pattern matched. Safe to execute.
type Verdict struct {
	Decision string // "BLOCK", "CHALLENGE", "ALLOW"
	Reason   string
}

// Evaluate performs a deterministic, sub-millisecond check to classify an action.
//
// The function checks three tiers in order:
//  1. Hard blocklist — immediately dangerous patterns → BLOCK
//  2. Challenge list — risky patterns that may be intentional → CHALLENGE
//  3. Default — no pattern matched → ALLOW
func Evaluate(input string) Verdict {
	lower := strings.ToLower(input)

	// ═══════════════════════════════════════════════════════════════════
	// TIER 1: HARD BLOCK — unambiguously destructive patterns
	// ═══════════════════════════════════════════════════════════════════

	hardBlockPatterns := []string{
		// ── Filesystem destruction ──
		"rm -rf", "rm -f", "rm -r", "rmdir", "unlink",
		"shred", "wipe", "srm ",

		// ── Disk-level destruction ──
		"dd if=", "mkfs", "format c:", "fdisk", "parted",
		"wipefs", "blkdiscard",

		// ── Shell-level danger ──
		":(){ :|:& };:", // fork bomb
		"eval(", "exec(",
		"mv / /dev/null", "mv /* /dev/null",

		// ── Process killing ──
		"killall", "pkill", "kill -9", "kill -KILL",
		"xkill", "taskkill /f",

		// ── Service disruption ──
		"shutdown", "reboot", "poweroff", "halt",
		"systemctl stop", "systemctl disable",
		"service stop", "init 0", "init 6",

		// ── Firewall / network destruction ──
		"ufw disable", "iptables -F", "iptables --flush",
		"nft flush", "firewall-cmd --panic-on",

		// ── Python destructive ──
		"shutil.rmtree", "os.remove", "os.unlink", "os.system('rm",
		"subprocess.call(['rm", "os.rmdir", "os.removedirs",
		"pathlib.path.unlink", "pathlib.path.rmdir",

		// ── Node.js destructive ──
		"fs.unlink", "fs.rmdir", "fs.rmsync", "fs.unlinksync",
		"child_process.exec('rm", "fs.rm(", "fs.promises.rm",
		"fs.writefilesync('/', ",
		"rimraf",

		// ── Ruby destructive ──
		"fileutils.rm_rf", "fileutils.remove_dir",
		"file.delete(", "dir.delete(",

		// ── SQL destructive (DDL / mass DML) ──
		"drop table", "drop database", "drop schema", "drop index",
		"drop view", "drop trigger", "drop procedure", "drop function",
		"truncate table", "truncate ",
		"delete from",
		"alter table", "alter database",

		// ── SQL injection vectors ──
		"'; drop", "'; delete", "'; update", "'; alter",
		"union select", "or 1=1", "or '1'='1'",
		"; --", "' or ''='", "\" or \"\"=\"",

		// ── Git destructive ──
		"git clean -fdx", "git clean -fd",
		"git checkout -- .", "git restore .",

		// ── Cloud resource destruction ──
		"aws s3 rm", "aws s3 rb",
		"aws ec2 terminate", "aws rds delete", "aws lambda delete",
		"aws cloudformation delete-stack",
		"gcloud delete", "gcloud compute instances delete",
		"gcloud sql instances delete",
		"az delete", "az group delete", "az vm delete",
		"az storage account delete",
		"terraform destroy",
		"kubectl delete namespace", "kubectl delete --all",
		"kubectl delete pod --all", "kubectl delete deployment",
		"helm uninstall", "helm delete",

		// ── Container / VM escape ──
		"docker rm -f", "docker rmi -f", "docker system prune -a",
		"docker container prune", "docker volume prune",
		"docker network prune",

		// ── Data exfiltration / reverse shells ──
		"curl | bash", "curl | sh", "wget | bash", "wget | sh",
		"nc -e", "netcat -e", "/dev/tcp/",
		"bash -i", "sh -i", "exec /bin/sh", "exec /bin/bash",
		"python -c 'import socket",
		"perl -e 'use socket",
		"socat exec:",
		"msfvenom", "metasploit",

		// ── Credential / secret access ──
		"shadow", "/etc/passwd",
		"cat ~/.ssh/", "cat ~/.aws/",
		".env", "credentials.json", "service-account",
		"aws sts get-session-token",
		"gcloud auth print-access-token",

		// ── Privilege escalation ──
		"chmod 777", "chmod -r 777", "chmod -R 777",
		"chown root", "chown -r root", "chown -R root",
		"visudo", "usermod -ag sudo", "usermod -aG sudo",
		"su root", "su -",
		"setuid", "setgid",

		// ── Package manager attacks ──
		"pip install --pre", "pip install --index-url",
		"npm install --registry",
		"curl -sS | pip", "wget -qO- | pip",

		// ── Encryption / ransomware patterns ──
		"openssl enc -aes", "gpg --encrypt",
		"7z a -p", "zip -e",

		// ── Environment manipulation ──
		"export PATH=", "export LD_PRELOAD",
		"export LD_LIBRARY_PATH",
		"unset PATH", "unset HOME",

		// ── Cron / scheduled task manipulation ──
		"crontab -r", "crontab -e",
		"schtasks /delete", "schtasks /create",
		"at ", "batch ",

		// ── Log tampering ──
		"truncate -s 0 /var/log",
		"cat /dev/null > /var/log",
		"> /var/log",
		"shred /var/log",
		"rm /var/log",

		// ── Kernel / system-level ──
		"insmod", "rmmod", "modprobe -r",
		"sysctl -w", "echo 1 > /proc/",
		"mount -o remount",
	}

	for _, pattern := range hardBlockPatterns {
		if strings.Contains(lower, pattern) {
			return Verdict{
				Decision: "BLOCK",
				Reason:   fmt.Sprintf("WAND HARD BLOCK: destructive pattern '%s'", pattern),
			}
		}
	}

	// Dynamic SQL: UPDATE without WHERE
	if strings.Contains(lower, "update ") && !strings.Contains(lower, "where ") {
		return Verdict{
			Decision: "BLOCK",
			Reason:   "WAND HARD BLOCK: UPDATE without WHERE clause",
		}
	}

	// Shell redirect to overwrite files
	// Only flag standalone redirects, not >> (append) inside benign contexts
	dangerousRedirects := []string{">", ">>", "2>"}
	for _, redir := range dangerousRedirects {
		if strings.Contains(input, redir) {
			return Verdict{
				Decision: "BLOCK",
				Reason:   fmt.Sprintf("WAND HARD BLOCK: shell redirect '%s' detected", redir),
			}
		}
	}

	// ═══════════════════════════════════════════════════════════════════
	// TIER 2: CHALLENGE — risky but potentially legitimate
	// Agent must halt and request explicit user re-approval.
	// ═══════════════════════════════════════════════════════════════════

	challengePatterns := []string{
		// ── Git risky operations ──
		"git push --force", "git push -f",
		"git reset --hard", "git reset --mixed",
		"git rebase", "git branch -d", "git branch -D",
		"git stash drop", "git stash clear",
		"git cherry-pick", "git merge --no-ff",
		"git filter-branch",

		// ── Sudo usage ──
		"sudo ",

		// ── Database admin ──
		"grant ", "revoke ",
		"create user", "drop user",
		"create role", "drop role",
		"create database", "alter system",

		// ── Package installation ──
		"pip install", "npm install", "yarn add",
		"apt install", "apt-get install",
		"brew install", "cargo install",
		"go install", "gem install",
		"apt remove", "apt-get remove",
		"pip uninstall", "npm uninstall",

		// ── File permission changes ──
		"chmod ", "chown ", "chgrp ",

		// ── Network operations ──
		"curl ", "wget ", "fetch(",
		"ssh ", "scp ", "rsync ",

		// ── Docker operations ──
		"docker run", "docker exec", "docker build",
		"docker push", "docker pull",
		"docker-compose up", "docker compose up",

		// ── System config changes ──
		"systemctl restart", "systemctl start",
		"service restart", "service start",

		// ── File moves/renames at scale ──
		"mv ", "cp -r",

		// ── Environment changes ──
		"export ", "source ",
		". /", // sourcing a script

		// ── Process management ──
		"nohup ", "screen ", "tmux ",
		"kill ", // non-force kill still risky

		// ── Write to system paths ──
		"/etc/", "/usr/local/bin/", "/usr/bin/",
		"/opt/", "/var/",
	}

	for _, pattern := range challengePatterns {
		if strings.Contains(lower, pattern) {
			return Verdict{
				Decision:  "CHALLENGE",
				Reason:    fmt.Sprintf("WAND CHALLENGE: risky pattern '%s' — requires explicit user re-approval", pattern),
			}
		}
	}

	// ═══════════════════════════════════════════════════════════════════
	// TIER 3: ALLOW — no dangerous pattern matched
	// ═══════════════════════════════════════════════════════════════════

	return Verdict{
		Decision: "ALLOW",
		Reason:   "Action passed deterministic policy evaluation — no dangerous patterns detected",
	}
}
